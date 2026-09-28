package bundle_test

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/bundle"
)

func strPtr(s string) *string { return &s }

func sampleBundle(includePrompts bool) bundle.Bundle {
	obs := []bundle.Observation{
		{
			SyncID:         "obs-aaaaaaaaaaaaaaaa",
			SessionID:      "sess-1",
			Type:           "decision",
			Title:          "Use widgets",
			Content:        "We decided to use widgets.",
			Project:        "demo",
			Scope:          "project",
			NormalizedHash: "hash1",
			RevisionCount:  1,
			LastSeenAt:     "2026-01-01T00:00:00Z",
			CreatedAt:      "2026-01-01T00:00:00Z",
			UpdatedAt:      "2026-01-01T00:00:00Z",
			Status:         "active",
		},
		{
			SyncID:             "obs-bbbbbbbbbbbbbbbb",
			SessionID:          "sess-1",
			Type:               "decision",
			Title:              "Use gadgets instead",
			Content:            "Replaced widgets with gadgets.",
			Project:            "demo",
			Scope:              "project",
			NormalizedHash:     "hash2",
			RevisionCount:      2,
			LastSeenAt:         "2026-01-02T00:00:00Z",
			CreatedAt:          "2026-01-02T00:00:00Z",
			UpdatedAt:          "2026-01-02T00:00:00Z",
			Status:             "active",
			SupersededBySyncID: nil,
		},
	}
	revs := []bundle.Revision{
		{
			ObservationSyncID: "obs-bbbbbbbbbbbbbbbb",
			Revision:          1,
			Type:              "decision",
			Title:             "Use widgets (old)",
			Content:           "Old content.",
			CreatedAt:         "2026-01-01T00:00:00Z",
			ArchivedAt:        "2026-01-02T00:00:00Z",
		},
	}
	var prompts []bundle.Prompt
	if includePrompts {
		prompts = []bundle.Prompt{
			{SyncID: "pr-cccccccccccccccc", SessionID: "sess-1", Content: "please use gadgets", Project: "demo", CreatedAt: "2026-01-02T00:00:00Z"},
		}
	}

	return bundle.Bundle{
		Manifest: bundle.Manifest{
			FormatVersion:   bundle.FormatVersion,
			Project:         "demo",
			ExportedAt:      "2026-01-03T00:00:00Z",
			SourceHost:      "test-host",
			IncludesPrompts: includePrompts,
			Counts: bundle.Counts{
				Observations: len(obs),
				Revisions:    len(revs),
				Prompts:      len(prompts),
			},
		},
		Observations: obs,
		Revisions:    revs,
		Prompts:      prompts,
	}
}

