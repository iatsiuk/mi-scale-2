package main

import (
	"bufio"
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"miscale/internal/app"
	"miscale/internal/bodycomp"
	"miscale/internal/scale"
	"miscale/internal/store"
)

func testEnv(t *testing.T, interactive bool, input string) (*env, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	d := &store.Data{Users: []store.User{{ID: "1", Name: "me", Sex: bodycomp.Male, Birth: "1987-04", HeightCm: 181, WeightKg: 70}}}
	return &env{path: filepath.Join(t.TempDir(), "data.json"), data: d, in: bufio.NewReader(strings.NewReader(input)),
		out: &out, log: &bytes.Buffer{}, format: formatJSON, interactive: interactive}, &out
}

func finalEvent(kind app.EventKind, kg float64, z uint16) app.Event {
	m := scale.Measurement{Unit: scale.UnitKg, Value: kg, Stable: true, Finished: true, Impedance: z,
		ImpedanceStable: z != scale.ImpedanceFailed, Time: time.Date(2026, 10, 1, 21, 0, 0, 0, time.UTC)}
	return app.Event{Kind: kind, Measurement: m, WithComposition: kind == app.EventResult}
}

func TestSaveLiveWithoutTerminalKeepsUnmatchedRecord(t *testing.T) {
	t.Parallel()
	e, out := testEnv(t, false, "")
	ev := finalEvent(app.EventResult, 90, 431)
	if err := saveLive(e, ev, ""); err != nil {
		t.Fatal(err)
	}
	if e.data.IsIgnored(ev.Measurement.Time) || len(e.data.Records) != 1 || e.data.Records[0].UserID != "" {
		t.Fatalf("an unanswered prompt must keep the record unassigned: %+v ignored %v", e.data.Records, e.data.Ignored)
	}
	var got recordView
	decodeJSON(t, out.String(), &got)
	if got.WeightKg != 90 || got.User != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestSaveLiveWithoutTerminalKeepsWeightOnly(t *testing.T) {
	t.Parallel()
	e, _ := testEnv(t, false, "")
	ev := finalEvent(app.EventImpedanceFailed, 70.5, scale.ImpedanceFailed)
	if err := saveLive(e, ev, "me"); err != nil {
		t.Fatal(err)
	}
	if e.data.IsIgnored(ev.Measurement.Time) || len(e.data.Records) != 1 || e.data.Records[0].UserID != "1" {
		t.Fatalf("got %+v ignored %v", e.data.Records, e.data.Ignored)
	}
}

func TestSaveLiveExplicitDiscardIsIgnored(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		ev    app.Event
		input string
	}{
		{"empty user choice", finalEvent(app.EventResult, 90, 431), "\n"},
		{"weight only declined", finalEvent(app.EventImpedanceFailed, 70.5, scale.ImpedanceFailed), "n\n"},
	} {
		e, _ := testEnv(t, true, tt.input)
		if err := saveLive(e, tt.ev, ""); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if !e.data.IsIgnored(tt.ev.Measurement.Time) || len(e.data.Records) != 0 {
			t.Fatalf("%s: got %+v", tt.name, e.data.Records)
		}
	}
}

func TestSaveBabyWithoutTerminalNeedsConfirmation(t *testing.T) {
	t.Parallel()
	e, out := testEnv(t, false, "")
	e.data.Users = append(e.data.Users, store.User{ID: "2", Name: "baby", Sex: bodycomp.Female, Birth: "2026-01", HeightCm: 60})
	adult := finalEvent(app.EventResult, 70, 431).Measurement
	both := adult
	both.Value, both.Time = 70.5, adult.Time.Add(10*time.Second)
	err := saveBaby(e, &e.data.Users[1], "", adult, both)
	if err == nil || !strings.Contains(err.Error(), "confirm") || out.Len() != 0 || len(e.data.Records) != 0 {
		t.Fatalf("got %v, output %q, records %+v", err, out.String(), e.data.Records)
	}
}

func TestSaveLiveClosedInputIsNoAnswer(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		ev   app.Event
		user string
	}{
		{"user choice", finalEvent(app.EventResult, 90, 431), ""},
		{"weight only", finalEvent(app.EventImpedanceFailed, 70.5, scale.ImpedanceFailed), "me"},
	} {
		e, _ := testEnv(t, true, "") // ctrl-d at the prompt
		if err := saveLive(e, tt.ev, tt.user); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if e.data.IsIgnored(tt.ev.Measurement.Time) || len(e.data.Records) != 1 {
			t.Fatalf("%s: closed input is not a refusal: %+v ignored %v", tt.name, e.data.Records, e.data.Ignored)
		}
	}
}

func TestSaveLiveInvalidAnswerIsNotADiscard(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		ev      app.Event
		user    string
		input   string
		ignored bool
	}{
		{"typo then closed input", finalEvent(app.EventResult, 90, 431), "", "me\n", false},
		{"out of range then a valid number", finalEvent(app.EventResult, 90, 431), "", "2\n1\n", false},
		{"weight only typo then closed input", finalEvent(app.EventImpedanceFailed, 70.5, scale.ImpedanceFailed), "me", "yse\n", false},
		{"weight only typo then no", finalEvent(app.EventImpedanceFailed, 70.5, scale.ImpedanceFailed), "me", "yse\nno\n", true},
	} {
		e, _ := testEnv(t, true, tt.input)
		if err := saveLive(e, tt.ev, tt.user); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		kept := len(e.data.Records) == 1
		if e.data.IsIgnored(tt.ev.Measurement.Time) != tt.ignored || kept == tt.ignored {
			t.Fatalf("%s: records %+v ignored %v", tt.name, e.data.Records, e.data.Ignored)
		}
	}
}

func TestSaveBalance(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name        string
		interactive bool
		input       string
		saved       bool
		user        string
	}{
		{"no terminal keeps it unassigned", false, "", true, ""},
		{"closed input keeps it unassigned", true, "", true, ""},
		{"picked user", true, "1\n", true, "1"},
		{"empty answer discards", true, "\n", false, ""},
	} {
		e, _ := testEnv(t, tt.interactive, tt.input)
		if err := saveBalance(e, nil, 12.34); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if !tt.saved {
			if len(e.data.Records) != 0 {
				t.Fatalf("%s: got %+v", tt.name, e.data.Records)
			}
			continue
		}
		if len(e.data.Records) != 1 || e.data.Records[0].Kind != store.KindBalance || e.data.Records[0].UserID != tt.user {
			t.Fatalf("%s: got %+v", tt.name, e.data.Records)
		}
	}
}
