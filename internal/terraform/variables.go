package terraform

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/tryfunc"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

type ParseOptions struct {
	VarFiles        []string
	Vars            []string
	MaxFiles        int
	MaxResources    int
	MaxFileSize     int64
	DownloadModules bool
	ProjectDir      string
}

func ParseVarFiles(dir string, extraVarFiles []string) map[string]cty.Value {
	result := make(map[string]cty.Value)

	var varFiles []string

	tfvars := filepath.Join(dir, "terraform.tfvars")
	if _, err := os.Stat(tfvars); err == nil {
		varFiles = append(varFiles, tfvars)
	}

	autoFiles, _ := filepath.Glob(filepath.Join(dir, "*.auto.tfvars"))
	sort.Strings(autoFiles)
	varFiles = append(varFiles, autoFiles...)

	for _, f := range extraVarFiles {
		if !filepath.IsAbs(f) {
			f = filepath.Join(dir, f)
		}
		varFiles = append(varFiles, f)
	}

	for _, f := range varFiles {
		content, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			continue
		}
		file, diags := hclsyntax.ParseConfig(content, f, hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			continue
		}
		body := file.Body.(*hclsyntax.Body)
		for name, attr := range body.Attributes {
			val, valDiags := attr.Expr.Value(nil)
			if !valDiags.HasErrors() {
				result[name] = val
			}
		}
	}

	return result
}

func ParseVarFlags(vars []string) map[string]cty.Value {
	result := make(map[string]cty.Value)
	for _, v := range vars {
		idx := strings.Index(v, "=")
		if idx < 0 {
			continue
		}
		key := v[:idx]
		val := v[idx+1:]
		result[key] = cty.StringVal(val)
	}
	return result
}

func ParseVariableDefaults(dir string) map[string]cty.Value {
	result := make(map[string]cty.Value)

	matches, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return result
	}

	for _, f := range matches {
		content, readErr := os.ReadFile(filepath.Clean(f))
		if readErr != nil {
			continue
		}
		file, diags := hclsyntax.ParseConfig(content, f, hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			continue
		}
		body := file.Body.(*hclsyntax.Body)
		for _, block := range body.Blocks {
			if block.Type != "variable" || len(block.Labels) < 1 {
				continue
			}
			varName := block.Labels[0]
			for attrName, attr := range block.Body.Attributes {
				if attrName != "default" {
					continue
				}
				val, valDiags := attr.Expr.Value(nil)
				if !valDiags.HasErrors() {
					result[varName] = val
				}
			}
		}
	}

	return result
}

func BuildEvalContext(vars map[string]cty.Value) *hcl.EvalContext {
	return BuildEvalContextWithLocals(vars, nil)
}

func BuildEvalContextWithLocals(vars map[string]cty.Value, locals map[string]cty.Value) *hcl.EvalContext {
	if len(vars) == 0 && len(locals) == 0 {
		return nil
	}

	ctx := &hcl.EvalContext{
		Variables: map[string]cty.Value{},
		Functions: evalFunctions(),
	}

	if len(vars) > 0 {
		ctx.Variables["var"] = cty.ObjectVal(vars)
	}
	if len(locals) > 0 {
		ctx.Variables["local"] = cty.ObjectVal(locals)
	}

	return ctx
}

// evalFunctions are the Terraform functions an expression can call here.
// Registry modules wrap nearly every optional input in try() or lookup().
func evalFunctions() map[string]function.Function {
	return map[string]function.Function{
		"join":       stdlib.JoinFunc,
		"lower":      stdlib.LowerFunc,
		"upper":      stdlib.UpperFunc,
		"trimspace":  stdlib.TrimSpaceFunc,
		"format":     stdlib.FormatFunc,
		"coalesce":   stdlib.CoalesceFunc,
		"concat":     stdlib.ConcatFunc,
		"length":     stdlib.LengthFunc,
		"replace":    stdlib.ReplaceFunc,
		"split":      stdlib.SplitFunc,
		"substr":     stdlib.SubstrFunc,
		"trim":       stdlib.TrimFunc,
		"trimprefix": stdlib.TrimPrefixFunc,
		"trimsuffix": stdlib.TrimSuffixFunc,
		"lookup":     lookupFunc,
		"try":        tryfunc.TryFunc,
	}
}

// lookupFunc is Terraform's lookup(), whose default may be null. go-cty's
// refuses a null default, and registry modules pass one throughout.
var lookupFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "inputMap", Type: cty.DynamicPseudoType},
		{Name: "key", Type: cty.String},
		{Name: "default", Type: cty.DynamicPseudoType, AllowNull: true, AllowDynamicType: true},
	},
	Type: function.StaticReturnType(cty.DynamicPseudoType),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		if !args[2].IsNull() {
			return stdlib.LookupFunc.Call(args)
		}
		return valueAt(args[0], args[1]), nil
	},
})

// valueAt is the element of a map or object at key, or null when there is none.
func valueAt(collection, key cty.Value) cty.Value {
	ty := collection.Type()
	switch {
	case ty.IsObjectType() && ty.HasAttribute(key.AsString()):
		return collection.GetAttr(key.AsString())
	case ty.IsMapType() && collection.HasIndex(key).True():
		return collection.Index(key)
	}
	return cty.NullVal(cty.DynamicPseudoType)
}

func ParseLocals(dir string, evalCtx *hcl.EvalContext) map[string]cty.Value {
	result := make(map[string]cty.Value)

	matches, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return result
	}

	type localExpr struct {
		name string
		expr hcl.Expression
	}
	var allLocals []localExpr

	for _, f := range matches {
		content, readErr := os.ReadFile(filepath.Clean(f))
		if readErr != nil {
			continue
		}
		file, diags := hclsyntax.ParseConfig(content, f, hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			continue
		}
		body := file.Body.(*hclsyntax.Body)
		for _, block := range body.Blocks {
			if block.Type != "locals" {
				continue
			}
			for name, attr := range block.Body.Attributes {
				allLocals = append(allLocals, localExpr{name: name, expr: attr.Expr})
			}
		}
	}

	if len(allLocals) == 0 {
		return result
	}

	const maxRounds = 10
	for round := 0; round < maxRounds; round++ {
		resolved := 0
		for _, l := range allLocals {
			if _, ok := result[l.name]; ok {
				continue
			}
			ctx := BuildEvalContextWithLocals(extractVars(evalCtx), result)
			val, diags := l.expr.Value(ctx)
			if !diags.HasErrors() {
				result[l.name] = val
				resolved++
			}
		}
		if resolved == 0 {
			break
		}
	}

	return result
}

func extractVars(evalCtx *hcl.EvalContext) map[string]cty.Value {
	if evalCtx == nil {
		return nil
	}
	varObj, ok := evalCtx.Variables["var"]
	if !ok || !varObj.IsKnown() || varObj.IsNull() {
		return nil
	}
	if !varObj.Type().IsObjectType() {
		return nil
	}
	result := make(map[string]cty.Value)
	for k := range varObj.Type().AttributeTypes() {
		result[k] = varObj.GetAttr(k)
	}
	return result
}
