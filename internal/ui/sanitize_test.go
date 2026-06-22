package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeControlStripsEscapeSequences(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"OSC52 clipboard write (BEL-terminated)", "q=\x1b]52;c;cm0gL2V0Yy9wYXNzd2Q=\x07", "q=]52;c;cm0gL2V0Yy9wYXNzd2Q="},
		{"OSC0 window title (BEL-terminated)", "\x1b]0;PWNED\x07", "]0;PWNED"},
		{"OSC with ST terminator (ESC backslash)", "\x1b]0;PWNED\x1b\\", "]0;PWNED\\"},
		{"CSI erase display", "\x1b[2J\x1b[H", "[2J[H"},
		{"SGR colour reset", "\x1b[31mred\x1b[0m", "[31mred[0m"},
		{"C1 CSI code point (JSON \\u009b)", "x" + string(rune(0x9b)) + "2Jy", "x2Jy"},
		{"C1 OSC+ST code points", "x" + string(rune(0x9d)) + "0;TITLE" + string(rune(0x9c)) + "y", "x0;TITLEy"},
		{"CR/LF row injection", "line1\r\nline2", "line1line2"},
		{"tab", "a\tb", "ab"},
		{"DEL", "a\x7fb", "ab"},
		{"NUL", "a\x00b", "ab"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, sanitizeControl(tc.in))
		})
	}
}

func TestSanitizeControlPreservesPrintable(t *testing.T) {
	cases := []string{
		"",
		"/api/v1/users?id=42",
		"GET",
		"example.com",
		"🚀 launch",          // multi-byte emoji must survive
		"日本語のパス",            // CJK must survive
		"naïve café résumé", // accented Latin must survive
		"plain ascii text 123",
	}
	for _, s := range cases {
		assert.Equal(t, s, sanitizeControl(s), "printable input must be returned unchanged")
	}
}

func TestSanitizeControlNeutralizesEveryControlByte(t *testing.T) {
	// Exhaustive: no C0, DEL or C1 code point may survive, whatever the payload.
	for r := rune(0); r <= 0x9f; r++ {
		if r >= 0x20 && r != 0x7f && r < 0x80 {
			continue // printable ASCII range
		}
		out := sanitizeControl("x" + string(r) + "y")
		assert.Equalf(t, "xy", out, "control code point %#x must be stripped", r)
	}
}
