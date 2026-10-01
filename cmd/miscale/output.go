package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"miscale/internal/app"
	"miscale/internal/bodycomp"
	"miscale/internal/scale"
	"miscale/internal/store"
)

const (
	formatMarkdown = "markdown"
	formatJSON     = "json"
)

func validFormat(f string) error {
	if f != formatMarkdown && f != formatJSON {
		return fmt.Errorf("--format must be %s or %s", formatMarkdown, formatJSON)
	}
	return nil
}

// view is a command result that renders as markdown or json.
type view interface {
	markdown(w io.Writer)
}

func (e *env) render(v view) error {
	if e.format == formatJSON {
		enc := json.NewEncoder(e.out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	w := &errWriter{w: e.out}
	v.markdown(w)
	return w.err
}

// errWriter keeps the first write error so markdown views need no checks.
type errWriter struct {
	w   io.Writer
	err error
}

func (w *errWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.w.Write(p)
	w.err = err
	return n, err
}

func mdEscape(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

func mdTable(w io.Writer, header []string, rows [][]string) {
	line := func(cells []string) {
		esc := make([]string, len(cells))
		for i, c := range cells {
			esc[i] = mdEscape(c)
		}
		_, _ = fmt.Fprintf(w, "| %s |\n", strings.Join(esc, " | "))
	}
	line(header)
	sep := make([]string, len(header))
	for i := range sep {
		sep[i] = "---"
	}
	line(sep)
	for _, r := range rows {
		line(r)
	}
}

// fields is an ordered key/value result.
type fields []field

type field struct {
	Key   string
	Value any
}

func (f fields) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, kv := range f {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(kv.Key)
		v, err := json.Marshal(kv.Value)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

func (f fields) markdown(w io.Writer) {
	rows := make([][]string, len(f))
	for i, kv := range f {
		rows[i] = []string{kv.Key, fmt.Sprint(kv.Value)}
	}
	mdTable(w, []string{"field", "value"}, rows)
}

// metric is one body composition value with its standard.
type metric struct {
	Name     string  `json:"name"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit,omitempty"`
	Level    string  `json:"level,omitempty"`
	Standard string  `json:"standard,omitempty"`

	text string
}

type recordView struct {
	ID             int        `json:"id"`
	User           string     `json:"user,omitempty"`
	Time           time.Time  `json:"time"`
	Kind           store.Kind `json:"kind"`
	Origin         string     `json:"origin"`
	Weight         float64    `json:"weight,omitempty"`
	Unit           string     `json:"unit,omitempty"`
	WeightKg       float64    `json:"weight_kg,omitempty"`
	Impedance      int        `json:"impedance,omitempty"`
	BalanceSeconds float64    `json:"balance_seconds,omitempty"`
	BalanceLevel   int        `json:"balance_level,omitempty"`
	Metrics        []metric   `json:"metrics,omitempty"`
	Notes          []string   `json:"notes,omitempty"`

	weightText string
}

func round(v float64, digits int) float64 {
	f, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', digits, 64), 64)
	return f
}

func newRecordView(d *store.Data, r *store.Record, unit scale.Unit) *recordView {
	v := &recordView{ID: r.ID, Time: r.Time.Local(), Kind: r.Kind, Origin: string(r.Origin), Impedance: r.Impedance}
	if u, err := d.User(r.UserID); err == nil {
		v.User = u.Name
		if r.Kind == store.KindBalance {
			y, m, _ := u.BirthYearMonth()
			v.BalanceLevel = app.BalanceLevel(r.BalanceSeconds, bodycomp.AgeAt(y, m, r.Time), u.Sex)
		}
	}
	if r.Kind == store.KindBalance {
		v.BalanceSeconds = round(r.BalanceSeconds, 1)
		return v
	}
	v.WeightKg = r.WeightKg
	v.Weight, v.Unit, v.weightText = weightIn(r.WeightKg, unit)
	return v
}

// weightIn gives kg in unit u as a number and as the app display text;
// the number for stone is decimal because the text is st:lb.
func weightIn(kg float64, u scale.Unit) (value float64, unit, text string) {
	text = app.FormatWeight(kg, u)
	if u == scale.UnitSt {
		return round(app.KgTo(kg, u), 2), u.String(), text
	}
	value, _ = strconv.ParseFloat(strings.Fields(text)[0], 64)
	return value, u.String(), text
}

// withMetrics adds the detail screen values with their standards.
func (v *recordView) withMetrics(r *store.Record, u *store.User, unit scale.Unit) *recordView {
	c := r.Composition
	if c == nil {
		return v
	}
	male := u.Sex == bodycomp.Male
	v.Metrics = append(v.Metrics, metric{Name: "bmi", Value: round(float64(c.BMI), 1), Level: bmiLabel(bodycomp.BMILevel(c.BMI, r.Age))})
	if !c.HasComposition {
		v.Metrics = append(v.Metrics, massMetric("ideal_weight", c.StandardWeight, unit, ""))
		if r.Impedance > 0 && (r.Age < 6 || r.Age > 99 || r.HeightCm < 90 || r.HeightCm > 220) {
			v.Notes = append(v.Notes, "body composition needs age 6..99 and height 90..220 cm")
		}
		return v
	}
	w := float32(r.WeightKg)
	std := bodycomp.BMRStandard(w, r.Age, male)
	bmrLevel := "below standard"
	if c.BMR >= std {
		bmrLevel = "standard reached"
	}
	v.Metrics = append(v.Metrics,
		percent("body_fat", c.BodyFat, bodycomp.FatLevel(c.BodyFat, r.Age, male)),
		massMetric("muscle", c.Muscle, unit, bodycomp.MuscleLevel(c.Muscle, r.Age, r.HeightCm, male).String()),
		percent("water", c.Water, bodycomp.WaterLevel(c.Water, male)),
		percent("protein", c.Protein, bodycomp.ProteinLevel(c.Protein)),
		massMetric("bone_mass", c.Bone, unit, bodycomp.BoneLevel(c.Bone, w, r.Age, male).String()),
		metric{Name: "visceral_fat", Value: float64(c.Visceral), Level: bodycomp.VisceralLevel(c.Visceral).String()},
		metric{Name: "bmr", Value: float64(c.BMR), Unit: "kcal", Level: bmrLevel, Standard: strconv.Itoa(std)},
		metric{Name: "body_age", Value: float64(c.BodyAge), Standard: strconv.Itoa(r.Age)},
		massMetric("ideal_weight", c.StandardWeight, unit, ""),
		metric{Name: "body_type", Value: float64(c.BodyType), Level: c.BodyType.String()},
		metric{Name: "score", Value: float64(c.Score)},
	)
	if r.Age < 18 {
		v.Notes = append(v.Notes, "for minors it is recommended to pay attention to weight changes only")
	}
	return v
}

func percent(name string, v float32, l bodycomp.Level) metric {
	return metric{Name: name, Value: round(float64(bodycomp.Trunc(v, 1)), 1), Unit: "%", Level: l.String()}
}

func massMetric(name string, kg float32, unit scale.Unit, level string) metric {
	value, u, text := weightIn(float64(bodycomp.Trunc(kg, 2)), unit)
	return metric{Name: name, Value: value, Unit: u, Level: level, text: text}
}

func bmiLabel(l bodycomp.Level) string {
	switch l {
	case bodycomp.LevelLow:
		return "underweight"
	case bodycomp.LevelNormal:
		return "normal"
	case bodycomp.LevelSlightlyHigh:
		return "overweight"
	case bodycomp.LevelHigh:
		return "obese"
	}
	return ""
}

func (v *recordView) summary() string {
	if v.Kind == store.KindBalance {
		return fmt.Sprintf("%.1f s", v.BalanceSeconds)
	}
	return v.weightText
}

func (v *recordView) markdown(w io.Writer) {
	user := v.User
	if user == "" {
		user = "unassigned"
	}
	_, _ = fmt.Fprintf(w, "## Record %d: %s, %s\n\n", v.ID, user, v.Time.Format("2006-01-02 15:04"))
	rows := [][]string{{"kind", string(v.Kind), ""}, {"origin", v.Origin, ""}}
	if v.Kind == store.KindBalance {
		rows = append(rows, []string{"balance", v.summary(), balanceLevel(v.BalanceLevel)})
	} else {
		rows = append(rows, []string{"weight", v.summary(), ""})
	}
	for i := range v.Metrics {
		rows = append(rows, v.Metrics[i].row())
	}
	if v.Impedance > 0 {
		rows = append(rows, []string{"impedance", strconv.Itoa(v.Impedance) + " ohm", ""})
	}
	mdTable(w, []string{"metric", "value", "level"}, rows)
	for _, n := range v.Notes {
		_, _ = fmt.Fprintf(w, "\n> %s\n", n)
	}
}

// row is the markdown cells of a metric: name, display value, level.
func (m *metric) row() []string {
	value := strconv.FormatFloat(m.Value, 'f', -1, 64)
	level := m.Level
	switch {
	case m.Name == "body_type":
		value, level = m.Level, ""
	case m.text != "":
		value = m.text
	case m.Unit != "":
		value += " " + m.Unit
	}
	if m.Standard != "" {
		level = strings.TrimSpace(level + " (standard " + m.Standard + ")")
	}
	return []string{strings.ReplaceAll(m.Name, "_", " "), value, level}
}

type recordList []*recordView

func (l recordList) markdown(w io.Writer) {
	rows := make([][]string, 0, len(l))
	for _, v := range l {
		bmi, fat, score := "", "", ""
		for _, m := range v.Metrics {
			switch m.Name {
			case "bmi":
				bmi = strconv.FormatFloat(m.Value, 'f', 1, 64)
			case "body_fat":
				fat = strconv.FormatFloat(m.Value, 'f', 1, 64) + " %"
			case "score":
				score = strconv.FormatFloat(m.Value, 'f', 0, 64)
			}
		}
		user := v.User
		if user == "" {
			user = "unassigned"
		}
		rows = append(rows, []string{strconv.Itoa(v.ID), v.Time.Format("2006-01-02 15:04"), user, string(v.Kind), v.summary(), bmi, fat, score})
	}
	mdTable(w, []string{"id", "time", "user", "kind", "weight", "bmi", "fat", "score"}, rows)
}

// detailView renders the record with its metrics for its user.
func detailView(d *store.Data, r *store.Record) *recordView {
	v := newRecordView(d, r, d.DisplayUnit)
	if u, err := d.User(r.UserID); err == nil {
		v = v.withMetrics(r, u, d.DisplayUnit)
	}
	return v
}
