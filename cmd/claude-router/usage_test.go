package main

import (
	"reflect"
	"testing"
	"time"
)

const sampleUsage = `You are currently using your subscription to power your Claude Code usage

Current session: 80% used · resets 3pm (Europe/London)
Current week (all models): 40% used · resets Nov 30 at 9am (Europe/London)
Current week (Sonnet only): 5% used · resets Nov 30 at 9am (Europe/London)

What's contributing to your limits usage?
Last 24h · 120 requests · 3 sessions
  97% of your usage came from long sessions
`

func TestParseUsage(t *testing.T) {
	got := parseUsage(sampleUsage)
	want := []Limit{
		{"Current session", 80, "3pm (Europe/London)"},
		{"Current week (all models)", 40, "Nov 30 at 9am (Europe/London)"},
		{"Current week (Sonnet only)", 5, "Nov 30 at 9am (Europe/London)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseUsage:\n got %+v\nwant %+v", got, want)
	}
}

func TestParseUsageNoResetAndCRLF(t *testing.T) {
	got := parseUsage("Current week (all models): 100% used\r\n")
	want := []Limit{{"Current week (all models)", 100, ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestParseUsageAPIKey(t *testing.T) {
	if got := parseUsage("You are currently using an API key\n"); len(got) != 0 {
		t.Fatalf("expected no limits, got %+v", got)
	}
}

func TestRoom(t *testing.T) {
	cases := []struct {
		name   string
		limits []Limit
		want   *int
	}{
		{"session and week: the higher counts", parseUsage(sampleUsage), ptr(20)},
		{"week only", []Limit{{"Current week (all models)", 60, ""}}, ptr(40)},
		{"sonnet ignored when main limits exist", []Limit{{"Current week (all models)", 10, ""}, {"Current week (Sonnet only)", 90, ""}}, ptr(90)},
		{"falls back to any limit", []Limit{{"Current week (Opus only)", 70, ""}}, ptr(30)},
		{"over 100 floors at 0", []Limit{{"Current session", 120, ""}}, ptr(0)},
		{"nothing reported", nil, nil},
	}
	for _, tc := range cases {
		got := room(tc.limits)
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Errorf("%s: got %v want %v", tc.name, deref(got), deref(tc.want))
		}
	}
}

func TestShortLabelAndZone(t *testing.T) {
	for in, want := range map[string]string{
		"Current session":            "session",
		"Current week (all models)":  "week",
		"Current week (Sonnet only)": "week Sonnet",
		"Something else":             "Something else",
	} {
		if got := shortLabel(in); got != want {
			t.Errorf("shortLabel(%q) = %q, want %q", in, got, want)
		}
	}
	if got := stripZone("Nov 30 at 9am (Europe/London)"); got != "Nov 30 at 9am" {
		t.Errorf("stripZone = %q", got)
	}
}

func TestAgo(t *testing.T) {
	for d, want := range map[time.Duration]string{
		10 * time.Second: "just now",
		45 * time.Minute: "45m ago",
		3 * time.Hour:    "3h ago",
		72 * time.Hour:   "3d ago",
	} {
		if got := ago(d); got != want {
			t.Errorf("ago(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/Users/a/.claude-1": "/Users/a/.claude-1",
		"/Users/a b/.c":      "'/Users/a b/.c'",
		"it's":               `'it'\''s'`,
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func ptr(n int) *int { return &n }

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
