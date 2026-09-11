package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var tagsCmd = &cobra.Command{
	Use:   "tags",
	Short: "Provider tags and virtual tags: spend, coverage and rules",
	Long: `Read provider tags and virtual tags, and manage virtual tags from a YAML file.

A provider tag comes from the bill: AWS cost allocation tags and GCP labels. A
virtual tag is a key LevelFour assigns to every cost line from an ordered list
of rules, across AWS and GCP, without touching the bill.

A <key> argument takes a key id (vtk_... for a virtual key, ptk_... for a
provider key) or a key name, matched case-insensitively. When a virtual key
shares a provider key's name, the name means the virtual key, and the provider
key stays reachable by its id.`,
}

const (
	tagOriginVirtual  = "virtual"
	tagOriginProvider = "provider"

	virtualKeyPrefix  = "vtk_"
	providerKeyPrefix = "ptk_"

	tagKeysPath     = "/api/v1/tags/keys"
	virtualTagsPath = "/api/v1/tags/virtual"

	paramOrigin   = "origin"
	paramProvider = "provider"
	paramSearch   = "search"

	dateLayout = "2006-01-02"

	defaultTagsPage     = 1
	defaultTagsPageSize = 20
)

var (
	tagOrigins       = []string{tagOriginVirtual, tagOriginProvider}
	tagProviders     = []string{"aws", "gcp"}
	tagCostProviders = []string{"aws", "gcp", "all"}
)

// The tags subcommands share these: only one runs per invocation.
var (
	flagTagsOrigin   string
	flagTagsProvider string
	flagTagsSearch   string
	flagTagsStart    string
	flagTagsEnd      string
	flagTagsValue    string
	flagTagsPage     int
	flagTagsPageSize int
	flagTagsFile     string
	flagTagsDryRun   bool
	flagTagsYes      bool
)

type tagKeyRow struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Origin        string   `json:"origin"`
	Providers     []string `json:"providers"`
	ValueCount    int      `json:"value_count"`
	ResourceCount int      `json:"resource_count"`
	Spend         float64  `json:"spend"`
	SpendSharePct float64  `json:"spend_share_pct"`
	Status        string   `json:"status"`
}

type tagValueRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type tagValueSummary struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Spend         float64 `json:"spend"`
	ResourceCount int     `json:"resource_count"`
}

type tagJob struct {
	Status       string `json:"status"`
	FromPeriod   string `json:"from_period"`
	ToPeriod     string `json:"to_period"`
	PeriodsDone  int    `json:"periods_done"`
	PeriodsTotal int    `json:"periods_total"`
	Error        string `json:"error"`
}

type tagWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type tagKeyDetail struct {
	tagDefinition
	ID               string            `json:"id"`
	Origin           string            `json:"origin"`
	Providers        []string          `json:"providers"`
	Status           string            `json:"status"`
	RulesVersion     int               `json:"rules_version"`
	Values           []tagValueSummary `json:"values"`
	TotalSpend       float64           `json:"total_spend"`
	UnallocatedSpend float64           `json:"unallocated_spend"`
	ResourceCount    int               `json:"resource_count"`
	Dependents       []tagValueRef     `json:"dependents"`
	Job              *tagJob           `json:"job"`
	Window           tagWindow         `json:"window"`
}

type tagMutation struct {
	Key tagKeyDetail `json:"key"`
	Job *tagJob      `json:"job"`
}

func addWindowFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&flagTagsStart, "start", "", "Window start, YYYY-MM-DD. The default window is the last 30 complete days")
	cmd.Flags().StringVar(&flagTagsEnd, "end", "", "Window end, YYYY-MM-DD")
}

func addProviderFlag(cmd *cobra.Command, choices []string) {
	cmd.Flags().StringVar(&flagTagsProvider, paramProvider, "", "Provider: "+strings.Join(choices, ", "))
}

func withQuery(path string, params url.Values) string {
	if len(params) == 0 {
		return path
	}
	return path + "?" + params.Encode()
}

func setParam(params url.Values, key, value string) {
	if value != "" {
		params.Set(key, value)
	}
}

