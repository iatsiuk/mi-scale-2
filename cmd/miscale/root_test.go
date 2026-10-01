package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	stdinIsTTY = func() bool { return false }
	os.Exit(m.Run())
}

// runCLI executes the root command against a data file in dir.
func runCLI(t *testing.T, dir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := newRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(append([]string{"--data", filepath.Join(dir, "data.json")}, args...))
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func mustRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, errOut, err := runCLI(t, dir, args...)
	if err != nil {
		t.Fatalf("%v: %v\nstderr: %s", args, err, errOut)
	}
	return out
}

func decodeJSON(t *testing.T, s string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("invalid json %q: %v", s, err)
	}
}

func TestRejectsUnknownFormat(t *testing.T) {
	t.Parallel()
	_, _, err := runCLI(t, t.TempDir(), "--format", "yaml", "config")
	if err == nil || !strings.Contains(err.Error(), "markdown or json") {
		t.Fatalf("got %v", err)
	}
}

func TestVersionFlag(t *testing.T) {
	t.Parallel()
	if out := mustRun(t, t.TempDir(), "--version"); !strings.Contains(out, version) {
		t.Fatalf("got %q", out)
	}
}

func TestDataPathFromEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "env.json")
	t.Setenv(envDataPath, path)
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"-f", "json", "config"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	decodeJSON(t, out.String(), &got)
	if got["data"] != path {
		t.Fatalf("data path %v, want %s", got["data"], path)
	}
}
