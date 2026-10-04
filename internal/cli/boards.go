package cli

import (
	"net/url"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	boardsPath = "/api/v1/boards"

	kindBoard = "board"

	columnName      = "Name"
	columnCreatedBy = "Created by"
	columnUpdated   = "Updated"

	everyProvider = "all"
)

var boardsCmd = &cobra.Command{
	Use:   "boards",
	Short: "Boards your organization built in the dashboard",
	Long: `Read the boards your organization built in the dashboard.

A board is a page of cards, and each card is a saved question about spend. A
board stores the questions and never their answers, so reading one returns what
it asks. For numbers, run a saved report with 'l4 reports run'.

A <board> argument takes a board id or a board name, matched whatever its case.`,
}

type board struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Providers     []string `json:"providers"`
	AllProviders  bool     `json:"all_providers"`
	CreatedByName string   `json:"created_by_name"`
	UpdatedAt     string   `json:"updated_at"`
}

func (b board) coverage() string {
	if b.AllProviders {
		return everyProvider
	}
	return orDash(strings.Join(b.Providers, ", "))
}

var boardsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the organization's boards",
	Args:  cobra.NoArgs,
	Example: `  l4 boards list
  l4 boards list --jq '.data.boards[].name'`,
	RunE: func(_ *cobra.Command, _ []string) error {
		return runBoardsList()
	},
}

var boardsGetCmd = &cobra.Command{
	Use:   "get <board>",
	Short: "Show one board",
	Long: `Show one board: what it covers, who made it and when it last changed.

The board's cards and layout are a document the dashboard draws from. Add --json
to read it.`,
	Args: cobra.ExactArgs(1),
	Example: `  l4 boards get Payments
  l4 boards get 6f1c2a9e-3b5d-4c7e-9a1b-2d4f6a8c0e12 --json`,
	RunE: func(_ *cobra.Command, args []string) error {
		return runBoardsGet(args[0])
	},
}

func listBoards() (map[string]interface{}, []board, error) {
	envelope, err := getJSON(boardsPath)
	if err != nil {
		return nil, nil, err
	}
	var listed struct {
		Boards []board `json:"boards"`
	}
	if err := decodeData(envelope, &listed); err != nil {
		return nil, nil, err
	}
	return envelope, listed.Boards, nil
}

func boardNames() ([]storedItem, error) {
	_, boards, err := listBoards()
	if err != nil {
		return nil, err
	}
	items := make([]storedItem, 0, len(boards))
	for _, b := range boards {
		items = append(items, storedItem{ID: b.ID, Name: b.Name})
	}
	return items, nil
}

func runBoardsList() error {
	envelope, boards, err := listBoards()
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}
	if len(boards) == 0 {
		output.Info("No boards yet. A board is created in the dashboard.")
		return nil
	}
	rows := make([][]string, 0, len(boards))
	for _, b := range boards {
		rows = append(rows, []string{b.Name, b.ID, b.coverage(), orDash(b.CreatedByName), dayOf(b.UpdatedAt)})
	}
	output.Table([]string{columnName, "ID", "Providers", columnCreatedBy, columnUpdated}, rows)
	return nil
}

func runBoardsGet(ref string) error {
	id, err := resolveStoredID(kindBoard, ref, boardNames)
	if err != nil {
		return err
	}
	envelope, err := getJSON(boardsPath + "/" + url.PathEscape(id))
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}
	var found board
	if err := decodeData(envelope, &found); err != nil {
		return err
	}
	output.KPICards([]output.KPICard{
		{Label: "Board", Value: found.Name},
		{Label: "Providers", Value: found.coverage()},
		{Label: columnCreatedBy, Value: orDash(found.CreatedByName)},
		{Label: columnUpdated, Value: dayOf(found.UpdatedAt)},
	})
	output.Info("Add --json to read the board's cards and layout.")
	return nil
}

func init() {
	boardsCmd.AddCommand(boardsListCmd)
	boardsCmd.AddCommand(boardsGetCmd)
}
