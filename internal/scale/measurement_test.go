package scale

import (
	"encoding/hex"
	"testing"
	"time"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseMeasurementFinishedKg(t *testing.T) {
	// captured from a real XMTZC05HM advertisement
	m, err := ParseMeasurement(mustHex(t, "02a4ea070a01142202af01fa4b"))
	if err != nil {
		t.Fatal(err)
	}
	want := Measurement{
		Unit:            UnitKg,
		Value:           97.25,
		Stable:          true,
		ImpedanceStable: false,
		Finished:        true,
		Impedance:       431,
		Time:            time.Date(2026, 10, 1, 20, 34, 2, 0, time.UTC),
	}
	if m != want {
		t.Fatalf("got %+v\nwant %+v", m, want)
	}
	if kg := m.WeightKg(); kg != 97.25 {
		t.Fatalf("WeightKg = %v", kg)
	}
}

func TestParseMeasurementFlags(t *testing.T) {
	tests := []struct {
		name string
		hex  string
		want Measurement
	}{
		{
			name: "impedance stable",
			hex:  "0226ea070a01142202af01fa4b",
			want: Measurement{Unit: UnitKg, Value: 97.25, Stable: true, ImpedanceStable: true, Impedance: 431},
		},
		{
			name: "measuring, not stable",
			hex:  "0200ea070a0114220200000a32",
			want: Measurement{Unit: UnitKg, Value: 64.05},
		},
		{
			name: "pounds",
			hex:  "0320ea070a011422020000bf53",
			want: Measurement{Unit: UnitLb, Value: 214.39, Stable: true},
		},
		{
			name: "jin wins over pound bit",
			hex:  "0360ea070a011422020000fa4b",
			want: Measurement{Unit: UnitJin, Value: 194.5, Stable: true},
		},
		{
			name: "part flag",
			hex:  "0620ea070a0114220200001027",
			want: Measurement{Unit: UnitKg, Value: 50, Stable: true, Part: true},
		},
		{
			name: "overload clears stable",
			hex:  "0220ea070a011422020000f0ff",
			want: Measurement{Unit: UnitKg, Value: 65520, Overload: true},
		},
		{
			name: "impedance failure sentinel",
			hex:  "0222ea070a01142202fdfffa4b",
			want: Measurement{Unit: UnitKg, Value: 97.25, Stable: true, ImpedanceStable: true, Impedance: ImpedanceFailed},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := ParseMeasurement(mustHex(t, tt.hex))
			if err != nil {
				t.Fatal(err)
			}
			tt.want.Time = time.Date(2026, 10, 1, 20, 34, 2, 0, time.UTC)
			if m != tt.want {
				t.Fatalf("got %+v\nwant %+v", m, tt.want)
			}
		})
	}
}

func TestParseMeasurementShort(t *testing.T) {
	if _, err := ParseMeasurement(make([]byte, 12)); err == nil {
		t.Fatal("expected error for 12 bytes")
	}
}

func TestWeightKgConversion(t *testing.T) {
	tests := []struct {
		m    Measurement
		want float64
	}{
		{Measurement{Unit: UnitKg, Value: 64.05}, 64.05},
		{Measurement{Unit: UnitJin, Value: 194.5}, 97.25},
		{Measurement{Unit: UnitLb, Value: 214.39}, 97.25},
		{Measurement{Unit: UnitLb, Value: 100}, 45.36},
	}
	for _, tt := range tests {
		if got := tt.m.WeightKg(); got != tt.want {
			t.Errorf("%+v: WeightKg = %v, want %v", tt.m, got, tt.want)
		}
	}
}

func TestHasImpedance(t *testing.T) {
	tests := []struct {
		z    uint16
		want bool
	}{{0, false}, {1, true}, {431, true}, {65532, true}, {ImpedanceFailed, false}, {65534, false}, {65535, true}}
	for _, tt := range tests {
		m := Measurement{Impedance: tt.z}
		if got := m.HasImpedance(); got != tt.want {
			t.Errorf("impedance %d: HasImpedance = %v, want %v", tt.z, got, tt.want)
		}
	}
}
