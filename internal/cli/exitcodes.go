package cli

import "errors"

const (
	ExitSuccess      = 0
	ExitError        = 1
	ExitIssuesFound  = 2
	ExitAuthRequired = 4
	ExitSIGINT       = 130
)

var ErrIssuesFound = errors.New("issues found")
