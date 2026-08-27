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
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
	"golang.org/x/term"

	"glogx/termwidth"
	"glogx/widthenv"
)

type probe struct {
	label string
	s     string
	// ui は glogx が実際に画面へ出す文字か。false = 幅モデルの判別だけが目的の診断用。
	// ⚠️ 分けないと結語 (「食い違った文字は使用をやめる」) が診断用文字にも掛かって無意味になり、
	//    「食い違い N 件」も診断ノイズに支配される (実測: 8 件中 6 件が診断用だった)。
	ui bool
}

// widths は 3 つの幅モデルの答えを並べたもの (issue 124)。
//
// ⚠️ かつてこのツールは `want` (= ansi.StringWidth) だけを「描画エンジンの幅」として出していたが、
// **その前提は偽**だった: bubbletea v2 の描画エンジン (ultraviolet) の既定は `ansi.WcWidth` で、
// 端末が Unicode Core (mode 2027) を報告したときだけ GraphemeWidth へ切り替わる。
// 3 つ並べないと「端末がどのモデルと一致するか」= 揃える先が決められない。
type widths struct {
	grapheme int // ansi.StringWidth        — glogx の dispWidth (termwidth.Of) がこれ
	wc       int // ansi.StringWidthWc      — v2 描画エンジンの既定
	uniseg   int // uniseg.StringWidth      — 分割に使っているライブラリの幅モデル
}

// depVersion は解決された依存のバージョンを返す (見つからなければ "?")。
//
// ⚠️ **wc 列は固定の座標系ではない**。ansi.StringWidthWc は内部で mattn/go-runewidth を使い、
// その版で答えが変わる (実測 2026-08-28: `ಕಾ` の wc は v0.0.23 で 1、v0.0.27 で 2)。
// go-runewidth は indirect 依存なので、無関係な更新で判定が反転しうる。
// **過去の測定結果を後から再現できるよう、必ず出力に残す。**
func depVersion(path string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "?"
	}
	for _, d := range info.Deps {
		if d.Path == path {
			return d.Version
		}
	}
	return "?"
}

// padDisp は表示幅 w まで右を空白で詰める (表の桁揃え用)。
func padDisp(s string, w int) string {
	if n := w - ansi.StringWidth(s); n > 0 {
		return s + termwidth.PadSpaces(n)
	}
	return s
}

func modelWidths(s string) widths {
	return widths{
		grapheme: ansi.StringWidth(s),
		wc:       ansi.StringWidthWc(s),
		uniseg:   uniseg.StringWidth(s),
	}
}

