package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	flagAPIMethod  string
	flagAPIFields  []string
	flagAPIInclude bool
)

var apiCmd = &cobra.Command{
	Use:   "api <endpoint>",
	Short: "Make an authenticated API request",
	Long: `Make an authenticated API request to any LevelFour API endpoint.

The endpoint should start with / (e.g., /api/v1/costs/summary).
GET is the default method. Use -X to specify a different method.`,
	Args: cobra.ExactArgs(1),
	Example: `- Fetch spending summary

  $ l4 api /api/v1/costs/summary

- Filter with jq

  $ l4 api /api/v1/recommendations --jq '.data.items[0]'

- POST with fields

  $ l4 api -X POST /api/v1/auth/device -f key=value`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient()
		if err != nil {
			return err
		}

		endpoint := args[0]
		method := strings.ToUpper(flagAPIMethod)

		if method == "GET" && len(flagAPIFields) == 0 {
			raw, err := client.DoRaw("GET", endpoint, nil)
			if err != nil {
				return err
			}

			if flagAPIInclude {
				fmt.Fprintf(output.Stdout, "HTTP %d\n", raw.StatusCode)
				for k, vals := range raw.Headers {
					for _, v := range vals {
						fmt.Fprintf(output.Stdout, "%s: %s\n", k, v)
					}
				}
				fmt.Fprintln(output.Stdout)
			}

			var parsed interface{}
			if json.Unmarshal(raw.Body, &parsed) == nil {
				return printAPIResult(parsed, raw.StatusCode, raw.Body)
			}

			output.PrintRaw(string(raw.Body))
			if raw.StatusCode >= 400 {
				return fmt.Errorf("API error (%d)", raw.StatusCode)
			}
			return nil
		}

		if len(flagAPIFields) > 0 {
			payload := make(map[string]interface{})
			for _, f := range flagAPIFields {
				parts := strings.SplitN(f, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid field format %q, expected key=value", f)
				}
				payload[parts[0]] = parts[1]
			}

			data, _ := json.Marshal(payload)
			raw, err := client.DoRaw(method, endpoint, bytes.NewReader(data))
			if err != nil {
				return err
			}

			if flagAPIInclude {
				fmt.Fprintf(output.Stdout, "HTTP %d\n", raw.StatusCode)
				for k, vals := range raw.Headers {
					for _, v := range vals {
						fmt.Fprintf(output.Stdout, "%s: %s\n", k, v)
					}
				}
				fmt.Fprintln(output.Stdout)
			}

			var parsed interface{}
			if json.Unmarshal(raw.Body, &parsed) == nil {
				return printAPIResult(parsed, raw.StatusCode, raw.Body)
			}

			output.PrintRaw(string(raw.Body))
			if raw.StatusCode >= 400 {
				return fmt.Errorf("API error (%d)", raw.StatusCode)
			}
			return nil
		}

		raw, err := client.DoRaw(method, endpoint, nil)
		if err != nil {
			return err
		}

		if flagAPIInclude {
			fmt.Fprintf(output.Stdout, "HTTP %d\n", raw.StatusCode)
			for k, vals := range raw.Headers {
				for _, v := range vals {
					fmt.Fprintf(output.Stdout, "%s: %s\n", k, v)
				}
			}
			fmt.Fprintln(output.Stdout)
		}

		var parsed interface{}
		if json.Unmarshal(raw.Body, &parsed) == nil {
			return printAPIResult(parsed, raw.StatusCode, raw.Body)
		}

		output.PrintRaw(string(raw.Body))
		if raw.StatusCode >= 400 {
			return fmt.Errorf("API error (%d)", raw.StatusCode)
		}
		return nil
	},
}

func printAPIResult(parsed interface{}, statusCode int, body []byte) error {
	if statusCode >= 400 {
		return fmt.Errorf("API error (%d): %s", statusCode, strings.TrimSpace(string(body)))
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(parsed)
	}
	output.PrintJSON(parsed)
	return nil
}

func init() {
	apiCmd.Flags().StringVarP(&flagAPIMethod, "method", "X", "GET", "HTTP method")
	apiCmd.Flags().StringArrayVarP(&flagAPIFields, "field", "f", nil, "POST body fields as key=value pairs")
	apiCmd.Flags().BoolVar(&flagAPIInclude, "include", false, "Include HTTP status and headers in output")
}
