package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func (h *harness) sharedHome() string { return filepath.Join(h.userHome, ".claude") }

func (h *harness) write(path, content string) {
	h.t.Helper()
	must(h.t, os.MkdirAll(filepath.Dir(path), 0o700))
	must(h.t, os.WriteFile(path, []byte(content), 0o600))
}

func (h *harness) read(path string) string {
	h.t.Helper()
	data, err := os.ReadFile(path)
	must(h.t, err)
	return string(data)
}

func (h *harness) writeJSON(path string, v any) {
	h.t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	must(h.t, err)
	h.write(path, string(data))
}

func (h *harness) readJSON(path string) map[string]any {
	h.t.Helper()
	var v map[string]any
	must(h.t, json.Unmarshal([]byte(h.read(path)), &v))
	return v
}

func isLinkTo(t *testing.T, link, target string) {
	t.Helper()
	got, err := os.Readlink(link)
	if err != nil {
		t.Errorf("%s is not a link: %v", link, err)
	} else if got != target {
		t.Errorf("%s links to %s, want %s", link, got, target)
	}
}

func TestSetupSharesHome(t *testing.T) {
	h := newHarness(t)
	home := h.sharedHome()
	h.write(filepath.Join(home, "settings.json"), `{"theme":"dark"}`)
	h.write(filepath.Join(home, "skills", "x", "SKILL.md"), "skill")
	h.write(filepath.Join(home, "projects", "p1", "s1.jsonl"), "home session")

	a1 := h.dir("1")
	h.write(filepath.Join(a1, "projects", "p2", "s2.jsonl"), "account session")                              // moves to home
	h.write(filepath.Join(a1, "projects", "p1", "s1.jsonl"), "different")                                    // clashes
	must(t, os.MkdirAll(filepath.Join(a1, "projects", "-private-var-folders-x-T-claude-router-123"), 0o700)) // junk
	h.write(filepath.Join(a1, "settings.json"), `{"theme":"dark"}`)                                          // same as home

	out, errOut, code := h.run("setup")
	if code != 0 {
		t.Fatalf("setup exit %d: %s %s", code, out, errOut)
	}
	for _, id := range []string{"0", "1"} {
		isLinkTo(t, filepath.Join(h.dir(id), "settings.json"), filepath.Join(home, "settings.json"))
		isLinkTo(t, filepath.Join(h.dir(id), "skills"), filepath.Join(home, "skills"))
		isLinkTo(t, filepath.Join(h.dir(id), "projects"), filepath.Join(home, "projects"))
		isLinkTo(t, filepath.Join(h.dir(id), "ide"), filepath.Join(home, "ide"))
	}
	if got := h.read(filepath.Join(home, "projects", "p2", "s2.jsonl")); got != "account session" {
		t.Errorf("account-only session not moved home: %q", got)
	}
	if got := h.read(filepath.Join(home, "projects", "p1", "s1.jsonl")); got != "home session" {
		t.Errorf("home session overwritten: %q", got)
	}
	if got := h.read(filepath.Join(a1, "projects.before-claude-router", "p1", "s1.jsonl")); got != "different" {
		t.Errorf("clashing session not kept aside: %q", got)
	}
	if _, err := os.Stat(filepath.Join(home, "projects", "-private-var-folders-x-T-claude-router-123")); err == nil {
		t.Error("leftover usage-read folder moved home instead of removed")
	}
	if _, err := os.Lstat(filepath.Join(a1, "settings.json.before-claude-router")); err == nil {
		t.Error("an identical settings.json should not be set aside")
	}
	contains(t, "setup output", out, "set "+filepath.Join(a1, "projects")+" aside")

	out, _, _ = h.run("setup")
	contains(t, "setup again", out, "claude0: already shares", "claude1: already shares")
}

