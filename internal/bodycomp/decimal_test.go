package bodycomp

import "testing"

func TestTruncDecUsesDecimalString(t *testing.T) {
	tests := []struct {
		in   float32
		n    int
		want float32
	}{
		{22.9, 1, 22.9}, // binary value is 22.8999996..., java string is "22.9"
		{22.86, 1, 22.8},
		{19.2283935546875, 1, 19.2},
		{53.662689208984375, 2, 53.66},
		{-1.25, 1, -1.2},
		{5, 1, 5},
	}
	for _, tt := range tests {
		if got := truncDec(tt.in, tt.n); got != tt.want {
			t.Errorf("truncDec(%v, %d) = %v, want %v", tt.in, tt.n, got, tt.want)
		}
	}
}

func TestDecimalBoundaries(t *testing.T) {
	tests := []struct {
		name string
		got  float32
		want float32
	}{
		{"18.4+0.1", dadd(18.4, 0.1), 18.5},
		{"18.5-0.1", dsub(18.5, 0.1), 18.4},
		{"24.9-0.9+0.1", t2(decAdd(decSub(dec(24.9), dec(0.9)), dec(0.1))), 24.1},
		{"t2 truncates", t2(dec(1.239)), 1.23},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
		}
	}
}
