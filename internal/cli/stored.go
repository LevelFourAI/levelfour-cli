package cli

import (
	"fmt"
	"regexp"
	"strings"
)

// A board or a saved report, as its list names it.
type storedItem struct {
	ID   string
	Name string
}

var storedIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

// Two boards, or two reports, may share a name, so a name that matches more than one is refused
// with their ids rather than resolved to whichever the list happens to return first.
func resolveStoredID(kind, ref string, list func() ([]storedItem, error)) (string, error) {
	if storedIDPattern.MatchString(ref) {
		return ref, nil
	}
	items, err := list()
	if err != nil {
		return "", err
	}
	var matches []string
	for _, item := range items {
		if strings.EqualFold(item.Name, ref) {
			matches = append(matches, item.ID)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no %s named %q: run 'l4 %ss list' to see them", kind, ref, kind)
	case 1:
		return matches[0], nil
	}
	return "", fmt.Errorf("%d %ss are named %q: use an id instead (%s)",
		len(matches), kind, ref, strings.Join(matches, ", "))
}

// The day of an ISO 8601 timestamp, which is all a list has room for.
func dayOf(timestamp string) string {
	if len(timestamp) < len(dateLayout) {
		return orDash(timestamp)
	}
	return timestamp[:len(dateLayout)]
}
