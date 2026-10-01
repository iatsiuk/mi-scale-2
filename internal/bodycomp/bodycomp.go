// Package bodycomp ports the Zepp Life 6.16.1 body composition path used for
// Mi Body Composition Scale 1/2 (device source 101/102): libBodyfat.so plus
// the java code around it.
//
// Native parts compute in float64, java parts in float32; every java float
// operation is wrapped in float32() so the compiler cannot fuse it.
package bodycomp

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"
)

type Sex int

const (
	Female Sex = 0
	Male   Sex = 1
)

type Input struct {
	WeightKg  float64 // as converted by the scale protocol (2 decimals)
	HeightCm  int
	Age       int
	Sex       Sex
	Impedance int // raw value from the scale
}

type Result struct {
	BMI            float32
	HasComposition bool
	Impedance      int     // clamped to 200..1200, as stored by the app
	BodyFat        float32 // percent
	Water          float32 // percent
	Bone           float32 // kg
	Muscle         float32 // kg
	Visceral       int
	BMR            int // kcal
	BodyAge        int
	Protein        float32 // percent
	StandardWeight float32 // kg
	BodyType       BodyType
	Score          int
}

// RangeError is a native input check failure; only BMI and the standard
// weight are available then.
type RangeError struct{ Code int }

func (e *RangeError) Error() string {
	field := map[int]string{1: "impedance (50..3000)", 2: "age (6..99)", 3: "weight (10..200 kg)", 4: "height (90..220 cm)"}[e.Code]
	return fmt.Sprintf("bodycomp: %s out of range", field)
}

// HasImpedance is the app gate before calling the algorithm.
func HasImpedance(z int) bool { return z > 0 && z != 65533 && z != 65534 }

// Compute reproduces BodyParamsUtils for one measurement. Without a usable
// impedance the result has only BMI and standard weight and err is nil.
func Compute(in Input) (Result, error) {
	if in.Sex != Male && in.Sex != Female {
		return Result{}, errors.New("bodycomp: sex must be 0 (female) or 1 (male)")
	}
	if math.IsNaN(in.WeightKg) || math.IsInf(in.WeightKg, 0) {
		return Result{}, errors.New("bodycomp: weight must be finite") // port validation, the scale sends integers
	}
	male := in.Sex == Male
	w := float32(in.WeightKg)
	r := Result{
		BMI:            bmi(w, in.HeightCm),
		StandardWeight: standardWeight(float32(in.HeightCm), male),
	}
	if !HasImpedance(in.Impedance) {
		return r, nil
	}
	n, code := newNative(float64(w), float64(float32(in.HeightCm)), in.Age, male, in.Impedance)
	if code != 0 {
		return r, &RangeError{Code: code}
	}
	fat := n.fat()
	bone := n.bone()
	r.HasComposition = true
	r.Impedance = n.z
	r.BodyFat = float32(fat)
	r.Water = float32(n.water(fat))
	r.Bone = float32(bone)
	r.Muscle = float32(n.muscle(fat, bone))
	r.Visceral = int(n.visceral())
	r.BMR = int(n.bmr())
	r.BodyAge = bodyAge(float32(in.HeightCm), male, in.Age, w, float32(in.Impedance))
	r.Protein = protein(r.Muscle, w, r.Water)
	r.BodyType = bodyType(r.BodyFat, r.Muscle, in.Age, in.HeightCm, male)

	bmrStd, _ := strconv.ParseFloat(strconv.FormatFloat(n.bmrStandard(), 'g', -1, 64), 32)
	r.Score = score(&r, w, in.HeightCm, in.Age, male, float32(bmrStd))
	return r, nil
}

// bmi is HMWeightUtils: float math, clamp 10..50, truncated to 1 decimal.
func bmi(w float32, height int) float32 {
	h := float32(float32(height) / 100)
	b := float32(-1) // app sentinel, clamped to 10 below
	if height > 0 && w > 0 {
		b = float32(w / float32(h*h))
	}
	if b < 10 {
		b = 10
	}
	if b > 50 {
		b = 50
	}
	return truncDec(b, 1)
}

// bodyAge is MA from BodyAge (o00O00oO/C37152OooO00o.java); it takes the raw
// impedance, not the clamped one.
func bodyAge(h float32, male bool, age int, w, z float32) int {
	a := float32(age)
	var v float32
	if male {
		v = float32(float32(float32(float32(float32(h*-0.7471)+float32(w*0.9161))+float32(a*0.4184))+float32(z*0.0517)) + 54.2267)
	} else {
		v = float32(float32(float32(float32(float32(h*-1.1165)+float32(w*1.5784))+float32(a*0.4615))+float32(z*0.0415)) + 83.2548)
	}
	if v < 15 {
		v = 15
	}
	if v > 80 {
		v = 80
	}
	return int(v)
}

// protein is PMR: muscle share minus water, clamped to 5..32.
func protein(muscle, w, water float32) float32 {
	p := float32(float32(float32(muscle/w)*100) - water)
	if p > 32 || p != p {
		return 32
	}
	if p < 5 {
		return 5
	}
	return p
}

// standardWeight is SBW, the ideal weight shown by the app.
func standardWeight(h float32, male bool) float32 {
	if male {
		return float32(float32(h-80) * 0.7)
	}
	return float32(float32(h-70) * 0.6)
}

type BodyType int

const (
	BodyTypeUnknown BodyType = iota
	Skinny
	BalancedSkinny
	SkinnyMuscular
	LacksExercise
	Balanced
	BalancedMuscular
	Obese
	Overweight
	ThickSet
)

func (t BodyType) String() string {
	return [...]string{"unknown", "skinny", "balanced skinny", "skinny muscular", "lacks exercise",
		"balanced", "balanced muscular", "obese", "overweight", "thick-set"}[t]
}

func bodyType(fat, muscle float32, age, height int, male bool) BodyType {
	if age > 99 || age < 6 || height > 220 || height < 90 {
		return BodyTypeUnknown
	}
	row := map[Level]int{LevelLow: 0, LevelSlightlyLow: 1, LevelNormal: 1, LevelSlightlyHigh: 2, LevelHigh: 2}
	col := map[Level]int{LevelLow: 1, LevelNormal: 2, LevelHigh: 3}
	r, okRow := row[FatLevel(fat, age, male)]
	c, okCol := col[MuscleLevel(muscle, age, height, male)]
	if !okRow || !okCol {
		return BodyTypeUnknown
	}
	return BodyType(r*3 + c)
}

// AgeAt is BodyParamsUtils age: whole years by year and month only; the
// birth month itself still counts as before the birthday. Unknown or future
// birth dates give 24 like the app.
func AgeAt(birthYear, birthMonth int, at time.Time) int {
	at = at.Local()
	i := at.Year() - birthYear
	if birthYear <= 0 || i < 0 {
		return 24
	}
	if i != 0 && int(at.Month()) <= birthMonth {
		i--
	}
	return i
}
