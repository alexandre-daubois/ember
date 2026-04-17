package fetcher

import (
	"testing"
)

// FuzzParseLogLine feeds arbitrary strings into ParseLogLine. The listener
// reads these lines from an untrusted TCP source (a misconfigured Caddy, a
// mis-wired `net` writer, a fuzzing probe), so the parser must tolerate any
// byte sequence without panicking. Beyond the panic safety, a handful of
// invariants are checked to catch silent corruption.
func FuzzParseLogLine(f *testing.F) {
	f.Add(`{"level":"info","ts":1742472000.123456,"logger":"http.log.access.log0","msg":"handled request","request":{"remote_ip":"192.168.1.1","remote_port":"54321","proto":"HTTP/2.0","method":"GET","host":"example.com","uri":"/api/users"},"bytes_read":0,"duration":0.001234,"size":1234,"status":200,"resp_headers":{}}`)
	f.Add(`{}`)
	f.Add(`null`)
	f.Add(``)
	f.Add(`   `)
	f.Add("\n\t")
	f.Add(`not json at all`)
	f.Add(`{"incomplete":`)

	f.Add(`{"ts":1234567890.1234}`)
	f.Add(`{"ts":0}`)
	f.Add(`{"ts":-1}`)
	f.Add(`{"ts":1e100}`)
	f.Add(`{"ts":"2026-04-13T10:15:30Z"}`)
	f.Add(`{"ts":"2026-04-13T10:15:30.123456789+02:00"}`)
	f.Add(`{"ts":null}`)
	f.Add(`{"ts":"not-a-date"}`)
	f.Add(`{"ts":""}`)

	f.Add(`{"status":999}`)
	f.Add(`{"status":-1}`)
	f.Add(`{"status":0}`)
	f.Add(`{"status":999999999}`)

	f.Add(`{"request":{"method":"GET","host":"a","uri":"/"}}`)
	f.Add(`{"request":null}`)
	f.Add(`{"request":{}}`)
	f.Add(`{"request":{"method":"` + string(make([]byte, 2000)) + `"}}`)
	f.Add(`{"request":{"uri":"/path?q=<script>alert(1)</script>"}}`)

	f.Add(`{"duration":0.000001}`)
	f.Add(`{"duration":99999}`)
	f.Add(`{"duration":-1}`)
	f.Add(`{"size":-1}`)
	f.Add(`{"size":9223372036854775807}`)

	f.Add("{\"msg\":\"line1\nline2\"}")
	f.Add(`{"msg":"unicode: \u0000\uFFFD"}`)

	f.Fuzz(func(t *testing.T, line string) {
		// ParseLogLine must never panic, whatever the input looks like.
		e := ParseLogLine(line)

		// Timestamp invariant: the entry always carries a usable timestamp,
		// either parsed from the input or filled in from time.Now. A zero
		// timestamp would display as "0001-01-01 00:00:00" in the UI, which
		// would look like a rendering bug to users.
		if e.Timestamp.IsZero() {
			t.Fatalf("ParseLogLine produced zero timestamp for input %q", line)
		}

		// Parse-error contract: the raw line is preserved verbatim (after
		// trimming for non-empty input) so the UI can still show it in grey.
		// An empty/whitespace-only input is reported as a parse error with the
		// original line kept, so the caller can distinguish it from valid JSON.
		if e.ParseError {
			// Successful parse fields must not be populated alongside the
			// parse error flag: the ParseError branch in ParseLogLine returns
			// early before touching Status/Host/etc.
			if e.Status != 0 || e.Host != "" || e.Method != "" || e.URI != "" {
				t.Fatalf("ParseError=true but structured fields populated: %+v", e)
			}
			return
		}

		// On a successful parse, RawLine is intentionally empty: the UI
		// branches on ParseError, and an empty RawLine makes the contract
		// explicit ("trust the structured fields, not the raw text").
		if e.RawLine != "" {
			t.Fatalf("successful parse must leave RawLine empty, got %q", e.RawLine)
		}
	})
}
