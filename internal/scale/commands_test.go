package scale

import (
	"bytes"
	"testing"
	"time"
)

func TestEncodeTime(t *testing.T) {
	// 2026-10-04 is a sunday: java Calendar.DAY_OF_WEEK = 1
	moscow := time.FixedZone("MSK", 3*3600)
	got := EncodeTime(time.Date(2026, 10, 4, 15, 4, 5, 0, moscow))
	want := mustHex(t, "ea070a040c0405010000")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x, want %x", got, want)
	}
}

func TestDecodeTime(t *testing.T) {
	// captured from a real XMTZC05HM 0x2A2B read
	got, err := DecodeTime(mustHex(t, "ea070a01150a0c000000"))
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 1, 21, 10, 12, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := DecodeTime(make([]byte, 9)); err == nil {
		t.Fatal("expected error for 9 bytes")
	}
}

func TestControlCommands(t *testing.T) {
	tests := []struct {
		name string
		got  []byte
		want string
	}{
		{"unit kg", CmdSetUnit(UnitKg), "06040000"},
		{"unit lb", CmdSetUnit(UnitLb), "06040001"},
		{"unit jin", CmdSetUnit(UnitJin), "06040002"},
		{"user mode", CmdUserMode(), "060b0000"},
		{"erase history", CmdEraseHistory(), "06120000"},
		{"part measure on", CmdPartMeasure(true), "06100000"},
		{"part measure off", CmdPartMeasure(false), "06100001"},
		{"one foot start", CmdOneFootStart(), "060f0000"},
		{"one foot stop", CmdOneFootStop(), "06110000"},
	}
	for _, tt := range tests {
		if !bytes.Equal(tt.got, mustHex(t, tt.want)) {
			t.Errorf("%s: got %x, want %s", tt.name, tt.got, tt.want)
		}
	}
}

func TestParseResponse(t *testing.T) {
	r, err := ParseResponse(mustHex(t, "1006120001"))
	if err != nil {
		t.Fatal(err)
	}
	if r != (Response{Group: 6, Command: 0x12, Status: StatusSuccess}) || !r.OK() {
		t.Fatalf("got %+v", r)
	}
	r, err = ParseResponse(mustHex(t, "1006100003"))
	if err != nil {
		t.Fatal(err)
	}
	if r.OK() || r.Status != StatusUnknownCommand {
		t.Fatalf("got %+v", r)
	}
	for _, bad := range []string{"10061200", "1106120001"} {
		if _, err := ParseResponse(mustHex(t, bad)); err == nil {
			t.Errorf("%s: expected error", bad)
		}
	}
}

func TestParseMode(t *testing.T) {
	// captured from a real XMTZC05HM 0x1542 read
	m, err := ParseMode(mustHex(t, "0301000000000000000000000000000000000000"))
	if err != nil {
		t.Fatal(err)
	}
	if m != 0x0103 || m.NeedsUserMode() {
		t.Fatalf("mode %#x, NeedsUserMode %v", uint16(m), m.NeedsUserMode())
	}
	if !Mode(3).NeedsUserMode() {
		t.Fatal("mode 3 must need user mode")
	}
}

func TestParsePnP(t *testing.T) {
	// captured from a real XMTZC05HM 0x2A50 read
	p, err := ParsePnP(mustHex(t, "01570119000001"))
	if err != nil {
		t.Fatal(err)
	}
	want := PnP{VendorIDSource: 1, VendorID: 0x0157, ProductID: 25, ProductVersion: 0x0100}
	if p != want {
		t.Fatalf("got %+v, want %+v", p, want)
	}
	if p.Model() != ModelBodyCompositionScale2 {
		t.Fatalf("model %v", p.Model())
	}
}

func TestHistoryCommands(t *testing.T) {
	const uid = 0x0a0b0c0d
	if got := CmdHistoryCount(uid); !bytes.Equal(got, mustHex(t, "010d0c0b0a")) {
		t.Errorf("count: %x", got)
	}
	if got := CmdHistoryAck(uid); !bytes.Equal(got, mustHex(t, "040d0c0b0a")) {
		t.Errorf("ack: %x", got)
	}
	if got := CmdHistoryFetch(); !bytes.Equal(got, []byte{2}) {
		t.Errorf("fetch: %x", got)
	}
	if got := CmdHistoryStop(); !bytes.Equal(got, []byte{3}) {
		t.Errorf("stop: %x", got)
	}
}

func TestParseHistoryCount(t *testing.T) {
	n, err := ParseHistoryCount(mustHex(t, "010500"))
	if err != nil || n != 5 {
		t.Fatalf("got %d, %v", n, err)
	}
	for _, bad := range []string{"0105", "020500"} {
		if _, err := ParseHistoryCount(mustHex(t, bad)); err == nil {
			t.Errorf("%s: expected error", bad)
		}
	}
}

func TestParseHistoryRecords(t *testing.T) {
	rec := "02a4ea070a01142202af01fa4b"
	data := mustHex(t, rec+rec+"0200ea07")
	got := ParseHistoryRecords(data)
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2 (tail ignored)", len(got))
	}
	if got[1].Value != 97.25 || got[1].Impedance != 431 {
		t.Fatalf("record %+v", got[1])
	}
	if !IsHistoryEnd([]byte{3}) || !IsHistoryEnd(nil) || IsHistoryEnd(data) {
		t.Fatal("IsHistoryEnd")
	}
}

func TestHistoryRecordValid(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		m    Measurement
		want bool
	}{
		{"ok", Measurement{Value: 70, Time: now.Add(-time.Hour)}, true},
		{"zero weight after reboot", Measurement{Value: 0, Time: now}, false},
		{"year before 2014", Measurement{Value: 70, Time: time.Date(2013, 12, 31, 0, 0, 0, 0, time.UTC)}, false},
		{"more than a day ahead", Measurement{Value: 70, Time: now.Add(24*time.Hour + time.Second)}, false},
		{"exactly a day ahead", Measurement{Value: 70, Time: now.Add(24 * time.Hour)}, true},
	}
	for _, tt := range tests {
		if got := tt.m.ValidHistory(now); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestParseOneFoot(t *testing.T) {
	got, err := ParseOneFoot(mustHex(t, "10060f022c01"))
	if err != nil {
		t.Fatal(err)
	}
	if got != (OneFoot{Done: true, Duration: 30 * time.Second}) {
		t.Fatalf("got %+v", got)
	}
	if _, err := ParseOneFoot(mustHex(t, "1006120001")); err == nil {
		t.Fatal("expected error for a 5-byte response")
	}
}
