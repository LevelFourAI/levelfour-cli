package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var tagsResourcesCmd = &cobra.Command{
	Use:   "resources <key>",
	Short: "List the resources a tag key covers and the value each one holds",
	Args:  cobra.ExactArgs(1),
	Example: `  l4 tags resources Teams
  l4 tags resources Teams --value data --provider aws
  l4 tags resources environment --search prod --page 2 --page-size 50`,
	RunE: func(_ *cobra.Command, args []string) error {
		return runTagsResources(args[0])
	},
}

type tagResourceRow struct {
	ResourceID  string  `json:"resource_id"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Provider    string  `json:"provider"`
	AccountID   string  `json:"account_id"`
	ValueName   string  `json:"value_name"`
	ValueSource string  `json:"value_source"`
	Ratio       float64 `json:"ratio"`
	Spend       float64 `json:"spend"`
}

type tagPagination struct {
	TotalItems  int  `json:"total_items"`
	TotalPages  int  `json:"total_pages"`
	CurrentPage int  `json:"current_page"`
	HasNext     bool `json:"has_next"`
}

type tagResourcePage struct {
	Items      []tagResourceRow `json:"items"`
	Pagination tagPagination    `json:"pagination"`
}

func runTagsResources(ref string) error {
	if err := validateChoice(paramProvider, flagTagsProvider, tagProviders); err != nil {
		return err
	}
	id, err := resolveTagKeyID(ref, "")
	if err != nil {
		return err
	}
	params := url.Values{}
	setParam(params, paramProvider, flagTagsProvider)
	setParam(params, paramSearch, flagTagsSearch)
	params.Set("page", strconv.Itoa(flagTagsPage))
	params.Set("page_size", strconv.Itoa(flagTagsPageSize))
	if flagTagsValue != "" {
		valueID, valueErr := resolveTagValueID(id, flagTagsValue)
		if valueErr != nil {
			return valueErr
		}
		params.Set("value_id", valueID)
	}
	envelope, err := getJSON(withQuery(tagKeyPath(id)+"/resources", params))
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}
	var page tagResourcePage
	if err := decodeData(envelope, &page); err != nil {
		return err
	}
	if len(page.Items) == 0 {
		output.Info("No resources found.")
		return nil
	}
	rows := make([][]string, 0, len(page.Items))
	for _, r := range page.Items {
		rows = append(rows, resourceRow(r))
	}
	output.Table([]string{"Resource", "Name", "Type", "Provider", "Account", "Value", "Source", "Spend"}, rows)
	pg := page.Pagination
	output.PaginationFooter(pg.CurrentPage, pg.TotalPages, pg.TotalItems, pg.HasNext)
	return nil
}

func resolveTagValueID(keyID, ref string) (string, error) {
	_, detail, err := fetchTagKeyDetail(keyID, nil)
	if err != nil {
		return "", err
	}
	for _, v := range detail.Values {
		if v.ID == ref || v.Name == ref {
			return v.ID, nil
		}
	}
	for _, v := range detail.Values {
		if strings.EqualFold(v.Name, ref) {
			return v.ID, nil
		}
	}
	return "", fmt.Errorf("%s has no value named %q: run 'l4 tags show %s' to see its values", detail.Name, ref, keyID)
}

func resourceRow(r tagResourceRow) []string {
	name := r.Name
	if name == r.ResourceID {
		name = ""
	}
	value := orDash(r.ValueName)
	if r.Ratio > 0 && r.Ratio < 1 {
		value = fmt.Sprintf("%s (%.0f%%)", value, r.Ratio*100)
	}
	return []string{
		r.ResourceID,
		orDash(name),
		orDash(r.Type),
		r.Provider,
		orDash(r.AccountID),
		value,
		orDash(r.ValueSource),
		formatSpend(r.Spend),
	}
}

func init() {
	tagsResourcesCmd.Flags().StringVar(&flagTagsValue, "value", "", "Only resources holding this value")
	addProviderFlag(tagsResourcesCmd, tagProviders)
	tagsResourcesCmd.Flags().StringVar(&flagTagsSearch, paramSearch, "", "Filter resources by id or name")
	tagsResourcesCmd.Flags().IntVar(&flagTagsPage, "page", defaultTagsPage, "Page number")
	tagsResourcesCmd.Flags().IntVar(&flagTagsPageSize, "page-size", defaultTagsPageSize, "Items per page (max 200)")
	tagsCmd.AddCommand(tagsResourcesCmd)
}
