package app

import (
	"testing"
	"time"

	"miscale/internal/scale"
)

var t0 = time.Date(2026, 10, 1, 20, 34, 0, 0, time.UTC)

func pkt(sec int, value float64, stable, impStable, finish bool, z uint16) scale.Measurement {
	return scale.Measurement{
		Unit: scale.UnitKg, Value: value, Stable: stable, ImpedanceStable: impStable, Finished: finish,
		Impedance: z, Time: t0.Add(time.Duration(sec) * time.Second),
	}
}

func run(tr *LiveTracker, now time.Time, ms ...scale.Measurement) []Event {
	out := make([]Event, 0, len(ms))
	for _, m := range ms {
		out = append(out, tr.Feed(m, now)...)
	}
	return out
}

func kinds(evs []Event) []EventKind {
	out := make([]EventKind, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Kind)
	}
	return out
}

func equalKinds(a, b []EventKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLiveFullMeasurementWithImpedance(t *testing.T) {
	tr := NewLiveTracker()
	now := t0
	evs := run(tr, now,
		pkt(0, 40.5, false, false, false, 0),
		pkt(1, 97.25, true, false, false, 0),
		pkt(1, 97.25, true, false, false, 65534),
		pkt(1, 97.25, true, true, false, 431),
		pkt(1, 97.25, true, true, false, 431), // advertisement repeats
		pkt(1, 97.25, true, false, true, 431),
	)
	want := []EventKind{EventWeight, EventWeight, EventMeasuringImpedance, EventResult}
	if got := kinds(evs); !equalKinds(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
	res := evs[len(evs)-1]
	if res.Measurement.Impedance != 431 || !res.WithComposition {
		t.Fatalf("result %+v", res)
	}
	if !tr.Done() {
		t.Fatal("tracker must be done after a result")
	}
}

func TestLiveImpedanceFailureOffersWeightOnly(t *testing.T) {
	tr := NewLiveTracker()
	evs := run(tr, t0,
		pkt(1, 70, true, false, false, 0),
		pkt(1, 70, true, false, false, scale.ImpedanceFailed),
	)
	last := evs[len(evs)-1]
	if last.Kind != EventImpedanceFailed || last.WithComposition {
		t.Fatalf("got %+v", last)
	}
}

func TestLiveSteppedOffEarly(t *testing.T) {
	tr := NewLiveTracker()
	evs := run(tr, t0,
		pkt(1, 70, true, false, false, 0),
		pkt(1, 70, false, false, true, 0),
	)
	if last := evs[len(evs)-1]; last.Kind != EventSteppedOff {
		t.Fatalf("got %v", kinds(evs))
	}
	if !tr.Done() {
		t.Fatal("tracker must be done")
	}
}

func TestLiveStableFinishWithoutImpedanceWaits(t *testing.T) {
	tr := NewLiveTracker()
	evs := run(tr, t0, pkt(1, 70, true, false, true, 500))
	for _, e := range evs {
		if e.Kind == EventResult {
			t.Fatalf("stable+finish without impedance flag must not be a result: %v", kinds(evs))
		}
	}
}

func TestLiveSmallObject(t *testing.T) {
	tr := NewLiveTracker()
	m := pkt(1, 2.5, true, false, false, 0)
	m.Part = true
	evs := run(tr, t0, m)
	if last := evs[len(evs)-1]; last.Kind != EventSmallObject {
		t.Fatalf("got %v", kinds(evs))
	}
}

func TestLiveOverload(t *testing.T) {
	tr := NewLiveTracker()
	m := pkt(1, 65520, false, false, false, 0)
	m.Overload = true
	if evs := run(tr, t0, m); len(evs) != 1 || evs[0].Kind != EventOverload {
		t.Fatalf("got %v", kinds(evs))
	}
}

func TestLiveDropsZeroWeight(t *testing.T) {
	tr := NewLiveTracker()
	if evs := run(tr, t0, pkt(1, 0, false, false, false, 0)); len(evs) != 0 {
		t.Fatalf("got %v", kinds(evs))
	}
}

func TestLiveFixesWrongYear(t *testing.T) {
	tr := NewLiveTracker()
	m := pkt(1, 70, true, true, false, 500)
	m.Time = time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC)
	evs := run(tr, t0, m)
	if last := evs[len(evs)-1]; last.Kind != EventResult || !last.Measurement.Time.Equal(t0) {
		t.Fatalf("got %+v", last)
	}
}

func TestLiveGapAfterUnstableMeansSteppedOff(t *testing.T) {
	tr := NewLiveTracker()
	run(tr, t0, pkt(0, 60, false, false, false, 0))
	evs := run(tr, t0.Add(5*time.Second), pkt(5, 61, false, false, false, 0))
	if last := evs[len(evs)-1]; last.Kind != EventSteppedOff {
		t.Fatalf("got %v", kinds(evs))
	}
}

func TestLiveMeasuringImpedanceFallsThroughToSmallObject(t *testing.T) {
	tr := NewLiveTracker()
	m := pkt(1, 2.5, true, false, false, 65534)
	m.Part = true
	want := []EventKind{EventWeight, EventMeasuringImpedance, EventSmallObject}
	if got := kinds(run(tr, t0, m)); !equalKinds(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLiveYearIsComparedInLocalTime(t *testing.T) {
	now := time.Date(2027, 1, 1, 0, 30, 0, 123456789, time.Local)
	m := pkt(1, 70, true, true, false, 500)
	m.Time = time.Date(2027, 1, 1, 0, 0, 5, 0, time.Local).UTC() // UTC year is 2026 east of Greenwich
	evs := run(NewLiveTracker(), now, m)
	if last := evs[len(evs)-1]; !last.Measurement.Time.Equal(m.Time) {
		t.Fatalf("timestamp in the same local year must stay: got %v, want %v", last.Measurement.Time, m.Time)
	}
}

func TestLiveWrongYearIsReplacedWithSeconds(t *testing.T) {
	now := t0.Add(123 * time.Millisecond)
	m := pkt(1, 70, true, true, false, 500)
	m.Time = time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC)
	evs := run(NewLiveTracker(), now, m)
	if last := evs[len(evs)-1]; !last.Measurement.Time.Equal(t0) {
		t.Fatalf("got %v, want %v", last.Measurement.Time, t0)
	}
}
