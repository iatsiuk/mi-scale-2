package main

import (
	"strings"
	"testing"
)

func TestAddShowMarkdown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	addDad(t, dir)
	out := mustRun(t, dir, "add", "--user", "dad", "--weight", "97.25", "--time", "2026-10-02T00:34:50+03:00")
	for _, want := range []string{"## Record 1: dad", "| weight | 97.25 kg |", "| bmi | 29.6 | overweight |", "| ideal weight | 70.70 kg |"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if show := mustRun(t, dir, "show", "1"); show != out {
		t.Fatalf("show differs from add output:\n%s", show)
	}
}

func TestShowJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	addDad(t, dir)
	mustRun(t, dir, "add", "--user", "dad", "--weight", "97.25")
	var got recordView
	decodeJSON(t, mustRun(t, dir, "--format", "json", "show", "1"), &got)
	if got.ID != 1 || got.User != "dad" || got.WeightKg != 97.25 || got.Unit != "kg" || len(got.Metrics) != 2 {
		t.Fatalf("got %+v", got)
	}
	if m := got.Metrics[0]; m.Name != "bmi" || m.Value != 29.6 || m.Level != "overweight" {
		t.Fatalf("bmi metric %+v", m)
	}
}

func TestHistoryFiltersAndLimits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	addDad(t, dir)
	mustRun(t, dir, "user", "add", "--name", "mom", "--sex", "f", "--birth", "1990-01", "--height", "165", "--weight", "60")
	mustRun(t, dir, "add", "--user", "dad", "--weight", "97", "--time", "2026-10-01T10:00:00Z")
	mustRun(t, dir, "add", "--user", "mom", "--weight", "60", "--time", "2026-10-01T11:00:00Z")
	mustRun(t, dir, "add", "--user", "dad", "--weight", "96", "--time", "2026-10-01T12:00:00Z")

	var all []recordView
	decodeJSON(t, mustRun(t, dir, "-f", "json", "history"), &all)
	if len(all) != 3 || all[0].WeightKg != 96 || all[2].WeightKg != 97 {
		t.Fatalf("history must be newest first: %+v", all)
	}
	var dad []recordView
	decodeJSON(t, mustRun(t, dir, "-f", "json", "history", "--user", "dad", "-n", "1"), &dad)
	if len(dad) != 1 || dad[0].WeightKg != 96 {
		t.Fatalf("got %+v", dad)
	}
	var unassigned []recordView
	decodeJSON(t, mustRun(t, dir, "-f", "json", "history", "--user", ""), &unassigned)
	if len(unassigned) != 0 {
		t.Fatalf("got %+v", unassigned)
	}
	md := mustRun(t, dir, "history")
	if !strings.HasPrefix(md, "| id | time | user | kind | weight | bmi | fat | score |\n") || strings.Count(md, "\n") != 5 {
		t.Fatalf("got\n%s", md)
	}
}

func TestAssignAndRemove(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	addDad(t, dir)
	mustRun(t, dir, "user", "add", "--name", "mom", "--sex", "f", "--birth", "1990-01", "--height", "165", "--weight", "60")
	mustRun(t, dir, "add", "--user", "dad", "--weight", "61")
	var got recordView
	decodeJSON(t, mustRun(t, dir, "-f", "json", "assign", "1", "mom"), &got)
	if got.User != "mom" || got.Metrics[0].Value != 22.4 {
		t.Fatalf("assign must recompute for the new user: %+v", got)
	}
	mustRun(t, dir, "rm", "1")
	if _, _, err := runCLI(t, dir, "show", "1"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got %v", err)
	}
}

func TestAddRequiresKnownUser(t *testing.T) {
	t.Parallel()
	_, _, err := runCLI(t, t.TempDir(), "add", "--user", "nobody", "--weight", "70")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got %v", err)
	}
}
