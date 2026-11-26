package main

// End-to-end tests: build the binary and run it against a fake claude. No real
// account is touched.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Fake claude. Its behaviour depends on the account folder it is pointed at:
//
//	signed_in  -> auth status says logged in
//	usage.txt  -> /usage prints that text
//	fail       -> /usage fails
const fakeClaude = `#!/bin/sh
d=${CLAUDE_CONFIG_DIR:-$HOME/.claude}
mkdir -p "$d"
echo "$PWD|$*" >> "$d/calls.log"
case "$1" in
  auth)
    if [ "$2" = status ]; then
      if [ -f "$d/signed_in" ]; then
        printf '{"loggedIn":true,"email":"%s@example.com","subscriptionType":"max"}\n' "${d##*-}"
      else
        printf '{"loggedIn":false}\n'; exit 1
      fi
    else
      echo "login under $d"
    fi ;;
  -p)
    [ -f "$d/fail" ] && { echo boom >&2; exit 1; }
    printf '{"type":"result","is_error":false,"result":'
    # JSON-encode the file: escape backslashes and quotes, join lines with \n.
    sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' "$d/usage.txt" | awk 'BEGIN{printf "\""} {printf "%s\\n", $0} END{printf "\""}'
    printf '}\n' ;;
  *) echo "ran claude $* under $d cfg=${CLAUDE_CONFIG_DIR-unset} cc=${CLAUDECODE-unset}" ;;
esac
`

type harness struct {
	t        *testing.T
	bin      string
	home     string // holds the account folders
	userHome string // $HOME: the shared ~/.claude and ~/.claude.json live here
	cache    string
	env      []string
}

var builtBin string

func TestMain(m *testing.M) {
	if runtime.GOOS == "windows" {
		os.Exit(m.Run()) // end-to-end tests skip themselves
	}
	dir, err := os.MkdirTemp("", "claude-router-bin-")
	if err != nil {
		panic(err)
	}
	builtBin = filepath.Join(dir, "claude-router")
	if out, err := exec.Command("go", "build", "-o", builtBin, ".").CombinedOutput(); err != nil {
		panic("build failed: " + string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("end-to-end tests use a shell-script fake claude")
	}
	tmp := t.TempDir()
	fake := filepath.Join(tmp, "claude")
	if err := os.WriteFile(fake, []byte(fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, bin: builtBin, home: filepath.Join(tmp, "home"), userHome: filepath.Join(tmp, "user"), cache: filepath.Join(tmp, "cache")}
	must(t, os.MkdirAll(h.userHome, 0o700))
	// A fresh HOME, so nothing can reach the real ~/.claude or ~/.claude.json.
	h.env = append(os.Environ(),
		"HOME="+h.userHome,
		"TZ=UTC",
		"CLAUDE_ROUTER_BACKGROUND_REFRESH=0",
		"CLAUDE_BIN="+fake,
		"CLAUDE_ROUTER_ACCOUNTS=0 1 2 3",
		"CLAUDE_ROUTER_DIR_PREFIX="+filepath.Join(h.home, ".claude-"),
		"CLAUDE_ROUTER_CACHE="+h.cache,
	)
	// claude0: session 80%, week 40% -> 20% left. claude1: week 60% -> 40% left.
	// claude2: no folder. claude3: folder, signed out.
	h.account("0", true, sampleUsage)
	h.account("1", true, "You are currently using your subscription\n\nCurrent week (all models): 60% used · resets Nov 30 at 8:59am (Europe/London)\n")
	h.account("3", false, "")
	return h
}

func (h *harness) dir(id string) string { return filepath.Join(h.home, ".claude-"+id) }

func (h *harness) account(id string, signedIn bool, usage string) {
	h.t.Helper()
	d := h.dir(id)
	must(h.t, os.MkdirAll(d, 0o700))
	if signedIn {
		must(h.t, os.WriteFile(filepath.Join(d, "signed_in"), nil, 0o600))
	}
	must(h.t, os.WriteFile(filepath.Join(d, "usage.txt"), []byte(usage), 0o600))
}

// run returns stdout, stderr and the exit code.
func (h *harness) run(args ...string) (string, string, int) {
	h.t.Helper()
	cmd := exec.Command(h.bin, args...)
	cmd.Env = h.env
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		h.t.Fatal(err)
	}
	return out.String(), errb.String(), code
}

func contains(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s: missing %q in:\n%s", what, w, got)
		}
	}
}