func TestWriteRead_RoundTrip(t *testing.T) {
	for _, includePrompts := range []bool{false, true} {
		b := sampleBundle(includePrompts)

		var buf bytes.Buffer
		if err := bundle.Write(&buf, b); err != nil {
			t.Fatalf("Write: %v", err)
		}

		got, err := bundle.Read(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if err != nil {
			t.Fatalf("Read: %v", err)
		}

		if got.Manifest.Project != b.Manifest.Project {
			t.Errorf("project = %q, want %q", got.Manifest.Project, b.Manifest.Project)
		}
		if got.Manifest.FormatVersion != bundle.FormatVersion {
			t.Errorf("format_version = %d, want %d", got.Manifest.FormatVersion, bundle.FormatVersion)
		}
		if got.Manifest.IncludesPrompts != includePrompts {
			t.Errorf("includes_prompts = %v, want %v", got.Manifest.IncludesPrompts, includePrompts)
		}
		if len(got.Observations) != len(b.Observations) {
			t.Fatalf("observations count = %d, want %d", len(got.Observations), len(b.Observations))
		}
		if got.Observations[1].SyncID != "obs-bbbbbbbbbbbbbbbb" {
			t.Errorf("observation[1].sync_id = %q, want obs-bbbbbbbbbbbbbbbb", got.Observations[1].SyncID)
		}
		if len(got.Revisions) != len(b.Revisions) {
			t.Fatalf("revisions count = %d, want %d", len(got.Revisions), len(b.Revisions))
		}
		if got.Revisions[0].ObservationSyncID != "obs-bbbbbbbbbbbbbbbb" {
			t.Errorf("revision.observation_sync_id = %q, want obs-bbbbbbbbbbbbbbbb", got.Revisions[0].ObservationSyncID)
		}
		if len(got.Prompts) != len(b.Prompts) {
			t.Fatalf("prompts count = %d, want %d", len(got.Prompts), len(b.Prompts))
		}
		if includePrompts && got.Prompts[0].Content != "please use gadgets" {
			t.Errorf("prompt content = %q, want %q", got.Prompts[0].Content, "please use gadgets")
		}
	}
}

func TestWrite_OmitsPromptsFileWhenNotIncluded(t *testing.T) {
	b := sampleBundle(false)

	var buf bytes.Buffer
	if err := bundle.Write(&buf, b); err != nil {
		t.Fatalf("Write: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == bundle.FilePrompts {
			t.Errorf("prompts.jsonl must not be present when IncludesPrompts=false")
		}
	}
}

func TestRead_RejectsUnknownFormatVersion(t *testing.T) {
	b := sampleBundle(false)
	b.Manifest.FormatVersion = 99

	var buf bytes.Buffer
	if err := bundle.Write(&buf, b); err != nil {
		t.Fatalf("Write: %v", err)
	}

	_, err := bundle.Read(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err == nil {
		t.Fatal("expected error for unknown format_version, got nil")
	}
	if !strings.Contains(err.Error(), "format_version") {
		t.Errorf("error should mention format_version, got: %v", err)
	}
}

func TestRead_DetectsTamperedObservations(t *testing.T) {
	b := sampleBundle(false)

	var buf bytes.Buffer
	if err := bundle.Write(&buf, b); err != nil {
		t.Fatalf("Write: %v", err)
	}

	tampered := tamperZipFile(t, buf.Bytes(), bundle.FileObservations, func(data []byte) []byte {
		return append(data, []byte(`{"sync_id":"obs-injected","project":"demo"}`+"\n")...)
	})

	_, err := bundle.Read(bytes.NewReader(tampered), int64(len(tampered)))
	if err == nil {
		t.Fatal("expected error for tampered observations.jsonl, got nil")
	}
	if !strings.Contains(err.Error(), "sha256") && !strings.Contains(err.Error(), "checksum") && !strings.Contains(err.Error(), "hash") {
		t.Errorf("error should mention hash/checksum mismatch, got: %v", err)
	}
}

func TestRead_DetectsTamperedManifestCounts(t *testing.T) {
	b := sampleBundle(false)

	var buf bytes.Buffer
	if err := bundle.Write(&buf, b); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Corrupt the manifest itself (not hash-checked, but must still fail to
	// parse or be internally consistent) by truncating the observations file
	// so its declared hash no longer matches.
	tampered := tamperZipFile(t, buf.Bytes(), bundle.FileObservations, func(data []byte) []byte {
		if len(data) < 2 {
			return data
		}
		return data[:len(data)-2]
	})

	_, err := bundle.Read(bytes.NewReader(tampered), int64(len(tampered)))
	if err == nil {
		t.Fatal("expected error for truncated observations.jsonl, got nil")
	}
}

// ─── zip-bomb / path-safety hardening ────────────────────────────────────────

// buildBundleZipReplacing builds a well-formed bundle archive (correct
// manifest, hashes, format version — via the real bundle.Write) and then
// re-emits it into a fresh zip, copying every original entry verbatim
// EXCEPT replaceName (dropped), then calling appendFn to add whatever
// entries the test wants in its place. This isolates each test to the ONE
// anomaly it's checking — an unrelated hash-mismatch or missing-file error
// arriving first would make these tests pass for the wrong reason.
func buildBundleZipReplacing(t *testing.T, replaceName string, appendFn func(zw *zip.Writer, orig *zip.Reader) error) []byte {
	t.Helper()
	var base bytes.Buffer
	if err := bundle.Write(&base, sampleBundle(false)); err != nil {
		t.Fatalf("Write base bundle: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(base.Bytes()), int64(base.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		if f.Name == replaceName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatalf("create %s: %v", f.Name, err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("write %s: %v", f.Name, err)
		}
	}
	if err := appendFn(zw, zr); err != nil {
		t.Fatalf("appendFn: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return out.Bytes()
}

func TestRead_RejectsEntryClaimingHugeUncompressedSize(t *testing.T) {
	// A genuine (if synthetic) zip-bomb shape: a real, honestly-declared
	// UncompressedSize64 over the cap, backed by real — but extremely
	// compressible — filler data, so the archive itself stays tiny. Read
	// must reject this by inspecting the header BEFORE ever
	// opening/decompressing the entry, not by exhausting memory first.
	const overCap = (64 << 20) + (1 << 20) // 1 MiB over the 64 MiB per-entry cap
	raw := buildBundleZipReplacing(t, bundle.FileObservations, func(zw *zip.Writer, _ *zip.Reader) error {
		w, err := zw.Create(bundle.FileObservations)
		if err != nil {
			return err
		}
		chunk := make([]byte, 1<<20) // 1 MiB of zeros — compresses to almost nothing
		for written := 0; written < overCap; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return err
			}
		}
		return nil
	})

	_, err := bundle.Read(bytes.NewReader(raw), int64(len(raw)))
	if err == nil {
		t.Fatal("expected error for an entry claiming a huge uncompressed size, got nil")
	}
	// The replaced entry's content no longer matches its original SHA-256
	// manifest entry either, so a hash-mismatch error would ALSO fire for
	// this fixture — assert on wording to prove the SIZE cap (checked
	// before ever decompressing) is what actually rejected it, not that
	// incidental mismatch.
	if strings.Contains(err.Error(), "sha256") {
		t.Errorf("error appears to be an incidental sha256 mismatch, not the size-cap check: %v", err)
	}
}

func TestRead_RejectsDuplicateEntryNames(t *testing.T) {
	// Duplicate observations.jsonl with the EXACT SAME bytes as the
	// original, so a hash-mismatch can't incidentally catch this — only an
	// explicit duplicate-name check can.
	raw := buildBundleZipReplacing(t, "", func(zw *zip.Writer, orig *zip.Reader) error {
		var original *zip.File
		for _, f := range orig.File {
			if f.Name == bundle.FileObservations {
				original = f
				break
			}
		}
		if original == nil {
			t.Fatal("base bundle missing observations.jsonl")
		}
		rc, err := original.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}
		w, err := zw.Create(bundle.FileObservations)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})

	_, err := bundle.Read(bytes.NewReader(raw), int64(len(raw)))
	if err == nil {
		t.Fatal("expected error for duplicate entry names, got nil")
	}
}

func TestRead_RejectsPathTraversalEntryName(t *testing.T) {
	raw := buildBundleZipReplacing(t, "", func(zw *zip.Writer, _ *zip.Reader) error {
		w, err := zw.Create("../evil.txt")
		if err != nil {
			return err
		}
		_, err = w.Write([]byte("nope"))
		return err
	})

	_, err := bundle.Read(bytes.NewReader(raw), int64(len(raw)))
	if err == nil {
		t.Fatal("expected error for a path-traversal entry name, got nil")
	}
}

func TestRead_RejectsUnknownEntryName(t *testing.T) {
	raw := buildBundleZipReplacing(t, "", func(zw *zip.Writer, _ *zip.Reader) error {
		w, err := zw.Create("not-a-real-bundle-file.txt")
		if err != nil {
			return err
		}
		_, err = w.Write([]byte("nope"))
		return err
	})

	_, err := bundle.Read(bytes.NewReader(raw), int64(len(raw)))
	if err == nil {
		t.Fatal("expected error for an unknown entry name, got nil")
	}
}

// tamperZipFile rewrites the zip archive in raw, replacing the contents of
// the file named target with mutate(originalContents), and returns the new
// archive bytes.
func tamperZipFile(t *testing.T, raw []byte, target string, mutate func([]byte) []byte) []byte {
	t.Helper()

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		data := make([]byte, 0)
		buf := make([]byte, 4096)
		for {
			n, rerr := rc.Read(buf)
			if n > 0 {
				data = append(data, buf[:n]...)
			}
			if rerr != nil {
				break
			}
		}
		rc.Close()

		if f.Name == target {
			data = mutate(data)
		}

		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatalf("create %s: %v", f.Name, err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("write %s: %v", f.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return out.Bytes()
}
