package terraform

import (
	"fmt"
	"math/big"
	"path/filepath"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

var moduleMetaArgs = map[string]bool{
	"source":     true,
	"version":    true,
	"providers":  true,
	"depends_on": true,
	"count":      true,
	"for_each":   true,
}

func resolveRemoteModule(name, source, version string, configMap map[string]interface{}, _ string, _ map[string]bool, depth int, _ *hcl.EvalContext, opts ParseOptions, parentModulePath string, downloader *ModuleDownloader) ([]api.ResourceSnapshot, []string) {
	moduleDir, err := downloader.Resolve(source, version)
	if err != nil {
		return []api.ResourceSnapshot{{
			Type:       blockModule,
			Name:       name,
			Attributes: configMap,
		}}, []string{fmt.Sprintf("module.%s: failed to download %s: %v (using opaque estimate)", name, source, err)}
	}

	if depth >= MaxDepth {
		return nil, []string{fmt.Sprintf("module.%s: max depth %d reached, skipping %s", name, MaxDepth, source)}
	}

	absModuleDir, _ := filepath.Abs(moduleDir)

	defaults := ParseVariableDefaults(absModuleDir)
	inputs := filterModuleInputs(configMap)

	merged := make(map[string]cty.Value)
	for k, v := range defaults {
		merged[k] = v
	}
	for k, v := range inputs {
		merged[k] = goToCty(v)
	}

	mergedEvalCtx := BuildEvalContext(merged)

	locals := ParseLocals(absModuleDir, mergedEvalCtx)
	if len(locals) > 0 {
		mergedEvalCtx = BuildEvalContextWithLocals(merged, locals)
	}

	moduleVisited := make(map[string]bool)
	moduleVisited[absModuleDir] = true
	modSnapshots, modWarnings, modErr := parseDirectoryWithVisited(moduleDir, moduleVisited, depth+1, mergedEvalCtx, opts, downloader)
	if modErr != nil {
		return []api.ResourceSnapshot{{
			Type:       blockModule,
			Name:       name,
			Attributes: configMap,
		}}, []string{fmt.Sprintf("module.%s: failed to parse downloaded module %s: %v (using opaque estimate)", name, source, modErr)}
	}

	modulePath := "module." + name
	if parentModulePath != "" {
		modulePath = parentModulePath + ".module." + name
	}

	for i := range modSnapshots {
		if modSnapshots[i].ModulePath == "" {
			modSnapshots[i].ModulePath = modulePath
		} else {
			modSnapshots[i].ModulePath = modulePath + "." + modSnapshots[i].ModulePath
		}
	}

	return modSnapshots, modWarnings
}

func filterModuleInputs(configMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range configMap {
		if !moduleMetaArgs[k] {
			result[k] = v
		}
	}
	return result
}

func goToCty(v interface{}) cty.Value {
	if v == nil {
		return cty.NilVal
	}

	switch val := v.(type) {
	case string:
		return cty.StringVal(val)
	case float64:
		return cty.NumberVal(new(big.Float).SetFloat64(val))
	case int:
		return cty.NumberIntVal(int64(val))
	case int64:
		return cty.NumberIntVal(val)
	case bool:
		return cty.BoolVal(val)
	case []interface{}:
		if len(val) == 0 {
			return cty.EmptyTupleVal
		}
		elems := make([]cty.Value, len(val))
		for i, elem := range val {
			elems[i] = goToCty(elem)
		}
		return cty.TupleVal(elems)
	case map[string]interface{}:
		if len(val) == 0 {
			return cty.EmptyObjectVal
		}
		attrs := make(map[string]cty.Value)
		for k, elem := range val {
			attrs[k] = goToCty(elem)
		}
		return cty.ObjectVal(attrs)
	default:
		return cty.StringVal(fmt.Sprintf("%v", val))
	}
}
