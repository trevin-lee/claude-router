package main

// Sharing: every account folder links its settings, skills, plugins, history and
// memory to one home folder (~/.claude), and the parts of .claude.json that are not
// about the account (MCP servers, per-project trust, preferences) are kept the same
// in every account. Logins stay separate: they live in the system keychain, keyed by
// folder, and claude-router never touches them.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// sharedDirs and sharedFiles are linked from each account folder to the home folder.
// Everything else in an account folder (its .claude.json, caches, backups, session
// state, telemetry) stays its own.
var sharedDirs = []string{
	"projects", // conversation history and memory, so /resume works across accounts
	"skills", "agents", "commands", "hooks", "output-styles",
	"plugins", // installed plugins and marketplaces
	"plans", "todos", "file-history", "paste-cache",
	"ide", // IDE lock files, so a terminal session connects to VS Code
}

var sharedFiles = []string{"settings.json", "settings.local.json", "CLAUDE.md", "keybindings.json", "history.jsonl", "statusline.sh"}

// Preferences in .claude.json kept the same in every account.
var sharedPrefs = []string{
	"hasCompletedOnboarding", "lastOnboardingVersion", "lastReleaseNotesSeen",
	"theme", "editorMode", "preferredNotifChannel", "autoCompactEnabled", "autoUpdates",
	"verbose", "showSpinnerTree", "diffTool", "optionAsMetaKeyInstalled",
	"shiftEnterKeyBindingInstalled", "claudeInChromeDefaultEnabled",
	"hasCompletedClaudeInChromeOnboarding", "githubRepoPaths", "deepLinkTerminal",
}

// Per-project fields in .claude.json kept the same in every account.
var sharedProjectFields = []string{
	"allowedTools", "hasTrustDialogAccepted", "mcpServers",
	"enabledMcpjsonServers", "disabledMcpjsonServers", "enabledMcpServers", "disabledMcpServers",
	"hasClaudeMdExternalIncludesApproved", "hasClaudeMdExternalIncludesWarningShown",
}

// Leftovers of usage reads by claude-router 0.1 (and claude-accounts), which ran from
// a new temporary folder each time.
var junkProject = regexp.MustCompile(`-T-claude-(router|accounts)-[A-Za-z0-9]+$`)
var junkProjectPath = regexp.MustCompile(`/T/claude-(router|accounts)-[A-Za-z0-9]+$`)

// share links one account folder to the home folder. It never deletes data: an
// account's own copy is merged into the home folder where nothing clashes, and set
// aside as <name>.before-claude-router where something does. It returns one line
// per change made.
func (c *config) share(id string) ([]string, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("sharing between accounts needs symbolic links, which this doesn't set up on Windows")
	}
	dir := c.dir(id)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("claude%s has no folder yet: run  claude-router login %s", id, id)
	}
	if err := os.MkdirAll(c.home, 0o700); err != nil {
		return nil, err
	}
	var notes []string
	removeJunk(filepath.Join(dir, "projects"))
	for _, name := range sharedDirs {
		n, err := c.link(dir, name, true)
		if err != nil {
			return notes, fmt.Errorf("sharing %s: %w", name, err)
		}
		notes = append(notes, n...)
	}
	for _, name := range sharedFiles {
		n, err := c.link(dir, name, false)
		if err != nil {
			return notes, fmt.Errorf("sharing %s: %w", name, err)
		}
		notes = append(notes, n...)
	}
	return notes, nil
}

func (c *config) link(dir, name string, isDir bool) ([]string, error) {
	acct := filepath.Join(dir, name)
	home := filepath.Join(c.home, name)

	if target, err := os.Readlink(acct); err == nil {
		if target == home {
			return nil, nil
		}
		return []string{fmt.Sprintf("left %s alone: it already links to %s", acct, target)}, nil
	}
	_, acctErr := os.Lstat(acct)
	_, homeErr := os.Stat(home)
	acctExists, homeExists := acctErr == nil, homeErr == nil

	var notes []string
	switch {
	case !acctExists && !homeExists:
		if !isDir {
			return nil, nil // nothing to share yet
		}
		if err := os.MkdirAll(home, 0o700); err != nil {
			return nil, err
		}
	case acctExists && !homeExists:
		if err := os.Rename(acct, home); err != nil {
			return nil, err
		}
		notes = append(notes, fmt.Sprintf("moved %s to %s", acct, home))
	case acctExists && homeExists:
		kept, err := mergeInto(acct, home)
		if err != nil {
			return nil, err
		}
		if kept {
			aside := acct + ".before-claude-router"
			if err := os.Rename(acct, aside); err != nil {
				return nil, err
			}
			notes = append(notes, fmt.Sprintf("set %s aside as %s (it differs from %s)", acct, aside, home))
		} else if err := os.RemoveAll(acct); err != nil {
			return nil, err
		}
	}
	if err := os.Symlink(home, acct); err != nil {
		return notes, err
	}
	return append(notes, fmt.Sprintf("linked %s", acct)), nil
}

