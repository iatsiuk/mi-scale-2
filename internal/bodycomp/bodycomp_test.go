package bodycomp

import (
	"errors"
	"math"
	"testing"
	"time"
)

// vectors from the reference model of the static analysis; standard weight is
// the exact float32 value, the analysis table prints it rounded
func TestComputeReferenceVectors(t *testing.T) {
	tests := []struct {
		in   Input
		want Result
	}{
		{
			Input{WeightKg: 70, HeightCm: 175, Age: 30, Sex: Male, Impedance: 500},
			Result{BMI: 22.8, HasComposition: true, Impedance: 500, BodyFat: 19.2283935546875, Water: 55.40932083129883,
				Bone: 2.877434730529785, Muscle: 53.662689208984375, Visceral: 9, BMR: 1525, BodyAge: 26,
				Protein: 21.251667022705078, StandardWeight: 66.5, Score: 94},
		},
		{
			Input{WeightKg: 55.3, HeightCm: 162, Age: 45, Sex: Female, Impedance: 620},
			Result{BMI: 21.0, HasComposition: true, Impedance: 620, BodyFat: 29.615768432617188, Water: 50.25434112548828,
				Bone: 2.080571174621582, Muscle: 36.8419075012207, Visceral: 4, BMR: 1085, BodyAge: 36,
				Protein: 16.3675537109375, StandardWeight: 55.2, Score: 94},
		},
		{
			Input{WeightKg: 92.45, HeightCm: 181, Age: 52, Sex: Male, Impedance: 430},
			Result{BMI: 28.2, HasComposition: true, Impedance: 430, BodyFat: 29.753904342651367, Water: 50.15571212768555,
				Bone: 3.3108298778533936, Muscle: 61.631683349609375, Visceral: 15, BMR: 1658, BodyAge: 47,
				Protein: 16.509174346923828, StandardWeight: 70.7, Score: 46},
		},
		{
			Input{WeightKg: 48.2, HeightCm: 158, Age: 23, Sex: Female, Impedance: 700},
			Result{BMI: 19.3, HasComposition: true, Impedance: 700, BodyFat: 25.921356201171875, Water: 50.81795120239258,
				Bone: 1.9370226860046387, Muscle: 33.768882751464844, Visceral: 1, BMR: 1151, BodyAge: 22,
				Protein: 19.24197006225586, StandardWeight: 52.800003, Score: 99},
		},
	}
	for _, tt := range tests {
		got, err := Compute(tt.in)
		if err != nil {
			t.Fatalf("%+v: %v", tt.in, err)
		}
		got.BodyType = 0 // checked separately
		if got != tt.want {
			t.Errorf("%+v\n got %+v\nwant %+v", tt.in, got, tt.want)
		}
	}
}

