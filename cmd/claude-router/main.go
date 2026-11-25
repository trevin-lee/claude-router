// Command claude-router keeps several Claude accounts signed in at once,
// switches between them, and reads each one's usage.
//
// Each account is a Claude Code config folder (~/.claude-<n>) picked with
// CLAUDE_CONFIG_DIR. Usage is read with Claude Code's own /usage command in
// print mode, which spends no tokens. claude-router never reads, copies or
// prints credentials.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"sort"
	"strings"
	"time"
)

var version = "" // set with -ldflags "-X main.version=..."

func buildVersion() string {
	if version != "" {
		return strings.TrimPrefix(version, "v")
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return "dev"
}

// errUsage marks a wrong command line; main exits 2 for it.
var errUsage = errors.New("usage")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	cfg, err := loadConfig()
	if err == nil {
		err = run(ctx, cfg, os.Args[1:], os.Stdout, os.Stderr)
	}
	switch {
	case err == nil:
	case errors.Is(err, errUsage):
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "claude-router:", err)
		os.Exit(1)
	}
}

func helpText(c *config) string {
	return fmt.Sprintf(`claude-router %s: several Claude accounts signed in at once.

Usage:
  claude-router login <n>            Sign account <n> in (once per account)
  claude-router use <n> [args...]    Start Claude Code as account <n> ("best": most room left)
  claude-router env <n>              Print the export line, for: eval "$(claude-router env <n>)"
  claude-router status [--cached] [--json]
                                     Each account's sign-in and usage, read live
  claude-router suggest [--cached]   Print the account number with the most room left
  claude-router list                 Accounts and their config folders
  claude-router completion bash|zsh|fish

<n> is one of: %s (also accepted as claude<n>). --cached skips the live read
(about 15 s) and uses readings under %d minutes old.

Settings (environment):
  CLAUDE_ROUTER_ACCOUNTS    account numbers (default "0 1 2")
  CLAUDE_ROUTER_DIR_PREFIX  folder prefix (default ~/.claude-)
  CLAUDE_ROUTER_CACHE       where readings are kept (default %s)
  CLAUDE_BIN                claude binary (default: claude on PATH)
`, buildVersion(), strings.Join(c.accounts, " "), int(freshFor/time.Minute), c.cacheDir)
}

func run(ctx context.Context, c *config, args []string, stdout, stderr io.Writer) error {
	cmd, rest := "help", []string(nil)
	if len(args) > 0 {
		cmd, rest = args[0], args[1:]
	}
	switch cmd {
	case "login":
		return c.cmdLogin(rest, stderr)
	case "use":
		return c.cmdUse(ctx, rest, stderr)
	case "env":
		return c.cmdEnv(rest, stdout)
	case "status":
		return c.cmdStatus(ctx, rest, stdout, stderr)
	case "suggest":
		id, err := c.cmdSuggest(ctx, rest, stderr)
		if err == nil {
			fmt.Fprintln(stdout, id)
		}
		return err
	case "list":
		return c.cmdList(stdout)
	case "completion":
		return cmdCompletion(rest, stdout)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, helpText(c))
		return nil
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, buildVersion())
		return nil
	default:
		fmt.Fprintf(stderr, "claude-router: unknown command %q\n\n%s", cmd, helpText(c))
		return errUsage
	}
}

func oneAccount(c *config, cmd string, args []string) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("%s takes one account number", cmd)
	}
	if len(args) == 0 {
		return c.accountID("")
	}
	return c.accountID(args[0])
}

func (c *config) cmdLogin(args []string, stderr io.Writer) error {
	id, err := oneAccount(c, "login", args)
	if err != nil {
		return err
	}
	dir := c.dir(id)
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "Created %s\n", dir)
	}
	bin, err := c.claude()
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "Signing in claude%s (folder %s). Use the account you want as claude%s.\n", id, dir, id)
	return execClaude(bin, []string{"auth", "login"}, dir)
}

func (c *config) cmdUse(ctx context.Context, args []string, stderr io.Writer) error {
	if len(args) == 0 {
		_, err := c.accountID("")
		return err
	}
	var id string
	var err error
	if args[0] == "best" {
		id, err = c.cmdSuggest(ctx, nil, stderr)
	} else {
		id, err = c.accountID(args[0])
	}
	if err != nil {
		return err
	}
	dir := c.dir(id)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return fmt.Errorf("claude%s has no folder yet: run  claude-router login %s", id, id)
	}
	bin, err := c.claude()
	if err != nil {
		return err
	}
	return execClaude(bin, args[1:], dir)
}

func (c *config) cmdEnv(args []string, stdout io.Writer) error {
	id, err := oneAccount(c, "env", args)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "export CLAUDE_CONFIG_DIR=%s\n", shellQuote(c.dir(id)))
	return nil
}

