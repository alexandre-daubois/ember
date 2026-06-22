package ui

import "strings"

// sanitizeControl drops terminal control bytes from s so attacker-supplied log
// fields (request URI, host, method, raw log lines, in-flight FrankenPHP
// requests…) cannot smuggle ANSI/OSC/CSI escape sequences into the operator's
// terminal when a row is written to stdout (CWE-150).
//
// It removes C0 controls (0x00–0x1F, including ESC, BEL, CR, LF and TAB), DEL
// (0x7F) and C1 controls (0x80–0x9F). Every escape-sequence introducer the
// terminal acts on is either a C0 byte (ESC 0x1B) or a C1 byte (CSI 0x9B,
// OSC 0x9D…), so once they are gone any remaining payload renders as inert
// text. Printable UTF-8 (emoji, CJK) is preserved; invalid bytes collapse to
// U+FFFD. strings.Map returns s unchanged — no allocation — when it is already
// clean, which is the common case.
//
// Neutralising happens here, at the render boundary, rather than in
// ParseLogLine: the parsed fields also feed the --json/metrics output, which
// JSON-escapes control bytes anyway and must keep the raw values intact.
func sanitizeControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}
