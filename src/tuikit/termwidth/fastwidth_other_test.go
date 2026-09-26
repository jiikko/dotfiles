//go:build !arm64

package termwidth

// archKernel は arm64 以外では Go 版そのもの (差分テストは恒真になる。対になる arm64 版の doc 参照)。
func archKernel(s string) (int, bool) { return fastDispWidthGeneric(s) }
