package bundle_test

import (
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/bundle"
)

func obsWith(syncID, title, content string) bundle.Observation {
	return bundle.Observation{SyncID: syncID, Title: title, Content: content}
}

func TestScanSecrets_DetectsAWSKey(t *testing.T) {
	b := bundle.Bundle{Observations: []bundle.Observation{
		obsWith("obs-1", "aws config", "export AWS_ACCESS_KEY_ID=AKIAABCDEFGHIJKLMNOP"),
	}}
	findings := bundle.ScanSecrets(b)
	if !hasPattern(findings, "aws_key") {
		t.Errorf("expected aws_key finding, got %+v", findings)
	}
}

func TestScanSecrets_DetectsOpenAIStyleKey(t *testing.T) {
	b := bundle.Bundle{Observations: []bundle.Observation{
		obsWith("obs-1", "api key", "OPENAI_API_KEY=sk-abcdefghijklmnopqrstuvwxyz0123456789"),
	}}
	findings := bundle.ScanSecrets(b)
	if !hasPattern(findings, "api_key") {
		t.Errorf("expected api_key finding, got %+v", findings)
	}
}

func TestScanSecrets_DetectsGitHubToken(t *testing.T) {
	b := bundle.Bundle{Observations: []bundle.Observation{
		obsWith("obs-1", "gh token", "token: ghp_1234567890abcdefghijklmnopqrstuvwxyz"),
	}}
	findings := bundle.ScanSecrets(b)
	if !hasPattern(findings, "github_token") {
		t.Errorf("expected github_token finding, got %+v", findings)
	}
}

func TestScanSecrets_DetectsGitLabToken(t *testing.T) {
	b := bundle.Bundle{Observations: []bundle.Observation{
		obsWith("obs-1", "gl token", "token: glpat-abcdefghijklmnopqrst"),
	}}
	findings := bundle.ScanSecrets(b)
	if !hasPattern(findings, "gitlab_token") {
		t.Errorf("expected gitlab_token finding, got %+v", findings)
	}
}

func TestScanSecrets_DetectsPrivateKey(t *testing.T) {
	b := bundle.Bundle{Observations: []bundle.Observation{
		obsWith("obs-1", "ssh key", "-----BEGIN RSA PRIVATE KEY-----\nMIIBOgIBAAJBAK...\n-----END RSA PRIVATE KEY-----"),
	}}
	findings := bundle.ScanSecrets(b)
	if !hasPattern(findings, "private_key") {
		t.Errorf("expected private_key finding, got %+v", findings)
	}
}

func TestScanSecrets_DetectsJWT(t *testing.T) {
	b := bundle.Bundle{Observations: []bundle.Observation{
		obsWith("obs-1", "jwt", "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dQw4w9WgXcQ"),
	}}
	findings := bundle.ScanSecrets(b)
	if !hasPattern(findings, "jwt") {
		t.Errorf("expected jwt finding, got %+v", findings)
	}
}

func TestScanSecrets_DetectsPasswordAssignment(t *testing.T) {
	b := bundle.Bundle{Observations: []bundle.Observation{
		obsWith("obs-1", "db creds", "password=Sup3rS3cretPass!"),
	}}
	findings := bundle.ScanSecrets(b)
	if !hasPattern(findings, "password_assignment") {
		t.Errorf("expected password_assignment finding, got %+v", findings)
	}
}

func TestScanSecrets_IgnoresPlaceholderPassword(t *testing.T) {
	cases := []string{
		"password=changeme",
		"password=<PASSWORD>",
		"password=your-password-here",
		"password=${PASSWORD}",
		"password=xxxxxxxx",
		"password=REDACTED",
	}
	for _, c := range cases {
		b := bundle.Bundle{Observations: []bundle.Observation{obsWith("obs-1", "t", c)}}
		findings := bundle.ScanSecrets(b)
		if hasPattern(findings, "password_assignment") {
			t.Errorf("content %q should NOT be flagged as a secret, got %+v", c, findings)
		}
	}
}

func TestScanSecrets_CleanContentHasNoFindings(t *testing.T) {
	b := bundle.Bundle{Observations: []bundle.Observation{
		obsWith("obs-1", "clean note", "We decided to use Postgres for the primary datastore."),
	}}
	findings := bundle.ScanSecrets(b)
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %+v", findings)
	}
}

func TestScanSecrets_ScansPromptsAndRevisions(t *testing.T) {
	b := bundle.Bundle{
		Revisions: []bundle.Revision{
			{ObservationSyncID: "obs-1", Title: "old", Content: "AKIAABCDEFGHIJKLMNOP"},
		},
		Prompts: []bundle.Prompt{
			{SyncID: "pr-1", Content: "here is my key sk-abcdefghijklmnopqrstuvwxyz0123456789"},
		},
	}
	findings := bundle.ScanSecrets(b)
	if len(findings) < 2 {
		t.Errorf("expected findings from both revisions and prompts, got %+v", findings)
	}
}

func TestScanSecrets_ReportsSyncIDAndTitle(t *testing.T) {
	b := bundle.Bundle{Observations: []bundle.Observation{
		obsWith("obs-xyz", "leaked key", "AKIAABCDEFGHIJKLMNOP"),
	}}
	findings := bundle.ScanSecrets(b)
	if len(findings) != 1 {
		t.Fatalf("expected exactly 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.SyncID != "obs-xyz" {
		t.Errorf("SyncID = %q, want obs-xyz", f.SyncID)
	}
	if f.Title != "leaked key" {
		t.Errorf("Title = %q, want %q", f.Title, "leaked key")
	}
	if f.Pattern != "aws_key" {
		t.Errorf("Pattern = %q, want aws_key", f.Pattern)
	}
}

func hasPattern(findings []bundle.Finding, pattern string) bool {
	for _, f := range findings {
		if f.Pattern == pattern {
			return true
		}
	}
	return false
}
