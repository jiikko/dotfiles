package termwidth

import (
	"strings"
	"testing"
)

// fastWidthBoundaryInputs は arch 版 (arm64 はアセンブリ) が Go 版と割れやすい形を並べる。
// arm64 版は 16 byte ずつ NEON で ASCII を数え、切れ目だけをスカラで見るので、
// **受理しない byte / SGR / 記号が 16 byte 境界のどこに落ちるか**を全位置で動かす。
func fastWidthBoundaryInputs() []string {
	specials := []string{
		"\x1b[32m", "\x1b[m", "\x1b[38;5;214m", // SGR (受理)
		"─", "·", "⠋", "✓", "…", // 表にある記号 (2 byte / 3 byte)
		"\x1b[2K", "\x1b[38;5;", "\x1b", "\x1bX", // SGR 以外・途中で切れた列 (棄却)
		"\t", "\x7f", "\x00", "\x1f", // C0 / DEL (棄却)
		"é", "漢", "　", "⚠️", "\U0001f680", // 表の外・結合 (棄却)
		"\xff", "\xc0\x80", "\xc1\xbf", "\xe0\x82\xb7", "\xe2\x94", "\xc2", "\x80", // 不正な UTF-8 (棄却)
		"\xed\xa0\x80", "\xe3\x80\x80", "\xf0\x9f\x9a\x80", // サロゲート・表の上限超え・4 byte
		"\xc2\xb7", "\xc2\xb6", "\xe2\xa3\xbf", "\xe2\xa4\x80", // 表の下限・その 1 つ下・上限・その 1 つ上
	}
	var out []string
	for _, sp := range specials {
		for pre := range 40 {
			a := strings.Repeat("a", pre)
			out = append(out, a+sp, a+sp+strings.Repeat("z", 20), sp+a)
			// 切れ目が 1 つの 16 byte の中に 2 回出る形
			out = append(out, a+sp+"bc"+sp+strings.Repeat("y", 17))
		}
	}
	return out
}

// arch 版の fastDispWidth が Go 版 (受理規則の正本) と入力によらず一致すること。
// arm64 以外では同じ関数なので恒真だが、arm64 ではアセンブリを通る。
func TestFastDispWidthMatchesGeneric(t *testing.T) {
	inputs := fastWidthBoundaryInputs()
	for _, s := range inputs {
		gw, gok := fastDispWidthGeneric(s)
		aw, aok := fastDispWidth(s)
		if gw != aw || gok != aok {
			t.Fatalf("arch=(%d,%v) generic=(%d,%v) for %q", aw, aok, gw, gok, s)
		}
	}
	t.Logf("%d 入力で一致", len(inputs))
}

// 差分 fuzz: arch 版が Go 版と一致すること (arm64 のアセンブリの境界を fuzzer に探させる)。
//
//	GOARCH=arm64 なマシンで: go test -run '^$' -fuzz FuzzFastDispWidthMatchesGeneric -fuzztime=60s .
func FuzzFastDispWidthMatchesGeneric(f *testing.F) {
	for _, s := range fastWidthBoundaryInputs() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		gw, gok := fastDispWidthGeneric(s)
		aw, aok := fastDispWidth(s)
		if gw != aw || gok != aok {
			t.Fatalf("arch=(%d,%v) generic=(%d,%v) for %q", aw, aok, gw, gok, s)
		}
	})
}

// fastWidthBenchCases は glogx の実際の行の形を模す (幅計算は 1 フレームの全可視行で走る)。
var fastWidthBenchCases = []struct {
	name string
	s    string
}{
	{"short_ascii_8B", "abcdefgh"},
	{"ascii_80B", strings.Repeat("0123456789", 8)},
	{"ascii_1KB", strings.Repeat("0123456789abcdef", 64)},
	// git log --color=always の 1 行 (verbatim 経路)
	{"sgr_commit_line", "\x1b[33mcommit 0123456789abcdef0123456789abcdef01234567\x1b[m (\x1b[1;36mHEAD -> \x1b[m\x1b[1;32mmaster\x1b[m)"},
	// 一覧の行: CI 状態の記号 + 色 + 枠
	{"status_row", "│ \x1b[32m✓\x1b[0m 2270ab5 \x1b[2m3 hours ago\x1b[0m  docs(rules): measure perf before fixing it  \x1b[31m✗\x1b[0m lint │"},
	// 罫線だけ (記号が連続し ASCII の連なりが無い = NEON が効かない側)
	{"box_120", "┌" + strings.Repeat("─", 118) + "┐"},
	// 日本語の subject: 途中で棄却されて ansi へ落ちる行 (棄却までのコスト)
	{"cjk_reject", "│ \x1b[32m✓\x1b[0m 2270ab5 fix(glogx): 請求計算の境界条件を是正し、回帰を実測で固定する"},
}

// Go 版とアーキ版 (arm64 ではアセンブリ) を同じ入力で並べて測る。
//
//	go test -run '^$' -bench BenchmarkFastDispWidth -benchmem -count 10 ./termwidth/ | tee new.txt
//	benchstat -col /impl new.txt
func BenchmarkFastDispWidth(b *testing.B) {
	impls := []struct {
		name string
		fn   func(string) (int, bool)
	}{
		{"generic", fastDispWidthGeneric},
		{"arch", fastDispWidth},
	}
	for _, c := range fastWidthBenchCases {
		for _, im := range impls {
			b.Run(c.name+"/impl="+im.name, func(b *testing.B) {
				b.SetBytes(int64(len(c.s)))
				for b.Loop() {
					im.fn(c.s)
				}
			})
		}
	}
}
