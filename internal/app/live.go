// Package app holds the Zepp Life logic around the scale: live weighing,
// user matching, history import and the auxiliary flows.
package app

import (
	"time"

	"miscale/internal/scale"
)

type EventKind int

const (
	EventWeight             EventKind = iota // live value changed
	EventMeasuringImpedance                  // weight is stable, impedance in progress
	EventResult                              // final measurement
	EventImpedanceFailed                     // final weight, body fat could not be measured
	EventSteppedOff                          // left the scale before the analysis finished
	EventSmallObject                         // small object weighing, nothing is saved
	EventOverload                            // above the measurement range
)

func (k EventKind) String() string {
	return [...]string{"weight", "measuring impedance", "result", "impedance failed", "stepped off", "small object", "overload"}[k]
}

type Event struct {
	Kind            EventKind
	Measurement     scale.Measurement
	WithComposition bool
}

type liveState int

const (
	stateIdle liveState = iota // noStable_noFinish
	stateStable
	stateMeasuringBF
	stateDone
)

// unstableGap mirrors HMWeightingActivity: an unstable packet arriving more
// than 4 s after the previous one means the user stepped off.
const unstableGap = 4 * time.Second

// LiveTracker is the HMWeightingActivity state machine for source 101/102.
type LiveTracker struct {
	state        liveState
	lastUnstable time.Time
	last         *scale.Measurement
	seen         map[time.Time]bool // timestamps already reported as final
}

func NewLiveTracker() *LiveTracker {
	return &LiveTracker{seen: map[time.Time]bool{}}
}

func (t *LiveTracker) Done() bool { return t.state == stateDone }

// Next starts a new weighing; timestamps already reported stay ignored.
func (t *LiveTracker) Next() {
	t.state = stateIdle
	t.last = nil
}

// Feed processes one advertisement or notification.
func (t *LiveTracker) Feed(m scale.Measurement, now time.Time) []Event {
	if t.state == stateDone || (m.Value == 0 && !m.Finished) {
		return nil
	}
	m = t.normalize(m, now)
	switch {
	case m.Finished:
		return t.finished(m)
	case m.Overload:
		return []Event{{Kind: EventOverload, Measurement: m}}
	case t.seen[m.Time]:
		return nil
	}
	return t.live(m, now)
}

// finished handles a packet sent after the user stepped off.
func (t *LiveTracker) finished(m scale.Measurement) []Event {
	if !m.Stable {
		if t.state == stateIdle || t.state == stateStable {
			t.state = stateDone
			return []Event{{Kind: EventSteppedOff, Measurement: m}}
		}
		return nil
	}
	if t.seen[m.Time] {
		return nil
	}
	t.state = stateStable
	return t.stable(m)
}

// normalize applies the 4 s step-off heuristic and the wrong year fix.
func (t *LiveTracker) normalize(m scale.Measurement, now time.Time) scale.Measurement {
	if t.state == stateIdle && !m.Stable && !m.Finished && !t.lastUnstable.IsZero() && now.Sub(t.lastUnstable) > unstableGap {
		m.Finished = true
	}
	// WeightAdvData compares local calendar years and uses now without millis
	if m.Time.Local().Year() != now.Local().Year() {
		m.Time = now.Truncate(time.Second)
	}
	return m
}

// live handles a packet of an ongoing weighing.
func (t *LiveTracker) live(m scale.Measurement, now time.Time) []Event {
	var out []Event
	if t.last == nil || t.last.Value != m.Value || t.last.Stable != m.Stable {
		out = append(out, Event{Kind: EventWeight, Measurement: m})
	}
	t.last = &m
	if !m.Stable {
		t.state = stateIdle
		t.lastUnstable = now
		return out
	}
	if t.state == stateIdle {
		t.state = stateStable
	}
	return append(out, t.stable(m)...)
}

// stable mirrors HMWeightingActivity.oOO00O; the 65534 check does not return.
func (t *LiveTracker) stable(m scale.Measurement) []Event {
	var out []Event
	if m.Impedance == 65534 && !m.ImpedanceStable && t.state != stateMeasuringBF {
		t.state = stateMeasuringBF
		out = append(out, Event{Kind: EventMeasuringImpedance, Measurement: m})
	}
	switch {
	case m.ImpedanceStable:
		t.finish(m)
		return append(out, Event{Kind: EventResult, Measurement: m, WithComposition: m.HasImpedance()})
	case m.Impedance != scale.ImpedanceFailed:
		if m.Part {
			t.state = stateDone
			out = append(out, Event{Kind: EventSmallObject, Measurement: m})
		}
		return out
	}
	t.finish(m)
	return append(out, Event{Kind: EventImpedanceFailed, Measurement: m})
}

func (t *LiveTracker) finish(m scale.Measurement) {
	t.state = stateDone
	t.seen[m.Time] = true
}
