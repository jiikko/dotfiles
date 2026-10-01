package runner

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"
)

// lockedBuffer は copyToTerminal が書く先 (書く goroutine と読む test の間で共有する)。
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// copyChunks は chunks を 1 つずつ別の読み取りとして UI の無い端末向けの経路へ流し、書かれた全体を返す。
// waitFor[i] が空でなければ、i 番目の chunk を流した後、次の chunk の前に出力がそれになるまで待つ (上限 2 秒。来なければ落とす)。
func copyChunks(t *testing.T, chunks, waitFor []string) string {
	t.Helper()
	out := &lockedBuffer{}
	sink := newLogSink(out, true)
	sink.terminalOutput = true
	r, w := io.Pipe()
	done := make(chan struct{})
	go func() { sink.CopyFrom(r, true); close(done) }()
	for i, c := range chunks {
		if _, err := w.Write([]byte(c)); err != nil { // io.Pipe の Write は読み手が読み終わるまで戻らない = 1 回の読み取り
			t.Fatal(err)
		}
		if i < len(waitFor) && waitFor[i] != "" {
			waitOutput(t, out, waitFor[i], i)
		}
	}
	_ = w.Close()
	<-done
	return out.String()
}

// waitOutput は出力が want になるまで待つ (条件のポーリング。上限を超えたら落とす)。
func waitOutput(t *testing.T, out *lockedBuffer, want string, i int) {
	t.Helper()
	for range 400 {
		if out.String() == want {
			return
		}
		time.Sleep(5 * time.Millisecond) // sleep-ok: tick: 条件を見ながら刻む待ちの helper の中の刻み (waitOutput)
	}
	t.Fatalf("%d 回目の読み取りの後の出力 = %q, want %q (次の読み取りを待たずに出るはず)", i+1, out.String(), want)
}

// 改行の無い催促は、次の改行を待たずに端末へ出る。\r の上書きは改行に化けず \r のまま渡る (変更前の素通しと同じ見え方)。
func TestCopyToTerminalWritesPromptsAndCarriageReturnsImmediately(t *testing.T) {
	got := copyChunks(t, []string{"Password: ", "\r 10%", "\r 20%", "\r 30%\n"},
		[]string{"Password: ", "Password: \r 10%", "Password: \r 10%\r 20%"})
	if want := "Password: \r 10%\r 20%\r 30%\n"; got != want {
		t.Fatalf("出力 = %q, want %q", got, want)
	}
}

// 読み取りの切れ目で途中になった制御シーケンスと UTF-8 の文字は、次の読み取りまで持ち越してから無害化する
// (切れ端を無害化すると、色が落ちる / 文字化けする)。端末を書き換える列は切れていても落とす。
func TestCopyToTerminalCarriesSplitSequences(t *testing.T) {
	for _, c := range []struct {
		name   string
		chunks []string
		want   string
	}{
		{"SGR が切れる", []string{"\x1b[3", "1mred\x1b[0m\n"}, "\x1b[0m\x1b[31mred\x1b[0m\x1b[0m\n"},
		{"UTF-8 が切れる", []string{"日\xe6\x9c", "\xac\n"}, "日本\n"},
		{"OSC が切れる", []string{"a\x1b]0;ev", "il\x07b\n"}, "ab\n"},
		{"画面消去が切れる", []string{"x\x1b[2", "Jy\n"}, "xy\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := copyChunks(t, c.chunks, nil); got != c.want {
				t.Fatalf("出力 = %q, want %q", got, c.want)
			}
		})
	}
}

// U+009C (C1 の ST) で閉じた OSC は終わったものとみなし、後ろの催促を持ち越さない (termsafe と終わりの判定を揃える)。
func TestCopyToTerminalTreatsC1STAsEndOfOSC(t *testing.T) {
	got := copyChunks(t, []string{"\x1b]0;t\xc2\x9cPassword: ", "x\n"}, []string{"Password: "})
	if want := "Password: x\n"; got != want {
		t.Fatalf("出力 = %q, want %q", got, want)
	}
}
