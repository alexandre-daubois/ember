package exporter

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func writeAndFlush(t *testing.T, instance string, in string, helpSeen map[string]struct{}) string {
	t.Helper()
	var out bytes.Buffer
	pw := newPluginWriter(&out, instance, helpSeen)
	_, err := pw.Write([]byte(in))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	pw.flush()
	return out.String()
}

func TestPluginWriter_NoLabelInjectionWhenInstanceEmpty(t *testing.T) {
	helpSeen := make(map[string]struct{})
	in := "# HELP foo bar\n# TYPE foo gauge\nfoo 42\nfoo{a=\"b\"} 7\n"
	out := writeAndFlush(t, "", in, helpSeen)
	assert.Equal(t, in, out, "single-instance writer must pass everything through unchanged")
}

func TestPluginWriter_InjectsLabelOnUnlabeledMetric(t *testing.T) {
	helpSeen := make(map[string]struct{})
	out := writeAndFlush(t, "web1", "foo 42\n", helpSeen)
	assert.Equal(t, "foo{ember_instance=\"web1\"} 42\n", out)
}

func TestPluginWriter_InjectsLabelOnLabeledMetric(t *testing.T) {
	helpSeen := make(map[string]struct{})
	out := writeAndFlush(t, "web1", "foo{a=\"b\"} 42\n", helpSeen)
	assert.Equal(t, "foo{a=\"b\",ember_instance=\"web1\"} 42\n", out)
}

func TestPluginWriter_InjectsLabelOnEmptyLabelSet(t *testing.T) {
	helpSeen := make(map[string]struct{})
	out := writeAndFlush(t, "web1", "foo{} 42\n", helpSeen)
	assert.Equal(t, "foo{ember_instance=\"web1\"} 42\n", out)
}

func TestPluginWriter_PreservesCommentsExceptDuplicateHelpType(t *testing.T) {
	helpSeen := make(map[string]struct{})
	first := writeAndFlush(t, "web1", "# HELP foo bar baz\n# TYPE foo gauge\nfoo 1\n", helpSeen)
	assert.Equal(t,
		"# HELP foo bar baz\n# TYPE foo gauge\nfoo{ember_instance=\"web1\"} 1\n",
		first)

	second := writeAndFlush(t, "web2", "# HELP foo bar baz\n# TYPE foo gauge\nfoo 2\n", helpSeen)
	assert.Equal(t, "foo{ember_instance=\"web2\"} 2\n", second,
		"duplicate HELP/TYPE lines must be suppressed across per-instance renders")
}

func TestPluginWriter_HelpTypeAreScopedPerMetricName(t *testing.T) {
	helpSeen := make(map[string]struct{})
	out := writeAndFlush(t, "web1",
		"# HELP foo first\n# HELP bar second\nfoo 1\nbar 2\n", helpSeen)
	assert.Contains(t, out, "# HELP foo first")
	assert.Contains(t, out, "# HELP bar second")
}

func TestPluginWriter_PreservesNonHelpTypeComments(t *testing.T) {
	helpSeen := make(map[string]struct{})
	out := writeAndFlush(t, "web1", "# arbitrary comment\nfoo 1\n", helpSeen)
	assert.Equal(t, "# arbitrary comment\nfoo{ember_instance=\"web1\"} 1\n", out)
}

func TestPluginWriter_FlushHandlesPartialFinalLine(t *testing.T) {
	helpSeen := make(map[string]struct{})
	var out bytes.Buffer
	pw := newPluginWriter(&out, "web1", helpSeen)
	_, _ = pw.Write([]byte("foo 1"))
	pw.flush()
	assert.Equal(t, "foo{ember_instance=\"web1\"} 1", out.String())
}

func TestPluginWriter_HandlesQuotedBraceInLabelValue(t *testing.T) {
	helpSeen := make(map[string]struct{})
	out := writeAndFlush(t, "web1", "foo{path=\"/x}y\"} 1\n", helpSeen)
	assert.Equal(t, "foo{path=\"/x}y\",ember_instance=\"web1\"} 1\n", out,
		"a literal '}' inside a quoted label value must not be treated as the label-set close")
}

func TestPluginWriter_HandlesEscapedQuoteInLabelValue(t *testing.T) {
	helpSeen := make(map[string]struct{})
	out := writeAndFlush(t, "web1", "foo{msg=\"a\\\"b\"} 1\n", helpSeen)
	assert.Equal(t, "foo{msg=\"a\\\"b\",ember_instance=\"web1\"} 1\n", out)
}

func TestPluginWriter_EmberInstanceValueIsEscaped(t *testing.T) {
	helpSeen := make(map[string]struct{})
	out := writeAndFlush(t, `we"b1`, "foo 1\n", helpSeen)
	assert.Equal(t, "foo{ember_instance=\"we\\\"b1\"} 1\n", out)
}

func TestPluginWriter_PassesEmptyAndBlankLines(t *testing.T) {
	helpSeen := make(map[string]struct{})
	out := writeAndFlush(t, "web1", "\nfoo 1\n   \n", helpSeen)
	assert.Equal(t, "\nfoo{ember_instance=\"web1\"} 1\n   \n", out)
}

func TestPluginWriter_BuffersAcrossMultipleWriteCalls(t *testing.T) {
	helpSeen := make(map[string]struct{})
	var out bytes.Buffer
	pw := newPluginWriter(&out, "web1", helpSeen)

	_, _ = pw.Write([]byte("foo "))
	_, _ = pw.Write([]byte("1\nbar 2"))
	pw.flush()
	assert.Equal(t, "foo{ember_instance=\"web1\"} 1\nbar{ember_instance=\"web1\"} 2", out.String())
}

func TestHelpTypeKey_RecognizesHelp(t *testing.T) {
	assert.Equal(t, "HELP foo", helpTypeKey([]byte("# HELP foo a description")))
}

func TestHelpTypeKey_RecognizesType(t *testing.T) {
	assert.Equal(t, "TYPE foo", helpTypeKey([]byte("# TYPE foo gauge")))
}

func TestHelpTypeKey_IgnoresOtherComments(t *testing.T) {
	assert.Empty(t, helpTypeKey([]byte("# UNIT foo seconds")))
	assert.Empty(t, helpTypeKey([]byte("# HELP")))
	assert.Empty(t, helpTypeKey([]byte("# arbitrary")))
}
