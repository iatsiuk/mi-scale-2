package app

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"miscale/internal/bodycomp"
	"miscale/internal/scale"
	"miscale/internal/store"
)

const (
	matchThresholdKg = 3.0
	mergeWindow      = 30 * time.Second
)

// LastWeight is the latest recorded weight of u, or the profile weight.
func LastWeight(d *store.Data, u store.User) float64 { //nolint:gocritic // users are small values everywhere
	var last *store.Record
	for i := range d.Records {
		r := &d.Records[i]
		if r.UserID != u.ID || r.WeightKg <= 0 || r.Kind == store.KindBalance {
			continue
		}
		if last == nil || r.Time.After(last.Time) {
			last = r
		}
	}
	if last != nil {
		return last.WeightKg
	}
	return u.WeightKg
}

func distance(d *store.Data, u store.User, kg float64) float32 {
	v := float32(float32(LastWeight(d, u)) - float32(kg))
	if v < 0 {
		return -v
	}
	return v
}

// Candidates lists all users sorted by distance to kg, like the user chooser.
func Candidates(d *store.Data, kg float64) []store.User {
	out := append([]store.User(nil), d.Users...)
	sort.SliceStable(out, func(i, j int) bool { return distance(d, out[i], kg) < distance(d, out[j], kg) })
	return out
}

// Match returns users whose last weight is less than 3 kg away (HMWeightUtils).
func Match(d *store.Data, kg float64) []store.User {
	var out []store.User
	for _, u := range d.Users {
		if distance(d, u, kg) < matchThresholdKg {
			out = append(out, u)
		}
	}
	return out
}

// Analyze fills height, age and composition of r for user u.
func Analyze(r *store.Record, u store.User) error {
	y, m, ok := u.BirthYearMonth()
	if !ok {
		y, m = 0, 0
	}
	r.HeightCm = u.HeightCm
	r.Age = bodycomp.AgeAt(y, m, r.Time)
	z := 0
	if r.ImpedanceStable {
		z = r.Impedance
	}
	res, err := bodycomp.Compute(bodycomp.Input{WeightKg: r.WeightKg, HeightCm: u.HeightCm, Age: r.Age, Sex: u.Sex, Impedance: z})
	r.Composition = &res
	var rangeErr *bodycomp.RangeError
	if errors.As(err, &rangeErr) {
		return nil // the app keeps weight and BMI
	}
	return err
}

func findByTime(d *store.Data, t time.Time) *store.Record {
	for i := range d.Records {
		if d.Records[i].Time.Equal(t) {
			return &d.Records[i]
		}
	}
	return nil
}

func newRecord(m scale.Measurement, origin store.Origin, withComposition bool) store.Record {
	r := store.Record{Time: m.Time, Kind: store.KindScale, Origin: origin, WeightKg: m.WeightKg(), Unit: m.Unit,
		Impedance: int(m.Impedance), ImpedanceStable: withComposition && m.ImpedanceStable}
	return r
}

// SaveMeasurement stores a final live measurement for userID. It is skipped
// when a record with the same timestamp exists. With merge enabled a scale
// record of the same user up to 30 s older is replaced. An empty userID
// keeps the record unassigned for Assign.
func SaveMeasurement(d *store.Data, m scale.Measurement, userID string, origin store.Origin, withComposition bool) (store.Record, bool, error) {
	if findByTime(d, m.Time) != nil {
		return store.Record{}, false, nil
	}
	if userID == "" {
		r := newRecord(m, origin, withComposition)
		return d.Add(&r), true, nil
	}
	u, err := d.User(userID)
	if err != nil {
		return store.Record{}, false, err
	}
	r := newRecord(m, origin, withComposition)
	r.UserID = u.ID
	if err := Analyze(&r, *u); err != nil {
		return store.Record{}, false, err
	}
	if d.Merge {
		removeMergedLive(d, &r)
	}
	return d.Add(&r), true, nil
}

// removeMergedLive drops scale records of r's user at most 30 s older.
func removeMergedLive(d *store.Data, r *store.Record) {
	var stale []int
	for i := range d.Records {
		old := &d.Records[i]
		if old.UserID == r.UserID && old.Kind == store.KindScale && !old.Time.After(r.Time) && r.Time.Sub(old.Time) <= mergeWindow {
			stale = append(stale, old.ID)
		}
	}
	for _, id := range stale {
		d.Remove(id)
	}
}

type ImportResult struct {
	Added      int
	Unassigned int
	Skipped    int
}

// ImportHistory merges offline records like MifitWeightDataSyncCallback.
// Users are matched against the data as it was before the batch. Records
// matching no user or several users are kept unassigned instead of being
// dropped; Assign completes them. Importing the same batch again is a no-op.
func ImportHistory(d *store.Data, ms []scale.Measurement, now time.Time) ImportResult {
	var res ImportResult
	batch := newHistoryRecords(d, ms, now)
	res.Skipped = len(ms) - len(batch)
	isNew := map[int]bool{}
	for i := range batch {
		isNew[d.Add(&batch[i]).ID] = true
		res.Added++
		if batch[i].UserID == "" {
			res.Unassigned++
		}
	}
	if d.Merge {
		for _, id := range mergeStale(d, isNew) {
			d.Remove(id)
			if isNew[id] {
				res.Added--
			}
		}
	}
	return res
}

