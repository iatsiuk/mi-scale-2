package scale

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// fakeScale emulates the GATT side of XMTZC05HM.
type fakeScale struct {
	t *testing.T

	mu     sync.Mutex
	values map[string][]byte
	subs   map[string]chan []byte
	writes []string // "char-suffix:hex"

	onWrite func(f *fakeScale, char string, data []byte)
}

func newFakeScale(t *testing.T) *fakeScale {
	return &fakeScale{
		t: t,
		values: map[string][]byte{
			CharControl:     mustHex(t, "0301000000000000000000000000000000000000"),
			CharPnP:         mustHex(t, "01570119000001"),
			CharSoftwareRev: []byte("V1.0.0.12"),
			CharHardwareRev: []byte("V0.24.131.09"),
			CharSerial:      []byte("a1b2c3d4e5f6"),
			CharSystemID:    mustHex(t, "112233fffe445566"),
		},
		subs: map[string]chan []byte{},
	}
}

func (f *fakeScale) Read(_ context.Context, service, char string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.values[char]
	if !ok {
		return nil, fmt.Errorf("fake: no value for %s", char)
	}
	return v, nil
}

func (f *fakeScale) Write(_ context.Context, service, char string, data []byte) error {
	f.mu.Lock()
	f.writes = append(f.writes, fmt.Sprintf("%s:%x", char[4:8], data))
	if char == CharCurrentTime {
		f.values[char] = append([]byte(nil), data...)
	}
	cb := f.onWrite
	f.mu.Unlock()
	if cb != nil {
		cb(f, char, data)
	}
	return nil
}

func (f *fakeScale) Subscribe(_ context.Context, service, char string) (<-chan []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan []byte, 64)
	f.subs[char] = ch
	return ch, nil
}

func (f *fakeScale) Unsubscribe(_ context.Context, service, char string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ch, ok := f.subs[char]; ok {
		close(ch)
		delete(f.subs, char)
	}
	return nil
}

func (f *fakeScale) notify(char string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ch, ok := f.subs[char]; ok {
		ch <- data
	}
}

func (f *fakeScale) written() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...)
}

func equalStrings(a, b []string) bool { return slices.Equal(a, b) }

func testSession(f *fakeScale) *Session {
	s := NewSession(f)
	s.Now = func() time.Time { return time.Date(2026, 10, 4, 12, 4, 5, 0, time.UTC) }
	s.ResponseTimeout = 200 * time.Millisecond
	s.HistoryIdle = 200 * time.Millisecond
	return s
}

func TestSessionInitSyncsTimeWithoutUserModeSwitch(t *testing.T) {
	f := newFakeScale(t)
	s := testSession(f)
	if err := s.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"2a2b:ea070a040c0405010000"}
	if got := f.written(); !equalStrings(got, want) {
		t.Fatalf("writes %v, want %v", got, want)
	}
}

func TestSessionInitLeavesMode3(t *testing.T) {
	f := newFakeScale(t)
	f.values[CharControl] = mustHex(t, "0300")
	s := testSession(f)
	if err := s.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"1542:060b0000", "2a2b:ea070a040c0405010000"}
	if got := f.written(); !equalStrings(got, want) {
		t.Fatalf("writes %v, want %v", got, want)
	}
}

func TestSessionDeviceInfo(t *testing.T) {
	s := testSession(newFakeScale(t))
	info, err := s.DeviceInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := DeviceInfo{
		Model:    ModelBodyCompositionScale2,
		PnP:      PnP{VendorIDSource: 1, VendorID: 0x0157, ProductID: 25, ProductVersion: 0x0100},
		Software: "V1.0.0.12",
		Hardware: "V0.24.131.09",
		Serial:   "a1b2c3d4e5f6",
		SystemID: "112233fffe445566",
	}
	if info != want {
		t.Fatalf("got %+v\nwant %+v", info, want)
	}
}

