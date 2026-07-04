package tui

import (
	"testing"
	"time"
)

func TestHumanizeTime(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{
			name: "zero time returns dash",
			t:    time.Time{},
			want: "—",
		},
		{
			name: "30 seconds ago returns just now",
			t:    now.Add(-30 * time.Second),
			want: "just now",
		},
		{
			name: "10 minutes ago returns minutes",
			t:    now.Add(-10 * time.Minute),
			want: "10m ago",
		},
		{
			name: "3 hours ago returns hours",
			t:    now.Add(-3 * time.Hour),
			want: "3h ago",
		},
		{
			name: "2 days ago returns days",
			t:    now.Add(-48 * time.Hour),
			want: "2d ago",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := humanizeTime(tt.t)
			if got != tt.want {
				t.Errorf("humanizeTime = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncStr(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{
			name: "short string unchanged",
			s:    "hello",
			n:    10,
			want: "hello",
		},
		{
			name: "exact length unchanged",
			s:    "hello",
			n:    5,
			want: "hello",
		},
		{
			name: "long string truncated with ellipsis",
			s:    "hello world",
			n:    8,
			want: "hello w…",
		},
		{
			name: "n=1 returns ellipsis",
			s:    "abc",
			n:    1,
			want: "…",
		},
		{
			name: "n=0 returns empty",
			s:    "abc",
			n:    0,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncStr(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("truncStr(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

func TestParseCreatedAt(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
	}{
		{
			name:    "RFC3339Nano parses",
			input:   "2024-01-15T10:30:00.123456789Z",
			wantNil: false,
		},
		{
			name:    "RFC3339 parses",
			input:   "2024-01-15T10:30:00Z",
			wantNil: false,
		},
		{
			name:    "SQLite datetime parses",
			input:   "2024-01-15 10:30:00",
			wantNil: false,
		},
		{
			name:    "empty string returns zero time",
			input:   "",
			wantNil: true,
		},
		{
			name:    "invalid string returns zero time",
			input:   "not-a-date",
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCreatedAt(tt.input)
			if tt.wantNil && !got.IsZero() {
				t.Errorf("parseCreatedAt(%q) = %v, want zero time", tt.input, got)
			}
			if !tt.wantNil && got.IsZero() {
				t.Errorf("parseCreatedAt(%q) = zero time, want non-zero", tt.input)
			}
		})
	}
}
