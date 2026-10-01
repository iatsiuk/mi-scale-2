// Package store keeps profiles, measurements and settings in a JSON file.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"miscale/internal/bodycomp"
	"miscale/internal/scale"
)

type User struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Sex      bodycomp.Sex `json:"sex"`
	Birth    string       `json:"birth"` // YYYY-MM
	HeightCm int          `json:"height_cm"`
	WeightKg float64      `json:"weight_kg"` // profile weight, used for matching before the first record
}

// BirthYearMonth parses Birth; ok is false when it is not YYYY-MM[-DD].
func (u User) BirthYearMonth() (year, month int, ok bool) {
	t, err := time.Parse("2006-01", firstN(u.Birth, 7))
	if err != nil {
		return 0, 0, false
	}
	return t.Year(), int(t.Month()), true
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

type Kind string

const (
	KindScale   Kind = "scale"
	KindManual  Kind = "manual"
	KindBaby    Kind = "baby"
	KindBalance Kind = "balance"
)

type Origin string

const (
	OriginLive    Origin = "live"
	OriginHistory Origin = "history"
	OriginManual  Origin = "manual"
)

type Record struct {
	ID              int              `json:"id"`
	UserID          string           `json:"user_id,omitempty"` // empty: unassigned
	Time            time.Time        `json:"time"`
	Kind            Kind             `json:"kind"`
	Origin          Origin           `json:"origin"`
	WeightKg        float64          `json:"weight_kg,omitempty"`
	Unit            scale.Unit       `json:"unit"`
	Impedance       int              `json:"impedance,omitempty"` // raw value from the scale
	ImpedanceStable bool             `json:"impedance_stable,omitempty"`
	HeightCm        int              `json:"height_cm,omitempty"`
	Age             int              `json:"age,omitempty"`
	Composition     *bodycomp.Result `json:"composition,omitempty"`
	BalanceSeconds  float64          `json:"balance_seconds,omitempty"`
}

type Scale struct {
	ID     string `json:"id,omitempty"` // CoreBluetooth peripheral uuid
	Serial string `json:"serial,omitempty"`
}

type Data struct {
	Scale       Scale      `json:"scale"`
	UID         uint32     `json:"uid"` // history key on the scale
	Merge       bool       `json:"merge"`
	DisplayUnit scale.Unit `json:"display_unit"`
	Users       []User     `json:"users"`
	Records     []Record   `json:"records"`
	// Ignored holds unix seconds of weighings that must not be imported from
	// the scale history: guests, cancelled results, baby combined weighings,
	// deleted and merged records.
	Ignored []int64 `json:"ignored,omitempty"`
	NextID  int     `json:"next_id"`
}

func (d *Data) User(id string) (*User, error) {
	for i := range d.Users {
		if d.Users[i].ID == id || d.Users[i].Name == id {
			return &d.Users[i], nil
		}
	}
	return nil, fmt.Errorf("user %q not found", id)
}

func (d *Data) Record(id int) (*Record, error) {
	for i := range d.Records {
		if d.Records[i].ID == id {
			return &d.Records[i], nil
		}
	}
	return nil, fmt.Errorf("record %d not found", id)
}

// Add stores r with a new id and returns the stored copy.
func (d *Data) Add(r *Record) Record {
	d.NextID++
	r.ID = d.NextID
	d.Records = append(d.Records, *r)
	return *r
}

func (d *Data) Remove(id int) {
	d.removeRecords(func(r *Record) bool { return r.ID == id })
}

// removeRecords deletes matching records and returns how many were removed;
// deleted scale weighings are ignored so a sync of the unacked scale history
// cannot bring them back.
func (d *Data) removeRecords(match func(*Record) bool) int {
	out := d.Records[:0]
	for i := range d.Records {
		switch r := &d.Records[i]; {
		case !match(r):
			out = append(out, *r)
		case r.Kind == KindScale:
			d.Ignore(r.Time)
		}
	}
	removed := len(d.Records) - len(out)
	d.Records = out
	return removed
}

// RemoveUser deletes a user and their records; it returns the record count.
func (d *Data) RemoveUser(id string) int {
	users := d.Users[:0]
	for _, u := range d.Users {
		if u.ID != id {
			users = append(users, u)
		}
	}
	d.Users = users
	return d.removeRecords(func(r *Record) bool { return r.UserID == id })
}

func (d *Data) Ignore(t time.Time) { d.Ignored = append(d.Ignored, t.Unix()) }

// IsIgnored reports whether t must not be imported; the list is permanent
// like the app guest list, so repeated syncs without ack stay consistent.
func (d *Data) IsIgnored(t time.Time) bool {
	for _, s := range d.Ignored {
		if s == t.Unix() {
			return true
		}
	}
	return false
}

// DefaultPath is ~/Library/Application Support/miscale/data.json on macOS.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "miscale", "data.json"), nil
}

// Load reads path; a missing file gives empty data with a random history uid.
func Load(path string, newUID func() uint32) (*Data, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Data{UID: newUID()}, nil
	}
	if err != nil {
		return nil, err
	}
	var d Data
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &d, nil
}

// Save writes atomically through a temporary file.
func Save(path string, d *Data) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".data-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
