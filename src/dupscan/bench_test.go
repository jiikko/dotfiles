package main

import (
	"io"
	"os"
	"testing"
)

// 実在の repo で確保量と所要を測る (性能の前後を比べるときだけ使う。DUPSCAN_BENCH_DIR が無ければ skip)。
//
//	DUPSCAN_BENCH_DIR=/path/to/repo/src go test -run xxx -bench . -benchtime 10x -benchmem .
//	DUPSCAN_BENCH_DIR=... go test -run xxx -bench Report -cpuprofile cpu.prof -memprofile mem.prof .
//
// 出力は捨てる。直す前後で出力が byte 同一であることは、このベンチではなく bin/dupscan の出力を cmp で確かめる。
func benchDir(b *testing.B) string {
	b.Helper()
	dir := os.Getenv("DUPSCAN_BENCH_DIR")
	if dir == "" {
		b.Skip("DUPSCAN_BENCH_DIR が無い")
	}
	return dir
}

func BenchmarkReport(b *testing.B) {
	dir := benchDir(b)
	b.ReportAllocs()
	for b.Loop() {
		run([]string{"report", "-limit", "0", dir}, io.Discard, io.Discard)
	}
}

func BenchmarkSweep(b *testing.B) {
	dir := benchDir(b)
	b.ReportAllocs()
	for b.Loop() {
		run([]string{"sweep", "-similar-max-units", "9000", dir}, io.Discard, io.Discard)
	}
}
