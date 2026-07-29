//go:build !darwin || !cgo

// TIS 直接呼び出し (ime_tis_darwin.go) の非 darwin / 非 cgo スタブ。常に「取れない/失敗」を
// 返し、呼び出し側 (ime.go) を従来の macism fork 経路へ落とす。macism 自体 macOS 専用なので
// 非 darwin では実質 no-op のまま。
package main

func tisCurrentSourceID() (string, bool) { return "", false }

func tisSelectSourceID(string) bool { return false }
