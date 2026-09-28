package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"testing"
)

// ─── parseFlagsWithHelp (unit-level, isolated FlagSet) ────────────────────────

func TestParseFlagsWithHelp_PrintsUsageAndReturnsSentinel(t *testing.T) {
	fs := flag.NewFlagSet("widget", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("color", "red", "Widget color.")

	var buf bytes.Buffer
	err := parseFlagsWithHelp(fs, []string{"--help"}, &buf)

	if !errors.Is(err, errHelpShown) {
		t.Fatalf("err = %v, want errHelpShown", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Usage: ion-mem widget [flags]") {
		t.Errorf("output missing usage header, got: %q", out)
	}
	if !strings.Contains(out, "-color") {
		t.Errorf("output missing flag description, got: %q", out)
	}
}

func TestParseFlagsWithHelp_BadFlagUnchanged(t *testing.T) {
	fs := flag.NewFlagSet("widget", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("color", "red", "Widget color.")

	var buf bytes.Buffer
	err := parseFlagsWithHelp(fs, []string{"--bogus=1"}, &buf)

	if err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
	if errors.Is(err, errHelpShown) {
		t.Fatalf("bad-flag error must not be errHelpShown, got: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for a bad flag, got: %q", buf.String())
	}
}

func TestParseFlagsWithHelp_SuccessUnchanged(t *testing.T) {
	fs := flag.NewFlagSet("widget", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("color", "red", "Widget color.")

	var buf bytes.Buffer
	err := parseFlagsWithHelp(fs, []string{"--color=blue"}, &buf)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output on success, got: %q", buf.String())
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it. Needed because the parse*Flags functions print
// --help output straight to os.Stdout (see parseFlagsWithHelp call sites).
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return buf.String()
}

// ─── search --help / doctor --help (the two commands named in the bug report) ─

func TestParseSearchFlags_Help(t *testing.T) {
	var err error
	out := captureStdout(t, func() {
		_, err = parseSearchFlags([]string{"--help"}, fakeHome)
	})

	if !errors.Is(err, errHelpShown) {
		t.Fatalf("err = %v, want errHelpShown", err)
	}
	if !strings.Contains(out, "Usage: ion-mem search [flags]") {
		t.Errorf("missing usage header, got: %q", out)
	}
	if !strings.Contains(out, "-limit") {
		t.Errorf("missing -limit flag in output, got: %q", out)
	}
}

func TestParseDoctorFlags_Help(t *testing.T) {
	var err error
	out := captureStdout(t, func() {
		_, err = parseDoctorFlags([]string{"--help"}, fakeHome)
	})

	if !errors.Is(err, errHelpShown) {
		t.Fatalf("err = %v, want errHelpShown", err)
	}
	if !strings.Contains(out, "Usage: ion-mem doctor [flags]") {
		t.Errorf("missing usage header, got: %q", out)
	}
	if !strings.Contains(out, "-timeout") {
		t.Errorf("missing -timeout flag in output, got: %q", out)
	}
}
