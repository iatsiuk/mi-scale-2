package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"miscale/internal/bodycomp"
	"miscale/internal/scale"
	"miscale/internal/store"
)

func TestMarkdownTableEscapesPipes(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	mdTable(&b, []string{"a", "b"}, [][]string{{"x|y", "z"}})
	if want := "| a | b |\n| --- | --- |\n| x\\|y | z |\n"; b.String() != want {
		t.Fatalf("got %q", b.String())
	}
}

func TestFieldsKeepOrderInJSON(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(fields{{"z", 1}, {"a", "x"}})
	if err != nil || string(b) != `{"z":1,"a":"x"}` {
		t.Fatalf("got %s, %v", b, err)
	}
}

func TestRecordViewWithComposition(t *testing.T) {
	t.Parallel()
	d := &store.Data{Users: []store.User{{ID: "1", Name: "me", Sex: bodycomp.Male, Birth: "1987-04", HeightCm: 181}}}
	res, err := bodycomp.Compute(bodycomp.Input{WeightKg: 97.25, HeightCm: 181, Age: 39, Sex: bodycomp.Male, Impedance: 421})
	if err != nil {
		t.Fatal(err)
	}
	r := store.Record{ID: 7, UserID: "1", Time: time.Date(2026, 10, 1, 21, 38, 31, 0, time.UTC), Kind: store.KindScale,
		Origin: store.OriginHistory, WeightKg: 97.25, Impedance: 421, HeightCm: 181, Age: 39, Composition: &res}
	d.Records = append(d.Records, r)
	var b bytes.Buffer
	detailView(d, &r).markdown(&b)
	for _, want := range []string{
		"| body fat | 30.8 % | high |",
		"| muscle | 63.81 kg | high |",
		"| water | 49.3 % | slightly low |",
		"| protein | 16.2 % | normal |",
		"| bone mass | 3.42 kg | normal |",
		"| visceral fat | 14 | slightly high |",
		"| bmr | 1846 kcal | below standard (standard 2042) |",
		"| body age | 46 | (standard 39) |",
		"| body type | thick-set |  |",
		"| score | 46 |  |",
		"| impedance | 421 ohm |  |",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("markdown lacks %q:\n%s", want, b.String())
		}
	}
}

func TestWeightIn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		unit  scale.Unit
		value float64
		text  string
	}{
		{scale.UnitKg, 97.25, "97.25 kg"},
		{scale.UnitLb, 214.4, "214.4 lb"},
		{scale.UnitJin, 194.5, "194.5 jin"},
		{scale.UnitSt, 15.31, "15:4 st"},
	}
	for _, tt := range tests {
		if v, u, text := weightIn(97.25, tt.unit); v != tt.value || u != tt.unit.String() || text != tt.text {
			t.Errorf("weightIn(97.25, %v) = %v %q %q", tt.unit, v, u, text)
		}
	}
}

func TestStoneKeepsMassMetrics(t *testing.T) {
	t.Parallel()
	d := &store.Data{DisplayUnit: scale.UnitSt, Users: []store.User{{ID: "1", Name: "me", Sex: bodycomp.Male, Birth: "1987-04", HeightCm: 181}}}
	res, err := bodycomp.Compute(bodycomp.Input{WeightKg: 97.25, HeightCm: 181, Age: 39, Sex: bodycomp.Male, Impedance: 421})
	if err != nil {
		t.Fatal(err)
	}
	r := store.Record{ID: 1, UserID: "1", Kind: store.KindScale, WeightKg: 97.25, Impedance: 421, HeightCm: 181, Age: 39, Composition: &res}
	v := detailView(d, &r)
	if v.Weight != 15.31 || v.Unit != "st" {
		t.Fatalf("weight %v %q", v.Weight, v.Unit)
	}
	for _, m := range v.Metrics {
		if m.Unit == "st" && m.Value == 0 {
			t.Errorf("%s lost its value", m.Name)
		}
	}
	var b bytes.Buffer
	v.markdown(&b)
	if !strings.Contains(b.String(), "| muscle | 10:1 st | high |") {
		t.Fatalf("got\n%s", b.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestRenderReportsWriteErrors(t *testing.T) {
	t.Parallel()
	for _, format := range []string{formatMarkdown, formatJSON} {
		e := &env{out: failingWriter{}, format: format}
		if err := e.render(fields{{"a", 1}}); !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("%s: got %v", format, err)
		}
	}
}

func TestDescribeUnit(t *testing.T) {
	t.Parallel()
	if u, err := parseUnit("JIN"); err != nil || u != scale.UnitJin {
		t.Fatalf("got %v %v", u, err)
	}
}

func TestPrintScanStopsOnHeaderWriteError(t *testing.T) {
	t.Parallel()
	e := &env{out: failingWriter{}, format: formatMarkdown}
	ch := make(chan advert) // open and idle, like a scan without packets
	done := make(chan error, 1)
	go func() { done <- printScan(e, ch) }()
	select {
	case err := <-done:
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("printScan waits for packets after a failed write")
	}
}
