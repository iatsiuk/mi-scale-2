// Package scale implements the Mi Body Composition Scale 2 (XMTZC05HM) wire
// protocol as used by Zepp Life 6.16.1.
package scale

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// Unit is the scale display unit, using Zepp Life unit codes.
type Unit int

const (
	UnitKg  Unit = 0
	UnitLb  Unit = 1
	UnitJin Unit = 16
	UnitSt  Unit = 17
)

func (u Unit) String() string {
	switch u {
	case UnitKg:
		return "kg"
	case UnitLb:
		return "lb"
	case UnitJin:
		return "jin"
	case UnitSt:
		return "st"
	}
	return fmt.Sprintf("unit(%d)", int(u))
}

// factor is the amount of the unit per 1 kg (WeightUnitUtils.kt).
func (u Unit) factor() float32 {
	switch u {
	case UnitLb:
		return 2.20462
	case UnitJin:
		return 2
	case UnitSt:
		return 0.15747304
	}
	return 1
}

const (
	// ImpedanceFailed is reported when the scale could not measure impedance.
	ImpedanceFailed  uint16 = 65533
	impedanceInvalid uint16 = 65534
	overloadRaw      uint16 = 65520
	measurementSize         = 13
)

// Measurement is a 13-byte 0x181B service data / 0x2A9C / history record.
type Measurement struct {
	Unit            Unit
	Value           float64 // in Unit; 65520 when Overload
	Stable          bool
	ImpedanceStable bool
	Finished        bool // weight removed from the scale
	Part            bool
	Overload        bool
	Impedance       uint16
	Time            time.Time // scale clock runs in UTC
}

// ParseMeasurement decodes the layout from WeightBfsProfile.
func ParseMeasurement(b []byte) (Measurement, error) {
	if len(b) < measurementSize {
		return Measurement{}, fmt.Errorf("scale: measurement is %d bytes, want %d", len(b), measurementSize)
	}
	raw := binary.LittleEndian.Uint16(b[11:13])
	m := Measurement{
		Stable:          b[1]&0x20 != 0,
		ImpedanceStable: b[1]&0x02 != 0,
		Finished:        b[1]&0x80 != 0,
		Part:            b[0]&0x04 != 0,
		Impedance:       binary.LittleEndian.Uint16(b[9:11]),
		Time: time.Date(int(binary.LittleEndian.Uint16(b[2:4])), time.Month(b[4]), int(b[5]),
			int(b[6]), int(b[7]), int(b[8]), 0, time.UTC),
	}
	switch {
	case raw == overloadRaw:
		m.Unit, m.Value, m.Overload, m.Stable = UnitKg, float64(raw), true, false
	case b[1]&0x40 != 0:
		m.Unit, m.Value = UnitJin, float64(raw)/100
	case b[0]&0x01 != 0:
		m.Unit, m.Value = UnitLb, float64(raw)/100
	default:
		m.Unit, m.Value = UnitKg, float64(raw)/200
	}
	return m, nil
}

// WeightKg converts Value to kg like WeightUnitUtils.kt: float32 division,
// then BigDecimal(float).setScale(2, HALF_UP).
func (m Measurement) WeightKg() float64 {
	return ToKg(m.Value, m.Unit)
}

// ToKg converts a weight in unit u to kg with app rounding.
func ToKg(v float64, u Unit) float64 {
	return RoundHalfUp(float64(float32(v)/u.factor()), 2)
}

// FromKg converts kg to unit u with app rounding.
func FromKg(kg float64, u Unit) float64 {
	return RoundHalfUp(float64(float32(kg)*u.factor()), 2)
}

// HasImpedance mirrors the gate before body composition is computed.
func (m Measurement) HasImpedance() bool {
	return m.Impedance > 0 && m.Impedance != ImpedanceFailed && m.Impedance != impedanceInvalid
}

// ValidHistory mirrors HMWeightSyncDataTask: drop zero weights (scale reboot),
// years before 2014 and timestamps more than a day in the future.
func (m Measurement) ValidHistory(now time.Time) bool {
	if m.Value == 0 || m.Time.Local().Year() < 2014 {
		return false
	}
	return !m.Time.After(now.Add(24 * time.Hour))
}

// RoundHalfUp rounds the exact binary value of x, like java BigDecimal(double).
func RoundHalfUp(x float64, places int) float64 {
	r, _ := new(big.Float).SetFloat64(x).Rat(nil)
	scale := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil))
	r.Mul(r, scale)
	neg := r.Sign() < 0
	r.Abs(r)
	half := big.NewRat(1, 2)
	r.Add(r, half)
	q := new(big.Int).Quo(r.Num(), r.Denom())
	if neg {
		q.Neg(q)
	}
	f, _ := new(big.Rat).SetFrac(q, scale.Num()).Float64()
	return f
}

var errShort = errors.New("scale: short payload")
