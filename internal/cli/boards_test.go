package cli

import (
	"net/http"
	"strings"
	"testing"
)

const (
	boardsRoute  = "GET /api/v1/boards"
	paymentsID   = "6f1c2a9e-3b5d-4c7e-9a1b-2d4f6a8c0e12"
	paymentsPath = "GET /api/v1/boards/" + paymentsID

	boardsJSON = `{"boards":[
  {"id":"6f1c2a9e-3b5d-4c7e-9a1b-2d4f6a8c0e12","name":"Payments","providers":["aws","gcp"],"all_providers":false,"created_by_name":"Ana","updated_at":"2026-09-30T14:02:11Z"},
  {"id":"0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d","name":"Everything","providers":[],"all_providers":true,"created_by_name":null,"updated_at":""}
]}`

	paymentsJSON = `{"id":"6f1c2a9e-3b5d-4c7e-9a1b-2d4f6a8c0e12","name":"Payments","providers":["aws","gcp"],` +
		`"all_providers":false,"created_by_name":"Ana","updated_at":"2026-09-30T14:02:11Z","content":{"cards":["cost-by-service"]}}`

	twinsJSON = `{"boards":[
  {"id":"11111111-1111-4111-8111-111111111111","name":"Twin","providers":[],"all_providers":true},
  {"id":"22222222-2222-4222-8222-222222222222","name":"twin","providers":[],"all_providers":true}
]}`
)

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Cleanup(resetFlags)
	out, _, err := executeCommand(t, args...)
	return out.String(), err
}

func TestBoardsListNamesWhatEachBoardCovers(t *testing.T) {
	serveTags(t, map[string]tagsRoute{boardsRoute: okRoute(boardsJSON)})

	out, err := runCLI(t, "boards", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "Payments", paymentsID, "aws, gcp", "Ana", "2026-09-30", "Everything", "all")
	assertNotContains(t, out, "14:02")
}

func TestBoardsListSaysWhereBoardsComeFrom(t *testing.T) {
	serveTags(t, map[string]tagsRoute{boardsRoute: okRoute(`{"boards":[]}`)})

	out, err := runCLI(t, "boards", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "created in the dashboard")
}

func TestBoardsListHandsFormattingFlagsTheWholeAnswer(t *testing.T) {
	serveTags(t, map[string]tagsRoute{boardsRoute: okRoute(boardsJSON)})

	out, err := runCLI(t, "boards", "list", "--jq", ".data.boards[1].name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "Everything")
	assertNotContains(t, out, "Payments")
}

func TestBoardsListReportsARefusal(t *testing.T) {
	serveTags(t, map[string]tagsRoute{boardsRoute: errRoute(http.StatusForbidden, `{"message":"Boards are not part of this plan"}`)})

	_, err := runCLI(t, "boards", "list")
	if err == nil || !strings.Contains(err.Error(), "not part of this plan") {
		t.Fatalf("error = %v, want the API's reason", err)
	}
}

func TestBoardsListRejectsAnAnswerItCannotRead(t *testing.T) {
	serveTags(t, map[string]tagsRoute{boardsRoute: okRoute(`{"boards":"none"}`)})

	if _, err := runCLI(t, "boards", "list"); err == nil {
		t.Fatal("expected an error for a list that is not one")
	}
}

func TestBoardsGetByID(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{paymentsPath: okRoute(paymentsJSON)})

	out, err := runCLI(t, "boards", "get", paymentsID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "Payments", "aws, gcp", "Ana", "2026-09-30", "--json")
	if srv.count() != 1 {
		t.Errorf("requests = %d, want the board alone: an id needs no lookup", srv.count())
	}
}

func TestBoardsGetByNameWhateverItsCase(t *testing.T) {
	serveTags(t, map[string]tagsRoute{boardsRoute: okRoute(boardsJSON), paymentsPath: okRoute(paymentsJSON)})

	out, err := runCLI(t, "boards", "get", "payments", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "cost-by-service")
}

func TestBoardsGetRefusesANameTwoBoardsShare(t *testing.T) {
	serveTags(t, map[string]tagsRoute{boardsRoute: okRoute(twinsJSON)})

	_, err := runCLI(t, "boards", "get", "twin")
	if err == nil {
		t.Fatal("expected an error for a name two boards share")
	}
	for _, want := range []string{"2 boards", "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestBoardsGetNamesTheListForAnUnknownName(t *testing.T) {
	serveTags(t, map[string]tagsRoute{boardsRoute: okRoute(boardsJSON)})

	_, err := runCLI(t, "boards", "get", "Marketing")
	if err == nil || !strings.Contains(err.Error(), "l4 boards list") {
		t.Fatalf("error = %v, want a pointer to the list", err)
	}
}

func TestBoardsGetFailsWhenTheLookupDoes(t *testing.T) {
	serveTags(t, map[string]tagsRoute{})

	if _, err := runCLI(t, "boards", "get", "Payments"); err == nil {
		t.Fatal("expected the list's error")
	}
}

func TestBoardsGetFailsForAnIDThatNamesNothing(t *testing.T) {
	serveTags(t, map[string]tagsRoute{})

	if _, err := runCLI(t, "boards", "get", paymentsID); err == nil {
		t.Fatal("expected a not found error")
	}
}

func TestBoardsGetRejectsAnAnswerItCannotRead(t *testing.T) {
	serveTags(t, map[string]tagsRoute{paymentsPath: okRoute(`"Payments"`)})

	if _, err := runCLI(t, "boards", "get", paymentsID); err == nil {
		t.Fatal("expected an error for a board that is not an object")
	}
}

func TestDayOfKeepsWhatIsNotATimestamp(t *testing.T) {
	for given, want := range map[string]string{"": "-", "soon": "soon", "2026-09-30T14:02:11Z": "2026-09-30"} {
		if got := dayOf(given); got != want {
			t.Errorf("dayOf(%q) = %q, want %q", given, got, want)
		}
	}
}
