package model

import (
	"regexp"
	"strings"
)

// Placeholder tokens emitted by NormalizeURI. Exported so renderers can
// highlight them without hard-coding the same literals on the UI side.
const (
	PlaceholderUUID = ":uuid"
	PlaceholderHash = ":hash"
	PlaceholderID   = ":id"
)

// Placeholders lists every synthetic segment NormalizeURI may emit, in the
// order they should be considered when scanning a pattern.
var Placeholders = []string{PlaceholderUUID, PlaceholderHash, PlaceholderID}

var (
	uuidPattern    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	longHexPattern = regexp.MustCompile(`^[0-9a-fA-F]{16,}$`)
	numericPattern = regexp.MustCompile(`^\d+$`)
)

// NormalizeURI collapses dynamic path segments to a stable pattern so that
// requests like /users/550e8400-... and /users/3fa85f64-... aggregate under
// /users/:uuid.
//
// The query string is dropped: a route is the path shape, not the parameter
// values, and keeping query strings would shatter every aggregation bucket.
//
// Heuristics are intentionally narrow (UUID, long hex, all-digits). Slug-like
// segments and short hex strings are kept verbatim to avoid false positives
// — reviewers consistently asked for less magic, not more.
func NormalizeURI(uri string) string {
	if uri == "" {
		return ""
	}
	if i := strings.IndexByte(uri, '?'); i >= 0 {
		uri = uri[:i]
	}
	if i := strings.IndexByte(uri, '#'); i >= 0 {
		uri = uri[:i]
	}

	segments := strings.Split(uri, "/")
	for i, seg := range segments {
		if seg == "" {
			continue
		}
		switch {
		case uuidPattern.MatchString(seg):
			segments[i] = PlaceholderUUID
		case longHexPattern.MatchString(seg):
			segments[i] = PlaceholderHash
		case numericPattern.MatchString(seg):
			segments[i] = PlaceholderID
		}
	}
	return strings.Join(segments, "/")
}
