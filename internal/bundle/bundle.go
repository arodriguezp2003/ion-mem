// Package bundle defines the portable per-project memory bundle format: a
// zip archive containing a manifest and JSONL dumps of one project's
// observations, revisions, and (optionally) prompts. Bundles are produced by
// store.ExportProject and consumed by store.ImportProject; this package is
// pure (no store/database dependency) so the format can be round-tripped and
// tamper-tested in isolation.
//
// Observation and Revision records are keyed by sync_id (globally unique),
// never by local numeric id, so a bundle can be imported into any checkout
// of the same repo without id collisions.
package bundle

// FormatVersion is the current bundle format version written by Write and
// the only version accepted by Read. Bump this when the file layout or
// record shape changes in a backwards-incompatible way.
const FormatVersion = 1

// File names inside the zip archive.
const (
	FileManifest     = "manifest.json"
	FileObservations = "observations.jsonl"
	FileRevisions    = "revisions.jsonl"
	FilePrompts      = "prompts.jsonl"
)

// Manifest describes the contents of a bundle. SHA256 maps each data file
// name (FileObservations, FileRevisions, and FilePrompts when present) to
// the hex-encoded SHA-256 digest of its raw bytes, so Read can detect
// tampering or corruption before decoding.
type Manifest struct {
	FormatVersion   int               `json:"format_version"`
	Project         string            `json:"project"`
	ExportedAt      string            `json:"exported_at"`
	SourceHost      string            `json:"source_host,omitempty"`
	Counts          Counts            `json:"counts"`
	IncludesPrompts bool              `json:"includes_prompts"`
	SHA256          map[string]string `json:"sha256"`
}

// Counts records how many rows of each kind the bundle carries.
type Counts struct {
	Observations int `json:"observations"`
	Revisions    int `json:"revisions"`
	Prompts      int `json:"prompts"`
}

// Observation is the portable representation of a store.Observation row.
// SyncID is the sole cross-checkout identity; SupersededBySyncID replaces
// the local numeric superseded_by column and is nil when the observation is
// active or when the superseding row could not be resolved at export time.
type Observation struct {
	SyncID             string  `json:"sync_id"`
	SessionID          string  `json:"session_id"`
	Type               string  `json:"type"`
	Title              string  `json:"title"`
	Content            string  `json:"content"`
	ToolName           *string `json:"tool_name,omitempty"`
	Project            string  `json:"project"`
	Scope              string  `json:"scope"`
	TopicKey           *string `json:"topic_key,omitempty"`
	NormalizedHash     string  `json:"normalized_hash"`
	RevisionCount      int     `json:"revision_count"`
	DuplicateCount     int     `json:"duplicate_count"`
	LastSeenAt         string  `json:"last_seen_at"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
	Status             string  `json:"status"`
	SupersededBySyncID *string `json:"superseded_by_sync_id,omitempty"`
	StatusReason       *string `json:"status_reason,omitempty"`
	StatusChangedAt    *string `json:"status_changed_at,omitempty"`
}

// Revision is the portable representation of an observation_revisions row,
// referencing its parent observation by sync_id instead of local id.
type Revision struct {
	ObservationSyncID string  `json:"observation_sync_id"`
	Revision          int     `json:"revision"`
	Type              string  `json:"type"`
	Title             string  `json:"title"`
	Content           string  `json:"content"`
	ToolName          *string `json:"tool_name,omitempty"`
	CreatedAt         string  `json:"created_at"`
	ArchivedAt        string  `json:"archived_at"`
}

// Prompt is the portable representation of a user_prompts row. Only
// included in a bundle when the caller explicitly opts in (with_prompts).
type Prompt struct {
	SyncID    string `json:"sync_id"`
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Project   string `json:"project"`
	CreatedAt string `json:"created_at"`
}

// Bundle is the full in-memory contents of a portable memory bundle.
type Bundle struct {
	Manifest     Manifest
	Observations []Observation
	Revisions    []Revision
	Prompts      []Prompt
}