func main() {
	// ⚠️ 幅を変える env の下では測定そのものが無意味になる (RUNEWIDTH_EASTASIAN=1 は
	//    ansi の wcOptions/dwOptions を両方反転させ、grapheme 列も wc 列も同時にずれる)。
	//    本体と各 TestMain は既にこの関門を通っているが、**揃える先を決めるこの道具だけが
	//    素通りして誤った判定列を静かに出していた** (2026-08-28)。
	widthenv.ExitIfUnsupported()

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		fmt.Fprintln(os.Stderr, "width-probe: 実端末で実行してください (CPR を使うため TTY が必要)")
		fmt.Fprintln(os.Stderr, "  例: go run ./tools/width-probe")
		os.Exit(2)
	}
	// ⚠️ CPR は **stdout へ書いて stdin から読む**。stdout をリダイレクトすると問い合わせが
	//    端末に届かず、stdin は TTY のままなので上のガードを通ってしまい、応答待ちで固まる。
	//    「出力を issue に貼るためファイルへ落とす」で実際に踏む形なので、ここで止める。
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, "width-probe: stdout もリダイレクトせず端末のまま実行してください")
		fmt.Fprintln(os.Stderr, "  (CPR の問い合わせは stdout へ書くので、リダイレクトすると応答が返りません)")
		fmt.Fprintln(os.Stderr, "  結果を残したいときは tee ではなく、画面をコピーするか script(1) を使う")
		os.Exit(2)
	}
	probes := []probe{
		{"ASCII x (基準)", "x", true},
		{"全角 あ (基準)", "あ", true},
		// ⚠ ❤ ✔ は Emoji かつ Extended_Pictographic だが Emoji_Presentation ではない
		// (実測 2026-07-25、Unicode 16.0 emoji-data.txt)。つまり「VS16 無しなら text 既定」だが
		// 既定をどう解釈するかは端末の裁量で、絵文字幅 (2) に振る実装がありうる。bare で割れる
		// なら VS15 (text presentation を明示) で端末に「文字として描け」と指示できる。
		// この 3 段 (bare / VS16 / VS15) の比較が対策選択の分岐点になる。
		{"bare ⚠ U+26A0", "⚠", true},
		{"⚠+VS16 U+FE0F", "⚠️", true},
		{"⚠+VS15 U+FE0E", "⚠︎", true},
		{"bare ✔ U+2714", "✔", true},
		{"✔+VS15", "✔︎", true},
		// 以下は非絵文字 (Emoji プロパティなし) なので構造的に幅 1 で安定するはず。
		// ここが割れるなら端末の幅モデル自体が疑わしい (glogx の枠・記号が全部ずれる)。
		{"✓ U+2713", "✓", true},
		{"✗ U+2717", "✗", true},
		{"● U+25CF", "●", true},
		{"⊘ U+2298", "⊘", true},
		{"│ U+2502", "│", true},
		{"╔ U+2554", "╔", true},
		{"█ U+2588", "█", true},
		{"▓ U+2593", "▓", true},
		{"▖ U+2596", "▖", true},
		{"⠋ spinner U+280B", "⠋", true},
		{"❯ U+276F", "❯", true},
		// ── ここから issue 124: 3 つの幅モデルが割れる範囲 ────────────────────────
		// ⚠️ **「絵文字は無情報」ではない**。上の `⚠+VS16` は (grapheme 2 / wc 1 / uniseg 2) で、
		//    実は一覧中で最も決定力のある probe (実測 2026-08-28)。外さないこと。
		// 以下は「grapheme と wc が割れる」形を増やすもの。glogx が出す文字ではない (ui=false)。
		{"RI 単独 🇦 U+1F1E6", "\U0001F1E6", false}, // (2,1,2)
		{"keycap 1️⃣", "1\uFE0F\u20E3", false},   // (2,1,1)
		// ⚠️ 上の一覧に「grapheme と wc が割れる」形は ⚠+VS16 の 1 つしか無かった。
		// 実測 (2026-08-28) では 3 モデルの答えの組み合わせは 32 種あり、以下はそのうち
		// 判別に効くものを足したもの。インド系の母音記号と Arabic format 文字は
		// 単一 rune クラスタで ansi と uniseg が食い違う 434 件の主要な出どころでもある。
		{"カンナダ ಕಾ", "ಕಾ", false},
		{"デーヴァナーガリー का", "का", false},
		{"タミル கா", "கா", false},
		{"Arabic sign U+0600", "\u0600", false},
		{"ベンガル母音 U+09BE", "\u09be", false},
		// 分割器の食い違い (uniseg=Unicode 15 / x/ansi=16)。U+0897 は 16 で追加された結合マーク。
		// uniseg は単独クラスタとして返し x/ansi は前の文字と 1 クラスタに数える → 「クラスタごとの
		// 幅の総和 ≠ 全体の幅」になり dropToColumn の列不変条件が崩れる。
		{"x+U+0897 (分割が割れる)", "x\u0897", false},
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
	fmt.Printf("width-probe  TERM=%s  TERM_PROGRAM=%s  TMUX=%v  SSH=%v\n",
		os.Getenv("TERM"), os.Getenv("TERM_PROGRAM"), inTmux, os.Getenv("SSH_TTY") != "")
	fmt.Printf("  deps: x/ansi=%s  uniseg=%s  go-runewidth=%s (wc 列はこの版に依存する)\n",
		depVersion("github.com/charmbracelet/x/ansi"),
		depVersion("github.com/rivo/uniseg"),
		depVersion("github.com/mattn/go-runewidth"))
	fmt.Printf("  grapheme = ansi.StringWidth (glogx の dispWidth) / wc = ansi.StringWidthWc\n")
	fmt.Printf("  (v2 描画エンジンの既定) / uniseg = 分割に使っているライブラリ / got = 端末の実測\n\n")
	// ⚠️ ラベルは全角と半角が混ざるので %-24s (バイト幅) では揃わない。表示幅で詰める
	//    (幅の道具が自分の表を揃えられていないのは説得力を欠く)
	fmt.Printf("%s %9s %4s %7s %5s  %s\n", padDisp("文字", 26), "grapheme", "wc", "uniseg", "got", "判定")
	mismatch := 0
	for _, r := range results {
		w := modelWidths(r.s)
		if r.err != nil {
			fmt.Printf("%s %9d %4d %7d %5s  応答なし (%v)\n", padDisp(r.label, 26), w.grapheme, w.wc, w.uniseg, "-", r.err)
			continue
		}
		// どのモデルが端末と一致したかを出す (揃える先を決めるのが目的。issue 124)
		var agree []string
		for _, m := range []struct {
			name string
			v    int
		}{{"grapheme", w.grapheme}, {"wc", w.wc}, {"uniseg", w.uniseg}} {
			if m.v == r.got {
				agree = append(agree, m.name)
			}
		}
		verdict := "一致: " + strings.Join(agree, ",")
		if len(agree) == 0 {
			verdict = "✗ どのモデルとも一致しない"
		}
		if r.got != w.grapheme {
			if r.ui {
				mismatch++
				verdict += "  ← glogx (grapheme) とズレる"
			} else {
				verdict += "  (診断用)"
			}
		}
		fmt.Printf("%s %9d %4d %7d %5d  %s\n", padDisp(r.label, 26), w.grapheme, w.wc, w.uniseg, r.got, verdict)
	}
	// ⚠️ 基準文字が期待どおりでなければ、この run 全体を無効として扱う。
	//    CPR は再同期を入れたが、それでも stale な応答や端末の非対応で列がずれることはある。
	//    「ASCII x が 1 でない」= 測定が壊れている、という判定は端末の幅モデルに依存しない。
	for _, r := range results {
		if r.s == "x" && r.err == nil && r.got != 1 {
			fmt.Printf("\n⚠️ 基準文字 ASCII x の測定が %d (期待 1)。**この run の結果は無効**です。\n", r.got)
			fmt.Println("   CPR の応答がずれています (キー入力の紛れ込み / 端末の非対応)。")
			fmt.Println("   何もキーを押さずに走らせ直してください。")
			return
		}
	}

	fmt.Printf("\n食い違い (glogx が実際に出す文字のみ): %d 件\n", mismatch)
	if mismatch > 0 {
		fmt.Println("→ 食い違った文字は width.go の正規化対象 (bare / VS15 へ倒す・使用をやめる) を検討する。")
	}
	fmt.Println("→ 「診断用」の行は幅モデルの判別だけが目的 (glogx は出さない)。got がどの列と一致したかを見る:")
	fmt.Println("   grapheme と一致 = 今の dispWidth のままでよい / wc と一致 = 描画エンジンの既定側へ揃える議論 (issue 124)。")
	if inTmux {
		fmt.Println("→ tmux の外でも実行して got を比べること (差があれば tmux が幅を書き換えている)。")
	} else {
		fmt.Println("→ tmux の中でも実行して got を比べること (差があれば tmux が幅を書き換えている)。")
	}

	// 症状は「静的なズレ」ではなく「毎秒 1↔2 を往復する揺れ」なので、1 回の測定では捕まらない。
	// 同じ文字を連続測定して got が振れるかを見る (振れるなら端末が状態依存で幅を変えている =
	// フォント fallback やリガチャ・再描画のタイミングが絡む)。
	if err := watchJitter(fd, probes); err != nil {
		fmt.Fprintln(os.Stderr, "揺れ観測でエラー:", err)
	}
}

