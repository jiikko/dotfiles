package pyjson

import (
	"math"
	"math/big"
	"strconv"
)

// Python の float.__round__ が、ndigits の大きさで結果を決めてしまう境界 (floatobject.c の NDIGITS_MAX・NDIGITS_MIN)。
const (
	roundDigitsMax = 323
	roundDigitsMin = -308
)

// Round は Python の round(x, ndigits) (x は float)。10 進で厳密に丸めるので、2.675 は 2.67、0.125 は ndigits=2 で 0.12 (厳密な中間は偶数へ)。
// 結果が float に収まらないとき (round(1.7976931348623157e308, -308)) は Python と同じく OverflowError。
func Round(x float64, ndigits int) (float64, error) {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x, nil
	}
	if ndigits > roundDigitsMax {
		return x, nil
	}
	if ndigits < roundDigitsMin {
		return math.Copysign(0, x), nil
	}
	if ndigits >= 0 {
		// 'f' の書式は厳密な 10 進展開を丸める (厳密な中間は偶数へ)。読み戻しも最も近い float で、Python の dtoa / strtod と同じ。
		f, err := strconv.ParseFloat(strconv.FormatFloat(x, 'f', ndigits, 64), 64)
		if err != nil {
			return 0, err
		}
		return f, nil
	}
	return roundToTens(x, -ndigits)
}

// roundToTens は x を 10^k の倍数へ丸める (厳密な中間は偶数へ)。
func roundToTens(x float64, k int) (float64, error) {
	rat := new(big.Rat).SetFloat64(x)
	neg := math.Signbit(x)
	rat.Abs(rat)
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(k)), nil)
	rat.Quo(rat, new(big.Rat).SetInt(scale))
	q, rem := new(big.Int).QuoRem(rat.Num(), rat.Denom(), new(big.Int))
	switch twice := new(big.Int).Lsh(rem, 1).Cmp(rat.Denom()); {
	case twice > 0, twice == 0 && q.Bit(0) == 1:
		q.Add(q, big.NewInt(1))
	}
	q.Mul(q, scale)
	f, _ := new(big.Float).SetInt(q).Float64()
	if math.IsInf(f, 0) {
		return 0, &Error{Class: ClassOverflowError, Msg: "rounded value too large to represent"}
	}
	if neg {
		f = -f
	}
	return f, nil
}

// RoundInt は Python の round(x) (ndigits なし。戻り値は int で、厳密な中間は偶数へ)。nan と inf は Python と同じ種類のエラー。
func RoundInt(x float64) (*big.Int, error) {
	switch {
	case math.IsNaN(x):
		return nil, &Error{Class: ClassValueError, Msg: "cannot convert float NaN to integer"}
	case math.IsInf(x, 0):
		return nil, &Error{Class: ClassOverflowError, Msg: "cannot convert float infinity to integer"}
	}
	rounded := math.Round(x)
	if math.Abs(x-rounded) == 0.5 {
		rounded = 2 * math.Round(x/2)
	}
	n, _ := new(big.Float).SetFloat64(rounded).Int(nil)
	return n, nil
}
