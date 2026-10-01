package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileCreatesUID(t *testing.T) {
	d, err := Load(filepath.Join(t.TempDir(), "data.json"), func() uint32 { return 42 })
	if err != nil {
		t.Fatal(err)
	}
	if d.UID != 42 || len(d.Users) != 0 {
		t.Fatalf("got %+v", d)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "data.json")
	d := &Data{UID: 7, Merge: true, Users: []User{{ID: "u1", Name: "anna", Birth: "1990-05", HeightCm: 165}}}
	at := time.Date(2026, 10, 1, 20, 34, 2, 0, time.UTC)
	r := d.Add(&Record{UserID: "u1", Time: at, Kind: KindScale, Origin: OriginLive, WeightKg: 60.5})
	d.Ignore(at.Add(time.Minute))
	if err := Save(path, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path, func() uint32 { t.Fatal("uid must come from the file"); return 0 })
	if err != nil {
		t.Fatal(err)
	}
	rec, err := got.Record(r.ID)
	if err != nil || rec.WeightKg != 60.5 || !rec.Time.Equal(at) {
		t.Fatalf("record %+v, %v", rec, err)
	}
	if !got.IsIgnored(at.Add(time.Minute)) || got.IsIgnored(at) {
		t.Fatal("IsIgnored")
	}
	if u, err := got.User("anna"); err != nil || u.ID != "u1" {
		t.Fatalf("user by name: %+v, %v", u, err)
	}
}

func TestBirthYearMonth(t *testing.T) {
	for _, s := range []string{"1990-05", "1990-05-12"} {
		y, m, ok := User{Birth: s}.BirthYearMonth()
		if !ok || y != 1990 || m != 5 {
			t.Errorf("%s: %d %d %v", s, y, m, ok)
		}
	}
	if _, _, ok := (User{Birth: "May 1990"}).BirthYearMonth(); ok {
		t.Error("expected failure")
	}
}

func TestRemoveIgnoresScaleRecords(t *testing.T) {
	at := time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name    string
		remove  func(d *Data)
		ignored []time.Time
		kept    int
	}{
		{"record", func(d *Data) { d.Remove(1); d.Remove(2) }, []time.Time{at}, 1},
		{"user", func(d *Data) { d.RemoveUser("u1") }, []time.Time{at, at.Add(time.Hour)}, 0},
	} {
		d := &Data{Users: []User{{ID: "u1", Name: "anna"}}}
		d.Add(&Record{UserID: "u1", Time: at, Kind: KindScale})
		d.Add(&Record{UserID: "u1", Time: at.Add(time.Minute), Kind: KindManual})
		d.Add(&Record{UserID: "u1", Time: at.Add(time.Hour), Kind: KindScale})
		tt.remove(d)
		if len(d.Records) != tt.kept || len(d.Ignored) != len(tt.ignored) {
			t.Fatalf("%s: records %+v ignored %v", tt.name, d.Records, d.Ignored)
		}
		for _, ts := range tt.ignored {
			if !d.IsIgnored(ts) {
				t.Errorf("%s: a deleted scale weighing %v must not come back from the history", tt.name, ts)
			}
		}
	}
}
