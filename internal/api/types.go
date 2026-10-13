// Package api defines request/response types and raw HTTP helpers for LevelFour
// API endpoints not yet covered by the github.com/LevelFourAI/levelfour-go SDK.
// Types here are expected to migrate to the typed SDK as it gains coverage.
package api

type ResourceSnapshot struct {
	Type       string                 `json:"type"`
	Name       string                 `json:"name"`
	ModulePath string                 `json:"module_path,omitempty"`
	Attributes map[string]interface{} `json:"attributes"`
	// Scope names the root module the resource belongs to, so two roots that
	// declare the same address stay apart.
	Scope string `json:"scope,omitempty"`
	// File is the source file, kept for the change it turns into.
	File string `json:"-"`
}

type ResourceChange struct {
	ResourceType      string                 `json:"resource_type"`
	ResourceName      string                 `json:"resource_name"`
	ChangeType        string                 `json:"change_type"`
	File              string                 `json:"file"`
	AttributesChanged map[string]interface{} `json:"attributes_changed"`
	Scope             string                 `json:"scope,omitempty"`
}

type AnalyzePrRequest struct {
	HeadResources   []ResourceSnapshot     `json:"head_resources,omitempty"`
	ResourceChanges []ResourceChange       `json:"resource_changes,omitempty"`
	Region          *string                `json:"region,omitempty"`
	ProviderRegions map[string]*string     `json:"provider_regions,omitempty"`
	UsageOverrides  map[string]interface{} `json:"usage_overrides,omitempty"`
	CloudFormation  *CloudFormationInput   `json:"cloudformation,omitempty"`
}

// CloudFormationInput carries the templates the API evaluates and prices, as
// they are at the base and at the head of the comparison.
type CloudFormationInput struct {
	Files CloudFormationFiles `json:"files"`
	// Parameters holds parameter values per template path.
	Parameters map[string]map[string]string `json:"parameters,omitempty"`
	// ParameterFiles names an uploaded parameters file per template path.
	ParameterFiles        map[string]string `json:"parameter_files,omitempty"`
	SamconfigEnv          string            `json:"samconfig_env,omitempty"`
	ExcludedResourceTypes []string          `json:"excluded_resource_types,omitempty"`
	// Region is the --region flag. Without it each template is priced in its
	// samconfig region.
	Region string `json:"region,omitempty"`
}

type CloudFormationFiles struct {
	Base []TemplateFile `json:"base"`
	Head []TemplateFile `json:"head"`
}

type TemplateFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type AnalyzePrResponse struct {
	ResourceCostEstimates []*ResourceCostEstimate `json:"resource_cost_estimates,omitempty"`
	CostSummary           *CostSummary            `json:"cost_summary,omitempty"`
	UpgradeSuggestions    []*UpgradeSuggestion    `json:"upgrade_suggestions,omitempty"`
	// Diagnostics lists the assumptions an estimate rests on, such as a
	// parameter priced at its default.
	Diagnostics []*Diagnostic `json:"diagnostics,omitempty"`
}

type Diagnostic struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	File    *string `json:"file,omitempty"`
	Line    *int    `json:"line,omitempty"`
}

type ResourceCostEstimate struct {
	ResourceType          string                   `json:"resource_type"`
	ResourceName          string                   `json:"resource_name"`
	ChangeType            string                   `json:"change_type"`
	Service               string                   `json:"service"`
	ModulePath            *string                  `json:"module_path,omitempty"`
	NewMonthlyCost        *float64                 `json:"new_monthly_cost,omitempty"`
	PreviousMonthlyCost   *float64                 `json:"previous_monthly_cost,omitempty"`
	MonthlyCostDifference *float64                 `json:"monthly_cost_difference,omitempty"`
	Components            []*CostComponentEstimate `json:"components,omitempty"`
	Note                  *string                  `json:"note,omitempty"`
	Scope                 *string                  `json:"scope,omitempty"`
}

type CostComponentEstimate struct {
	Name                string   `json:"name"`
	MonthlyCost         float64  `json:"monthly_cost"`
	PreviousMonthlyCost *float64 `json:"previous_monthly_cost,omitempty"`
	Units               *float64 `json:"units,omitempty"`
	UnitLabel           *string  `json:"unit_label,omitempty"`
	IsEstimated         *bool    `json:"is_estimated,omitempty"`
	Subresource         *string  `json:"subresource,omitempty"`
	Attribute           *string  `json:"attribute,omitempty"`
}

type CostSummary struct {
	TotalMonthlyDifference float64 `json:"total_monthly_difference"`
	TotalPreviousMonthly   float64 `json:"total_previous_monthly"`
	TotalNewMonthly        float64 `json:"total_new_monthly"`
	TotalNewInfrastructure float64 `json:"total_new_infrastructure"`
	Formatted              string  `json:"formatted"`
	EstimableCount         int     `json:"estimable_count"`
	TotalCount             int     `json:"total_count"`
	FreeCount              *int    `json:"free_count,omitempty"`
}

type UpgradeSuggestion struct {
	ResourceType            string                `json:"resource_type"`
	ResourceName            string                `json:"resource_name"`
	File                    string                `json:"file"`
	Attribute               string                `json:"attribute"`
	CurrentValue            string                `json:"current_value"`
	SuggestedValue          string                `json:"suggested_value"`
	Category                string                `json:"category"`
	Reason                  string                `json:"reason"`
	EstimatedMonthlySavings *float64              `json:"estimated_monthly_savings,omitempty"`
	CompanionAttributes     []*CompanionAttribute `json:"companion_attributes,omitempty"`
	Note                    *string               `json:"note,omitempty"`
	GravitonAlternative     *GravitonAlternative  `json:"graviton_alternative,omitempty"`
	ModulePath              *string               `json:"module_path,omitempty"`
	Scope                   *string               `json:"scope,omitempty"`
	Location                *SourceLocation       `json:"location,omitempty"`
}

// SourceLocation is where in a template the value a suggestion changes is written.
type SourceLocation struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

type CompanionAttribute struct {
	Attribute string `json:"attribute"`
	Value     string `json:"value"`
	Reason    string `json:"reason"`
}

type GravitonAlternative struct {
	SuggestedValue          string   `json:"suggested_value"`
	EstimatedMonthlySavings *float64 `json:"estimated_monthly_savings,omitempty"`
	Reason                  string   `json:"reason"`
}

type DeviceCodeResponse struct {
	Data *DeviceCodeData `json:"data"`
}

type DeviceCodeData struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       *int   `json:"expires_in,omitempty"`
	Interval        *int   `json:"interval,omitempty"`
}

type PollResponse struct {
	Data *PollData `json:"data"`
}

type PollData struct {
	Status string  `json:"status"`
	APIKey *string `json:"api_key,omitempty"`
}

func StringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func IntPtr(i int) *int {
	return &i
}
