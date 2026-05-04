package exporter

import (
	"bytes"
	"io"
)

// pluginWriter wraps an io.Writer with two transparent rewrites for plugin
// metric output in multi-instance mode:
//   - inject ember_instance="<name>" into every metric line
//   - suppress duplicate "# HELP <metric>" and "# TYPE <metric>" lines that
//     the same plugin would otherwise emit once per instance, since the
//     resulting Prometheus text would be invalid.
//
// In single-instance mode the writer passes everything through unchanged
// (instance is empty); helpSeen still dedupes if a plugin somehow emits a
// family twice in the same call. Lines are buffered until a newline is
// observed; flush handles a trailing partial line.
type pluginWriter struct {
	out      io.Writer
	label    string
	helpSeen map[string]struct{}
	buf      bytes.Buffer
}

func newPluginWriter(out io.Writer, instance string, helpSeen map[string]struct{}) *pluginWriter {
	pw := &pluginWriter{out: out, helpSeen: helpSeen}
	if instance != "" {
		pw.label = `ember_instance="` + escapeLabelValue(instance) + `"`
	}
	return pw
}

func (w *pluginWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.buf.Write(p)
	for {
		i := bytes.IndexByte(w.buf.Bytes(), '\n')
		if i < 0 {
			return n, nil
		}
		line := w.buf.Next(i + 1)
		if err := w.emitLine(line); err != nil {
			return n, err
		}
	}
}

func (w *pluginWriter) flush() {
	if w.buf.Len() == 0 {
		return
	}
	_ = w.emitLine(w.buf.Bytes())
	w.buf.Reset()
}

func (w *pluginWriter) emitLine(line []byte) error {
	trimmed := bytes.TrimLeft(line, " \t")
	if len(trimmed) == 0 {
		_, err := w.out.Write(line)
		return err
	}
	if trimmed[0] == '#' {
		if key := helpTypeKey(trimmed); key != "" {
			if _, dup := w.helpSeen[key]; dup {
				return nil
			}
			w.helpSeen[key] = struct{}{}
		}
		_, err := w.out.Write(line)
		return err
	}
	if w.label == "" {
		_, err := w.out.Write(line)
		return err
	}
	_, err := w.out.Write(injectInstanceLabel(line, w.label))
	return err
}

// helpTypeKey returns "HELP <name>" or "TYPE <name>" for "# HELP foo ..." /
// "# TYPE foo ..." comments, otherwise "". Used to dedupe family directives
// across per-instance plugin renders.
func helpTypeKey(s []byte) string {
	rest := bytes.TrimLeft(s[1:], " \t")
	var directive string
	switch {
	case bytes.HasPrefix(rest, []byte("HELP ")):
		directive = "HELP"
		rest = rest[5:]
	case bytes.HasPrefix(rest, []byte("TYPE ")):
		directive = "TYPE"
		rest = rest[5:]
	default:
		return ""
	}
	rest = bytes.TrimLeft(rest, " \t")
	name := rest
	if i := bytes.IndexAny(rest, " \t\n"); i >= 0 {
		name = rest[:i]
	}
	if len(name) == 0 {
		return ""
	}
	return directive + " " + string(name)
}

// injectInstanceLabel rewrites a single metric line to carry an extra label.
// Three shapes are handled:
//
//	name value             -> name{label} value
//	name{} value           -> name{label} value
//	name{a="b"} value      -> name{a="b",label} value
//
// Label values are scanned in a quote-aware way so a stray '}' inside a
// quoted value cannot prematurely close the label set.
func injectInstanceLabel(line []byte, label string) []byte {
	nameEnd := metricNameEnd(line)
	if nameEnd < 0 {
		return line
	}
	if nameEnd < len(line) && line[nameEnd] == '{' {
		closeIdx := findLabelClose(line, nameEnd)
		if closeIdx < 0 {
			return line
		}
		existing := bytes.TrimSpace(line[nameEnd+1 : closeIdx])
		out := make([]byte, 0, len(line)+len(label)+1)
		out = append(out, line[:nameEnd+1]...)
		if len(existing) > 0 {
			out = append(out, line[nameEnd+1:closeIdx]...)
			out = append(out, ',')
		}
		out = append(out, label...)
		out = append(out, line[closeIdx:]...)
		return out
	}
	out := make([]byte, 0, len(line)+len(label)+2)
	out = append(out, line[:nameEnd]...)
	out = append(out, '{')
	out = append(out, label...)
	out = append(out, '}')
	out = append(out, line[nameEnd:]...)
	return out
}

// metricNameEnd returns the byte index where the metric name ends on the line:
// the position of the first '{' or whitespace after the name, or -1 when the
// line carries no name (whitespace-only).
func metricNameEnd(line []byte) int {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i == len(line) {
		return -1
	}
	start := i
	for i < len(line) {
		c := line[i]
		if c == '{' || c == ' ' || c == '\t' || c == '\n' {
			break
		}
		i++
	}
	if i == start {
		return -1
	}
	return i
}

// findLabelClose returns the index of the unquoted '}' that closes the label
// set opened at openIdx, or -1 if none is found. Quoted strings honour '\\',
// '\"' and other backslash escapes per the Prometheus exposition format.
func findLabelClose(line []byte, openIdx int) int {
	inString := false
	escape := false
	for j := openIdx + 1; j < len(line); j++ {
		c := line[j]
		if inString {
			if escape {
				escape = false
				continue
			}
			switch c {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '}':
			return j
		}
	}
	return -1
}
