package cli

import "fmt"

const (
	measureCost  = "cost"
	measureUsage = "usage"
)

// What a breakdown's figures are: a cost, or a usage quantity in one unit. The API sends a usage
// quantity in the fields that otherwise hold a cost, so a figure is only safe to print once the
// answer has said which of the two it is.
type measured struct {
	measure string
	unit    string
}

func (m measured) isUsage() bool {
	return m.measure == measureUsage
}

func (m measured) header() string {
	if m.isUsage() {
		return fmt.Sprintf("Usage (%s)", m.unit)
	}
	return columnCost
}

func (m measured) amount(value float64) string {
	if m.isUsage() {
		return fmt.Sprintf("%.2f", value)
	}
	return fmt.Sprintf("$%.2f", value)
}

// A figure the API may leave out. None is printed for it, never a zero nobody sent.
func (m measured) sent(value *float64) string {
	if value == nil {
		return "-"
	}
	return m.amount(*value)
}

func (m measured) total(value float64) string {
	if m.isUsage() {
		return fmt.Sprintf("%.2f %s", value, m.unit)
	}
	return m.amount(value)
}
