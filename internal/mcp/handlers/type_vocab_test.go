package handlers_test

import (
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
)

// ─── ValidObservationTypes / IsValidObservationType ──────────────────────────

func TestValidObservationTypes_ContainsExpectedTypes(t *testing.T) {
	want := []string{
		"decision", "architecture", "bugfix", "discovery",
		"config", "preference", "pattern", "session_summary", "manual",
	}
	for _, typ := range want {
		if !store.IsValidObservationType(typ) {
			t.Errorf("IsValidObservationType(%q) = false, want true", typ)
		}
	}
}

func TestIsValidObservationType_ReturnsFalseForUnknown(t *testing.T) {
	for _, typ := range []string{"banana", "foo", "DECISION", "arch"} {
		if store.IsValidObservationType(typ) {
			t.Errorf("IsValidObservationType(%q) = true, want false for unknown type", typ)
		}
	}
}

// ─── ion_save: type validation ────────────────────────────────────────────────

func TestSave_InvalidTypeReturnsInvalidArgument(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_save", map[string]any{
		"title": "some title",
		"type":  "banana",
	})
	env := decodeText(t, res)

	if env["status"] != "error" {
		t.Errorf("status = %v, want %q for invalid type", env["status"], "error")
	}
	if env["error_code"] != "invalid_argument" {
		t.Errorf("error_code = %v, want %q for invalid type", env["error_code"], "invalid_argument")
	}
}

func TestSave_EmptyTypeDefaultsToManual(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_save", map[string]any{
		"title": "some title",
		// type omitted — should default to manual
	})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Errorf("status = %v, want %q when type is empty (defaults to manual)", env["status"], "ok")
	}
}

func TestSave_EachValidTypeSucceeds(t *testing.T) {
	validTypes := []string{
		"decision", "architecture", "bugfix", "discovery",
		"config", "preference", "pattern", "session_summary", "manual",
	}
	for _, typ := range validTypes {
		t.Run(typ, func(t *testing.T) {
			st := mustStore(t)
			_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
				return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
			}))

			res := callTool(t, ts, "ion_save", map[string]any{
				"title": "note for " + typ,
				"type":  typ,
			})
			env := decodeText(t, res)

			if env["status"] != "ok" {
				t.Errorf("type=%q: status = %v, want %q", typ, env["status"], "ok")
			}
		})
	}
}

// ─── ion_update: type validation ─────────────────────────────────────────────

func TestUpdate_InvalidTypeReturnsInvalidArgument(t *testing.T) {
	st := mustStore(t)
	orig := seedObservation(t, st, "myproj", "title", "content", "manual")
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_update", map[string]any{
		"id":   float64(orig.ID),
		"type": "banana",
	})
	env := decodeText(t, res)

	if env["status"] != "error" {
		t.Errorf("status = %v, want %q for invalid type on update", env["status"], "error")
	}
	if env["error_code"] != "invalid_argument" {
		t.Errorf("error_code = %v, want %q for invalid type", env["error_code"], "invalid_argument")
	}
}

// ─── ion_suggest_topic_key: typeToFamily additions ───────────────────────────

func TestSuggestTopicKey_PreferenceTypeUsesPreferenceFamily(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_suggest_topic_key", map[string]any{
		"title": "My Preference",
		"type":  "preference",
	})
	env := decodeText(t, res)

	key, _ := env["topic_key"].(string)
	if len(key) < len("preference/") || key[:len("preference/")] != "preference/" {
		t.Errorf("topic_key = %q, want prefix %q for type=preference", key, "preference/")
	}
}

func TestSuggestTopicKey_SessionSummaryTypeUsesSessionFamily(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_suggest_topic_key", map[string]any{
		"title": "Session done",
		"type":  "session_summary",
	})
	env := decodeText(t, res)

	key, _ := env["topic_key"].(string)
	if len(key) < len("session/") || key[:len("session/")] != "session/" {
		t.Errorf("topic_key = %q, want prefix %q for type=session_summary", key, "session/")
	}
}

func TestSuggestTopicKey_ManualTypeUsesLearningFamily(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_suggest_topic_key", map[string]any{
		"title": "My Note",
		"type":  "manual",
	})
	env := decodeText(t, res)

	key, _ := env["topic_key"].(string)
	if len(key) < len("learning/") || key[:len("learning/")] != "learning/" {
		t.Errorf("topic_key = %q, want prefix %q for type=manual", key, "learning/")
	}
}
