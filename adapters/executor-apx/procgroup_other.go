//go:build !unix

package main

import "os/exec"

// setProcGroup is a no-op on non-unix platforms; WaitDelay alone bounds
// the wait after a deadline.
func setProcGroup(_ *exec.Cmd) {}
