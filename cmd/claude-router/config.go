package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Readings older than this are never shown as current or used to pick an account.
const freshFor = 2 * time.Hour

type config struct {
	accounts  []string // account numbers, in order
	dirPrefix string   // account n lives in dirPrefix + n
	cacheDir  string
	claudeBin string // empty: look up "claude" on PATH
	authWait  time.Duration
	usageWait time.Duration
	now       func() time.Time
}

func loadConfig() (*config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("finding home folder: %w", err)
	}
	c := &config{
		accounts:  []string{"0", "1", "2"},
		dirPrefix: filepath.Join(home, ".claude-"),
		claudeBin: os.Getenv("CLAUDE_BIN"),
		authWait:  30 * time.Second,
		usageWait: 90 * time.Second,
		now:       time.Now,
	}
	if v := os.Getenv("CLAUDE_ROUTER_ACCOUNTS"); strings.TrimSpace(v) != "" {
		c.accounts = strings.FieldsFunc(v, func(r rune) bool { return r == ' ' || r == ',' })
	}
	if v := os.Getenv("CLAUDE_ROUTER_DIR_PREFIX"); v != "" {
		c.dirPrefix = v
	}
	if v := os.Getenv("CLAUDE_ROUTER_CACHE"); v != "" {
		c.cacheDir = v
	} else {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("finding cache folder: %w", err)
		}
		c.cacheDir = filepath.Join(base, "claude-router")
	}
	return c, nil
}

// accountID accepts "2" or "claude2" and returns "2" if it is a configured account.
func (c *config) accountID(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("missing account number (one of: %s)", strings.Join(c.accounts, " "))
	}
	id := strings.TrimPrefix(raw, "claude")
	for _, a := range c.accounts {
		if a == id {
			return id, nil
		}
	}
	return "", fmt.Errorf("unknown account %q (one of: %s)", raw, strings.Join(c.accounts, " "))
}

func (c *config) dir(id string) string { return c.dirPrefix + id }

func (c *config) claude() (string, error) {
	name := c.claudeBin
	if name == "" {
		name = "claude"
	}
	path, err := lookPath(name)
	if err != nil {
		return "", fmt.Errorf("claude not found (install Claude Code, or set CLAUDE_BIN): %w", err)
	}
	return path, nil
}

// envWith returns the current environment with CLAUDE_CONFIG_DIR set to dir.
func envWith(dir string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
			out = append(out, kv)
		}
	}
	return append(out, "CLAUDE_CONFIG_DIR="+dir)
}
