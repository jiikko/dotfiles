//go:build !arm64

package termwidth

// fastDispWidth は arm64 以外では Go 版そのもの (契約は fastDispWidthGeneric の doc)。
func fastDispWidth(s string) (int, bool) {
	return fastDispWidthGeneric(s)
}