// mergeInto moves whatever in src does not clash into dst. It reports whether
// anything was left in src (a clash), in which case src must not be deleted.
func mergeInto(src, dst string) (kept bool, err error) {
	sst, err := os.Lstat(src)
	if err != nil {
		return false, err
	}
	dstSt, err := os.Lstat(dst)
	if err != nil {
		return false, err
	}
	if !sst.IsDir() || !dstSt.IsDir() {
		if !sst.IsDir() && !dstSt.IsDir() && sameFile(src, dst) {
			return false, nil
		}
		return true, nil
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		s, d := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if _, err := os.Lstat(d); errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(s, d); err != nil {
				return false, err
			}
			continue
		}
		k, err := mergeInto(s, d)
		if err != nil {
			return false, err
		}
		if k {
			kept = true
		} else if err := os.RemoveAll(s); err != nil {
			return false, err
		}
	}
	return kept, nil
}

func sameFile(a, b string) bool {
	x, err1 := os.ReadFile(a)
	y, err2 := os.ReadFile(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

// removeJunk deletes leftover usage-read folders in a projects folder, only when
// they hold no files.
func removeJunk(projects string) {
	entries, err := os.ReadDir(projects)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !junkProject.MatchString(e.Name()) {
			continue
		}
		p := filepath.Join(projects, e.Name())
		hasFile := false
		filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				hasFile = true
				return fs.SkipAll
			}
			return nil
		})
		if !hasFile {
			os.RemoveAll(p)
		}
	}
}

// --- .claude.json ---

// flat is the shared part of one .claude.json: key -> canonical JSON value.
// Keys: "mcp\t<server>", "pref\t<name>", "proj\t<path>\t<field>".
type flat map[string]string

type configFile struct {
	path string
	raw  map[string]json.RawMessage
	mode fs.FileMode
}

