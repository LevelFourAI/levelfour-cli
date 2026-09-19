package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func definitionJSON(t *testing.T, def tagDefinition) interface{} {
	t.Helper()
	raw, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded interface{}
	_ = json.Unmarshal(raw, &decoded)
	return decoded
}

func TestLoadTagDefinitionMapsTheDocumentedFile(t *testing.T) {
	def, err := loadTagDefinition(writeSpecFile(t, teamsYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const want = `{
  "name":"Teams","description":"Owning team across AWS and GCP.","effective_from":"2026-06","can_override":false,
  "collapsed_keys":[{"source":{"origin":"provider","key":"team"},"scope":{"providers":["aws"]},"value_prefix":""}],
  "configs":[
    {"filter":{"rules":[{"providers":["aws","gcp"],"conditions":[{"dimension":"service","operator":"is","values":["Amazon Redshift","BigQuery"]}]}]},"assign":{"type":"value","value":"data"}},
    {"filter":{"rules":[{"providers":["aws","gcp"],"conditions":[{"dimension":"tag","key":"team","operator":"flexible_match","values":["Team A"]}]}]},"assign":{"type":"value","value":"team-a"}},
    {"filter":{"rules":[{"providers":["aws"],"conditions":[{"dimension":"account","operator":"is","values":["999900001111"]}]}]},"assign":{"type":"percent","splits":[{"value":"infra","pct":60},{"value":"mobile","pct":40}]}},
    {"filter":{"rules":[{"providers":["aws"],"conditions":[{"dimension":"service","operator":"starts_with","values":["AWS Support"]}]}]},"time_frame":{"start":"2026-07-01"},
     "assign":{"type":"cost_based","input_filter":{"rules":[{"providers":["aws"],"conditions":[{"dimension":"charge_type","operator":"is","values":["Usage"]}]}]},"source":{"origin":"provider","key":"team"}}}
  ]}`
	var wantJSON interface{}
	if err := json.Unmarshal([]byte(want), &wantJSON); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	if got := definitionJSON(t, def); !reflect.DeepEqual(got, wantJSON) {
		gotRaw, _ := json.Marshal(got)
		t.Errorf("definition body =\n%s\nwant\n%s", gotRaw, want)
	}
}

func TestLoadTagDefinitionDefaults(t *testing.T) {
	const spec = `name: environment
collapsed_keys:
  - source: {origin: provider, key: env}
configs:
  - value: prod
    rules:
      - where:
          - {dimension: tagged, key: env}
          - {dimension: account, values: [999900001111]}
    time_frame: {start: 2026-07-01, end: 2026-09-30}
`
	def, err := loadTagDefinition(writeSpecFile(t, spec))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := definitionJSON(t, def).(map[string]interface{})
	if got["effective_from"] != nil {
		t.Errorf("effective_from = %v, want null so the API uses the current month", got["effective_from"])
	}
	collapsed := got["collapsed_keys"].([]interface{})[0].(map[string]interface{})
	if _, scoped := collapsed["scope"]; scoped {
		t.Error("a collapsed key without providers must send no scope")
	}
	config := got["configs"].([]interface{})[0].(map[string]interface{})
	rule := config["filter"].(map[string]interface{})["rules"].([]interface{})[0].(map[string]interface{})
	if providers, ok := rule["providers"].([]interface{}); !ok || len(providers) != 0 {
		t.Errorf("providers = %v, want an empty list rather than null", rule["providers"])
	}
	conditions := rule["conditions"].([]interface{})
	tagged := conditions[0].(map[string]interface{})
	if tagged["operator"] != opIs {
		t.Errorf("operator = %v, want is by default", tagged["operator"])
	}
	if values, ok := tagged["values"].([]interface{}); !ok || len(values) != 0 {
		t.Errorf("values = %v, want an empty list rather than null", tagged["values"])
	}
	if account := conditions[1].(map[string]interface{})["values"].([]interface{})[0]; account != "999900001111" {
		t.Errorf("an unquoted account id = %v, want it kept as written", account)
	}
	timeFrame := config["time_frame"].(map[string]interface{})
	if timeFrame["start"] != "2026-07-01" || timeFrame["end"] != "2026-09-30" {
		t.Errorf("time_frame = %v, want the unquoted dates kept as written", timeFrame)
	}
}

func TestLoadTagDefinitionErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{"no path", "", "pass the definition file with -f FILE"},
		{"missing file", missing, "no such file"},
		{"empty file", writeSpecFile(t, "# nothing yet\n"), "is empty"},
		{"typo", writeSpecFile(t, "name: Teams\nconfigs:\n  - valu: data\n"), "line 3: field valu not found"},
		{"two assignments", writeSpecFile(t, "name: Teams\nconfigs:\n  - value: data\n    split: [{value: a, pct: 100}]\n"), "config 1 sets value and split: keep exactly one of value, split or cost_based"},
		{"no assignment", writeSpecFile(t, "name: Teams\nconfigs:\n  - value: data\n  - rules: []\n"), "config 2 sets none of value, split or cost_based"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadTagDefinition(tt.path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadTagDefinitionReadsThroughOSReadFile(t *testing.T) {
	orig := osReadFile
	osReadFile = func(string) ([]byte, error) { return nil, os.ErrPermission }
	t.Cleanup(func() { osReadFile = orig })

	if _, err := loadTagDefinition("teams.yaml"); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error = %v, want the read error", err)
	}
}
