package bodycomp

// Tables from BodyParamsStandardData.kt (bodyfat/utils/OooOOO.java) and their
// selection in BodyParamsScoreHelper (bodyfat/utils/OooOOO0.java). Only the
// non-chinese locale is implemented.

// Level is a classification of a metric against its standard.
type Level int

const (
	LevelUnknown Level = iota
	LevelLow
	LevelSlightlyLow
	LevelNormal
	LevelSlightlyHigh
	LevelHigh
)

func (l Level) String() string {
	return [...]string{"unknown", "low", "slightly low", "normal", "slightly high", "high"}[l]
}

func validAge(age int) bool { return age >= 6 && age <= 99 }

type fatRow struct {
	maxAge int
	t      []float32
}

var (
	maleFat = []fatRow{
		{13, []float32{6.9, 15.9, 24.9, 29.9}},
		{14, []float32{6.9, 14.9, 24.9, 28.9}},
		{15, []float32{7.9, 14.9, 23.9, 28.9}},
		{16, []float32{7.9, 15.9, 23.9, 27.9}},
		{17, []float32{8.9, 15.9, 22.9, 27.9}},
		{39, []float32{10.9, 16.9, 21.9, 26.9}},
		{59, []float32{11.9, 17.9, 22.9, 27.9}},
		{99, []float32{13.9, 19.9, 24.9, 29.9}},
	}
	femaleFat = []fatRow{
		{6, []float32{7.9, 15.9, 24.9, 28.9}},
		{7, []float32{8.9, 16.9, 24.9, 29.9}},
		{8, []float32{9.9, 17.9, 25.9, 30.9}},
		{9, []float32{9.9, 18.9, 27.9, 31.9}},
		{10, []float32{10.9, 19.9, 28.9, 32.9}},
		{11, []float32{12.9, 21.9, 30.9, 34.9}},
		{12, []float32{13.9, 22.9, 31.9, 35.9}},
		{13, []float32{14.9, 24.9, 33.9, 37.9}},
		{14, []float32{16.9, 25.9, 34.9, 38.9}},
		{15, []float32{17.9, 26.9, 35.9, 39.9}},
		{16, []float32{18.9, 27.9, 36.9, 40.9}},
		{17, []float32{19.9, 27.9, 36.9, 40.9}},
		{39, []float32{20.9, 27.9, 34.9, 39.9}},
		{59, []float32{21.9, 28.9, 35.9, 40.9}},
		{99, []float32{21.9, 29.9, 36.9, 41.9}},
	}
)

// fatTable returns the 4 body fat endpoints or nil outside ages 6..99.
func fatTable(age int, male bool) []float32 {
	if !validAge(age) {
		return nil
	}
	rows := femaleFat
	if male {
		rows = maleFat
	}
	for _, r := range rows {
		if age <= r.maxAge {
			return r.t
		}
	}
	return nil
}

func muscleTable(male bool, height int) []float32 {
	i := 2
	if height < 160 {
		i = 0
	} else if height < 170 {
		i = 1
	}
	if male {
		return [][]float32{{38.4, 46.4}, {43.9, 52.3}, {49.4, 59.3}}[i]
	}
	return [][]float32{{29.0, 34.6}, {32.8, 37.4}, {36.4, 42.4}}[i]
}

// boneTable keeps the 0.1 kg gaps of the java ranges: they fall to the last row.
func boneTable(male bool, w float32) []float32 {
	if male {
		switch {
		case w >= 0 && w <= 60:
			return []float32{2.4, 3.8}
		case w >= 60.1 && w <= 74.9:
			return []float32{2.8, 4.0}
		}
		return []float32{3.1, 4.1}
	}
	switch {
	case w >= 0 && w <= 45:
		return []float32{1.7, 3.5}
	case w >= 45.1 && w <= 60:
		return []float32{2.1, 3.7}
	}
	return []float32{2.4, 3.8}
}

func waterTable(male bool) []float32 {
	if male {
		return []float32{54.9, 65.0}
	}
	return []float32{44.9, 60.0}
}

var (
	proteinTable  = []float32{15.9, 17.9}
	visceralTable = []float32{9, 14, 50}
)

// bmiOverweight is the score BMI pair for the non-chinese locale.
func bmiOverweight(age int) []float32 {
	if age >= 18 {
		return []float32{24.9, 29.9}
	}
	return nil
}

func classify(v float32, t []float32, levels ...Level) Level {
	for i, e := range t {
		if v < e+0.1 {
			return levels[i]
		}
	}
	return levels[len(levels)-1]
}

// FatLevel classifies body fat percent truncated to 1 decimal.
func FatLevel(fat float32, age int, male bool) Level {
	t := fatTable(age, male)
	if fat == 0 || t == nil {
		return LevelUnknown
	}
	return classify(truncDec(fat, 1), t, LevelLow, LevelSlightlyLow, LevelNormal, LevelSlightlyHigh, LevelHigh)
}

// MuscleLevel classifies muscle mass in kg truncated to 2 decimals.
func MuscleLevel(muscle float32, age, height int, male bool) Level {
	if muscle == 0 || !validAge(age) || height < 90 || height > 220 {
		return LevelUnknown
	}
	return classify(truncDec(muscle, 2), muscleTable(male, height), LevelLow, LevelNormal, LevelHigh)
}

func BoneLevel(bone, weight float32, age int, male bool) Level {
	if !validAge(age) {
		return LevelUnknown
	}
	return classify(truncDec(bone, 2), boneTable(male, weight), LevelLow, LevelNormal, LevelHigh)
}

func WaterLevel(water float32, male bool) Level {
	return classify(truncDec(water, 1), waterTable(male), LevelSlightlyLow, LevelNormal, LevelSlightlyHigh)
}

// ProteinLevel: low < 16, normal <= 18, high above.
func ProteinLevel(protein float32) Level {
	p := truncDec(protein, 1)
	switch {
	case p < proteinTable[0]+0.1:
		return LevelLow
	case p <= proteinTable[1]+0.1:
		return LevelNormal
	}
	return LevelHigh
}

// VisceralLevel: normal < 10, slightly high < 15, high above.
func VisceralLevel(v int) Level {
	switch {
	case float32(v) < dadd(visceralTable[0], 1):
		return LevelNormal
	case float32(v) < dadd(visceralTable[1], 1):
		return LevelSlightlyHigh
	}
	return LevelHigh
}

// BMRStandard is the normal BMR shown by the app.
func BMRStandard(weight float32, age int, male bool) int {
	return int(float32(bmrFactor(age, male)) * weight)
}

// BMILevel for the non-chinese locale; children are not classified.
func BMILevel(bmi float32, age int) Level {
	if age < 18 || bmi <= 0 {
		return LevelUnknown
	}
	switch {
	case bmi < 18.5:
		return LevelLow
	case bmi < 25:
		return LevelNormal
	case bmi < 30:
		return LevelSlightlyHigh
	}
	return LevelHigh
}