func readConfigFile(path string) (*configFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	f := &configFile{path: path, mode: st.Mode().Perm()}
	if err := json.Unmarshal(data, &f.raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

func canonical(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (f *configFile) shared() flat {
	out := flat{}
	var servers map[string]json.RawMessage
	if json.Unmarshal(f.raw["mcpServers"], &servers) == nil {
		for name, v := range servers {
			out["mcp\t"+name] = canonical(v)
		}
	}
	for _, p := range sharedPrefs {
		if v, ok := f.raw[p]; ok {
			out["pref\t"+p] = canonical(v)
		}
	}
	var projects map[string]map[string]json.RawMessage
	if json.Unmarshal(f.raw["projects"], &projects) == nil {
		for path, fields := range projects {
			if junkProjectPath.MatchString(path) {
				continue
			}
			for _, name := range sharedProjectFields {
				if v, ok := fields[name]; ok {
					out["proj\t"+path+"\t"+name] = canonical(v)
				}
			}
		}
	}
	return out
}

// apply makes the shared part of f equal to want, leaving everything else as it is.
// It also drops leftover usage-read projects. It reports whether f changed.
func (f *configFile) apply(want flat) (bool, error) {
	before := f.shared()
	junk := false
	var projects map[string]map[string]json.RawMessage
	if r, ok := f.raw["projects"]; ok {
		if err := json.Unmarshal(r, &projects); err != nil {
			return false, fmt.Errorf("%s: projects: %w", f.path, err)
		}
	}
	for path := range projects {
		if junkProjectPath.MatchString(path) {
			delete(projects, path)
			junk = true
		}
	}
	if equalFlat(before, want) && !junk {
		return false, nil
	}

	servers := map[string]json.RawMessage{}
	if r, ok := f.raw["mcpServers"]; ok {
		if err := json.Unmarshal(r, &servers); err != nil {
			return false, fmt.Errorf("%s: mcpServers: %w", f.path, err)
		}
	}
	if projects == nil {
		projects = map[string]map[string]json.RawMessage{}
	}
	// Remove shared values that are no longer wanted, then set the wanted ones.
	for k := range before {
		if _, ok := want[k]; ok {
			continue
		}
		parts := strings.Split(k, "\t")
		switch parts[0] {
		case "mcp":
			delete(servers, parts[1])
		case "pref":
			delete(f.raw, parts[1])
		case "proj":
			if p, ok := projects[parts[1]]; ok {
				delete(p, parts[2])
			}
		}
	}
	for k, v := range want {
		parts := strings.Split(k, "\t")
		switch parts[0] {
		case "mcp":
			servers[parts[1]] = json.RawMessage(v)
		case "pref":
			f.raw[parts[1]] = json.RawMessage(v)
		case "proj":
			p := projects[parts[1]]
			if p == nil {
				p = map[string]json.RawMessage{}
				projects[parts[1]] = p
			}
			p[parts[2]] = json.RawMessage(v)
		}
	}
	var err error
	if f.raw["mcpServers"], err = json.Marshal(servers); err != nil {
		return false, err
	}
	if f.raw["projects"], err = json.Marshal(projects); err != nil {
		return false, err
	}
	return true, nil
}

func (f *configFile) write() error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f.raw); err != nil {
		return err
	}
	tmp := f.path + ".claude-router.tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), f.mode); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

func equalFlat(a, b flat) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// syncConfig keeps the shared part of .claude.json the same in the home folder and
// every signed-in account. It is a three-way merge against the last synced state, so
// a change made under any account (an MCP server added or removed, a folder trusted)
// reaches all of them. When two places change the same thing, the later one in the
// list wins: home, then the accounts in order. It returns the files it changed.
func (c *config) syncConfig() ([]string, error) {
	var files []*configFile
	for _, p := range append([]string{c.homeJSON}, c.accountJSONs()...) {
		f, err := readConfigFile(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, nil
	}

	// The state of the last sync: the agreed values, and which files took part.
	// A file that did not take part (an account signed in since) can add values
	// but not remove them; otherwise its empty config would read as "delete all".
	statePath := filepath.Join(c.cacheDir, "shared-config.json")
	var state struct {
		Files  []string `json:"files"`
		Values flat     `json:"values"`
	}
	if data, err := os.ReadFile(statePath); err == nil {
		if json.Unmarshal(data, &state) != nil {
			state.Values = nil
		}
	}
	known := map[string]bool{}
	for _, p := range state.Files {
		known[p] = true
	}
	base := state.Values
	if base == nil {
		base = flat{}
	}

	merged := flat{}
	for k, v := range base {
		merged[k] = v
	}
	// Files that took part last time: their changes since then, in order.
	for _, f := range files {
		if !known[f.path] {
			continue
		}
		cur := f.shared()
		for k := range keys(base, cur) {
			bv, inBase := base[k]
			cv, inCur := cur[k]
			switch {
			case inCur && (!inBase || cv != bv):
				merged[k] = cv
			case !inCur && inBase:
				delete(merged, k)
			}
		}
	}
	// New files: only what nobody has a value for yet (the home folder first).
	for _, f := range files {
		if known[f.path] {
			continue
		}
		for k, v := range f.shared() {
			if _, ok := merged[k]; !ok {
				merged[k] = v
			}
		}
	}

	var changed []string
	for _, f := range files {
		ch, err := f.apply(merged)
		if err != nil {
			return changed, err
		}
		if ch {
			if err := f.write(); err != nil {
				return changed, err
			}
			changed = append(changed, f.path)
		}
	}
	state.Files = state.Files[:0]
	for _, f := range files {
		state.Files = append(state.Files, f.path)
	}
	state.Values = merged
	return changed, writeJSON(statePath, state)
}

func (c *config) accountJSONs() []string {
	var out []string
	for _, id := range c.accounts {
		out = append(out, filepath.Join(c.dir(id), ".claude.json"))
	}
	return out
}

func keys(maps ...flat) map[string]bool {
	out := map[string]bool{}
	for _, m := range maps {
		for k := range m {
			out[k] = true
		}
	}
	return out
}
