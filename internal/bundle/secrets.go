package bundle

import (
	"regexp"
	"strings"
)

// Finding is a single secret-shaped match surfaced by ScanSecrets. SyncID
// and Title identify the source record (an observation's own sync_id/title,
// a revision's parent observation sync_id/title, or a prompt's sync_id with
// an empty title) so a human can locate and redact it before sharing.
type Finding struct {
	SyncID  string
	Title   string
	Pattern string
}

// secretPattern pairs a named heuristic with its compiled matcher.
type secretPattern struct {
	name  string
	regex *regexp.Regexp
}

// secretPatterns is the closed set of heuristics ScanSecrets checks, in
// report order. Deliberately conservative: false negatives are safer to
// live with here than false positives that make --allow-secrets a reflex.
var secretPatterns = []secretPattern{
	{"aws_key", regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"api_key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`)},
	{"github_token", regexp.MustCompile(`\bghp_[A-Za-z0-9]{20,}\b`)},
	{"gitlab_token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{16,}\b`)},
	{"private_key", regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)},
}

// passwordAssignment matches password=/passwd:/secret= (or similarly spelled)
// followed by a value token. Whether that value is a real secret vs. a
// placeholder is decided separately by isPlaceholderValue.
var passwordAssignment = regexp.MustCompile(`(?i)\b(password|passwd|secret)\s*[:=]\s*(\S+)`)

// placeholderValues is a closed set of common non-secret placeholder tokens
// (compared case-insensitively, punctuation-stripped) seen in docs, sample
// .env files, and config templates.
var placeholderValues = map[string]struct{}{
	"changeme":         {},
	"change_me":        {},
	"redacted":         {},
	"redact":           {},
	"xxx":              {},
	"placeholder":      {},
	"secret":           {},
	"password":         {},
	"none":             {},
	"null":             {},
	"na":               {},
	"todo":             {},
	"example":          {},
	"yourpassword":     {},
	"yourpasswordhere": {},
}

// isPlaceholderValue reports whether value looks like a non-secret
// placeholder: a templating expression (${...}, {{...}}, <...>), a run of
// a single repeated character (xxxxxxxx, ********), or a member of
// placeholderValues after stripping surrounding punctuation.
func isPlaceholderValue(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return true
	}
	if strings.HasPrefix(v, "${") || strings.HasPrefix(v, "{{") ||
		strings.HasPrefix(v, "<") || strings.HasPrefix(v, "%") {
		return true
	}
	// Run of a single repeated character (xxxx, ****, ----).
	if isRepeatedChar(v) {
		return true
	}
	stripped := strings.ToLower(strings.Trim(v, "!@#$%^&*()_+-.,;:'\"<>{}[]"))
	stripped = strings.ReplaceAll(stripped, "-", "")
	stripped = strings.ReplaceAll(stripped, "_", "")
	if _, ok := placeholderValues[stripped]; ok {
		return true
	}
	if strings.Contains(stripped, "yourpassword") || strings.Contains(stripped, "changeme") ||
		strings.Contains(stripped, "redacted") || strings.Contains(stripped, "placeholder") {
		return true
	}
	return false
}

func isRepeatedChar(s string) bool {
	if len(s) == 0 {
		return false
	}
	first := s[0]
	for i := 1; i < len(s); i++ {
		if s[i] != first {
			return false
		}
	}
	return true
}

// scanText runs every heuristic against text and returns the distinct
// pattern names that matched.
func scanText(text string) []string {
	var matched []string
	for _, p := range secretPatterns {
		if p.regex.MatchString(text) {
			matched = append(matched, p.name)
		}
	}
	for _, m := range passwordAssignment.FindAllStringSubmatch(text, -1) {
		if !isPlaceholderValue(m[2]) {
			matched = append(matched, "password_assignment")
			break
		}
	}
	return matched
}

// ScanSecrets scans every observation, revision, and prompt in b (title and
// content) for secret-shaped strings and returns one Finding per (record,
// pattern) match. Nothing in b is mutated; ScanSecrets is read-only and
// side-effect free so callers can run it before deciding whether to write or
// print the bundle.
func ScanSecrets(b Bundle) []Finding {
	var findings []Finding

	for _, o := range b.Observations {
		for _, pattern := range scanText(o.Title + "\n" + o.Content) {
			findings = append(findings, Finding{SyncID: o.SyncID, Title: o.Title, Pattern: pattern})
		}
	}
	for _, r := range b.Revisions {
		for _, pattern := range scanText(r.Title + "\n" + r.Content) {
			findings = append(findings, Finding{SyncID: r.ObservationSyncID, Title: r.Title, Pattern: pattern})
		}
	}
	for _, p := range b.Prompts {
		for _, pattern := range scanText(p.Content) {
			findings = append(findings, Finding{SyncID: p.SyncID, Title: "", Pattern: pattern})
		}
	}

	return findings
}
