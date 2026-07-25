// width-probe は「端末が各文字に実際に何セル割り当てるか」を端末自身に問い合わせて表示する。
//
// なぜ必要か: glogx の幅ズレ問題は 3 層 (glogx / 描画エンジン x/ansi / 端末) の幅解釈が
// 一致しないと起きる。前 2 層はコードから読めるが、端末の割り当ては推測でしか語られてこなかった
// (2026-07-24 の対策と 2026-07-25 の再発報告で、同じ文字について正反対の前提が併存した)。
// CPR (Cursor Position Report, CSI 6n) で端末に直接聞けば推測が消える。
//
// 使い方 (必ず実端末で・tmux の内と外の両方で走らせて比較する):
//
//	go run ./tools/width-probe
//
// 出力の読み方: want と got が食い違う文字が「glogx が出してはいけない文字」。tmux の内と外で
// got が違う文字は tmux が幅を書き換えているので、tmux 側の設定 (allow-passthrough や
// TERM の再交渉) も疑う。
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

type probe struct {
	label string
	s     string
	want  int // x/ansi (= 描画エンジン) が信じる幅。端末がこれと違えばズレる
}

func main() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		fmt.Fprintln(os.Stderr, "width-probe: 実端末で実行してください (CPR を使うため TTY が必要)")
		fmt.Fprintln(os.Stderr, "  例: go run ./tools/width-probe")
		os.Exit(2)
	}
	probes := []probe{
		{"ASCII x (基準)", "x", 1},
		{"全角 あ (基準)", "あ", 2},
		{"bare ⚠ U+26A0", "⚠", 0},
		{"⚠+VS16 U+FE0F", "⚠️", 0},
		{"⚠+VS15 U+FE0E", "⚠︎", 0},
		{"✓ U+2713", "✓", 0},
		{"✗ U+2717", "✗", 0},
		{"● U+25CF", "●", 0},
		{"⊘ U+2298", "⊘", 0},
		{"│ U+2502", "│", 0},
		{"╔ U+2554", "╔", 0},
		{"█ U+2588", "█", 0},
		{"▓ U+2593", "▓", 0},
		{"▖ U+2596", "▖", 0},
		{"⠋ spinner U+280B", "⠋", 0},
		{"❯ U+276F", "❯", 0},
	}
	for i := range probes {
		if probes[i].want == 0 {
			probes[i].want = ansi.StringWidth(probes[i].s)
		}
	}

	// raw mode: CPR の応答を行バッファに邪魔されず読む
	old, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "width-probe: raw mode に入れません:", err)
		os.Exit(1)
	}
	defer func() { _ = term.Restore(fd, old) }()

	type result struct {
		probe
		got int
		err error
	}
	results := make([]result, 0, len(probes))
	for _, p := range probes {
		got, err := advance(p.s)
		results = append(results, result{probe: p, got: got, err: err})
	}
	_ = term.Restore(fd, old)

	inTmux := os.Getenv("TMUX") != ""
	fmt.Printf("width-probe  TERM=%s  TMUX=%v  (want = x/ansi = 描画エンジンの幅)\n\n",
		os.Getenv("TERM"), inTmux)
	fmt.Printf("%-22s %6s %6s  %s\n", "文字", "want", "got", "判定")
	mismatch := 0
	for _, r := range results {
		switch {
		case r.err != nil:
			fmt.Printf("%-22s %6d %6s  応答なし (%v)\n", r.label, r.want, "-", r.err)
		case r.got != r.want:
			mismatch++
			fmt.Printf("%-22s %6d %6d  ✗ 食い違い — glogx で出すとズレる\n", r.label, r.want, r.got)
		default:
			fmt.Printf("%-22s %6d %6d  ok\n", r.label, r.want, r.got)
		}
	}
	fmt.Printf("\n食い違い: %d 件\n", mismatch)
	if mismatch > 0 {
		fmt.Println("→ 食い違った文字は width.go の正規化対象 (bare へ倒す / 使用をやめる) を検討する。")
	}
	if inTmux {
		fmt.Println("→ tmux の外でも実行して got を比べること (差があれば tmux が幅を書き換えている)。")
	} else {
		fmt.Println("→ tmux の中でも実行して got を比べること (差があれば tmux が幅を書き換えている)。")
	}
}

// advance は s を出力してカーソルが進んだセル数を CPR で測る。行は測定後に消す。
func advance(s string) (int, error) {
	if _, err := fmt.Print("\r\x1b[K" + s); err != nil {
		return 0, err
	}
	col, err := cursorCol()
	fmt.Print("\r\x1b[K")
	if err != nil {
		return 0, err
	}
	return col - 1, nil // CPR は 1-origin
}

// cursorCol は CSI 6n の応答 (ESC [ row ; col R) から列を取り出す。
func cursorCol() (int, error) {
	if _, err := fmt.Print("\x1b[6n"); err != nil {
		return 0, err
	}
	// 応答が来ない端末で固まらないよう deadline を張る
	if f, ok := any(os.Stdin).(*os.File); ok {
		_ = f.SetReadDeadline(time.Now().Add(2 * time.Second))
		defer func() { _ = f.SetReadDeadline(time.Time{}) }()
	}
	var buf []byte
	b := make([]byte, 1)
	for range 32 {
		n, err := os.Stdin.Read(b)
		if err != nil {
			return 0, err
		}
		if n == 0 {
			continue
		}
		if b[0] == 'R' {
			break
		}
		buf = append(buf, b[0])
	}
	// buf = "\x1b[<row>;<col>"
	i := strings.LastIndexByte(string(buf), ';')
	if i < 0 {
		return 0, fmt.Errorf("CPR 応答を解析できません: %q", buf)
	}
	return strconv.Atoi(strings.TrimSpace(string(buf[i+1:])))
}
