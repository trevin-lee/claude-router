package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// Readings older than this make launch start a live read in the background, at most
// once per refreshGap.
const (
	refreshAfter = 30 * time.Minute
	refreshGap   = 10 * time.Minute
)

// pick chooses the account a new session should start on, without any live read:
// signed-in accounts with room left, most room first; then accounts with no usage
// known yet; then full ones. Ties go to the first in the list. ok is false when no
// account is signed in.
func (c *config) pick(views []view) (v view, ok bool) {
	rank := func(v view) int {
		switch {
		case v.anyRoom == nil:
			return 1
		case *v.anyRoom > 0:
			return 0
		default:
			return 2
		}
	}
	var cands []view
	for _, v := range views {
		if v.signedIn {
			if st, err := os.Stat(v.Folder); err == nil && st.IsDir() {
				cands = append(cands, v)
			}
		}
	}
	if len(cands) == 0 {
		return view{}, false
	}
	sort.SliceStable(cands, func(i, j int) bool {
		ri, rj := rank(cands[i]), rank(cands[j])
		if ri != rj {
			return ri < rj
		}
		if ri == 0 {
			return *cands[i].anyRoom > *cands[j].anyRoom
		}
		return false
	})
	return cands[0], true
}

// prepare links an account folder to the shared home folder and syncs the shared
// settings, so a session started there has the same setup as every other account.
func (c *config) prepare(id string) (notes []string, err error) {
	notes, err = c.share(id)
	if err != nil {
		return notes, err
	}
	removeJunk(filepath.Join(c.home, "projects"))
	changed, err := c.syncConfig()
	for _, p := range changed {
		notes = append(notes, "synced shared settings into "+p)
	}
	return notes, err
}

// cmdLaunch is what a claude wrapper calls: claude-router launch <claude> [args...].
// It picks the account with the most room, prepares it and starts claude there. It
// prints nothing (the caller may be an editor reading claude's output) and never
// stops claude from starting: if anything goes wrong it starts claude as it would
// have without claude-router, and notes why in the log.
func (c *config) cmdLaunch(args []string) error {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return errors.New("launch needs the claude binary to start, then its arguments")
	}
	bin, rest := args[0], args[1:]
	if p, err := lookPath(bin); err == nil {
		bin = p
	}
	c.claudeBin = bin

	dir := ""
	func() {
		defer func() {
			if r := recover(); r != nil {
				c.logf("launch: internal error, starting claude unrouted: %v", r)
				dir = ""
			}
		}()
		views := c.views()
		v, ok := c.pick(views)
		if !ok {
			c.logf("launch: no account signed in, starting claude unrouted")
			return
		}
		notes, err := c.prepare(v.Account)
		for _, n := range notes {
			c.logf("launch: %s", n)
		}
		if err != nil {
			c.logf("launch: preparing claude%s: %v (starting it anyway)", v.Account, err)
		}
		left := "usage unknown"
		if v.anyRoom != nil {
			left = fmt.Sprintf("%d%% left as of %s", *v.anyRoom, ago(v.age))
		}
		c.logf("launch: claude%s (%s, %s)", v.Account, v.Email, left)
		dir = v.Folder
		c.maybeRefresh(views, bin)
	}()
	return execClaude(bin, rest, dir)
}

// maybeRefresh starts a live read of every account in the background when the
// readings launch relied on are getting old.
func (c *config) maybeRefresh(views []view, bin string) {
	if !c.background {
		return
	}
	stale := false
	for _, v := range views {
		if v.signedIn && (v.AgeSeconds == nil || v.age > refreshAfter) {
			stale = true
		}
	}
	if !stale {
		return
	}
	marker := filepath.Join(c.cacheDir, "refresh.started")
	if st, err := os.Stat(marker); err == nil && c.now().Sub(st.ModTime()) < refreshGap {
		return
	}
	if err := os.MkdirAll(c.cacheDir, 0o700); err != nil {
		return
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "refresh")
	cmd.Env = append(os.Environ(), "CLAUDE_BIN="+bin)
	if err := startDetached(cmd); err != nil {
		c.logf("launch: background refresh: %v", err)
	}
}

// cmdRefresh reads every account live, quietly (used in the background by launch).
func (c *config) cmdRefresh(ctx context.Context) error {
	err := c.readAll(ctx)
	if err != nil {
		c.logf("refresh: %v", err)
	}
	return err
}

func (c *config) cmdSetup(stdout io.Writer) error {
	any := false
	var errs []error
	for _, id := range c.accounts {
		if st, err := os.Stat(c.dir(id)); err != nil || !st.IsDir() {
			fmt.Fprintf(stdout, "claude%s: no folder yet (run  claude-router login %s)\n", id, id)
			continue
		}
		any = true
		notes, err := c.share(id)
		for _, n := range notes {
			fmt.Fprintf(stdout, "claude%s: %s\n", id, n)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("claude%s: %w", id, err))
		} else if len(notes) == 0 {
			fmt.Fprintf(stdout, "claude%s: already shares %s\n", id, c.home)
		}
	}
	if !any {
		return errors.Join(errs...)
	}
	removeJunk(filepath.Join(c.home, "projects"))
	changed, err := c.syncConfig()
	if err != nil {
		errs = append(errs, err)
	}
	if len(changed) == 0 {
		fmt.Fprintln(stdout, "Shared settings (MCP servers, trusted folders, preferences) already match.")
	}
	for _, p := range changed {
		fmt.Fprintf(stdout, "Synced shared settings into %s\n", p)
	}
	return errors.Join(errs...)
}

// logf appends a line to the log in the cache folder, which launch uses instead of
// printing. The log is kept under about 1 MB.
func (c *config) logf(format string, args ...any) {
	if err := os.MkdirAll(c.cacheDir, 0o700); err != nil {
		return
	}
	path := filepath.Join(c.cacheDir, "router.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 1<<20 {
		os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", c.now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}
