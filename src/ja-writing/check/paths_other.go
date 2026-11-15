//go:build !darwin

package check

// diskSpelling は macOS 以外では使わない (本家の _disk_path は darwin 以外で realpath をそのまま返す)。
func diskSpelling(string) string { return "" }

const isDarwin = false
