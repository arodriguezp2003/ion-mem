package store_test

import (
	"context"
	"os"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// mustOpen opens a fresh store backed by a temp directory.
// The store is closed automatically when the test ends.
func mustOpen(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("mustOpen: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// mustSession inserts a session into s with a unique ID and returns it.
// Fatal on any error. project is used as the project field and in the ID.
func mustSession(t *testing.T, s *store.Store, project string) store.Session {
	t.Helper()
	sess, err := s.CreateSession(context.Background(), store.CreateSessionParams{
		ID:        "sess-" + project + "-" + randomSuffix(),
		Project:   project,
		Directory: "/tmp/" + project,
	})
	if err != nil {
		t.Fatalf("mustSession: %v", err)
	}
	return sess
}

// mustObservation inserts a minimal observation into s for the given sessionID
// and returns it. Fatal on any error.
func mustObservation(t *testing.T, s *store.Store, sessionID string) store.Observation {
	t.Helper()
	obs, err := s.AddObservation(context.Background(), store.AddObservationParams{
		SessionID: sessionID,
		Type:      "decision",
		Title:     "test-title-" + randomSuffix(),
		Content:   "test content " + randomSuffix(),
		Project:   "test-project",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("mustObservation: %v", err)
	}
	return obs
}

// mustObservationForProject inserts a minimal observation for a specific project.
func mustObservationForProject(t *testing.T, s *store.Store, sessionID, project string) store.Observation {
	t.Helper()
	obs, err := s.AddObservation(context.Background(), store.AddObservationParams{
		SessionID: sessionID,
		Type:      "decision",
		Title:     "title-" + randomSuffix(),
		Content:   "content " + randomSuffix(),
		Project:   project,
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("mustObservationForProject: %v", err)
	}
	return obs
}

// mustPrompt inserts a prompt into s for the given sessionID, content, and
// project and returns it. Fatal on any error.
func mustPrompt(t *testing.T, s *store.Store, sessionID, content, project string) store.Prompt {
	t.Helper()
	p, err := s.AddPromptIfMissing(context.Background(), store.AddPromptParams{
		SessionID: sessionID,
		Content:   content,
		Project:   project,
	})
	if err != nil {
		t.Fatalf("mustPrompt: %v", err)
	}
	return p
}

// queryPragmaString executes PRAGMA <name> and returns the string result.
func queryPragmaString(t *testing.T, s *store.Store, name string) string {
	t.Helper()
	var val string
	if err := s.DB().QueryRow("PRAGMA " + name).Scan(&val); err != nil {
		t.Fatalf("queryPragmaString %q: %v", name, err)
	}
	return val
}

// queryPragmaInt executes PRAGMA <name> and returns the integer result.
func queryPragmaInt(t *testing.T, s *store.Store, name string) int {
	t.Helper()
	var val int
	if err := s.DB().QueryRow("PRAGMA " + name).Scan(&val); err != nil {
		t.Fatalf("queryPragmaInt %q: %v", name, err)
	}
	return val
}

// sqliteMasterNames returns the names of all sqlite_master objects of the given type.
func sqliteMasterNames(t *testing.T, s *store.Store, objType string) []string {
	t.Helper()
	rows, err := s.DB().Query("SELECT name FROM sqlite_master WHERE type=?", objType)
	if err != nil {
		t.Fatalf("sqliteMasterNames %q: %v", objType, err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("sqliteMasterNames scan: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("sqliteMasterNames rows.Err: %v", err)
	}
	return names
}

// createFile creates an empty file at path.
func createFile(t *testing.T, path string) error {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return f.Close()
}

// randomSuffix returns a short random string for unique IDs in tests.
func randomSuffix() string {
	return suffixFromCounter()
}

var testCounter int

func suffixFromCounter() string {
	testCounter++
	return intToHex(testCounter)
}

func intToHex(n int) string {
	const hexChars = "0123456789abcdef"
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = hexChars[n&0xf]
		n >>= 4
	}
	return string(buf[i:])
}
