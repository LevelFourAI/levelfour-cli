package tuicommon

import (
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type fakePaginator struct{ next, prev bool }

func (f fakePaginator) GetHasNext() bool     { return f.next }
func (f fakePaginator) GetHasPrevious() bool { return f.prev }

var (
	testNextKey = key.NewBinding(key.WithKeys("right"))
	testPrevKey = key.NewBinding(key.WithKeys("left"))
)

func keyMsg(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func TestAdvancePageNext(t *testing.T) {
	p, changed := AdvancePage(2, PageNext, true, false)
	if p != 3 || !changed {
		t.Errorf("expected (3, true), got (%d, %v)", p, changed)
	}
}

func TestAdvancePageNextNoMore(t *testing.T) {
	p, changed := AdvancePage(3, PageNext, false, true)
	if changed || p != 3 {
		t.Errorf("expected no-op at end, got (%d, %v)", p, changed)
	}
}

func TestAdvancePagePrev(t *testing.T) {
	p, changed := AdvancePage(2, PagePrev, false, true)
	if p != 1 || !changed {
		t.Errorf("expected (1, true), got (%d, %v)", p, changed)
	}
}

func TestAdvancePagePrevNoMore(t *testing.T) {
	p, changed := AdvancePage(1, PagePrev, true, false)
	if changed || p != 1 {
		t.Errorf("expected no-op at start, got (%d, %v)", p, changed)
	}
}

func TestAdvancePageNone(t *testing.T) {
	p, changed := AdvancePage(5, PageNone, true, true)
	if changed || p != 5 {
		t.Errorf("expected no-op for PageNone, got (%d, %v)", p, changed)
	}
}

func TestResolvePageNavNext(t *testing.T) {
	r := ResolvePageNav(keyMsg("right"), 2, fakePaginator{next: true}, testNextKey, testPrevKey)
	if !r.Changed || r.Page != 3 {
		t.Errorf("expected (Page=3, Changed=true), got (Page=%d, Changed=%v)", r.Page, r.Changed)
	}
}

func TestResolvePageNavNextNoMore(t *testing.T) {
	r := ResolvePageNav(keyMsg("right"), 3, fakePaginator{next: false}, testNextKey, testPrevKey)
	if r.Changed || r.Page != 3 {
		t.Errorf("expected no-op at end, got (Page=%d, Changed=%v)", r.Page, r.Changed)
	}
}

func TestResolvePageNavPrev(t *testing.T) {
	r := ResolvePageNav(keyMsg("left"), 2, fakePaginator{prev: true}, testNextKey, testPrevKey)
	if !r.Changed || r.Page != 1 {
		t.Errorf("expected (Page=1, Changed=true), got (Page=%d, Changed=%v)", r.Page, r.Changed)
	}
}

func TestResolvePageNavNonNavigationKey(t *testing.T) {
	r := ResolvePageNav(keyMsg("a"), 5, fakePaginator{next: true, prev: true}, testNextKey, testPrevKey)
	if r.Changed || r.Page != 5 {
		t.Errorf("expected no-op for non-nav key, got (Page=%d, Changed=%v)", r.Page, r.Changed)
	}
}

func TestResolvePageNavNilPaginator(t *testing.T) {
	r := ResolvePageNav(keyMsg("right"), 2, nil, testNextKey, testPrevKey)
	if r.Changed || r.Page != 2 {
		t.Errorf("expected no-op with nil paginator, got (Page=%d, Changed=%v)", r.Page, r.Changed)
	}
}

func TestResolveSearchActionCancel(t *testing.T) {
	cancelKey := key.NewBinding(key.WithKeys("esc"))
	confirmKey := key.NewBinding(key.WithKeys("enter"))
	if got := ResolveSearchAction(keyMsg("esc"), cancelKey, confirmKey); got != SearchActionCancel {
		t.Errorf("expected Cancel, got %v", got)
	}
}

func TestResolveSearchActionConfirm(t *testing.T) {
	cancelKey := key.NewBinding(key.WithKeys("esc"))
	confirmKey := key.NewBinding(key.WithKeys("enter"))
	if got := ResolveSearchAction(keyMsg("enter"), cancelKey, confirmKey); got != SearchActionConfirm {
		t.Errorf("expected Confirm, got %v", got)
	}
}

func TestResolveSearchActionTyping(t *testing.T) {
	cancelKey := key.NewBinding(key.WithKeys("esc"))
	confirmKey := key.NewBinding(key.WithKeys("enter"))
	if got := ResolveSearchAction(keyMsg("a"), cancelKey, confirmKey); got != SearchActionTyping {
		t.Errorf("expected Typing for non-binding key, got %v", got)
	}
}

func TestSearchKeyActionConstants(t *testing.T) {
	if SearchActionTyping == SearchActionCancel {
		t.Error("constants should be distinct")
	}
	if SearchActionCancel == SearchActionConfirm {
		t.Error("constants should be distinct")
	}
}
