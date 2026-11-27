package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// A Limit is one usage limit, e.g. "Current session: 80% used · resets 3pm".
type Limit struct {
	Label    string `json:"label"`
	Used     int    `json:"used"`
	Resets   string `json:"resets,omitempty"`
	ResetsAt int64  `json:"resets_at,omitempty"` // unix seconds, when known
}

// A reading is what the cache holds for one account after a live read.
type reading struct {
	Account   string  `json:"account"`
	State     string  `json:"state"` // ok, signed_out, missing
	ReadEpoch int64   `json:"read_epoch"`
	Email     string  `json:"email,omitempty"`
	Plan      string  `json:"plan,omitempty"`
	Limits    []Limit `json:"limits,omitempty"`
}

// A failedRead records the latest live read that failed; the previous reading stays.
type failedRead struct {
	FailedEpoch int64  `json:"failed_epoch"`
	Email       string `json:"email,omitempty"`
	Plan        string `json:"plan,omitempty"`
}

var limitLine = regexp.MustCompile(`^([A-Z][^:]*): ([0-9]+)% used(?: · resets (.*))?$`)

// parseUsage picks the limit lines out of /usage text and ignores everything else.
func parseUsage(text string) []Limit {
	limits := []Limit{}
	for _, line := range strings.Split(text, "\n") {
		m := limitLine.FindStringSubmatch(strings.TrimRight(line, "\r "))
		if m == nil {
			continue
		}
		used := 0
		for _, d := range m[2] {
			used = used*10 + int(d-'0')
		}
		limits = append(limits, Limit{Label: m[1], Used: used, Resets: m[3]})
	}
	return limits
}

type authStatus struct {
	LoggedIn         bool   `json:"loggedIn"`
	Email            string `json:"email"`
	SubscriptionType string `json:"subscriptionType"`
}

// readAccount reads one account live and stores the result in the cache.
func (c *config) readAccount(ctx context.Context, id string) error {
	dir := c.dir(id)
	r := reading{Account: id, ReadEpoch: c.now().Unix()}

	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		r.State = "missing"
		return c.store(r)
	}
	bin, err := c.claude()
	if err != nil {
		return err
	}

	// `claude auth status` exits non-zero when signed out but still prints JSON.
	actx, cancel := context.WithTimeout(ctx, c.authWait)
	defer cancel()
	cmd := exec.CommandContext(actx, bin, "auth", "status", "--json")
	cmd.Env = envWith(dir)
	out, _ := cmd.Output()
	var auth authStatus
	if json.Unmarshal(out, &auth) != nil || !auth.LoggedIn {
		r.State = "signed_out"
		return c.store(r)
	}

	text, err := c.runUsage(ctx, bin, dir)
	if err != nil {
		return c.storeFailed(id, failedRead{FailedEpoch: c.now().Unix(), Email: auth.Email, Plan: auth.SubscriptionType})
	}
	r.State = "ok"
	r.Email = auth.Email
	r.Plan = auth.SubscriptionType
	r.Limits = parseUsage(text)
	return c.store(r)
}

// runUsage runs `claude -p /usage` under dir. It spends no tokens, and as a side
// effect Claude Code refreshes the usage it keeps in the account's .claude.json.
// It runs from an empty folder of its own, loading no user or project settings
// (so no hooks or plugins), no MCP servers, and saving no transcript.
func (c *config) runUsage(ctx context.Context, bin, dir string) (string, error) {
	work := filepath.Join(c.cacheDir, "probe")
	if err := os.MkdirAll(work, 0o700); err != nil {
		return "", err
	}
	uctx, cancel := context.WithTimeout(ctx, c.usageWait)
	defer cancel()
	cmd := exec.CommandContext(uctx, bin, "-p", "/usage", "--output-format", "json",
		"--no-session-persistence", "--setting-sources", "project",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`)
	cmd.Dir = work
	cmd.Env = envWith(dir)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	var res struct {
		Result  *string `json:"result"`
		IsError bool    `json:"is_error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &res); err != nil {
		return "", err
	}
	if res.Result == nil || res.IsError {
		return "", errors.New("/usage gave no result")
	}
	return *res.Result, nil
}

