package difflib

import "math/bits"

// 部分文字列の多項式の hash (本家の _rolling_hashes と同じ式。法は素数 2^61-1、底は字の番号の最大 0x10FFFF より大きい)。
// 本家は短い部分で Python の hash(str) を使うが、Go に同じものは無いので全部これにする。hash は表を引く手がかりにだけ使い、引いた位置は必ず字で比べるので、
// hash の選び方は答えを変えない。
const (
	rollingMod  = 1<<61 - 1
	rollingBase = 1000000007
)

// mulMod は a*b mod 2^61-1 (a, b < 2^61)。
func mulMod(a, b uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	s := (hi<<3 | lo>>61) + lo&rollingMod
	for s >= rollingMod {
		s -= rollingMod
	}
	return s
}

func addMod(a, b uint64) uint64 {
	s := a + b
	if s >= rollingMod {
		s -= rollingMod
	}
	return s
}

// code は字の番号を hash の計算に使える値にする。mulMod と addMod は引数が 2^61 未満であることを前提にするので、負の rune (uint64 にすると 2^63 以上) を
// uint32 で 2^32 未満に収める。Python の str と pyunicode.Runes は負の値を作らないが、外から渡した []rune で前提が崩れると、同じ並びが別の hash になって一致を取りこぼす。
func code(r rune) uint64 { return uint64(uint32(r)) }

// polyHash は rs 全体の hash (rollingHashes(rs, len(rs))[0] と同じ値)。
func polyHash(rs []rune) uint64 {
	var v uint64
	for _, r := range rs {
		v = addMod(mulMod(v, rollingBase), code(r))
	}
	return v
}

// rollingHashes は rs の長さ size の部分ごとの hash を、位置の順に返す (部分が無ければ空)。1 字ずつずらして求めるので、1 つの部分の手間は size によらない。
func rollingHashes(rs []rune, size int) []uint64 {
	if size <= 0 || len(rs) < size {
		return nil
	}
	top := uint64(1)
	for range size - 1 {
		top = mulMod(top, rollingBase)
	}
	out := make([]uint64, 0, len(rs)-size+1)
	v := polyHash(rs[:size])
	out = append(out, v)
	for k := size; k < len(rs); k++ {
		// v から先頭の字の分 (old * base^(size-1)) を引き、底を掛けて次の字を足す。引き算は法を足してから行う (負にしない)。
		v = addMod(v, rollingMod-mulMod(code(rs[k-size]), top))
		v = addMod(mulMod(v, rollingBase), code(rs[k]))
		out = append(out, v)
	}
	return out
}

// indexRunes は hay[lo:hi] の中で needle が最初に現れる位置 (hay の添字)。無ければ -1。Python の hay.find(needle, lo, hi) と同じ値
// (needle が空のときは lo)。KMP なので、同じ字の長い繰り返しでも手間は hi-lo と len(needle) の和に比例する。
func indexRunes(hay []rune, lo, hi int, needle []rune) int {
	n := len(needle)
	if n == 0 {
		return lo
	}
	if hi-lo < n {
		return -1
	}
	fail := make([]int, n)
	for i, k := 1, 0; i < n; i++ {
		for k > 0 && needle[i] != needle[k] {
			k = fail[k-1]
		}
		if needle[i] == needle[k] {
			k++
		}
		fail[i] = k
	}
	k := 0
	for i := lo; i < hi; i++ {
		for k > 0 && hay[i] != needle[k] {
			k = fail[k-1]
		}
		if hay[i] == needle[k] {
			k++
		}
		if k == n {
			return i - n + 1
		}
	}
	return -1
}
