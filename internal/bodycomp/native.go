package bodycomp

import "math"

// native reproduces libBodyfat.so (com.holtek.libHTBodyfat.HTBodyfat) from
// Zepp Life 6.16.1 arm64. It computes in double; every fmadd/fmsub of the
// original is math.FMA and every other product is wrapped in float64() so the
// compiler cannot fuse it. Addresses refer to the disassembly.
type native struct {
	w, h float64
	age  int
	male bool
	z    int // impedance clamped to 200..1200
}

// clamp is checkValueOverflow @0x12a8.
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return v
}

// newNative is NN @0x12c8; it returns the native error code (0 = ok).
func newNative(w, h float64, age int, male bool, z0 int) (n native, code int) {
	switch {
	case !(h >= 90) || h > 220:
		return native{}, 4
	case w <= 10 || w >= 200:
		return native{}, 3
	case age < 6 || age > 99:
		return native{}, 2
	case z0 < 50 || z0 > 3000:
		return native{}, 1
	}
	return native{w: w, h: h, age: age, male: male, z: int(clamp(float64(z0), 200, 1200))}, 0
}

// lbm is getlbmCoefficient @0x1200.
func (n native) lbm() float64 {
	t := float64(n.h*float64(float64(n.h*9.058)/100)) / 100
	t += 12.226
	t = math.FMA(n.w, 0.32, t)
	t = math.FMA(-float64(n.z), 0.0068, t)
	return math.FMA(-float64(n.age), 0.0542, t)
}

// fat is CC @0x1608, body fat percent.
func (n native) fat() float64 {
	l := n.lbm()
	switch {
	case n.male:
		l -= 0.8
	case n.age <= 49:
		l -= 9.25
	default:
		l -= 7.25
	}
	if n.male {
		l = n.maleFatFactor(l)
	} else {
		l = n.femaleFatFactor(l)
	}
	return clamp((1-l/n.w)*100, 5, 75)
}

func (n native) maleFatFactor(l float64) float64 {
	if n.w < 61 {
		l *= 0.98
	}
	return l
}

// femaleFatFactor applies three independent checks in this order.
func (n native) femaleFatFactor(l float64) float64 {
	if n.w < 50 {
		l *= 1.02
	}
	if n.w > 60 {
		l *= 0.96
	}
	if n.h > 160 {
		l *= 1.03
	}
	return l
}

// water is HH @0x1dc0, percent; it uses the unrounded native fat.
func (n native) water(fat float64) float64 {
	v := (100 - fat) * 0.7
	if v > 50 {
		v *= 0.98
	} else {
		v *= 1.02
	}
	return clamp(v, 35, 75)
}

// bone is DD @0x1820, kg.
func (n native) bone() float64 {
	b := float64(n.lbm() * 0.05158)
	if n.male {
		b -= 0.18016894
	} else {
		b -= 0.245691014
	}
	if b > 2.2 {
		b += 0.1
	} else {
		b -= 0.1
	}
	return clamp(b, 0.5, 8)
}

// muscle is FF @0x19c8, kg; it reads the fat and bone globals.
func (n native) muscle(fat, bone float64) float64 {
	m := math.FMA(-float64(fat*0.01), n.w, n.w)
	return clamp(m-bone, 10, 120)
}

// visceral is GG @0x1b80.
func (n native) visceral() float64 {
	if n.male {
		return clamp(n.visceralMale(), 1, 50)
	}
	return clamp(n.visceralFemale(), 1, 50)
}

func (n native) visceralMale() float64 {
	a := float64(n.age)
	if math.FMA(n.w, 1.6, 63) > n.h {
		d := math.FMA(n.h, float64(n.h*0.0826), -float64(n.h*0.4)) + 48
		return math.FMA(a, 0.15, float64(n.w*305)/d-2.9)
	}
	k := math.FMA(n.h, -0.0015, 0.765)
	return math.FMA(a, 0.15, math.FMA(n.w, k, -float64(n.h*0.143))) - 5
}

func (n native) visceralFemale() float64 {
	a := float64(n.age)
	if math.FMA(n.h, 0.5, -13) >= n.w {
		k := math.FMA(n.h, -0.0024, 0.691)
		return math.FMA(a, 0.07, math.FMA(n.w, k, -float64(n.h*0.027))) - 10.5
	}
	d := math.FMA(n.h, float64(n.h*0.1158), float64(n.h*1.45)) - 120
	return math.FMA(a, 0.07, float64(n.w*500)/d-6)
}

// bmr is BB @0x1420, kcal.
func (n native) bmr() float64 {
	a := float64(n.age)
	var v float64
	if n.male {
		v = math.FMA(-a, 8.976, math.FMA(-n.h, 0.726, math.FMA(n.w, 14.916, 877.8)))
	} else {
		v = math.FMA(-a, 6.204, math.FMA(-n.h, 0.39336, math.FMA(n.w, 10.2036, 864.6)))
	}
	return clamp(v, 500, 10000)
}

// bmrStandard is JJ()[0], the normal BMR for age and sex.
func (n native) bmrStandard() float64 {
	return float64(bmrFactor(n.age, n.male)) * n.w
}

func bmrFactor(age int, male bool) int {
	f := [2][6]int{{34, 29, 24, 22, 20, 19}, {36, 30, 26, 23, 21, 20}}
	s := 0
	if male {
		s = 1
	}
	switch {
	case age <= 12:
		return f[s][0]
	case age <= 15:
		return f[s][1]
	case age <= 17:
		return f[s][2]
	case age <= 29:
		return f[s][3]
	case age <= 49:
		return f[s][4]
	}
	return f[s][5]
}
