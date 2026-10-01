package bodycomp

// Body score from BodyParamsUtils + BodyParamsScoreHelper
// (bodyfat/utils/OooOOO0.java). Subcutaneous fat and skeletal muscle are not
// produced for this scale, so their penalties are always 0.

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// lerp keeps the app formula: |boundary-start|/|max-min|*|cur-start|+min.
func lerp(cur, boundary, start, hi, lo float32) float32 {
	v := float32(float32(float32(abs32(float32(boundary-start))/abs32(float32(hi-lo)))*abs32(float32(cur-start))) + lo)
	v = t2(dec(v))
	if v > hi {
		return hi
	}
	if v < lo {
		return lo
	}
	return v
}

func scoreBMI(bmi, fat float32, age int, male bool) float32 {
	r := bmiOverweight(age)
	t := fatTable(age, male)
	if len(r) == 2 && bmi > r[0] && len(t) == 4 && fat > t[2] {
		if bmi > r[1] {
			return 10
		}
		return lerp(bmi, dadd(r[1], 0.1), dadd(r[0], 0.1), 10, 5)
	}
	if age >= 18 {
		return scoreBMIAdultLow(bmi)
	}
	return 0
}

func scoreBMIAdultLow(bmi float32) float32 {
	lo := dadd(18.4, 0.1)
	switch {
	case bmi == 15:
		return 9
	case bmi > 15 && bmi < lo:
		return lerp(bmi, 15, dsub(lo, 0.1), 9, 3)
	case bmi <= 14:
		return 30
	case bmi < 15:
		return lerp(bmi, 14, dsub(15, 0.1), 30, 15)
	}
	return 0
}

func scoreFat(fat float32, age int, male bool) float32 {
	t := fatTable(age, male)
	if len(t) == 4 && fat > t[2] {
		if fat > t[3] {
			return 20
		}
		return lerp(fat, dadd(t[3], 0.1), dadd(t[2], 0.1), 20, 10)
	}
	if len(t) == 4 && fat > dsub(t[2], 0.9) {
		if fat == t[2] {
			return 9
		}
		return lerp(fat, t[2], t2(decAdd(decSub(dec(t[2]), dec(0.9)), dec(0.1))), 9, 3)
	}
	if len(t) != 4 || fat >= dadd(t[0], 0.1) {
		return 0
	}
	f0 := float32(t[0] + 0.1)
	if fat > dsub(f0, 6) {
		return lerp(fat, dsub(f0, 6), t[0], 9, 3)
	}
	return 9
}

func scoreMuscle(muscle float32, height int, male bool) float32 {
	m := muscleTable(male, height)
	lo := dadd(m[0], 0.1)
	if muscle < lo {
		if muscle <= dsub(lo, 5) {
			return 15
		}
		return lerp(muscle, dsub(lo, 5), m[0], 15, 10)
	}
	if muscle >= float32(lo+0.9) {
		return 0
	}
	if muscle == lo {
		return 9
	}
	return lerp(muscle, lo, float32(m[0]+0.9), 9, 3)
}

func scoreWater(water float32, male bool) float32 {
	t := waterTable(male)
	lo := dadd(t[0], 0.1)
	if water >= lo {
		return 0
	}
	if water <= dsub(lo, 5) {
		return 5
	}
	return lerp(water, dsub(lo, 5), t[0], 5, 1)
}

func scoreVisceral(v float32) float32 {
	if v > visceralTable[0] {
		if v > visceralTable[1] {
			return 15
		}
		return lerp(v, dadd(visceralTable[1], 1), dadd(visceralTable[0], 1), 15, 10)
	}
	if v < 8 || v > 9 {
		return 0
	}
	if v == 9 {
		return 5
	}
	return lerp(v, 9, 8, 5, 3)
}

func scoreBone(bone, w float32, male bool) float32 {
	t := boneTable(male, w)
	lo := dadd(t[0], 0.1)
	if bone >= lo {
		return 0
	}
	if bone <= dsub(lo, 0.3) {
		return 5
	}
	return lerp(bone, dsub(lo, 0.3), t[0], 5, 1)
}

// scoreBMR takes the native standard (JJ()[0]); the java table is the fallback.
func scoreBMR(bmr int, w float32, age int, male bool, std float32) float32 {
	if std <= 0 {
		std = float32(float32(bmrFactor(age, male)) * w)
	}
	b := float32(bmr)
	if b >= std {
		return 0
	}
	if b <= dsub(std, 300) {
		return 5
	}
	return lerp(b, dsub(std, 300), dsub(std, 1), 5, 1)
}

func score(r *Result, w float32, height, age int, male bool, bmrStd float32) int {
	s := float32(100)
	for _, p := range []float32{
		scoreBMI(r.BMI, r.BodyFat, age, male),
		scoreFat(r.BodyFat, age, male),
		scoreMuscle(r.Muscle, height, male),
		scoreWater(r.Water, male),
		scoreVisceral(float32(r.Visceral)),
		scoreBone(r.Bone, w, male),
		scoreBMR(r.BMR, w, age, male, bmrStd),
	} {
		s = float32(s - p)
	}
	return int(s)
}
