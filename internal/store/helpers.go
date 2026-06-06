package store

import (
	"strings"
	"time"
)

// nowISO returns the current UTC time formatted as RFC3339Nano.
// All Go-side timestamp writes use this function.
func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// parseISO parses an RFC3339Nano UTC string returned from the database.
func parseISO(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

// normalizeScope coerces s to one of the three allowed scope values.
// Unknown values default to "project".
func normalizeScope(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "personal", "global":
		return s
	default:
		return "project"
	}
}

// sanitizeFTS wraps each whitespace-separated term in double quotes to prevent
// FTS5 from interpreting special characters as operators.
// Preserves kebab-case and dotted identifiers as single tokens.
func sanitizeFTS(query string) string {
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		quoted = append(quoted, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " ")
}