func TestSyncSharedSettings(t *testing.T) {
	h := newHarness(t)
	homeJSON := filepath.Join(h.userHome, ".claude.json")
	h.writeJSON(homeJSON, map[string]any{
		"oauthAccount": map[string]any{"emailAddress": "home@example.com"},
		"mcpServers":   map[string]any{"a": map[string]any{"command": "a"}},
		"theme":        "dark",
		"numStartups":  7,
		"projects": map[string]any{
			"/work": map[string]any{"hasTrustDialogAccepted": true, "allowedTools": []string{"Bash"}, "lastCost": 5},
			"/private/var/folders/x/T/claude-router-99": map[string]any{"hasTrustDialogAccepted": false},
		},
	})
	a1 := filepath.Join(h.dir("1"), ".claude.json")
	h.writeJSON(a1, map[string]any{
		"oauthAccount": map[string]any{"emailAddress": "1@example.com"},
		"userID":       "u1",
		"mcpServers":   map[string]any{"b": map[string]any{"command": "b"}},
	})
	a0 := filepath.Join(h.dir("0"), ".claude.json")
	h.writeJSON(a0, map[string]any{"oauthAccount": map[string]any{"emailAddress": "0@example.com"}})

	if _, errOut, code := h.run("setup"); code != 0 {
		t.Fatalf("setup: %s", errOut)
	}
	acct1 := h.readJSON(a1)
	servers := acct1["mcpServers"].(map[string]any)
	if servers["a"] == nil || servers["b"] == nil {
		t.Errorf("account 1 should have servers a and b: %v", servers)
	}
	if acct1["theme"] != "dark" {
		t.Errorf("theme not shared: %v", acct1["theme"])
	}
	work := acct1["projects"].(map[string]any)["/work"].(map[string]any)
	if work["hasTrustDialogAccepted"] != true || work["lastCost"] != nil {
		t.Errorf("project sharing wrong: %v", work)
	}
	if acct1["oauthAccount"].(map[string]any)["emailAddress"] != "1@example.com" || acct1["userID"] != "u1" || acct1["numStartups"] != nil {
		t.Errorf("account-specific fields changed: %v", acct1)
	}
	home := h.readJSON(homeJSON)
	if home["mcpServers"].(map[string]any)["b"] == nil {
		t.Error("server b added under account 1 did not reach home")
	}
	if home["oauthAccount"].(map[string]any)["emailAddress"] != "home@example.com" || home["numStartups"] != float64(7) {
		t.Errorf("home's own fields changed: %v", home)
	}
	if _, ok := home["projects"].(map[string]any)["/private/var/folders/x/T/claude-router-99"]; ok {
		t.Error("leftover usage-read project not removed")
	}
	if h.readJSON(a0)["mcpServers"].(map[string]any)["b"] == nil {
		t.Error("server b did not reach account 0")
	}

	// Removing a server under one account removes it everywhere.
	acct1 = h.readJSON(a1)
	delete(acct1["mcpServers"].(map[string]any), "b")
	h.writeJSON(a1, acct1)
	h.run("setup")
	for _, p := range []string{homeJSON, a0, a1} {
		if h.readJSON(p)["mcpServers"].(map[string]any)["b"] != nil {
			t.Errorf("server b still in %s after removal", p)
		}
	}

	// An account signed in later gets the shared settings and removes nothing.
	a3 := filepath.Join(h.dir("3"), ".claude.json")
	h.writeJSON(a3, map[string]any{"oauthAccount": map[string]any{"emailAddress": "3@example.com"}})
	h.run("setup")
	if h.readJSON(a3)["mcpServers"].(map[string]any)["a"] == nil {
		t.Error("new account did not get server a")
	}
	if h.readJSON(homeJSON)["mcpServers"].(map[string]any)["a"] == nil {
		t.Error("new account's empty config removed server a")
	}
}

// usageFile writes an account .claude.json with Claude Code's cached usage.
func (h *harness) usageFile(id, email string, week float64, resets time.Time, fetched time.Time) {
	h.t.Helper()
	h.writeJSON(filepath.Join(h.dir(id), ".claude.json"), map[string]any{
		"oauthAccount": map[string]any{"emailAddress": email},
		"cachedUsageUtilization": map[string]any{
			"fetchedAtMs": fetched.UnixMilli(),
			"utilization": map[string]any{
				"five_hour": nil,
				"seven_day": map[string]any{"utilization": week, "resets_at": resets.UTC().Format(time.RFC3339Nano)},
			},
		},
	})
}