// readAll reads every account live, at the same time.
func (c *config) readAll(ctx context.Context) error {
	var wg sync.WaitGroup
	errs := make([]error, len(c.accounts))
	for i, id := range c.accounts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = c.readAccount(ctx, id)
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (c *config) cachePath(id, suffix string) string {
	return filepath.Join(c.cacheDir, id+suffix+".json")
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *config) store(r reading) error {
	if err := writeJSON(c.cachePath(r.Account, ""), r); err != nil {
		return err
	}
	err := os.Remove(c.cachePath(r.Account, ".failed"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (c *config) storeFailed(id string, f failedRead) error {
	return writeJSON(c.cachePath(id, ".failed"), f)
}

// accountFile is the part of an account's .claude.json claude-router reads. It holds
// no credentials (those are in the system keychain).
type accountFile struct {
	OAuthAccount *struct {
		EmailAddress string `json:"emailAddress"`
	} `json:"oauthAccount"`
	CachedUsage *struct {
		FetchedAtMs int64 `json:"fetchedAtMs"`
		Utilization struct {
			FiveHour     *window `json:"five_hour"`
			SevenDay     *window `json:"seven_day"`
			SevenDayOpus *window `json:"seven_day_opus"`
			SevenDaySonn *window `json:"seven_day_sonnet"`
		} `json:"utilization"`
	} `json:"cachedUsageUtilization"`
}

type window struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

// claudeCodeReading turns the usage Claude Code keeps in an account's .claude.json
// into a reading. ok is false when the file has no sign-in or no usage.
func (c *config) claudeCodeReading(id string) (r reading, signedIn bool, ok bool) {
	data, err := os.ReadFile(filepath.Join(c.dir(id), ".claude.json"))
	if err != nil {
		return r, false, false
	}
	var f accountFile
	if json.Unmarshal(data, &f) != nil {
		return r, false, false
	}
	signedIn = f.OAuthAccount != nil && f.OAuthAccount.EmailAddress != ""
	if !signedIn || f.CachedUsage == nil || f.CachedUsage.FetchedAtMs == 0 {
		return r, signedIn, false
	}
	r = reading{Account: id, State: "ok", ReadEpoch: f.CachedUsage.FetchedAtMs / 1000, Email: f.OAuthAccount.EmailAddress, Limits: []Limit{}}
	u := f.CachedUsage.Utilization
	for _, w := range []struct {
		label string
		w     *window
	}{
		{"Current session", u.FiveHour},
		{"Current week (all models)", u.SevenDay},
		{"Current week (Opus only)", u.SevenDayOpus},
		{"Current week (Sonnet only)", u.SevenDaySonn},
	} {
		if w.w == nil || w.w.Utilization == nil {
			continue
		}
		l := Limit{Label: w.label, Used: int(math.Floor(*w.w.Utilization))}
		if t, err := time.Parse(time.RFC3339Nano, w.w.ResetsAt); err == nil {
			l.ResetsAt = t.Unix()
			l.Resets = formatReset(t, c.now())
		}
		r.Limits = append(r.Limits, l)
	}
	return r, true, true
}

// formatReset writes a reset time the way /usage does: "3pm" today, "Dec 1 at 3am"
// on another day, in local time.
func formatReset(t, now time.Time) string {
	t = t.In(now.Location()).Round(time.Minute)
	clock := t.Format("3:04pm")
	clock = strings.Replace(clock, ":00", "", 1)
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return clock
	}
	return fmt.Sprintf("%s at %s", t.Format("Jan 2"), clock)
}

// A view is one account as shown by status and used by suggest and launch.
type view struct {
	Account    string      `json:"account"`
	Folder     string      `json:"folder"`
	State      string      `json:"state"` // ok, signed_out, missing, unread
	Email      string      `json:"email,omitempty"`
	Plan       string      `json:"plan,omitempty"`
	Limits     []Limit     `json:"limits,omitempty"`
	ReadEpoch  int64       `json:"read_epoch,omitempty"`
	AgeSeconds *int64      `json:"age_seconds"`
	Fresh      bool        `json:"fresh"`
	Room       *int        `json:"room"` // percent left; null unless fresh and known
	LatestRead *failedRead `json:"latest_read_failed,omitempty"`
	Next       bool        `json:"next"` // the account the next launched session starts on

	age      time.Duration
	signedIn bool // as far as the account's own files say (no claude call)
	anyRoom  *int // room from the newest reading, however old
}

func (c *config) views() []view {
	now := c.now()
	out := make([]view, 0, len(c.accounts))
	for _, id := range c.accounts {
		v := view{Account: id, Folder: c.dir(id), State: "unread"}
		var r reading
		have := false
		if data, err := os.ReadFile(c.cachePath(id, "")); err == nil && json.Unmarshal(data, &r) == nil {
			have = true
		}
		// Claude Code's own record is used when it is at least as new: it is
		// structured, and it is refreshed by every live read and by normal use.
		if cc, signedIn, ok := c.claudeCodeReading(id); ok && (!have || cc.ReadEpoch >= r.ReadEpoch) {
			if have && r.Email == cc.Email {
				cc.Plan = r.Plan
			}
			r, have = cc, true
			v.signedIn = signedIn
		} else {
			v.signedIn = signedIn
		}
		if have {
			v.State, v.Email, v.Plan, v.Limits, v.ReadEpoch = r.State, r.Email, r.Plan, r.Limits, r.ReadEpoch
			v.age = now.Sub(time.Unix(r.ReadEpoch, 0))
			age := int64(v.age / time.Second)
			v.AgeSeconds = &age
			v.Fresh = v.age <= freshFor
			if r.State == "ok" {
				v.signedIn = true
			}
			if r.State == "signed_out" || r.State == "missing" {
				v.signedIn = false
			}
		}
		var f failedRead
		if data, err := os.ReadFile(c.cachePath(id, ".failed")); err == nil && json.Unmarshal(data, &f) == nil {
			v.LatestRead = &f
		}
		if v.State == "ok" {
			v.anyRoom = room(v.Limits, now)
			if v.Fresh {
				v.Room = v.anyRoom
			}
		}
		out = append(out, v)
	}
	return out
}

// room is 100% minus the higher of the session and all-models weekly figures (or of
// every limit, if neither is reported). A limit whose reset time has passed counts
// as 0. Nil when no limits are reported.
func room(limits []Limit, now time.Time) *int {
	var main []Limit
	for _, l := range limits {
		if l.Label == "Current session" || l.Label == "Current week (all models)" {
			main = append(main, l)
		}
	}
	if len(main) == 0 {
		main = limits
	}
	if len(main) == 0 {
		return nil
	}
	most := 0
	for _, l := range main {
		used := l.Used
		if l.ResetsAt != 0 && now.Unix() >= l.ResetsAt {
			used = 0
		}
		most = max(most, used)
	}
	left := max(100-most, 0)
	return &left
}
