//go:build !darwin || !cgo

// TIS 直接呼び出し (ime_tis_darwin.go) の非 darwin / 非 cgo スタブ。常に「取れない/失敗」を
// 返し、IME 自動切替を安全に無効化する。
package main

func tisCurrentSourceID() (string, bool) { return "", false }

func tisSelectSourceID(string) bool { return false }
