package hybrid

import (
	"strings"
	"testing"
)

func TestMakeSnippet_ShortContentUnchanged(t *testing.T) {
	content := "a short piece of content"
	got := makeSnippet(content)
	if got != content {
		t.Errorf("makeSnippet(%q) = %q, want unchanged", content, got)
	}
}

func TestMakeSnippet_TruncatesLongContent(t *testing.T) {
	content := strings.Repeat("a", 500)
	got := makeSnippet(content)
	// snippetMaxChars runes + a trailing ellipsis rune.
	wantRuneLen := snippetMaxChars + 1
	if got2 := []rune(got); len(got2) != wantRuneLen {
		t.Errorf("makeSnippet: rune length = %d, want %d", len(got2), wantRuneLen)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("makeSnippet(long content) = %q, want trailing ellipsis", got)
	}
}

func TestMakeSnippet_RuneSafeTruncation(t *testing.T) {
	// Multi-byte runes (emoji, accented characters) must never be cut mid-byte,
	// which byte-slicing would do and produce invalid UTF-8 / mangled output.
	content := strings.Repeat("é", 200) // 'é' is 2 bytes in UTF-8
	got := makeSnippet(content)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("makeSnippet(long unicode content) = %q, want trailing ellipsis", got)
	}
	trimmed := strings.TrimSuffix(got, "…")
	runeLen := len([]rune(trimmed))
	if runeLen != snippetMaxChars {
		t.Errorf("makeSnippet: truncated rune length = %d, want %d", runeLen, snippetMaxChars)
	}
	for _, r := range trimmed {
		if r != 'é' {
			t.Fatalf("makeSnippet produced a corrupted rune: %q in %q", r, trimmed)
		}
	}
}

func TestMakeSnippet_EmptyContent(t *testing.T) {
	if got := makeSnippet(""); got != "" {
		t.Errorf("makeSnippet(\"\") = %q, want empty", got)
	}
}
