package usage

// statusline (shell) との乖離検出。
//
// glogx の pace 判定 (paceBand / paceState) と同じルールが `_claude/statusline-command.sh` の
// `pace_row` にも書かれている (帯 25/10・超過/先行/適正/余裕/余剰 の閾値・状態語)。**言語が
// 違うので Go パッケージへ切り出しても共有できない**ため、実装は 2 本のまま「乖離したら赤に
// なる」形で固定する。
//
// 🚨 定数の綴りを比較しない。shell は $(( -pr_band * 5 / 2 )) のような整数式で書き、Go は
// -band*2.5 と書くので、綴りで比べると「同じ意味の別表記」を乖離と誤検出する。代わりに
// **整数 delta の全域で状態語が一致するか**を見る (両実装の判定を突き合わせる差分テスト)。
//
// 🚨 抽出に失敗したら FAIL する (skip しない)。「検査できなかった」を緑にすると、shell の
// 書き方が変わった日から乖離検出が黙って止まる (rules/adversarial-review-own-safeguards)。
//
// 🚨 **状態語の一致まで要求する**のは、同じ枠を 2 画面が違う語で呼ぶと読み替えが要るため
// (issue 144 の目視確認に「statusline のペース行と同じ見え方になっているか」が入っていたのは
// これを人の目で見ようとしていた)。🚨 意図的に片方だけ語を変えたくなったら、この検査を
// 「語の対応表」を持つ形へ変えること (両方書き換えて無理に揃えない。片方の表示都合で
// もう片方の語を歪めるのが一番損)。
//
// 🚨 **`go test` のキャッシュはこのファイルが読む shell の変更を見ない** (実測 2026-09-01:
// shell の帯を変えても `(cached) ok` が返る)。shell だけを変えたときに確実に走らせる経路は
// tests/claude/test_statusline.sh が `-count=1` つきで持っている。

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// statuslinePath は statusline スクリプトの絶対パス。テストのソース位置から repo root を
// 探して組む (テストの cwd はパッケージディレクトリで、repo root からの相対では届かない)。
func statuslinePath(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("テスト自身のパスが取れない (乖離を検査できない)")
	}
	dir := filepath.Dir(self)
	for range 8 {
		p := filepath.Join(dir, "_claude", "statusline-command.sh")
		// 🚨 「見つかった」だけでは採らない。repo root であること (.git が隣にある) も確かめる。
		// この repo の tmp/ 配下には過去の検証で作った古い複製が実在し (tmp/verify-tests/sbx/
		// _claude/statusline-command.sh 等)、module を tmp へ丸ごとコピーして走らせると、
		// 本物より先にそれが見つかって**古い shell と比較して緑になる** (red team 指摘 2026-09-01)。
		if _, err := os.Stat(p); err == nil {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				return p
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// 🚨 ここで skip しない。見つからない = 乖離を検査できていない、であって合格ではない。
	t.Fatalf("repo root の _claude/statusline-command.sh が見つからない (%s から上へ探索。.git の隣にあるものだけを採る)", filepath.Dir(self))
	return ""
}

// shellBands は `hour)` / `day)` の分岐から pr_band を取る。
func shellBands(t *testing.T, src string) map[string]int {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\s*(hour|day)\)\s+.*?pr_band=(\d+)`)
	ms := re.FindAllStringSubmatch(src, -1)
	if len(ms) != 2 {
		t.Fatalf("pr_band の抽出が %d 件 (hour / day の 2 件のはず。shell の書き方が変わった?)", len(ms))
	}
	out := map[string]int{}
	for _, m := range ms {
		n, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatalf("pr_band=%q が数値でない", m[2])
		}
		out[m[1]] = n
	}
	return out
}

// shellRung は「この式以上なら この語」の 1 段。
type shellRung struct {
	expr string // 例: "pr_band * 2" / "pr_band" / "-pr_band" / "-pr_band * 5 / 2"
	word string // 例: "超過"
}

// shellLadder は pace_row の状態判定 (elif の連鎖) を上から順に取る。最後の else は語だけ。
func shellLadder(t *testing.T, src string) (rungs []shellRung, elseWord string, limitWord string) {
	t.Helper()
	limit := regexp.MustCompile(`\[ "\$pr_used" -ge 100 \];\s*then\s*\n\s*pr_color="\$\w+";\s*pr_word=" *(\S+)"`)
	if m := limit.FindStringSubmatch(src); m != nil {
		limitWord = m[1]
	} else {
		t.Fatal("used >= 100 の段が抽出できない (shell の書き方が変わった?)")
	}
	// `-ge $(( ... ))` と `-ge "$pr_band"` の両方の綴りを拾う
	re := regexp.MustCompile(`-ge (?:\$\(\( ([^)]+?) \)\)|"\$(pr_band)")\s*\];\s*then\s*\n\s*pr_color="\$\w+";\s*pr_word=" *(\S+)"`)
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		expr := m[1]
		if expr == "" {
			expr = m[2]
		}
		rungs = append(rungs, shellRung{expr: strings.TrimSpace(expr), word: m[3]})
	}
	if len(rungs) < 4 {
		t.Fatalf("状態の段が %d 件しか取れない (超過/先行/適正/余裕 の 4 件以上のはず)", len(rungs))
	}
	els := regexp.MustCompile(`else\s*\n\s*pr_color="\$\w+";\s*pr_word=" *(\S+)"`)
	if m := els.FindStringSubmatch(src); m != nil {
		elseWord = m[1]
	} else {
		t.Fatal("else の段が抽出できない (shell の書き方が変わった?)")
	}
	return rungs, elseWord, limitWord
}

// evalShellExpr は shell の $(( )) と同じ整数演算で式を評価する。対応するのは
// 「[-]pr_band ((*|/) 整数)*」の形だけで、それ以外は FAIL させる (黙って 0 を返さない)。
func evalShellExpr(t *testing.T, expr string, band int) int {
	t.Helper()
	toks := strings.Fields(expr)
	if len(toks) == 0 {
		t.Fatalf("空の式")
	}
	neg := false
	head := toks[0]
	if strings.HasPrefix(head, "-") {
		neg, head = true, strings.TrimPrefix(head, "-")
	}
	if head != "pr_band" {
		t.Fatalf("未対応の式 %q (pr_band 以外の項が入った)", expr)
	}
	v := band
	for i := 1; i < len(toks); i += 2 {
		if i+1 >= len(toks) {
			t.Fatalf("未対応の式 %q (演算子の後ろが無い)", expr)
		}
		n, err := strconv.Atoi(toks[i+1])
		if err != nil {
			t.Fatalf("未対応の式 %q (%q が整数でない)", expr, toks[i+1])
		}
		switch toks[i] {
		case "*":
			v *= n
		case "/":
			v /= n // shell と同じ 0 方向への切り捨て
		default:
			t.Fatalf("未対応の演算子 %q (式 %q)", toks[i], expr)
		}
	}
	if neg {
		v = -v
	}
	return v
}

// shellWord は抽出した ladder を shell と同じ順で評価して状態語を返す。
func shellWord(t *testing.T, rungs []shellRung, elseWord, limitWord string, band, used, delta int) string {
	t.Helper()
	if used >= 100 {
		return limitWord
	}
	for _, r := range rungs {
		if delta >= evalShellExpr(t, r.expr, band) {
			return r.word
		}
	}
	return elseWord
}

// glogx (paceState) と statusline (pace_row) の状態判定が、整数 delta の全域で一致すること。
//
// 🚨 この 2 つは同じ数字を 2 言語に書いた**二重実装**で、片方だけ直すと黙って乖離する。
// issue 144 の目視確認に「statusline のペース行と同じ見え方になっているか」が入っていたのは、
// その乖離を人の目で確かめようとしていたということ。ここで機械に見せる。
func TestPaceRulesMatchStatusline(t *testing.T) {
	src, err := os.ReadFile(statuslinePath(t))
	if err != nil {
		t.Fatalf("statusline を読めない: %v", err)
	}
	text := string(src)
	bands := shellBands(t, text)
	rungs, elseWord, limitWord := shellLadder(t, text)

	// 窓の種別と Go の span の対応: shell の hour = 5 時間窓 / day = 7 日窓。
	for _, tc := range []struct {
		kind string
		span time.Duration
	}{{"hour", 5 * time.Hour}, {"day", 7 * 24 * time.Hour}} {
		goBand := paceBand(tc.span)
		if int(goBand) != bands[tc.kind] {
			t.Errorf("%s の帯が乖離: glogx=%v / statusline=%d", tc.kind, goBand, bands[tc.kind])
		}
		// 🚨 経過率は**小数まで**回す。本番の cardPace は窓の残り時間から連続値を作るので、
		// 整数だけで比べると「Go が小数で判定し shell が切り捨てで判定する」差を構造的に
		// 見落とす (初版がその穴を持っていた: 経過 24.9% / 使用 49% で shell=先行・glogx=適正。
		// red team 指摘 2026-09-01)。shell は $(( ... )) の整数除算しか持たないので、
		// **切り捨てた整数**を渡した結果と一致しなければならない。
		checked := 0
		for used := range 101 {
			for tenth := range 1001 { // 0.0 .. 100.0 を 0.1 刻み
				elapsed := float64(tenth) / 10
				_, gotGo := paceState(used, paceElapsed(elapsed), goBand)
				shExp := int(math.Floor(elapsed)) // shell の整数除算と同じ切り捨て
				wantSh := shellWord(t, rungs, elseWord, limitWord, bands[tc.kind], used, used-shExp)
				if gotGo != wantSh {
					t.Fatalf("%s used=%d elapsed=%.1f (shell の想定=%d%%): glogx=%q / statusline=%q",
						tc.kind, used, elapsed, shExp, gotGo, wantSh)
				}
				checked++
			}
		}
		// 🚨 件数を出す。0 件でも緑になる形 (ループの上限を壊す変更) をここで弾く。
		if checked != 101*1001 {
			t.Fatalf("%s の検査が %d 件 (101x1001 のはず)", tc.kind, checked)
		}
		t.Logf("%s: %d 通りで一致 (帯 %d)", tc.kind, checked, bands[tc.kind])
	}
}

// shellSGR は statusline の `name="\033[...m"` 形式の色定義を、名前 → SGR (ESC は \x1b) で返す。
func shellSGR(src string) map[string]string {
	re := regexp.MustCompile(`(?m)^([a-z_]+)="\\033(\[[0-9;]*m)"`)
	out := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		out[m[1]] = "\x1b" + m[2]
	}
	return out
}

// ペースゲージと状態語の色が statusline と一致する (同じゲージを 2 画面が別の色で描かない)。
//
// 🚨 色の定義だけでなく、shell の pace_gauge がその変数を**使っていること**も見る。定義が一致して
// いても描画が別の変数を参照していれば、画面の色は食い違う (2026-09-24 までの「使い残し」が
// この形: glogx は BrightBlue、statusline は cyan_fg のままだった)。
func TestPaceColorsMatchStatusline(t *testing.T) {
	src, err := os.ReadFile(statuslinePath(t))
	if err != nil {
		t.Fatalf("statusline を読めない: %v", err)
	}
	text := string(src)
	sgrs := shellSGR(text)

	start := strings.Index(text, "\npace_gauge() {")
	if start < 0 {
		t.Fatal("statusline の pace_gauge() が見つからない (抽出できない = 検査できない)")
	}
	end := strings.Index(text[start:], "\n}\n")
	if end < 0 {
		t.Fatal("statusline の pace_gauge() の終わりが見つからない")
	}
	gauge := text[start : start+end]

	for _, c := range []struct{ shell, goVal, what string }{
		{"bg_in", paceOnTrack, "想定内の消化"},
		{"bg_over", paceOverdraw, "前借り"},
		{"under_sgr", paceNow, "いま居るスロット"},
		{"unspent_fg", paceUnspent, "使い残し"},
		{"dim_fg", paceFuture, "まだ来ていない未来"},
	} {
		v, ok := sgrs[c.shell]
		if !ok {
			t.Errorf("%s: statusline に %s の定義が無い (抽出できない = 検査できない)", c.what, c.shell)
			continue
		}
		if v != c.goVal {
			t.Errorf("%s の色が乖離: glogx=%q / statusline %s=%q", c.what, c.goVal, c.shell, v)
		}
		if !strings.Contains(gauge, "${"+c.shell+"}") {
			t.Errorf("%s: statusline の pace_gauge が %s を使っていない (定義だけ一致して描画は別の色)", c.what, c.shell)
		}
	}

	// 状態語の色: shell は pr_color="$var"; pr_word=" 語" の行で、glogx は paceState の戻り値で決まる
	shellColor := map[string]string{}
	for _, m := range regexp.MustCompile(`pr_color="\$(\w+)";\s*pr_word=" *(\S+)"`).FindAllStringSubmatch(text, -1) {
		v, ok := sgrs[m[1]]
		if !ok {
			t.Fatalf("状態語 %s の色 %s の定義が statusline に無い", m[2], m[1])
		}
		shellColor[m[2]] = v
	}
	goColor := map[string]string{}
	band := paceBand(7 * 24 * time.Hour)
	for used := range 101 {
		for elapsed := range 101 {
			col, word := paceState(used, float64(elapsed), band)
			if prev, ok := goColor[word]; ok && prev != col {
				t.Fatalf("glogx の %s が 2 色ある: %q / %q", word, prev, col)
			}
			goColor[word] = col
		}
	}
	// 🚨 語の数を数える。どちらかの抽出が空振りすると、比べる語が 0 個のまま緑になる
	if len(goColor) != 6 || len(shellColor) != 6 {
		t.Fatalf("状態語の数が 6 でない: glogx=%d %v / statusline=%d %v", len(goColor), goColor, len(shellColor), shellColor)
	}
	for word, col := range goColor {
		if shellColor[word] != col {
			t.Errorf("状態語 %s の色が乖離: glogx=%q / statusline=%q", word, col, shellColor[word])
		}
	}
}
