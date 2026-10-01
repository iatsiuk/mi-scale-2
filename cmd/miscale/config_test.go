package main

import (
	"strings"
	"testing"
)

func TestConfigChangesSettings(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var got map[string]any
	decodeJSON(t, mustRun(t, dir, "-f", "json", "config", "--merge", "on", "--unit", "lb", "--uid", "0x1234"), &got)
	if got["merge"] != true || got["unit"] != "lb" || got["uid"] != float64(0x1234) {
		t.Fatalf("got %v", got)
	}
	md := mustRun(t, dir, "config")
	if !strings.Contains(md, "| merge | true |") || !strings.Contains(md, "| uid | 4660 |") {
		t.Fatalf("settings must persist:\n%s", md)
	}
}

func TestConfigDisplayUnitAppliesToOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	addDad(t, dir)
	mustRun(t, dir, "config", "--unit", "st")
	out := mustRun(t, dir, "add", "--user", "dad", "--weight", "97.25")
	if !strings.Contains(out, "| weight | 15:4 st |") {
		t.Fatalf("got\n%s", out)
	}
}

func TestConfigValidation(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"config", "--merge", "maybe"}, {"config", "--unit", "oz"}, {"config", "--uid", "0x1ffffffff"}} {
		if _, _, err := runCLI(t, t.TempDir(), args...); err == nil {
			t.Errorf("%v: expected error", args)
		}
	}
}
