package termwidth

// fastDispWidth は arm64 (Apple Silicon) ではアセンブリ版を使う。受理規則と返り値は
// fastDispWidthGeneric と同一 (契約はそちらの doc。一致は FuzzFastDispWidthMatchesGeneric が守る)。
func fastDispWidth(s string) (int, bool) {
	return fastDispWidthAsm(s)
}

// fastDispWidthAsm は fastwidth_arm64.s にある。symWidthTable を読むので、init が表を
// 焼き終える前 (= パッケージの init 中) に呼んではいけない。
//
//go:noescape
func fastDispWidthAsm(s string) (int, bool)
