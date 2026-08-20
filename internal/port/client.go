// Package port speaks the evol wire protocol to adapter processes.
//
// One invocation per action: the client spawns the adapter's argv,
// writes a single JSON request envelope to stdin, and reads a single
// JSON response envelope from stdout. A non-zero exit is an adapter
// error; stdout is then ignored and stderr carries diagnostics.
package port

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Version is the wire contract version every envelope carries.
const Version = "1"

// DefaultTimeout bounds a single adapter invocation when the port
// config does not override it.
const DefaultTimeout = 60 * time.Second

// Client invokes one adapter executable for one port.
type Client struct {
	// Port is the port name stamped into every envelope.
	Port string
	// Cmd is the adapter argv. Cmd[0] is the executable.
	Cmd []string
	// Timeout bounds each Call. Zero means DefaultTimeout.
	Timeout time.Duration
	// Env is config-provided environment for the adapter process,
	// merged under the caller's environment: a variable set in the
	// process environment overrides the same key here. Empty means
	// plain inheritance.
	Env map[string]string
}

// Configured reports whether the client has an adapter command bound.
func (c *Client) Configured() bool {
	return c != nil && len(c.Cmd) > 0
}

// Call performs one action. params are the request payload fields laid
// alongside the envelope; out receives the decoded response (envelope
// fields included if its type declares them).
func (c *Client) Call(ctx context.Context, action string, params map[string]any, out any) error {
	if !c.Configured() {
		return fmt.Errorf("port %s: no adapter configured", c.Port)
	}

	req := map[string]any{
		"evol":   Version,
		"port":   c.Port,
		"action": action,
	}
	for k, v := range params {
		req[k] = v
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("port %s/%s: marshal request: %w", c.Port, action, err)
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Adapter argv comes from operator-owned configuration; spawning
	// it is the whole point of the port layer.
	cmd := exec.CommandContext(ctx, c.Cmd[0], c.Cmd[1:]...) // #nosec G204
	if len(c.Env) > 0 {
		// Config pairs first, process environment after: os/exec keeps
		// the last value for a duplicated key, so the real environment
		// overrides config.
		keys := make([]string, 0, len(c.Env))
		for k := range c.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		env := make([]string, 0, len(keys)+len(os.Environ()))
		for _, k := range keys {
			env = append(env, k+"="+c.Env[k])
		}
		cmd.Env = append(env, os.Environ()...)
	}
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("port %s/%s: timeout after %s", c.Port, action, timeout)
		}
		diag := strings.TrimSpace(stderr.String())
		if diag == "" {
			diag = err.Error()
		}
		return fmt.Errorf("port %s/%s: adapter failed: %s", c.Port, action, diag)
	}

	if out == nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("port %s/%s: decode response: %w (stdout: %.200s)",
			c.Port, action, err, stdout.String())
	}
	return nil
}
