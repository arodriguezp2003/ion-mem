package handlers_test

import (
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
)

func TestSavePrompt_stores_prompt_and_buffers_it(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{ID: "sp-sess-1", Project: "myproj"})

	res := callTool(t, ts, "ion_save_prompt", map[string]any{
		"session_id": "sp-sess-1",
		"content":    "What is the architecture?",
	})
	env := decodeText(t, res)

	if _, ok := env["id"]; !ok {
		t.Fatal("ion_save_prompt: missing 'id' in response")
	}
	if env["id"].(float64) <= 0 {
		t.Errorf("id = %v, want > 0", env["id"])
	}
	if env["session_id"] != "sp-sess-1" {
		t.Errorf("session_id = %v, want %q", env["session_id"], "sp-sess-1")
	}
}

func TestSavePromptThenSave_prompt_attached(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{ID: "sp-chain-sess", Project: "myproj"})

	// Save a prompt first.
	callTool(t, ts, "ion_save_prompt", map[string]any{
		"session_id": "sp-chain-sess",
		"content":    "user question here",
	})

	// Then save an observation with capture_prompt:true.
	res := callTool(t, ts, "ion_save", map[string]any{
		"title":          "answer",
		"content":        "my answer",
		"session_id":     "sp-chain-sess",
		"capture_prompt": true,
	})
	env := decodeText(t, res)

	if env["prompt_attached"] != true {
		t.Errorf("prompt_attached = %v, want true after ion_save_prompt", env["prompt_attached"])
	}
}

func TestSavePromptTwiceThenSave_only_latest_prompt_attached(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{ID: "sp-two-sess", Project: "myproj"})

	// Two distinct prompts stored durably; both have consumed_at=NULL.
	callTool(t, ts, "ion_save_prompt", map[string]any{
		"session_id": "sp-two-sess",
		"content":    "first prompt",
	})
	callTool(t, ts, "ion_save_prompt", map[string]any{
		"session_id": "sp-two-sess",
		"content":    "second prompt",
	})

	// ConsumeLatestPrompt picks the newest unconsumed row (ORDER BY created_at DESC).
	res := callTool(t, ts, "ion_save", map[string]any{
		"title":          "answer",
		"content":        "my answer",
		"session_id":     "sp-two-sess",
		"capture_prompt": true,
	})
	env := decodeText(t, res)

	// Prompt must be attached (second prompt is the latest unconsumed row).
	if env["prompt_attached"] != true {
		t.Errorf("prompt_attached = %v, want true (second prompt is the latest unconsumed)", env["prompt_attached"])
	}
}

func TestSavePrompt_StatusOkOnSuccess(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{ID: "sp-status-ok", Project: "myproj"})

	res := callTool(t, ts, "ion_save_prompt", map[string]any{
		"session_id": "sp-status-ok",
		"content":    "a real prompt",
	})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Errorf("status = %v, want %q on success", env["status"], "ok")
	}
	if _, hasCode := env["error_code"]; hasCode {
		t.Error("success envelope must not contain error_code")
	}
}

func TestSavePrompt_EmptyContent_InvalidArgument(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_save_prompt", map[string]any{
		"content": "",
	})
	env := decodeText(t, res)

	// R-ENV-02: empty content → invalid_argument
	if env["status"] != "error" {
		t.Errorf("status = %v, want %q for empty content", env["status"], "error")
	}
	if env["error_code"] != "invalid_argument" {
		t.Errorf("error_code = %v, want %q", env["error_code"], "invalid_argument")
	}
}

func TestSavePrompt_empty_content_does_not_overwrite_buffer(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{ID: "sp-empty-sess", Project: "myproj"})

	// Seed a real prompt durably first.
	callTool(t, ts, "ion_save_prompt", map[string]any{
		"session_id": "sp-empty-sess",
		"content":    "real prompt",
	})

	// Try to save empty content — must be rejected (no DB write, no buffer write).
	res := callTool(t, ts, "ion_save_prompt", map[string]any{
		"session_id": "sp-empty-sess",
		"content":    "",
	})
	env := decodeText(t, res)

	// Empty content must result in an error message in result.
	result, _ := env["result"].(string)
	if result == "" {
		t.Error("ion_save_prompt empty content: result is empty, want error message")
	}

	// The real prompt row is still unconsumed in the DB — ion_save must find it.
	saveRes := callTool(t, ts, "ion_save", map[string]any{
		"title":          "post-empty",
		"content":        "content",
		"session_id":     "sp-empty-sess",
		"capture_prompt": true,
	})
	saveEnv := decodeText(t, saveRes)
	if saveEnv["prompt_attached"] != true {
		t.Error("prompt_attached = false after empty save_prompt; real prompt should still be unconsumed in DB")
	}
}

// TestSave_SecondSaveNopPromptAttach verifies spec R-TOOL-SAVE-04: after a
// first ion_save consumes the prompt, a second ion_save returns prompt_attached:false.
func TestSave_SecondSaveNopPromptAttach(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{ID: "sp-second-save", Project: "myproj"})

	callTool(t, ts, "ion_save_prompt", map[string]any{
		"session_id": "sp-second-save",
		"content":    "user question",
	})

	// First save: consumes the prompt.
	res1 := callTool(t, ts, "ion_save", map[string]any{
		"title":          "first answer",
		"content":        "answer",
		"session_id":     "sp-second-save",
		"capture_prompt": true,
	})
	env1 := decodeText(t, res1)
	if env1["prompt_attached"] != true {
		t.Errorf("first save: prompt_attached = %v, want true", env1["prompt_attached"])
	}

	// Second save: no unconsumed prompt remains.
	res2 := callTool(t, ts, "ion_save", map[string]any{
		"title":          "second answer",
		"content":        "more content",
		"session_id":     "sp-second-save",
		"capture_prompt": true,
	})
	env2 := decodeText(t, res2)
	if env2["prompt_attached"] != false {
		t.Errorf("second save: prompt_attached = %v, want false (already consumed)", env2["prompt_attached"])
	}
}