// newHistoryRecords builds records for importable measurements, matched
// against the current data.
func newHistoryRecords(d *store.Data, ms []scale.Measurement, now time.Time) []store.Record {
	var batch []store.Record
	seen := map[time.Time]bool{}
	for _, m := range ms {
		y := m.Time.Local().Year()
		if y < 2014 || y > now.Local().Year() || seen[m.Time] || d.IsIgnored(m.Time) || findByTime(d, m.Time) != nil {
			continue
		}
		seen[m.Time] = true
		r := newRecord(m, store.OriginHistory, true)
		if users := Match(d, r.WeightKg); len(users) == 1 {
			r.UserID = users[0].ID
			_ = Analyze(&r, users[0]) // profiles are validated, a range error keeps weight and BMI
		}
		batch = append(batch, r)
	}
	return batch
}

// mergeStale returns earlier records of a user that have a later scale
// record within 30 s, looking at new records and the stored ones around them.
func mergeStale(d *store.Data, isNew map[int]bool) []int {
	var stale []int
	for _, g := range mergeGroups(d, isNew) {
		sort.Slice(g, func(i, j int) bool { return g[i].Time.Before(g[j].Time) })
		for i := 0; i+1 < len(g); i++ {
			if g[i+1].Time.Sub(g[i].Time) <= mergeWindow {
				stale = append(stale, g[i].ID)
			}
		}
	}
	return stale
}

type mergeItem struct {
	ID   int
	Time time.Time
}

// mergeGroups collects per user the scale records within 30 s of a new one.
func mergeGroups(d *store.Data, isNew map[int]bool) map[string][]mergeItem {
	var newRecs []*store.Record
	for i := range d.Records {
		if r := &d.Records[i]; isNew[r.ID] && r.UserID != "" {
			newRecs = append(newRecs, r)
		}
	}
	groups := map[string][]mergeItem{}
	for i := range d.Records {
		r := &d.Records[i]
		if r.UserID == "" || r.Kind != store.KindScale {
			continue
		}
		for _, n := range newRecs {
			if n.UserID == r.UserID && r.Time.Sub(n.Time).Abs() <= mergeWindow {
				groups[r.UserID] = append(groups[r.UserID], mergeItem{r.ID, r.Time})
				break
			}
		}
	}
	return groups
}

// Assign gives a record to a user and recomputes its composition.
func Assign(d *store.Data, recordID int, userID string) error {
	r, err := d.Record(recordID)
	if err != nil {
		return err
	}
	u, err := d.User(userID)
	if err != nil {
		return err
	}
	r.UserID = u.ID
	if r.Kind == store.KindBalance {
		return nil
	}
	return Analyze(r, *u)
}

// BabyWeight is the difference of an adult weighing and adult+baby, rounded
// to 0.1 in the scale unit and converted to kg; the record time is one second
// after the later weighing.
func BabyWeight(adult, both scale.Measurement) (float64, time.Time) {
	diff := float32(float32(both.Value) - float32(adult.Value))
	v := math.Abs(scale.RoundHalfUp(float64(diff), 1))
	at := both.Time
	if adult.Time.After(at) {
		at = adult.Time
	}
	return scale.ToKg(v, both.Unit), at.Add(time.Second)
}

// BalanceLevel is OneFootUtils: level 1..5, or -1 outside ages 20..69.
func BalanceLevel(seconds float64, age int, sex bodycomp.Sex) int {
	if age < 20 || age >= 70 {
		return -1
	}
	female := [][]float32{{0, 6, 16, 37, 90}, {0, 6, 15, 33, 85}, {0, 5, 13, 29, 73}, {0, 4, 10, 24, 63}, {0, 4, 8, 19, 46},
		{0, 3, 7, 16, 40}, {0, 3, 6, 14, 34}, {0, 3, 6, 11, 27}, {0, 3, 6, 13, 41}, {0, 3, 5, 11, 36}}
	male := [][]float32{{0, 6, 18, 42, 99}, {0, 6, 15, 36, 86}, {0, 5, 13, 30, 75}, {0, 4, 12, 28, 70}, {0, 4, 10, 22, 55},
		{0, 4, 9, 20, 49}, {0, 5, 8, 17, 40}, {0, 3, 7, 14, 34}, {0, 4, 7, 15, 49}, {0, 3, 6, 13, 41}}
	t := female[(age-20)/5]
	if sex == bodycomp.Male {
		t = male[(age-20)/5]
	}
	s := float32(seconds)
	level := -1
	for i, lo := range t {
		if s >= lo {
			level = i + 1
		}
	}
	return level
}

// KgTo converts kg for display with the UI-layer factors of HMWeightUtils.
func KgTo(kg float64, u scale.Unit) float64 {
	switch u {
	case scale.UnitLb:
		return kg / 0.45359236
	case scale.UnitJin:
		return kg * 2
	case scale.UnitSt:
		return kg / 6.350293
	}
	return kg
}

// FormatWeight mirrors HMWeightUtils display: kg with 2 decimals below 100,
// lb and jin with 1, stone as st:lb.
func FormatWeight(kg float64, u scale.Unit) string {
	f := float32(kg)
	if u == scale.UnitSt {
		lb := int(float32(KgTo(float64(f), scale.UnitLb)) + 0.5)
		return fmt.Sprintf("%d:%d st", lb/14, lb%14)
	}
	digits := 1
	if u == scale.UnitKg && f >= 0.1 && f < 100 || u == scale.UnitKg && f <= -0.1 && f > -100 {
		digits = 2
	}
	v := float32(KgTo(float64(f), u))
	return strconv.FormatFloat(float64(v), 'f', digits, 64) + " " + u.String()
}
