package termwidth

// fastDispWidth は arm64 (Apple Silicon) ではアセンブリ版を使う。受理規則と返り値は
// fastDispWidthGeneric と同一 (契約はそちらの doc。一致は FuzzFastDispWidthMatchesGeneric が守る)。
//
// 実測 2026-09-26 (macos-15 runner = Apple M1 Virtual, go1.25.0, BenchmarkFastDispWidth -count 10,
// benchstat の中央値 generic → asm): ascii_80B 51.2→6.6ns (-87%) / ascii_1KB 718→61ns (-92%) /
// sgr_commit_line 62.0→46.7ns (-25%) / status_row 87.2→65.4ns (-25%) / box_120 563→303ns (-46%) /
// cjk_reject 37.0→21.5ns (-42%) / short_ascii_8B 7.1→7.9ns (**+12%**。16 byte 未満は NEON を使わず、
// 呼び出しと定数の準備の分だけ負ける)。
// glogx の 1 フレーム (BenchmarkViewSteady 等 6 本、master と交互に 8 回) は geomean -7.4% だが
// 全項目 p>0.2 で**有意差なし** (runner のばらつき ±15〜50% に埋もれる)。フレームの支配項は幅計算の
// 外にある。
func fastDispWidth(s string) (int, bool) {
	return fastDispWidthAsm(s)
}

// fastDispWidthAsm は fastwidth_arm64.s にある。symWidthTable を読むので、init が表を
// 焼き終える前 (= パッケージの init 中) に呼んではいけない。
//
//go:noescape
func fastDispWidthAsm(s string) (int, bool)
