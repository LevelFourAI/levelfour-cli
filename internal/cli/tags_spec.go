package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	assignValue     = "value"
	assignPercent   = "percent"
	assignCostBased = "cost_based"

	opIs    = "is"
	opIsNot = "is_not"

	dimensionTagged     = "tagged"
	dimensionVirtualTag = "virtual_tag"
)

type tagSpecFile struct {
	Name          string             `yaml:"name"`
	Description   string             `yaml:"description"`
	EffectiveFrom *string            `yaml:"effective_from"`
	CanOverride   bool               `yaml:"can_override"`
	CollapsedKeys []collapsedKeySpec `yaml:"collapsed_keys"`
	Configs       []configSpec       `yaml:"configs"`
}

type collapsedKeySpec struct {
	Source    tagSource `yaml:"source"`
	Providers []string  `yaml:"providers"`
	Prefix    string    `yaml:"prefix"`
}

type ruleSpec struct {
	Providers []string       `yaml:"providers"`
	Where     []tagCondition `yaml:"where"`
}

type costBasedSpec struct {
	Source tagSource  `yaml:"source"`
	Input  []ruleSpec `yaml:"input"`
}

type configSpec struct {
	Value     *string        `yaml:"value"`
	Split     []tagSplit     `yaml:"split"`
	CostBased *costBasedSpec `yaml:"cost_based"`
	Rules     []ruleSpec     `yaml:"rules"`
	TimeFrame *tagTimeFrame  `yaml:"time_frame"`
}

type tagSource struct {
	Origin string `yaml:"origin" json:"origin"`
	Key    string `yaml:"key" json:"key"`
}

type tagCondition struct {
	Dimension string   `yaml:"dimension" json:"dimension"`
	Key       string   `yaml:"key" json:"key,omitempty"`
	Operator  string   `yaml:"operator" json:"operator"`
	Values    []string `yaml:"values" json:"values"`
}

type tagSplit struct {
	Value string  `yaml:"value" json:"value"`
	Pct   float64 `yaml:"pct" json:"pct"`
}

type tagTimeFrame struct {
	Start string `yaml:"start" json:"start,omitempty"`
	End   string `yaml:"end" json:"end,omitempty"`
}

type tagScope struct {
	Providers []string `json:"providers"`
}

type tagCollapsedKey struct {
	Source      tagSource `json:"source"`
	Scope       *tagScope `json:"scope,omitempty"`
	ValuePrefix string    `json:"value_prefix"`
}

type tagRule struct {
	Providers  []string       `json:"providers"`
	Conditions []tagCondition `json:"conditions"`
}

type tagFilter struct {
	Rules []tagRule `json:"rules"`
}

type tagAssign struct {
	Type        string     `json:"type"`
	Value       *string    `json:"value,omitempty"`
	Splits      []tagSplit `json:"splits,omitempty"`
	InputFilter *tagFilter `json:"input_filter,omitempty"`
	Source      *tagSource `json:"source,omitempty"`
}

type tagConfig struct {
	Filter    tagFilter     `json:"filter"`
	TimeFrame *tagTimeFrame `json:"time_frame,omitempty"`
	Assign    tagAssign     `json:"assign"`
}

type tagDefinition struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	EffectiveFrom *string           `json:"effective_from"`
	CanOverride   bool              `json:"can_override"`
	CollapsedKeys []tagCollapsedKey `json:"collapsed_keys"`
	Configs       []tagConfig       `json:"configs"`
}

func loadTagDefinition(path string) (tagDefinition, error) {
	if path == "" {
		return tagDefinition{}, errors.New("pass the definition file with -f FILE")
	}
	data, err := osReadFile(filepath.Clean(path))
	if err != nil {
		return tagDefinition{}, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	// A misspelled field fails with its line instead of being dropped on the way to the API.
	decoder.KnownFields(true)
	var spec tagSpecFile
	if err := decoder.Decode(&spec); err != nil {
		if errors.Is(err, io.EOF) {
			return tagDefinition{}, fmt.Errorf("%s is empty", path)
		}
		return tagDefinition{}, fmt.Errorf("%s: %w", path, err)
	}
	def, err := spec.definition()
	if err != nil {
		return tagDefinition{}, fmt.Errorf("%s: %w", path, err)
	}
	return def, nil
}

// The API rejects null where it expects a list, so no list built here is ever nil.
func (f tagSpecFile) definition() (tagDefinition, error) {
	def := tagDefinition{
		Name:          f.Name,
		Description:   f.Description,
		EffectiveFrom: f.EffectiveFrom,
		CanOverride:   f.CanOverride,
		CollapsedKeys: make([]tagCollapsedKey, 0, len(f.CollapsedKeys)),
		Configs:       make([]tagConfig, 0, len(f.Configs)),
	}
	for _, k := range f.CollapsedKeys {
		collapsed := tagCollapsedKey{Source: k.Source, ValuePrefix: k.Prefix}
		if len(k.Providers) > 0 {
			collapsed.Scope = &tagScope{Providers: k.Providers}
		}
		def.CollapsedKeys = append(def.CollapsedKeys, collapsed)
	}
	for i, c := range f.Configs {
		assign, err := c.assign()
		if err != nil {
			return tagDefinition{}, fmt.Errorf("config %d %w", i+1, err)
		}
		def.Configs = append(def.Configs, tagConfig{Filter: filterOf(c.Rules), TimeFrame: c.TimeFrame, Assign: assign})
	}
	return def, nil
}

func (c configSpec) assign() (tagAssign, error) {
	var named []string
	if c.Value != nil {
		named = append(named, assignValue)
	}
	if c.Split != nil {
		named = append(named, "split")
	}
	if c.CostBased != nil {
		named = append(named, assignCostBased)
	}
	switch {
	case len(named) == 0:
		return tagAssign{}, errors.New("sets none of value, split or cost_based: give it exactly one")
	case len(named) > 1:
		return tagAssign{}, fmt.Errorf("sets %s: keep exactly one of value, split or cost_based", strings.Join(named, " and "))
	case c.Value != nil:
		return tagAssign{Type: assignValue, Value: c.Value}, nil
	case c.Split != nil:
		return tagAssign{Type: assignPercent, Splits: c.Split}, nil
	}
	input := filterOf(c.CostBased.Input)
	source := c.CostBased.Source
	return tagAssign{Type: assignCostBased, InputFilter: &input, Source: &source}, nil
}

func filterOf(specs []ruleSpec) tagFilter {
	rules := make([]tagRule, 0, len(specs))
	for _, r := range specs {
		conditions := make([]tagCondition, 0, len(r.Where))
		for _, c := range r.Where {
			if c.Operator == "" {
				c.Operator = opIs
			}
			if c.Values == nil {
				c.Values = []string{}
			}
			conditions = append(conditions, c)
		}
		providers := r.Providers
		if providers == nil {
			providers = []string{}
		}
		rules = append(rules, tagRule{Providers: providers, Conditions: conditions})
	}
	return tagFilter{Rules: rules}
}