func TestComputeWithoutImpedanceKeepsBMIAndStandardWeight(t *testing.T) {
	got, err := Compute(Input{WeightKg: 70, HeightCm: 175, Age: 30, Sex: Male, Impedance: 0})
	if err != nil {
		t.Fatal(err)
	}
	want := Result{BMI: 22.8, StandardWeight: 66.5}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestComputeRejectsNativeRanges(t *testing.T) {
	base := Input{WeightKg: 70, HeightCm: 175, Age: 30, Sex: Male, Impedance: 500}
	tests := []struct {
		name string
		mod  func(*Input)
		code int
	}{
		{"height below 90", func(in *Input) { in.HeightCm = 89 }, 4},
		{"height above 220", func(in *Input) { in.HeightCm = 221 }, 4},
		{"weight 10", func(in *Input) { in.WeightKg = 10 }, 3},
		{"weight 200", func(in *Input) { in.WeightKg = 200 }, 3},
		{"age 5", func(in *Input) { in.Age = 5 }, 2},
		{"age 100", func(in *Input) { in.Age = 100 }, 2},
		{"impedance 49", func(in *Input) { in.Impedance = 49 }, 1},
		{"impedance 3001", func(in *Input) { in.Impedance = 3001 }, 1},
	}
	for _, tt := range tests {
		in := base
		tt.mod(&in)
		got, err := Compute(in)
		var re *RangeError
		if !errors.As(err, &re) || re.Code != tt.code {
			t.Errorf("%s: err %v, want code %d", tt.name, err, tt.code)
		}
		if got.HasComposition || got.BMI == 0 {
			t.Errorf("%s: result %+v must keep only BMI and standard weight", tt.name, got)
		}
	}
}

func TestComputeClampsImpedanceButBodyAgeUsesRaw(t *testing.T) {
	low, err := Compute(Input{WeightKg: 70, HeightCm: 175, Age: 30, Sex: Male, Impedance: 100})
	if err != nil {
		t.Fatal(err)
	}
	clamped, err := Compute(Input{WeightKg: 70, HeightCm: 175, Age: 30, Sex: Male, Impedance: 200})
	if err != nil {
		t.Fatal(err)
	}
	if low.Impedance != 200 || low.BodyFat != clamped.BodyFat {
		t.Fatalf("native must clamp impedance to 200: %+v vs %+v", low, clamped)
	}
	// -0.7471*175 + 0.9161*70 + 0.4184*30 + 0.0517*100 + 54.2267 = 5.3332 -> clamped to 15
	if low.BodyAge != 15 {
		t.Fatalf("body age %d, want 15", low.BodyAge)
	}
	// with 200 ohm the raw term adds 5.17: 10.5032 -> still 15
	if clamped.BodyAge != 15 {
		t.Fatalf("body age %d, want 15", clamped.BodyAge)
	}
}

func TestComputeBMISentinelIsClamped(t *testing.T) {
	// the app maps weight or height <= 0 to -1, which its clamp turns into 10
	for _, in := range []Input{
		{WeightKg: 0, HeightCm: 175, Age: 30, Sex: Male},
		{WeightKg: 75, HeightCm: 0, Age: 30, Sex: Male},
	} {
		got, err := Compute(in)
		if err != nil || got.BMI != 10 {
			t.Errorf("%+v: BMI %v, err %v", in, got.BMI, err)
		}
	}
}

func TestComputeRejectsNonFiniteWeight(t *testing.T) {
	for _, w := range []float64{math.NaN(), math.Inf(1)} {
		if _, err := Compute(Input{WeightKg: w, HeightCm: 175, Age: 30, Sex: Male}); err == nil {
			t.Errorf("weight %v: expected error", w)
		}
	}
}

func TestComputeRejectsUnknownSex(t *testing.T) {
	if _, err := Compute(Input{WeightKg: 70, HeightCm: 175, Age: 30, Sex: 2, Impedance: 500}); err == nil {
		t.Fatal("expected error")
	}
}

func TestBMI(t *testing.T) {
	tests := []struct {
		w    float32
		h    int
		want float32
	}{
		{70, 175, 22.8},
		{20, 200, 10},  // clamped low
		{199, 100, 50}, // clamped high
		{97.25, 181, 29.6},
	}
	for _, tt := range tests {
		if got := bmi(tt.w, tt.h); got != tt.want {
			t.Errorf("bmi(%v, %d) = %v, want %v", tt.w, tt.h, got, tt.want)
		}
	}
}

func TestAgeAt(t *testing.T) {
	tests := []struct {
		year, month int
		at          time.Time
		want        int
	}{
		{1990, 5, time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local), 36},
		{1990, 5, time.Date(2026, 5, 31, 0, 0, 0, 0, time.Local), 35}, // whole birth month counts as before the birthday
		{1990, 5, time.Date(2026, 4, 1, 0, 0, 0, 0, time.Local), 35},
		{2026, 5, time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local), 0},  // same year: no decrement
		{2027, 1, time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local), 24}, // negative falls back to 24
	}
	for _, tt := range tests {
		if got := AgeAt(tt.year, tt.month, tt.at); got != tt.want {
			t.Errorf("AgeAt(%d-%02d, %v) = %d, want %d", tt.year, tt.month, tt.at, got, tt.want)
		}
	}
}

func TestBodyType(t *testing.T) {
	tests := []struct {
		name   string
		fat    float32
		muscle float32
		want   BodyType
	}{
		{"low fat, low muscle", 10, 40, Skinny},
		{"normal fat, normal muscle", 18, 55, Balanced},
		{"high fat, high muscle", 30, 65, ThickSet},
		{"high fat, low muscle", 30, 40, Obese},
	}
	for _, tt := range tests {
		// male, 30 years, 175 cm: fat thresholds 11/17/22/27, muscle 49.5/59.4
		if got := bodyType(tt.fat, tt.muscle, 30, 175, true); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
	if got := bodyType(18, 55, 5, 175, true); got != BodyTypeUnknown {
		t.Errorf("age 5: got %v", got)
	}
}

func TestLerpKeepsAppProportion(t *testing.T) {
	// abs(boundary-start)/abs(max-min)*abs(cur-start)+min, not the usual mapping
	if got := lerp(26, 30, 25, 20, 10); got != 10.5 {
		t.Fatalf("lerp = %v, want 10.5", got)
	}
	if got := lerp(1000, 2000, 0, 5, 1); got != 5 {
		t.Fatalf("lerp must clamp to max: %v", got)
	}
}
