package lint

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"ja-writing/cmdenv"
)

// BenchmarkLintFile は JA_WRITING_BENCH_FILE のファイルを標準入力で lint に通す (pprof で処理の時間の内訳を見る。issue 1079)。
// 入力は bench/gen.py の出力を渡す。環境変数が無ければ飛ばす。
//
//	JA_WRITING_BENCH_FILE=<gen.py の出力>/len-1024k.md go test -run '^$' -bench BenchmarkLintFile -cpuprofile cpu.out ./lint/
func BenchmarkLintFile(b *testing.B) {
	path := os.Getenv("JA_WRITING_BENCH_FILE")
	if path == "" {
		b.Skip("JA_WRITING_BENCH_FILE が無い")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	dir := b.TempDir()
	for b.Loop() {
		env := cmdenv.Env{Args: []string{"-"}, Stdin: bytes.NewReader(data), Stdout: io.Discard, Stderr: io.Discard, Cwd: dir}
		if rc := Main(context.Background(), env); rc != 0 {
			b.Fatalf("rc=%d", rc)
		}
	}
}