// watchJitter は各文字を繰り返し測り、測定間で幅が変化するかを報告する。
// 「毎秒 1↔2 を繰り返す」症状は静的な食い違いではないので、これが本題の観測になる。
func watchJitter(fd int, probes []probe) error {
	const rounds = 12
	old, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer func() { _ = term.Restore(fd, old) }()

	seen := make([]map[int]int, len(probes)) // probe ごとに 観測幅 → 回数
	for i := range seen {
		seen[i] = map[int]int{}
	}
	for range rounds {
		for i, p := range probes {
			if got, err := advance(p.s); err == nil {
				seen[i][got]++
			}
		}
		time.Sleep(120 * time.Millisecond) // 再描画/status-interval と位相をずらす
	}
	_ = term.Restore(fd, old)

	fmt.Printf("\n--- 揺れ観測 (%d 回測定。同じ文字で幅が変われば端末が状態依存で幅を変えている) ---\n", rounds)
	unstable := 0
	for i, p := range probes {
		if len(seen[i]) > 1 {
			unstable++
			fmt.Printf("%-22s ✗ 揺れた: %v\n", p.label, seen[i])
		}
	}
	if unstable == 0 {
		fmt.Println("揺れなし — 全文字が毎回同じ幅。この層 (端末単体) は安定している。")
		fmt.Println("→ ズレが glogx で再現するなら原因は端末の幅解釈ではなく、")
		fmt.Println("   glogx / 描画エンジンが出すバイト列側 (再描画の差分計算) を疑う。")
	} else {
		fmt.Printf("→ %d 文字が揺れた。これが症状の直接原因。該当文字を使わない方向で対策する。\n", unstable)
	}
	return nil
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
// readByte は 1 バイト読む。**タイムアウトは goroutine + select で実装する**。
//
// ⚠️ `os.Stdin.SetReadDeadline` は TTY では効かない: darwin/Go 1.25 で
// `file type does not support deadline` を返す (実測 2026-08-28)。以前はその戻り値を
// `_ =` で握り潰しており、**CPR が返らない端末では raw mode のまま無限ハングした**
// (raw mode は ISIG を落とすので Ctrl-C も効かず、defer の Restore にも到達しない)。
func readByte(timeout time.Duration) (byte, error) {
	type res struct {
		b   byte
		err error
	}
	ch := make(chan res, 1)
	go func() {
		var b [1]byte
		n, err := os.Stdin.Read(b[:])
		if err != nil {
			ch <- res{err: err}
			return
		}
		if n == 0 {
			ch <- res{err: errors.New("0 バイト読み込み")}
			return
		}
		ch <- res{b: b[0]}
	}()
	select {
	case r := <-ch:
		return r.b, r.err
	case <-time.After(timeout):
		// ⚠️ goroutine は read でブロックしたまま残る。診断ツールなので、呼び出し側は
		//    端末を戻してから exit する (プロセスごと終わるので leak は問題にならない)。
		return 0, errTimeout
	}
}

var errTimeout = errors.New("端末から応答がありません (CPR/DECRQM のタイムアウト)")

// cursorCol は CPR で現在の列を聞く。
//
// ⚠️ **ESC から読み直す (再同期)**。以前は「R が来るまで読む」だけで、stale な応答や
// ユーザーのキー入力が紛れると**以降すべての測定が 1 つ前の応答を読み続けた**。
// 偽端末の実験で「R を 1 バイト注入するだけで、決定論的な端末に対して『8 文字が揺れた』という
// 存在しない結論を捏造できる」ことを確認している (2026-08-28)。
func cursorCol() (int, error) {
	if _, err := fmt.Print("\x1b[6n"); err != nil {
		return 0, err
	}
	// ESC を見つけるまで捨てる (紛れ込んだバイトをここで落とす)
	for range 64 {
		b, err := readByte(2 * time.Second)
		if err != nil {
			return 0, err
		}
		if b == 0x1b {
			goto body
		}
	}
	return 0, errors.New("CPR 応答の ESC が見つかりません")
body:
	buf := make([]byte, 0, 32)
	for range 32 {
		b, err := readByte(2 * time.Second)
		if err != nil {
			return 0, err
		}
		if b == 'R' {
			break
		}
		buf = append(buf, b)
	}
	// buf = "[<row>;<col>"
	i := bytes.LastIndexByte(buf, ';')
	if i < 0 {
		return 0, fmt.Errorf("CPR 応答を解析できません: %q", buf)
	}
	return strconv.Atoi(strings.TrimSpace(string(buf[i+1:])))
}