func TestLaunchPicksMostRoom(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	later := now.Add(48 * time.Hour)
	h.usageFile("0", "0@example.com", 80, later, now)
	h.usageFile("1", "1@example.com", 30, later, now.Add(-5*time.Hour)) // old reading still counts for launch
	h.env = append(h.env, "CLAUDECODE=1", "CLAUDE_CODE_MESSAGING_SOCKET=/tmp/x")

	// A missing binary fails cleanly rather than hanging or starting something else.
	if out, _, code := h.run("launch", "/nonexistent/claude"); code == 0 || out != "" {
		t.Errorf("launch with a missing binary: code %d out %q", code, out)
	}

	out, errOut, code := h.run("launch", envValue(h.env, "CLAUDE_BIN"), "--print", "hi")
	if code != 0 {
		t.Fatalf("launch exit %d: %s", code, errOut)
	}
	want := fmt.Sprintf("ran claude --print hi under %s cfg=%s cc=unset\n", h.dir("1"), h.dir("1"))
	if out != want {
		t.Errorf("launch output:\n got %q\nwant %q", out, want)
	}
	isLinkTo(t, filepath.Join(h.dir("1"), "projects"), filepath.Join(h.sharedHome(), "projects"))
	contains(t, "log", h.read(filepath.Join(h.cache, "router.log")), "launch: claude1 (1@example.com, 70% left")
}

func TestLaunchCountsPassedResetAsEmpty(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.usageFile("0", "0@example.com", 100, now.Add(-time.Hour), now.Add(-3*time.Hour)) // reset an hour ago
	h.usageFile("1", "1@example.com", 30, now.Add(48*time.Hour), now)
	out, _, _ := h.run("launch", envValue(h.env, "CLAUDE_BIN"))
	contains(t, "launch", out, "cfg="+h.dir("0"))
}

func TestLaunchWithNoAccountRunsUnrouted(t *testing.T) {
	h := newHarness(t)
	h.env = append(h.env, "CLAUDE_CONFIG_DIR=/somewhere/else")
	out, _, code := h.run("launch", envValue(h.env, "CLAUDE_BIN"), "x")
	if code != 0 {
		t.Fatalf("launch exit %d", code)
	}
	contains(t, "unrouted", out, "ran claude x under "+filepath.Join(h.userHome, ".claude")+" cfg=unset")
	contains(t, "log", h.read(filepath.Join(h.cache, "router.log")), "no account signed in")
}

func TestUseStripsParentSession(t *testing.T) {
	h := newHarness(t)
	h.env = append(h.env, "CLAUDECODE=1")
	out, _, _ := h.run("use", "1")
	contains(t, "use", out, "cfg="+h.dir("1")+" cc=unset")
}

func TestStatusUsesClaudeCodeUsage(t *testing.T) {
	h := newHarness(t)
	h.run("status") // our own readings, from /usage text
	resets := time.Now().Add(72 * time.Hour).Truncate(time.Hour)
	h.usageFile("1", "1@example.com", 12, resets, time.Now().Add(time.Second))
	out, _, _ := h.run("status", "--cached")
	contains(t, "status", out, "claude1  1@example.com (max)  week 12% (resets "+resets.UTC().Format("Jan 2")+" at "+strings.Replace(resets.UTC().Format("3:04pm"), ":00", "", 1)+")")
	contains(t, "next", out, "claude1  1@example.com (max)  week 12%")
	contains(t, "next marker", out, "<- next session")
}

func TestFormatReset(t *testing.T) {
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	for in, want := range map[time.Time]string{
		time.Date(2026, 11, 25, 15, 0, 0, 0, time.UTC):  "3pm",
		time.Date(2026, 11, 25, 15, 30, 0, 0, time.UTC): "3:30pm",
		time.Date(2026, 12, 1, 3, 0, 0, 76, time.UTC):   "Dec 1 at 3am",
	} {
		if got := formatReset(in, now); got != want {
			t.Errorf("formatReset(%v) = %q, want %q", in, got, want)
		}
	}
}

func envValue(env []string, name string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], name+"="); ok {
			return v
		}
	}
	return ""
}
