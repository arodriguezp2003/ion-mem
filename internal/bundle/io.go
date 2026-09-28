package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Size caps against decompression-bomb bundles: a zip whose headers
// declare (or whose actual content turns out to be) far more data than any
// real bundle should ever contain. maxBundleEntryBytes bounds any single
// entry; maxBundleTotalBytes bounds the sum across all entries. Both are
// enforced BEFORE decompressing an entry (via its header's declared
// UncompressedSize64) and again WHILE reading it (via a capped reader), so
// a header that lies about its own size can't be used to bypass the cap.
const (
	maxBundleEntryBytes = 64 << 20  // 64 MiB
	maxBundleTotalBytes = 256 << 20 // 256 MiB
)

// validBundleEntryNames is the closed set of file names a bundle zip may
// contain. Any other name — including a path-traversal attempt like
// "../evil.txt" or a legitimate-looking but unrecognized file — is
// rejected outright rather than silently ignored.
var validBundleEntryNames = map[string]bool{
	FileManifest:     true,
	FileObservations: true,
	FileRevisions:    true,
	FilePrompts:      true,
}

// Write serializes b as a zip archive to w: manifest.json, observations.jsonl,
// revisions.jsonl, and (when b.Manifest.IncludesPrompts is true) prompts.jsonl.
//
// The manifest's SHA256 map is always recomputed from the actual serialized
// data files before being written, so a caller-supplied Manifest.SHA256 is
// ignored on input and replaced with the true digests.
func Write(w io.Writer, b Bundle) error {
	obsData, err := encodeJSONL(b.Observations)
	if err != nil {
		return fmt.Errorf("bundle.Write: encode observations: %w", err)
	}
	revData, err := encodeJSONL(b.Revisions)
	if err != nil {
		return fmt.Errorf("bundle.Write: encode revisions: %w", err)
	}

	sums := map[string]string{
		FileObservations: sha256Hex(obsData),
		FileRevisions:    sha256Hex(revData),
	}

	var promptData []byte
	if b.Manifest.IncludesPrompts {
		promptData, err = encodeJSONL(b.Prompts)
		if err != nil {
			return fmt.Errorf("bundle.Write: encode prompts: %w", err)
		}
		sums[FilePrompts] = sha256Hex(promptData)
	}

	manifest := b.Manifest
	manifest.SHA256 = sums
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("bundle.Write: encode manifest: %w", err)
	}

	zw := zip.NewWriter(w)

	if err := writeZipFile(zw, FileManifest, manifestData); err != nil {
		return err
	}
	if err := writeZipFile(zw, FileObservations, obsData); err != nil {
		return err
	}
	if err := writeZipFile(zw, FileRevisions, revData); err != nil {
		return err
	}
	if manifest.IncludesPrompts {
		if err := writeZipFile(zw, FilePrompts, promptData); err != nil {
			return err
		}
	}

	if err := zw.Close(); err != nil {
		return fmt.Errorf("bundle.Write: close zip: %w", err)
	}
	return nil
}

