package app

import (
	"testing"
	"time"

	"miscale/internal/bodycomp"
	"miscale/internal/scale"
	"miscale/internal/store"
)

func family() *store.Data {
	return &store.Data{Users: []store.User{
		{ID: "dad", Name: "dad", Sex: bodycomp.Male, Birth: "1984-03", HeightCm: 181, WeightKg: 95},
		{ID: "mom", Name: "mom", Sex: bodycomp.Female, Birth: "1987-11", HeightCm: 165, WeightKg: 60},
		{ID: "kid", Name: "kid", Sex: bodycomp.Male, Birth: "2022-06", HeightCm: 95, WeightKg: 14},
	}}
}

func ids(us []store.User) []string {
	out := make([]string, 0, len(us))
	for _, u := range us {
		out = append(out, u.ID)
	}
	return out
}

func TestMatchUsesLastRecordThenProfileWeight(t *testing.T) {
	d := family()
	d.Add(&store.Record{UserID: "mom", Time: t0, Kind: store.KindScale, WeightKg: 63.5})
	tests := []struct {
		kg   float64
		want []string
	}{
		{97.25, []string{"dad"}},
		{61.5, []string{"mom"}},
		{66.4, []string{"mom"}}, // last record 63.5 + 2.9, profile weight 60 would not match
		{66.5, nil},             // exactly 3.0 is not a match
		{14.5, []string{"kid"}},
	}
	for _, tt := range tests {
		if got := ids(Match(d, tt.kg)); !equalStrings(got, tt.want) {
			t.Errorf("Match(%v) = %v, want %v", tt.kg, got, tt.want)
		}
	}
}

func TestCandidatesSortedByDistance(t *testing.T) {
	d := family()
	if got := ids(Candidates(d, 62)); !equalStrings(got, []string{"mom", "dad", "kid"}) {
		t.Fatalf("got %v", got)
	}
}

