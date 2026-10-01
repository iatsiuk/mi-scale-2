package bodycomp

import (
	"math/big"
	"strconv"
)

// The app does its rounding on java decimal strings of floats
// (String.valueOf(float) + BigDecimal), not on binary values.

// dec is new BigDecimal(String.valueOf(f)); NaN and Inf give 0 like the
// app helpers that catch the BigDecimal exception.
func dec(f float32) *big.Rat {
	r, ok := new(big.Rat).SetString(strconv.FormatFloat(float64(f), 'g', -1, 32))
	if !ok {
		return new(big.Rat)
	}
	return r
}

func decAdd(a, b *big.Rat) *big.Rat { return new(big.Rat).Add(a, b) }
func decSub(a, b *big.Rat) *big.Rat { return new(big.Rat).Sub(a, b) }

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

// roundDown truncates toward zero to n decimals.
func roundDown(r *big.Rat, n int) *big.Rat {
	s := pow10(n)
	num := new(big.Int).Mul(r.Num(), s)
	q := new(big.Int).Quo(num, r.Denom())
	return new(big.Rat).SetFrac(q, s)
}

// roundHalfEven rounds to n decimals, the java NumberFormat default.
func roundHalfEven(r *big.Rat, n int) *big.Rat {
	s := pow10(n)
	x := new(big.Rat).Mul(r, new(big.Rat).SetInt(s))
	q, m := new(big.Int).QuoRem(x.Num(), x.Denom(), new(big.Int))
	// compare 2*|remainder| with denominator
	m.Abs(m).Lsh(m, 1)
	if c := m.Cmp(x.Denom()); c > 0 || (c == 0 && q.Bit(0) == 1) {
		if x.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return new(big.Rat).SetFrac(q, s)
}

func toFloat32(r *big.Rat) float32 {
	f, _ := r.Float32()
	return f
}

// t2 is BodyParamsScoreHelper: DecimalFormat("#######.##", DOWN) + parseFloat.
func t2(r *big.Rat) float32 { return toFloat32(roundDown(r, 2)) }

func dadd(a, b float32) float32 { return t2(decAdd(dec(a), dec(b))) }
func dsub(a, b float32) float32 { return t2(decSub(dec(a), dec(b))) }

// truncDec is HMWeightUtils: NumberFormat(max 6 fraction digits) on
// Float.toString, then setScale(n, ROUND_DOWN).
func truncDec(f float32, n int) float32 {
	return toFloat32(roundDown(roundHalfEven(dec(f), 6), n))
}

// Trunc truncates like the app detail screens: fat, water and protein use 1
// decimal, muscle, bone and ideal weight 2.
func Trunc(f float32, digits int) float32 { return truncDec(f, digits) }
