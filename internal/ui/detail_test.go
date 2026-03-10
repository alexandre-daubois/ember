package ui

import "testing"

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 KB"},
		{512, "0 KB"},
		{1024, "1 KB"},
		{1048576, "1 MB"},
		{10485760, "10 MB"},
		{536870912, "512 MB"},
	}

	for _, tt := range tests {
		got := formatBytes(tt.input)
		if got != tt.want {
			t.Errorf("formatBytes(%d): expected %q, got %q", tt.input, tt.want, got)
		}
	}
}