func windowParams() url.Values {
	params := url.Values{}
	setParam(params, "start", flagTagsStart)
	setParam(params, "end", flagTagsEnd)
	return params
}

func tagKeyPath(id string) string {
	return tagKeysPath + "/" + url.PathEscape(id)
}

func virtualTagPath(id string) string {
	return virtualTagsPath + "/" + url.PathEscape(id)
}

func decodeData(envelope map[string]interface{}, out interface{}) error {
	raw, _ := json.Marshal(envelope["data"])
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("unexpected response from the API: %w", err)
	}
	return nil
}

func listTagKeys(params url.Values) (map[string]interface{}, []tagKeyRow, error) {
	envelope, err := getJSON(withQuery(tagKeysPath, params))
	if err != nil {
		return nil, nil, err
	}
	var rows []tagKeyRow
	if err := decodeData(envelope, &rows); err != nil {
		return nil, nil, err
	}
	return envelope, rows, nil
}

func fetchTagKeyDetail(id string, params url.Values) (map[string]interface{}, tagKeyDetail, error) {
	var detail tagKeyDetail
	envelope, err := getJSON(withQuery(tagKeyPath(id), params))
	if err != nil {
		return nil, detail, err
	}
	err = decodeData(envelope, &detail)
	return envelope, detail, err
}

// A virtual key wins a name tie: it takes a provider key's name only to extend or replace it.
func matchTagKey(rows []tagKeyRow, name string) (tagKeyRow, bool) {
	var match tagKeyRow
	found := false
	for _, row := range rows {
		if !strings.EqualFold(row.Name, name) {
			continue
		}
		if row.Origin == tagOriginVirtual {
			return row, true
		}
		match, found = row, true
	}
	return match, found
}

func resolveTagKeyID(ref, origin string) (string, error) {
	id, _, err := resolveTagKey(ref, origin)
	return id, err
}

// Hands back the rows it read, so rendering a stored key id as a name costs no second list.
func resolveTagKey(ref, origin string) (string, []tagKeyRow, error) {
	if strings.HasPrefix(ref, virtualKeyPrefix) || strings.HasPrefix(ref, providerKeyPrefix) {
		return ref, nil, nil
	}
	params := url.Values{}
	setParam(params, paramOrigin, origin)
	_, rows, err := listTagKeys(params)
	if err != nil {
		return "", nil, err
	}
	if row, ok := matchTagKey(rows, ref); ok {
		return row.ID, rows, nil
	}
	noun := "tag key"
	if origin == tagOriginVirtual {
		noun = "virtual tag key"
	}
	return "", nil, fmt.Errorf("no %s named %q: run 'l4 tags list' to see the keys", noun, ref)
}

func validateChoice(flag, value string, choices []string) error {
	if value == "" || slices.Contains(choices, value) {
		return nil
	}
	return fmt.Errorf("invalid --%s %q: choose one of %s", flag, value, strings.Join(choices, ", "))
}

func validateWindow() error {
	start, err := parseDateFlag("start", flagTagsStart)
	if err != nil {
		return err
	}
	end, err := parseDateFlag("end", flagTagsEnd)
	if err != nil {
		return err
	}
	if !start.IsZero() && !end.IsZero() && end.Before(start) {
		return fmt.Errorf("--end %s is before --start %s", flagTagsEnd, flagTagsStart)
	}
	return nil
}

func parseDateFlag(flag, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(dateLayout, value)
	if err != nil {
		return t, fmt.Errorf("invalid --%s %q: use YYYY-MM-DD", flag, value)
	}
	return t, nil
}

func formatSpend(v float64) string {
	return fmt.Sprintf("$%.2f", v)
}

func formatShare(v float64) string {
	return fmt.Sprintf("%.1f%%", v)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func windowLabel(w tagWindow) string {
	return w.Start + " to " + w.End
}

func refNames(refs []tagValueRef) string {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.Name
	}
	return strings.Join(names, ", ")
}

func describeJob(job *tagJob) string {
	line := fmt.Sprintf("%s, %s..%s, %d of %d periods", job.Status, job.FromPeriod, job.ToPeriod, job.PeriodsDone, job.PeriodsTotal)
	if job.Error != "" {
		line += ": " + job.Error
	}
	return line
}
