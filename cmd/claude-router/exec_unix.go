//go:build !windows

package main

import (
	"os/exec"
	"path/filepath"
	"syscall"
)

var lookPath = exec.LookPath

// execClaude replaces this process with claude running under the account folder, so
// the terminal, signals and exit code all belong to claude.
func execClaude(bin string, args []string, dir string) error {
	argv := append([]string{filepath.Base(bin)}, args...)
	return syscall.Exec(bin, argv, envWith(dir))
}
