package main

import (
	"strings"
	"testing"
)

func addDad(t *testing.T, dir string) {
	t.Helper()
	mustRun(t, dir, "user", "add", "--name", "dad", "--sex", "m", "--birth", "1987-04", "--height", "181", "--weight", "97.25")
}

func TestUserAddListMarkdown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	addDad(t, dir)
	out := mustRun(t, dir, "user", "list")
	want := "| name | sex | birth | height | last weight |\n| --- | --- | --- | --- | --- |\n| dad | m | 1987-04 | 181 cm | 97.25 kg |\n"
	if out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}

func TestUserListJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	addDad(t, dir)
	var got []userView
	decodeJSON(t, mustRun(t, dir, "-f", "json", "user", "list"), &got)
	if len(got) != 1 || got[0].Name != "dad" || got[0].HeightCm != 181 || got[0].LastKg != 97.25 {
		t.Fatalf("got %+v", got)
	}
}

func TestUserAddValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing fields", []string{"user", "add", "--name", "x"}, "needs"},
		{"bad sex", []string{"user", "add", "--name", "x", "--sex", "x", "--birth", "1990-01", "--height", "170", "--weight", "70"}, "--sex"},
		{"bad birth", []string{"user", "add", "--name", "x", "--sex", "m", "--birth", "1990", "--height", "170", "--weight", "70"}, "--birth"},
		{"bad height", []string{"user", "add", "--name", "x", "--sex", "m", "--birth", "1990-01", "--height", "300", "--weight", "70"}, "--height"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := runCLI(t, t.TempDir(), tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want %q", err, tt.want)
			}
		})
	}
}

func TestUserAddDuplicate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	addDad(t, dir)
	_, _, err := runCLI(t, dir, "user", "add", "--name", "dad", "--sex", "m", "--birth", "1990-01", "--height", "170", "--weight", "70")
	if err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("got %v", err)
	}
}

func TestUserEditAndRemove(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	addDad(t, dir)
	mustRun(t, dir, "add", "--user", "dad", "--weight", "96")
	mustRun(t, dir, "user", "edit", "--height", "182", "--name", "papa", "dad")
	var users []userView
	decodeJSON(t, mustRun(t, dir, "-f", "json", "user", "list"), &users)
	if len(users) != 1 || users[0].Name != "papa" || users[0].HeightCm != 182 || users[0].LastKg != 96 {
		t.Fatalf("got %+v", users)
	}
	var res map[string]any
	decodeJSON(t, mustRun(t, dir, "-f", "json", "user", "rm", "papa"), &res)
	if res["removed_user"] != "papa" || res["removed_records"] != float64(1) {
		t.Fatalf("got %v", res)
	}
}