func (c *config) cmdList(stdout io.Writer) error {
	for _, id := range c.accounts {
		note := ""
		if st, err := os.Stat(c.dir(id)); err != nil || !st.IsDir() {
			note = "  (not created)"
		}
		fmt.Fprintf(stdout, "claude%s  %s%s\n", id, c.dir(id), note)
	}
	return nil
}

func parseFlags(cmd string, args []string, allowed ...string) (map[string]bool, error) {
	set := map[string]bool{}
	for _, a := range args {
		ok := false
		for _, f := range allowed {
			if a == f {
				set[f], ok = true, true
			}
		}
		if !ok {
			return nil, fmt.Errorf("%s: unknown option %s", cmd, a)
		}
	}
	return set, nil
}

// refresh reads every account live unless --cached was given.
func (c *config) refresh(ctx context.Context, cached bool, stderr io.Writer) error {
	if cached {
		return nil
	}
	if isTerminal(stderr) {
		fmt.Fprintln(stderr, "Reading usage live (about 15 s)...")
	}
	return c.readAll(ctx)
}

func (c *config) cmdStatus(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags, err := parseFlags("status", args, "--cached", "--json")
	if err != nil {
		return err
	}
	if err := c.refresh(ctx, flags["--cached"], stderr); err != nil {
		return err
	}
	views := c.views()
	if flags["--json"] {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(views)
	}
	for _, v := range views {
		fmt.Fprintln(stdout, statusLine(v))
	}
	return nil
}

func (c *config) cmdSuggest(ctx context.Context, args []string, stderr io.Writer) (string, error) {
	flags, err := parseFlags("suggest", args, "--cached")
	if err != nil {
		return "", err
	}
	if err := c.refresh(ctx, flags["--cached"], stderr); err != nil {
		return "", err
	}
	var best []view
	for _, v := range c.views() {
		if v.Room != nil {
			best = append(best, v)
		}
	}
	if len(best) == 0 {
		return "", fmt.Errorf("no account has a current usage reading (signed in, read within %d minutes)", int(freshFor/time.Minute))
	}
	// Most room first; ties go to the first account in the list.
	sort.SliceStable(best, func(i, j int) bool { return *best[i].Room > *best[j].Room })
	pick := best[0]
	if isTerminal(stderr) {
		fmt.Fprintf(stderr, "claude%s has %d%% left (%s)\n", pick.Account, *pick.Room, pick.Email)
	}
	return pick.Account, nil
}

func statusLine(v view) string {
	head := "claude" + v.Account + "  "
	who := fmt.Sprintf("%s (%s)", v.Email, v.Plan)
	var body string
	switch {
	case v.State == "missing":
		body = "no folder yet: run  claude-router login " + v.Account
	case v.State == "signed_out":
		body = "not signed in: run  claude-router login " + v.Account
	case v.State == "unread" && v.LatestRead != nil:
		return head + fmt.Sprintf("%s (%s): usage could not be read", v.LatestRead.Email, v.LatestRead.Plan)
	case v.State == "unread":
		return head + "not read yet: run  claude-router status"
	case v.Fresh:
		body = fmt.Sprintf("%s  %s  [read %s]", who, describeLimits(v.Limits), ago(v.ageDuration))
	default:
		body = fmt.Sprintf("%s  STALE, not current: last read %s showed %s", who, ago(v.ageDuration), describeLimits(v.Limits))
	}
	if v.LatestRead != nil {
		body += "  (latest read failed)"
	}
	return head + body
}

func describeLimits(limits []Limit) string {
	if len(limits) == 0 {
		return "no plan limits reported"
	}
	parts := make([]string, 0, len(limits))
	for _, l := range limits {
		s := fmt.Sprintf("%s %d%%", shortLabel(l.Label), l.Used)
		if l.Resets != "" {
			s += " (resets " + stripZone(l.Resets) + ")"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " · ")
}

// shortLabel: "Current session" -> "session", "Current week (all models)" -> "week",
// "Current week (Sonnet only)" -> "week Sonnet".
func shortLabel(label string) string {
	s := strings.TrimPrefix(label, "Current ")
	s = strings.TrimSuffix(s, " (all models)")
	if strings.HasPrefix(s, "week (") && strings.HasSuffix(s, " only)") {
		s = "week " + strings.TrimSuffix(strings.TrimPrefix(s, "week ("), " only)")
	}
	return s
}

// stripZone drops a trailing " (Europe/London)".
func stripZone(s string) string {
	if strings.HasSuffix(s, ")") {
		if i := strings.LastIndex(s, " ("); i > 0 {
			return s[:i]
		}
	}
	return s
}

func ago(d time.Duration) string {
	s := int64(d / time.Second)
	switch {
	case s < 90:
		return "just now"
	case s < 5400:
		return fmt.Sprintf("%dm ago", (s+30)/60)
	case s < 172800:
		return fmt.Sprintf("%dh ago", (s+1800)/3600)
	default:
		return fmt.Sprintf("%dd ago", s/86400)
	}
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+:@%,", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
