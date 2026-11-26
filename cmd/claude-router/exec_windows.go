//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
)

var lookPath = exec.LookPath

// execClaude runs claude under the account folder and exits with its exit code
// (Windows has no exec that replaces the process).
func execClaude(bin string, args []string, dir string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = envWith(dir)
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.ExitCode())
	}
	if err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// startDetached starts cmd without waiting for it.
func startDetached(cmd *exec.Cmd) error { return cmd.Start() }
