package output

import (
	"charm.land/huh/v2"
	"charm.land/huh/v2/spinner"
	lipglossv2 "charm.land/lipgloss/v2"
)

var (
	BrandPrimary = lipglossv2.ANSIColor(75)
	BrandAccent  = lipglossv2.ANSIColor(159)
	BrandMuted   = lipglossv2.ANSIColor(242)
	BrandSuccess = lipglossv2.ANSIColor(114)
	BrandError   = lipglossv2.ANSIColor(167)
	BrandWarning = lipglossv2.ANSIColor(173)
)

func L4SpinnerTheme() spinner.Theme {
	return spinner.ThemeFunc(func(_ bool) *spinner.Styles {
		return &spinner.Styles{
			Spinner: lipglossv2.NewStyle().Foreground(BrandPrimary),
			Title:   lipglossv2.NewStyle().Foreground(BrandMuted),
		}
	})
}

func L4Theme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		s := huh.ThemeCharm(isDark)

		s.Focused.Title = s.Focused.Title.Foreground(BrandPrimary).Bold(true)
		s.Focused.Description = s.Focused.Description.Foreground(BrandMuted)
		s.Focused.SelectedOption = s.Focused.SelectedOption.Foreground(BrandAccent)
		s.Focused.SelectSelector = s.Focused.SelectSelector.Foreground(BrandPrimary)
		s.Focused.TextInput.Cursor = s.Focused.TextInput.Cursor.Foreground(BrandPrimary)
		s.Focused.Base = s.Focused.Base.BorderForeground(BrandPrimary)

		s.Blurred.Title = s.Blurred.Title.Foreground(BrandMuted)
		s.Blurred.SelectedOption = s.Blurred.SelectedOption.Foreground(BrandMuted)

		return s
	})
}