func equalStrings(a, b []string) bool {
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

func live(sec int, kg float64, z uint16, impStable bool) scale.Measurement {
	return scale.Measurement{Unit: scale.UnitKg, Value: kg, Stable: true, ImpedanceStable: impStable, Impedance: z,
		Time: t0.Add(time.Duration(sec) * time.Second)}
}

func TestSaveMeasurementComputesComposition(t *testing.T) {
	d := family()
	r, ok, err := SaveMeasurement(d, live(0, 97.25, 431, true), "dad", store.OriginLive, true)
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if r.Composition == nil || !r.Composition.HasComposition || r.Age != 42 || r.HeightCm != 181 {
		t.Fatalf("record %+v", r)
	}
	want, _ := bodycomp.Compute(bodycomp.Input{WeightKg: 97.25, HeightCm: 181, Age: 42, Sex: bodycomp.Male, Impedance: 431})
	if *r.Composition != want {
		t.Fatalf("composition %+v, want %+v", *r.Composition, want)
	}
	if _, ok, _ := SaveMeasurement(d, live(0, 97.25, 431, true), "dad", store.OriginLive, true); ok {
		t.Fatal("same timestamp must not be saved twice")
	}
}

func TestSaveMeasurementWeightOnly(t *testing.T) {
	d := family()
	r, _, err := SaveMeasurement(d, live(0, 97.25, scale.ImpedanceFailed, false), "dad", store.OriginLive, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Composition == nil || r.Composition.HasComposition || r.Composition.BMI != 29.6 {
		t.Fatalf("weight-only record must keep BMI: %+v", r.Composition)
	}
}

func TestSaveMeasurementMergesWithin30s(t *testing.T) {
	d := family()
	d.Merge = true
	first, _, _ := SaveMeasurement(d, live(0, 97.25, 431, true), "dad", store.OriginLive, true)
	second, _, _ := SaveMeasurement(d, live(30, 97.3, 432, true), "dad", store.OriginLive, true)
	if _, err := d.Record(first.ID); err == nil {
		t.Fatal("earlier record within 30 s must be replaced")
	}
	if _, _, err := SaveMeasurement(d, live(61, 97.4, 433, true), "dad", store.OriginLive, true); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Record(second.ID); err != nil {
		t.Fatal("record 31 s earlier must stay")
	}
}

func TestImportHistory(t *testing.T) {
	d := family()
	now := t0.Add(time.Hour)
	existing := live(-60, 60.1, 500, true)
	d.Add(&store.Record{UserID: "mom", Time: existing.Time, Kind: store.KindScale, Origin: store.OriginLive, WeightKg: 60.1})
	guest := live(-30, 80, 500, true)
	d.Ignore(guest.Time)
	old := live(0, 70, 500, true)
	old.Time = time.Date(2013, 5, 1, 0, 0, 0, 0, time.UTC)

	res := ImportHistory(d, []scale.Measurement{
		live(-120, 96.9, 430, true), // dad
		existing,                    // already stored
		guest,                       // ignored guest weighing
		old,                         // invalid year
		live(-90, 76, 500, true),    // nobody is within 3 kg
	}, now)
	if res.Added != 2 || res.Unassigned != 1 || res.Skipped != 3 {
		t.Fatalf("result %+v", res)
	}
	if !d.IsIgnored(guest.Time) {
		t.Fatal("ignored timestamps are permanent like the app guest list")
	}
	var unassigned *store.Record
	for i := range d.Records {
		if d.Records[i].UserID == "" {
			unassigned = &d.Records[i]
		}
	}
	if unassigned == nil || unassigned.Composition != nil || unassigned.Impedance != 500 {
		t.Fatalf("unassigned record %+v", unassigned)
	}
	if err := Assign(d, unassigned.ID, "dad"); err != nil {
		t.Fatal(err)
	}
	if unassigned.Composition == nil || !unassigned.Composition.HasComposition {
		t.Fatalf("assign must compute composition: %+v", unassigned)
	}
}

func TestImportHistoryMergesWithin30s(t *testing.T) {
	d := family()
	d.Merge = true
	res := ImportHistory(d, []scale.Measurement{live(0, 96.9, 430, true), live(20, 97.0, 431, true)}, t0.Add(time.Hour))
	if res.Added != 1 || len(d.Records) != 1 || d.Records[0].WeightKg != 97.0 {
		t.Fatalf("result %+v records %+v", res, d.Records)
	}
}

func TestImportHistoryRepeatedBatchIsIdempotent(t *testing.T) {
	d := family()
	d.Merge = true
	guest := live(-300, 80, 500, true)
	d.Ignore(guest.Time)
	batch := []scale.Measurement{guest, live(0, 96.9, 430, true), live(20, 97.0, 431, true)}
	now := t0.Add(time.Hour)
	ImportHistory(d, batch, now)
	res := ImportHistory(d, batch, now) // e.g. after sync -no-ack
	if len(d.Records) != 1 || d.Records[0].WeightKg != 97.0 || res.Added != 0 {
		t.Fatalf("result %+v records %+v", res, d.Records)
	}
}

func TestImportHistoryMatchesAgainstStateBeforeBatch(t *testing.T) {
	d := &store.Data{Users: []store.User{{ID: "a", Name: "a", Sex: bodycomp.Male, Birth: "1990-01", HeightCm: 180, WeightKg: 70}}}
	res := ImportHistory(d, []scale.Measurement{live(0, 72, 500, true), live(60, 74, 500, true)}, t0.Add(time.Hour))
	if res.Added != 2 || res.Unassigned != 1 {
		t.Fatalf("result %+v", res)
	}
	for _, r := range d.Records {
		if r.WeightKg == 74 && r.UserID != "" {
			t.Fatalf("74 kg is 4 kg from the stored weight and must stay unassigned: %+v", r)
		}
	}
}

func TestBabyWeight(t *testing.T) {
	adult := live(0, 60.05, 0, false)
	both := live(40, 67.6, 0, false)
	// 67.6f - 60.05f = 7.5499992 and BigDecimal(float) rounds the binary value
	kg, at := BabyWeight(adult, both)
	if kg != 7.5 || !at.Equal(both.Time.Add(time.Second)) {
		t.Fatalf("got %v %v", kg, at)
	}
	lbAdult := scale.Measurement{Unit: scale.UnitLb, Value: 132.4, Time: t0}
	lbBoth := scale.Measurement{Unit: scale.UnitLb, Value: 149.2, Time: t0.Add(time.Minute)}
	if kg, _ := BabyWeight(lbAdult, lbBoth); kg != 7.62 {
		t.Fatalf("lb baby: %v", kg)
	}
}

func TestBalanceLevel(t *testing.T) {
	tests := []struct {
		sec  float64
		age  int
		sex  bodycomp.Sex
		want int
	}{
		{0, 30, bodycomp.Male, 1},
		{12.9, 30, bodycomp.Male, 2},
		{13, 30, bodycomp.Male, 3},
		{75, 30, bodycomp.Male, 5},
		{90, 22, bodycomp.Female, 5},
		{89.9, 22, bodycomp.Female, 4},
		{30, 19, bodycomp.Male, -1},
		{30, 70, bodycomp.Male, -1},
	}
	for _, tt := range tests {
		if got := BalanceLevel(tt.sec, tt.age, tt.sex); got != tt.want {
			t.Errorf("BalanceLevel(%v, %d, %d) = %d, want %d", tt.sec, tt.age, tt.sex, got, tt.want)
		}
	}
}

func TestFormatWeight(t *testing.T) {
	tests := []struct {
		kg   float64
		u    scale.Unit
		want string
	}{
		{97.25, scale.UnitKg, "97.25 kg"},
		{100.4, scale.UnitKg, "100.4 kg"},
		{97.25, scale.UnitLb, "214.4 lb"},
		{97.25, scale.UnitJin, "194.5 jin"},
		{97.25, scale.UnitSt, "15:4 st"},
	}
	for _, tt := range tests {
		if got := FormatWeight(tt.kg, tt.u); got != tt.want {
			t.Errorf("FormatWeight(%v, %v) = %q, want %q", tt.kg, tt.u, got, tt.want)
		}
	}
}

func TestImportHistoryReplayAfterMergeKeepsMergedRecordOut(t *testing.T) {
	d := &store.Data{Merge: true, Users: []store.User{{ID: "a", Name: "a", Sex: bodycomp.Male, Birth: "1990-01", HeightCm: 180, WeightKg: 70}}}
	batch := []scale.Measurement{live(0, 68, 500, true), live(20, 72, 500, true)}
	now := t0.Add(time.Hour)
	ImportHistory(d, batch, now)
	// the last weight is now 72, so 68 would no longer match the user
	res := ImportHistory(d, batch, now)
	if res.Added != 0 || res.Unassigned != 0 || len(d.Records) != 1 || d.Records[0].WeightKg != 72 {
		t.Fatalf("result %+v records %+v", res, d.Records)
	}
}

func TestImportHistoryAfterLiveMergeKeepsMergedRecordOut(t *testing.T) {
	d := family()
	d.Merge = true
	first := live(0, 92.5, 431, true)
	if _, _, err := SaveMeasurement(d, first, "dad", store.OriginLive, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SaveMeasurement(d, live(20, 97.25, 432, true), "dad", store.OriginLive, true); err != nil {
		t.Fatal(err)
	}
	// the scale history still holds the first weighing, 4.75 kg from the last one
	res := ImportHistory(d, []scale.Measurement{first}, t0.Add(time.Hour))
	if res.Added != 0 || len(d.Records) != 1 {
		t.Fatalf("result %+v records %+v", res, d.Records)
	}
}

func TestSaveMeasurementUnassigned(t *testing.T) {
	d := family()
	d.Merge = true
	if _, _, err := SaveMeasurement(d, live(0, 97.25, 431, true), "dad", store.OriginLive, true); err != nil {
		t.Fatal(err)
	}
	r, ok, err := SaveMeasurement(d, live(20, 97.3, 432, true), "", store.OriginLive, true)
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if r.UserID != "" || r.Composition != nil || !r.ImpedanceStable || len(d.Records) != 2 {
		t.Fatalf("unassigned record must keep the impedance and merge nothing: %+v", d.Records)
	}
	if err := Assign(d, r.ID, "dad"); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.Record(r.ID); got.Composition == nil || !got.Composition.HasComposition {
		t.Fatalf("assign must compute composition: %+v", got)
	}
}

func TestImportHistoryAfterRemoveKeepsRecordOut(t *testing.T) {
	d := family()
	m := live(0, 95.2, 431, true)
	r, _, err := SaveMeasurement(d, m, "dad", store.OriginLive, true)
	if err != nil {
		t.Fatal(err)
	}
	d.Remove(r.ID)
	// the scale history still holds the deleted weighing without an ack
	res := ImportHistory(d, []scale.Measurement{m}, t0.Add(time.Hour))
	if res.Added != 0 || len(d.Records) != 0 {
		t.Fatalf("result %+v records %+v", res, d.Records)
	}
}
