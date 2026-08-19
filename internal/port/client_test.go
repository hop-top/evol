package port

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func script(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCallRoundTrip(t *testing.T) {
	c := &Client{Port: "echo", Cmd: []string{"sh", script(t, "ok.sh")}}
	var resp struct {
		Evol   string `json:"evol"`
		Port   string `json:"port"`
		Action string `json:"action"`
		Got    string `json:"got"`
	}
	err := c.Call(context.Background(), "ping",
		map[string]any{"payload": "hello"}, &resp)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Evol != Version {
		t.Errorf("evol = %q, want %q", resp.Evol, Version)
	}
	if resp.Got != "hello" {
		t.Errorf("got = %q, want %q (adapter must see payload fields)", resp.Got, "hello")
	}
	if resp.Port != "echo" || resp.Action != "ping" {
		t.Errorf("envelope echo = %s/%s, want echo/ping", resp.Port, resp.Action)
	}
}

func TestCallAdapterFailure(t *testing.T) {
	c := &Client{Port: "boom", Cmd: []string{"sh", script(t, "fail.sh")}}
	err := c.Call(context.Background(), "x", nil, &struct{}{})
	if err == nil {
		t.Fatal("want error on non-zero exit")
	}
	if !strings.Contains(err.Error(), "broken config") {
		t.Errorf("error should carry stderr diagnostics, got: %v", err)
	}
}

func TestCallTimeout(t *testing.T) {
	c := &Client{
		Port:    "slow",
		Cmd:     []string{"sh", script(t, "slow.sh")},
		Timeout: 100 * time.Millisecond,
	}
	err := c.Call(context.Background(), "x", nil, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("want timeout error, got: %v", err)
	}
}

func TestCallUnconfigured(t *testing.T) {
	var c *Client
	if c.Configured() {
		t.Error("nil client must report unconfigured")
	}
	c = &Client{Port: "none"}
	if err := c.Call(context.Background(), "x", nil, nil); err == nil {
		t.Fatal("want error for unconfigured port")
	}
}
