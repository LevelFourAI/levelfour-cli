package cli

import "testing"

func TestExitCodeConstants(t *testing.T) {
	tests := []struct {
		name string
		code int
		want int
	}{
		{"ExitSuccess", ExitSuccess, 0},
		{"ExitError", ExitError, 1},
		{"ExitIssuesFound", ExitIssuesFound, 2},
		{"ExitAuthRequired", ExitAuthRequired, 4},
		{"ExitSIGINT", ExitSIGINT, 130},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.code, tt.want)
			}
		})
	}
}
