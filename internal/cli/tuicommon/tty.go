package tuicommon

import (
	"regexp"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type PageDirection int

const (
	PageNone PageDirection = iota
	PageNext
	PagePrev
)

func AdvancePage(page int, direction PageDirection, hasNext, hasPrev bool) (int, bool) {
	switch direction {
	case PageNext:
		if hasNext {
			return page + 1, true
		}
	case PagePrev:
		if hasPrev {
			return page - 1, true
		}
	}
	return page, false
}

// Paginator is the contract any paginated TUI model exposes for
// page-navigation handlers to consult. Both costs and recommendations
// list models embed an API client pagination object that satisfies it.
type Paginator interface {
	GetHasNext() bool
	GetHasPrevious() bool
}

// PageNavResult is the resolved next state after a page-navigation key
// event.
type PageNavResult struct {
	Page    int
	Changed bool
}

// ResolvePageNav maps a key event to a page transition, given the
// current page, the underlying paginator (may be nil), and the model's
// next/prev key bindings. Callers apply Page to their model state only
// when Changed is true.
func ResolvePageNav(
	msg tea.KeyPressMsg,
	currentPage int,
	pagination Paginator,
	nextKey, prevKey key.Binding,
) PageNavResult {
	dir := PageNone
	switch {
	case key.Matches(msg, nextKey):
		dir = PageNext
	case key.Matches(msg, prevKey):
		dir = PagePrev
	}
	hasNext := pagination != nil && pagination.GetHasNext()
	hasPrev := pagination != nil && pagination.GetHasPrevious()
	newPage, changed := AdvancePage(currentPage, dir, hasNext, hasPrev)
	return PageNavResult{Page: newPage, Changed: changed}
}

type SearchKeyAction int

const (
	SearchActionTyping SearchKeyAction = iota
	SearchActionCancel
	SearchActionConfirm
)

// ResolveSearchAction maps a search-input key event to a SearchKeyAction
// based on the model's cancel/confirm bindings. All other keys are
// treated as typing input.
func ResolveSearchAction(
	msg tea.KeyPressMsg,
	cancelKey, confirmKey key.Binding,
) SearchKeyAction {
	switch {
	case key.Matches(msg, cancelKey):
		return SearchActionCancel
	case key.Matches(msg, confirmKey):
		return SearchActionConfirm
	default:
		return SearchActionTyping
	}
}

var oscNoiseRegex = regexp.MustCompile(`^[\d;:rgb/\\]+$`)

func FilterTerminalNoise(_ tea.Model, msg tea.Msg) tea.Msg {
	if kp, ok := msg.(tea.KeyPressMsg); ok {
		if oscNoiseRegex.MatchString(kp.String()) {
			return nil
		}
	}
	return msg
}