func TestSessionEraseHistory(t *testing.T) {
	f := newFakeScale(t)
	f.onWrite = func(f *fakeScale, char string, data []byte) {
		if char == CharControl && bytes.Equal(data, CmdEraseHistory()) {
			f.notify(CharControl, mustHex(t, "1006120001"))
		}
	}
	if err := testSession(f).EraseHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSessionCommandFailureStatus(t *testing.T) {
	f := newFakeScale(t)
	f.onWrite = func(f *fakeScale, char string, data []byte) {
		f.notify(CharControl, mustHex(t, "1006100004"))
	}
	err := testSession(f).SetPartMeasure(context.Background(), true)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != StatusOperationFailed {
		t.Fatalf("got %v", err)
	}
}

func TestSessionCommandTimeout(t *testing.T) {
	err := testSession(newFakeScale(t)).EraseHistory(context.Background())
	if !errors.Is(err, ErrNoResponse) {
		t.Fatalf("got %v", err)
	}
}

func TestSessionCommandIgnoresStaleNotification(t *testing.T) {
	f := newFakeScale(t)
	f.onWrite = func(f *fakeScale, char string, data []byte) {
		f.notify(CharControl, mustHex(t, "1006100001")) // answer to another command
		f.notify(CharControl, mustHex(t, "1006120001"))
	}
	if err := testSession(f).EraseHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSessionSetUnitIsFireAndForget(t *testing.T) {
	f := newFakeScale(t)
	if err := testSession(f).SetUnit(context.Background(), UnitJin); err != nil {
		t.Fatal(err)
	}
	if got := f.written(); !equalStrings(got, []string{"1542:06040002"}) {
		t.Fatalf("writes %v", got)
	}
}

const rec1 = "02a4e90701010a0000af01fa4b" // 2025-01-01 10:00:00, 97.25 kg, 431 ohm
const rec2 = "02a4e90701020a0000b0010a4b" // 2025-01-02 10:00:00, 96.05 kg, 432 ohm

func historyScale(t *testing.T, count int, chunks ...string) *fakeScale {
	f := newFakeScale(t)
	f.onWrite = func(f *fakeScale, char string, data []byte) {
		if char != CharHistory {
			return
		}
		switch data[0] {
		case 0x01:
			f.notify(CharHistory, binary.LittleEndian.AppendUint16([]byte{0x01}, uint16(count))) //nolint:gosec // test counts are small
		case 0x02:
			for _, c := range chunks {
				f.notify(CharHistory, mustHex(t, c))
			}
		}
	}
	return f
}

func TestSessionHistoryThenAck(t *testing.T) {
	f := historyScale(t, 2, rec1+rec2, "03")
	s := testSession(f)
	got, _, err := s.History(context.Background(), 0x0a0b0c0d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Value != 97.25 || got[1].Value != 96.05 {
		t.Fatalf("records %+v", got)
	}
	// the caller acks only after the records are stored
	want := []string{"2a2f:010d0c0b0a", "2a2f:02", "2a2f:03"}
	if w := f.written(); !equalStrings(w, want) {
		t.Fatalf("writes %v, want %v", w, want)
	}
	if err := s.AckHistory(context.Background(), 0x0a0b0c0d); err != nil {
		t.Fatal(err)
	}
	if w := f.written(); w[len(w)-1] != "2a2f:040d0c0b0a" {
		t.Fatalf("writes %v", w)
	}
}

func TestSessionFailsOnClosedSubscription(t *testing.T) {
	f := historyScale(t, 1, rec1, "03")
	s := testSession(f)
	if _, _, err := s.History(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Unsubscribe(context.Background(), ServiceBodyComposition, CharHistory) // link dropped
	done := make(chan error, 1)
	go func() {
		_, _, err := s.History(context.Background(), 1)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrDisconnected) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("History hangs on a closed subscription")
	}
}

func TestSessionHistoryIncompleteIsNotAcked(t *testing.T) {
	// no end marker: the transfer ends by idle timeout with 1 of 2 records
	f := historyScale(t, 2, rec1)
	got, _, err := testSession(f).History(context.Background(), 1)
	if !errors.Is(err, ErrIncompleteHistory) {
		t.Fatalf("got %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("records %+v", got)
	}
	want := []string{"2a2f:0101000000", "2a2f:02", "2a2f:03"}
	if w := f.written(); !equalStrings(w, want) {
		t.Fatalf("writes %v, want %v", w, want)
	}
}

func TestSessionHistoryEmpty(t *testing.T) {
	f := historyScale(t, 0)
	got, received, err := testSession(f).History(context.Background(), 1)
	if err != nil || len(got) != 0 || received != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
	want := []string{"2a2f:0101000000", "2a2f:03"}
	if w := f.written(); !equalStrings(w, want) {
		t.Fatalf("writes %v, want %v", w, want)
	}
}

func TestSessionHistoryDropsInvalidRecords(t *testing.T) {
	zero := "02a4e90701030a0000af010000"   // weight 0: reboot artifact
	future := "02a4eb0701010a0000af01fa4b" // 2027-01-01
	f := historyScale(t, 3, rec1+zero+future, "03")
	got, received, err := testSession(f).History(context.Background(), 1)
	if err != nil {
		t.Fatalf("filtered records still make a complete transfer: %v", err)
	}
	// the app acks by the raw count, so received includes dropped records
	if len(got) != 1 || got[0].Value != 97.25 || received != 3 {
		t.Fatalf("records %+v, received %d", got, received)
	}
}

func TestSessionOneFoot(t *testing.T) {
	f := newFakeScale(t)
	f.onWrite = func(f *fakeScale, char string, data []byte) {
		if char == CharControl && bytes.Equal(data, CmdOneFootStart()) {
			f.notify(CharControl, mustHex(t, "10060f0001"))
			f.notify(CharControl, mustHex(t, "10060f010a00"))
			f.notify(CharControl, mustHex(t, "10060f022c01"))
		}
	}
	var progress []OneFoot
	err := testSession(f).OneFoot(context.Background(), func(p OneFoot) { progress = append(progress, p) })
	if err != nil {
		t.Fatal(err)
	}
	want := []OneFoot{{Duration: time.Second}, {Done: true, Duration: 30 * time.Second}}
	if len(progress) != 2 || progress[0] != want[0] || progress[1] != want[1] {
		t.Fatalf("progress %+v", progress)
	}
	if w := f.written(); !equalStrings(w, []string{"1542:060f0000"}) {
		t.Fatalf("writes %v", w)
	}
}

func TestSessionLiveMeasurements(t *testing.T) {
	f := newFakeScale(t)
	s := testSession(f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := s.Live(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.notify(CharMeasurement, mustHex(t, "02a4ea070a01142202af01fa4b"))
	m := <-ch
	if m.Value != 97.25 || !m.Finished {
		t.Fatalf("got %+v", m)
	}
}
