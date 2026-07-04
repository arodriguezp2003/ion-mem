package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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

// sanitizeFTSOr quotes each term like sanitizeFTS but joins them with OR,
// used as a recall fallback when the implicit-AND query matches nothing.
// Returns "" for queries with fewer than two terms (fallback would be identical).
func sanitizeFTSOr(query string) string {
	terms := strings.Fields(query)
	if len(terms) < 2 {
		return ""
	}
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		quoted = append(quoted, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " OR ")
}

// normalizeForHash normalizes s for deduplication: lowercase + collapse whitespace.
func normalizeForHash(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// computeDedupHash returns the SHA-256 hex digest of the normalized content.
// The dedup KEY is the composite (hash, project, scope, type, title); only
// the content contributes to the hash itself.
func computeDedupHash(content string) string {
	n := normalizeForHash(content)
	sum := sha256.Sum256([]byte(n))
	return hex.EncodeToString(sum[:])
}

// generateSyncID generates a unique sync_id with the given prefix
// (e.g. "obs-" or "pr-") followed by 16 random hex characters.
func generateSyncID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback: use time-based value (should never happen in practice).
		_ = err
		return prefix + hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405")))[:16]
	}
	return prefix + hex.EncodeToString(b[:])
}