func lacks(t *testing.T, what, got string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if strings.Contains(got, n) {
			t.Errorf("%s: should not contain %q in:\n%s", what, n, got)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestListAndEnv(t *testing.T) {
	h := newHarness(t)
	out, _, _ := h.run("list")
	contains(t, "list", out, "claude0  "+h.dir("0")+"\n", "claude2  "+h.dir("2")+"  (not created)")

	out, _, code := h.run("env", "claude1")
	if code != 0 || out != "export CLAUDE_CONFIG_DIR="+h.dir("1")+"\n" {
		t.Errorf("env claude1: code %d out %q", code, out)
	}
	out, errOut, code := h.run("env", "9")
	if code == 0 || out != "" {
		t.Errorf("env 9 should fail with no output, got code %d out %q", code, out)
	}
	contains(t, "env 9", errOut, `unknown account "9"`)
}

func TestUse(t *testing.T) {
	h := newHarness(t)
	out, _, code := h.run("use", "1", "--resume", "abc")
	if code != 0 {
		t.Fatalf("use exit %d", code)
	}
	contains(t, "use", out, "ran claude --resume abc under "+h.dir("1"))

	_, errOut, code := h.run("use", "2")
	if code == 0 {
		t.Error("use 2 should fail without a folder")
	}
	contains(t, "use 2", errOut, "claude2 has no folder yet")

	out, _, code = h.run("use", "best", "--model", "x")
	if code != 0 {
		t.Fatalf("use best exit %d", code)
	}
	contains(t, "use best", out, "ran claude --model x under "+h.dir("1"))
}

func TestStatusLive(t *testing.T) {
	h := newHarness(t)
	out, _, code := h.run("status")
	if code != 0 {
		t.Fatalf("status exit %d", code)
	}
	contains(t, "status", out,
		"claude0  0@example.com (max)  session 80% (resets 3pm) · week 40% (resets Nov 30 at 9am) · week Sonnet 5% (resets Nov 30 at 9am)  [read just now]",
		"claude1  1@example.com (max)  week 60% (resets Nov 30 at 8:59am)  [read just now]",
		"claude2  no folder yet: run  claude-router login 2",
		"claude3  not signed in: run  claude-router login 3",
	)
	lacks(t, "status", out, "97%")

	// /usage ran from an empty temporary folder with no MCP servers or transcript.
	calls, err := os.ReadFile(filepath.Join(h.dir("0"), "calls.log"))
	must(t, err)
	wd, _ := os.Getwd()
	lacks(t, "calls", string(calls), wd+"|-p")
	contains(t, "calls", string(calls), "/cache/probe|-p /usage --output-format json --no-session-persistence --setting-sources project --strict-mcp-config --mcp-config {\"mcpServers\":{}}")
}

func TestStatusJSONAndSuggest(t *testing.T) {
	h := newHarness(t)
	h.run("status")
	out, _, _ := h.run("status", "--cached", "--json")
	var views []view
	must(t, json.Unmarshal([]byte(out), &views))
	if len(views) != 4 || views[0].Room == nil || *views[0].Room != 20 || views[1].Room == nil || *views[1].Room != 40 {
		t.Fatalf("unexpected rooms: %s", out)
	}
	if views[2].State != "missing" || views[3].State != "signed_out" || views[2].Room != nil {
		t.Errorf("unexpected states: %s", out)
	}

	out, _, code := h.run("suggest", "--cached")
	if code != 0 || out != "1\n" {
		t.Errorf("suggest: code %d out %q, want 1", code, out)
	}
}

func (h *harness) age(id string, by time.Duration) {
	h.t.Helper()
	p := filepath.Join(h.cache, id+".json")
	data, err := os.ReadFile(p)
	must(h.t, err)
	var r reading
	must(h.t, json.Unmarshal(data, &r))
	r.ReadEpoch -= int64(by / time.Second)
	data, _ = json.Marshal(r)
	must(h.t, os.WriteFile(p, data, 0o600))
}

func TestStaleReadings(t *testing.T) {
	h := newHarness(t)
	h.run("status")
	h.age("1", 3*time.Hour)

	out, _, _ := h.run("status", "--cached")
	contains(t, "stale", out, "claude1  1@example.com (max)  STALE, not current: last read 3h ago showed week 60%")
	lacks(t, "stale", out, "claude1  1@example.com (max)  week")

	// The stale account has more room but must not be picked.
	got, _, _ := h.run("suggest", "--cached")
	if got != "0\n" {
		t.Errorf("suggest should skip the stale reading, got %q", got)
	}
}

func TestFailedReadKeepsLastReading(t *testing.T) {
	h := newHarness(t)
	h.run("status")
	h.age("1", 3*time.Hour)
	must(t, os.WriteFile(filepath.Join(h.dir("1"), "fail"), nil, 0o600))

	out, _, _ := h.run("status")
	contains(t, "failed", out, "STALE, not current: last read 3h ago", "(latest read failed)")

	must(t, os.Remove(filepath.Join(h.dir("1"), "fail")))
	out, _, _ = h.run("status")
	contains(t, "recovered", out, "claude1  1@example.com (max)  week 60%")
	lacks(t, "recovered", out, "(latest read failed)")
}

func TestFailedFirstRead(t *testing.T) {
	h := newHarness(t)
	must(t, os.WriteFile(filepath.Join(h.dir("0"), "fail"), nil, 0o600))
	out, _, _ := h.run("status")
	contains(t, "first read failed", out, "claude0  0@example.com (max): usage could not be read")
}

func TestSuggestWithNothingCurrent(t *testing.T) {
	h := newHarness(t)
	out, errOut, code := h.run("suggest", "--cached")
	if code == 0 || out != "" {
		t.Errorf("suggest with no readings: code %d out %q", code, out)
	}
	contains(t, "suggest", errOut, "no account has a current usage reading")
}

func TestLoginCreatesPrivateFolder(t *testing.T) {
	h := newHarness(t)
	out, errOut, code := h.run("login", "2")
	if code != 0 {
		t.Fatalf("login exit %d: %s", code, errOut)
	}
	contains(t, "login", out, "login under "+h.dir("2"))
	st, err := os.Stat(h.dir("2"))
	must(t, err)
	if st.Mode().Perm() != 0o700 {
		t.Errorf("folder mode %v, want 0700", st.Mode().Perm())
	}
}

func TestBadCommandLine(t *testing.T) {
	h := newHarness(t)
	if _, _, code := h.run("frobnicate"); code != 2 {
		t.Errorf("unknown command exit %d, want 2", code)
	}
	if _, errOut, code := h.run("status", "--nope"); code == 0 {
		t.Error("unknown option should fail")
	} else {
		contains(t, "bad option", errOut, "status: unknown option --nope")
	}
	out, _, _ := h.run("--version")
	if strings.TrimSpace(out) == "" {
		t.Error("--version printed nothing")
	}
	for _, sh := range []string{"bash", "zsh", "fish"} {
		if out, _, code := h.run("completion", sh); code != 0 || !strings.Contains(out, "claude-router") {
			t.Errorf("completion %s failed", sh)
		}
	}
}