// Read parses a bundle zip archive from r (of the given size), verifying the
// format version and every data file's SHA-256 digest against the manifest
// before decoding. Returns an error on format-version mismatch, a missing
// required file, or any hash mismatch (corruption or tampering).
func Read(r io.ReaderAt, size int64) (Bundle, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return Bundle{}, fmt.Errorf("bundle.Read: open zip: %w", err)
	}

	files := make(map[string][]byte, len(zr.File))
	var totalBytes int64
	for _, f := range zr.File {
		if !validBundleEntryNames[f.Name] || strings.Contains(f.Name, "..") || strings.ContainsRune(f.Name, '/') {
			return Bundle{}, fmt.Errorf("bundle.Read: unexpected entry %q (only manifest.json, observations.jsonl, revisions.jsonl, prompts.jsonl are valid)", f.Name)
		}
		if _, dup := files[f.Name]; dup {
			return Bundle{}, fmt.Errorf("bundle.Read: duplicate entry %q", f.Name)
		}
		if f.UncompressedSize64 > maxBundleEntryBytes {
			return Bundle{}, fmt.Errorf("bundle.Read: entry %q declares uncompressed size %d bytes, exceeding the %d-byte per-entry cap",
				f.Name, f.UncompressedSize64, uint64(maxBundleEntryBytes))
		}
		totalBytes += int64(f.UncompressedSize64)
		if totalBytes > maxBundleTotalBytes {
			return Bundle{}, fmt.Errorf("bundle.Read: total declared uncompressed size exceeds the %d-byte total cap", int64(maxBundleTotalBytes))
		}

		data, err := readZipFile(f)
		if err != nil {
			return Bundle{}, fmt.Errorf("bundle.Read: read %s: %w", f.Name, err)
		}
		files[f.Name] = data
	}

	manifestData, ok := files[FileManifest]
	if !ok {
		return Bundle{}, fmt.Errorf("bundle.Read: missing %s", FileManifest)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return Bundle{}, fmt.Errorf("bundle.Read: decode manifest: %w", err)
	}
	if manifest.FormatVersion != FormatVersion {
		return Bundle{}, fmt.Errorf("bundle.Read: unsupported format_version %d (want %d)",
			manifest.FormatVersion, FormatVersion)
	}

	obsData, err := verifiedFile(files, manifest, FileObservations, true)
	if err != nil {
		return Bundle{}, err
	}
	revData, err := verifiedFile(files, manifest, FileRevisions, true)
	if err != nil {
		return Bundle{}, err
	}

	var promptData []byte
	if manifest.IncludesPrompts {
		promptData, err = verifiedFile(files, manifest, FilePrompts, true)
		if err != nil {
			return Bundle{}, err
		}
	}

	var b Bundle
	b.Manifest = manifest
	if b.Observations, err = decodeJSONL[Observation](obsData); err != nil {
		return Bundle{}, fmt.Errorf("bundle.Read: decode observations: %w", err)
	}
	if b.Revisions, err = decodeJSONL[Revision](revData); err != nil {
		return Bundle{}, fmt.Errorf("bundle.Read: decode revisions: %w", err)
	}
	if manifest.IncludesPrompts {
		if b.Prompts, err = decodeJSONL[Prompt](promptData); err != nil {
			return Bundle{}, fmt.Errorf("bundle.Read: decode prompts: %w", err)
		}
	}

	return b, nil
}

// verifiedFile looks up name in files, verifies its SHA-256 digest against
// manifest.SHA256[name] (when required, a missing file or missing manifest
// entry is an error), and returns its bytes.
func verifiedFile(files map[string][]byte, manifest Manifest, name string, required bool) ([]byte, error) {
	data, ok := files[name]
	if !ok {
		if required {
			return nil, fmt.Errorf("bundle.Read: missing %s", name)
		}
		return nil, nil
	}
	wantSum, ok := manifest.SHA256[name]
	if !ok {
		return nil, fmt.Errorf("bundle.Read: manifest missing sha256 entry for %s", name)
	}
	gotSum := sha256Hex(data)
	if gotSum != wantSum {
		return nil, fmt.Errorf("bundle.Read: sha256 mismatch for %s: got %s, want %s", name, gotSum, wantSum)
	}
	return data, nil
}

func writeZipFile(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("bundle.Write: create %s: %w", name, err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("bundle.Write: write %s: %w", name, err)
	}
	return nil
}

// readZipFile reads f's decompressed content, capped at maxBundleEntryBytes+1
// via io.LimitReader regardless of what f's header claims. The caller
// already rejected f.UncompressedSize64 > maxBundleEntryBytes before ever
// calling this, but a header can lie about its own size — Deflate's
// declared size isn't verified against the compressed stream until fully
// decompressed, so this is the backstop that actually bounds memory use
// even for a dishonest header.
func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	limited := io.LimitReader(rc, maxBundleEntryBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > maxBundleEntryBytes {
		return nil, fmt.Errorf("decompressed content exceeds the %d-byte per-entry cap (header under-declared its size)", maxBundleEntryBytes)
	}
	return data, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// encodeJSONL encodes each element of rows as one JSON line.
func encodeJSONL[T any](rows []T) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// decodeJSONL decodes newline-delimited JSON into a slice of T. Returns a
// non-nil empty slice for empty input so callers get [] rather than null
// when re-serialized.
func decodeJSONL[T any](data []byte) ([]T, error) {
	out := []T{}
	if len(bytes.TrimSpace(data)) == 0 {
		return out, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var v T
		if err := dec.Decode(&v); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
