package diff

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"ja-writing/cmdenv"
)

// BenchmarkDiffPair は JA_WRITING_BENCH_BEFORE と JA_WRITING_BENCH_AFTER のファイルを diff に通す (pprof で処理の時間の内訳を見る)。
// check run の所要の大半もこの diff (check が子として起こす) なので、check の内訳もここで見る。
// 入力は bench/gen.py の pair-*-before.md と pair-*-after.md を渡す。環境変数が無ければ飛ばす。
//
//	JA_WRITING_BENCH_BEFORE=<dir>/pair-256k-before.md JA_WRITING_BENCH_AFTER=<dir>/pair-256k-after.md \
//	  go test -run '^$' -bench BenchmarkDiffPair -cpuprofile cpu.out ./diff/
func BenchmarkDiffPair(b *testing.B) {
	before, after := os.Getenv("JA_WRITING_BENCH_BEFORE"), os.Getenv("JA_WRITING_BENCH_AFTER")
	if before == "" || after == "" {
		b.Skip("JA_WRITING_BENCH_BEFORE か JA_WRITING_BENCH_AFTER が無い")
	}
	cwd, err := os.Getwd()
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		var errOut strings.Builder
		env := cmdenv.Env{Args: []string{before, after}, Stdout: io.Discard, Stderr: &errOut, Cwd: cwd}
		if rc := Main(context.Background(), env); rc != 0 {
			b.Fatalf("rc=%d: %s", rc, errOut.String())
		}
	}
}
