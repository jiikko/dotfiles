package usage

// 全画面 ratelimit ダッシュボードの描画。枠 (5h / weekly) 1 つを 1 枚のアナログ盤にして
// 格子状に並べる。盤の読み方は 1 つだけ覚えればよい形にしてある:
//
//	1 周 = その枠の長さ / 12 時 = リセット点 / 針 = いまの経過位置
//	外周の明るい弧 = 復活までの残り時間 (縮んでいく) / 内周の弧 = 消費した割合
//
// 「弧が針より先 = 前借り」が図形だけで読めるのは、経過も消費もどちらも「窓に対する割合」
// = 同じ目盛りに乗るため。5h と weekly で窓幅が違っても読み方は変わらない。
//
// RenderLine / RenderTable と同じく bubbletea 非依存の純関数で、now は引数で受ける。

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"glogx/sgr"
	"glogx/termsafe"
	"glogx/termwidth"
)

const (
	// dialMinW / dialMinH は 1 枚のカードに盤を描く最小の桁数・行数。これを下回る割り当てでは
	// 盤が潰れて読めないので、同じ情報をテキストカード (バー + 数値) で出す。
	dialMinW = 26
	dialMinH = 9
	// dialGap はカード間の空き桁。「盤が隣のカードと地続きに見える」のを防ぐ余白
	// (ユーザー要望 2026-08-31)。段と段のあいだは空行ではなく罫線で仕切る (下記)。
	dialGap = 4
	// dialRuleMinTail は肩書き付き罫線 (案 A) を採るのに必要な、肩書きの右に残る線の長さ。
	// これを下回るなら「線に見えない」ので素の罫線 + 1 行見出しへ落とす。
	dialRuleMinTail = 4
)

// RenderDashboard は Snapshot の全枠を格子状のアナログ盤にして描く。返す行数はちょうど
// height。s が nil / 枠が 1 つも無いときは nil を返す (呼び出し側が取得中・失敗を出す)。
func RenderDashboard(s *Snapshot, now time.Time, width, height int, colored bool) []string {
	groups := dialGroups(s)
	if len(groups) == 0 || width <= 0 || height <= 0 {
		return nil
	}
	out := make([]string, 0, height)
	groupH := height / len(groups)
	bands := dialBands(groups, now, width, groupH, colored)
	for i, g := range groups {
		// 段の区切りは空行ではなく罫線 (ユーザー要望 2026-09-01)。各段が自分の 1 行目に持つので
		// ここでは挟まない: 罫線の形 (素 / 肩書き付き) は段が採った見出しの形で決まるため。
		out = append(out, renderGroup(g, now, width, groupH, bands[i], colored)...)
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out[:height]
}

// dialGroup は 1 つの CLI の枠をまとめた段 (大見出し + その CLI の盤)。
type dialGroup struct {
	cli     string // "Claude Code" / "codex"
	version string // 取れていれば "2.1.216" (取得失敗時は空)
	cards   []dialCard
}

// dialGroups は枠を CLI ごとの段へまとめる。段の順序と段内の順序はどちらも renderWindows
// (Claude → codex) に従う: 同じ Snapshot を見る 3 つの描画で枠の並びが食い違わないようにする。
func dialGroups(s *Snapshot) []dialGroup {
	cards := dialCards(s)
	groups := make([]dialGroup, 0, len(cards)) // 上限は枠の数 (CLI が 1 枠ずつのとき)
	for _, c := range cards {
		if n := len(groups); n > 0 && groups[n-1].cli == c.cli {
			groups[n-1].cards = append(groups[n-1].cards, c)
			continue
		}
		ver := s.Version
		if c.cli == "codex" {
			ver = s.CodexVersion
		}
		groups = append(groups, dialGroup{cli: c.cli, version: ver, cards: []dialCard{c}})
	}
	return groups
}

// dialCols は盤を何カラムで並べるか (1 枚に最低 dialMinW 桁が要る)。
func dialCols(width int) int {
	if width < dialMinW*2+dialGap {
		return 1
	}
	return 2
}

// dialBands は各段の合体帯 (案 B) を返す。**全段そろって組めるときだけ**採り、1 段でも
// 組めなければ全段 nil を返す (画面の中で見出しの形を 1 つに保つ)。バージョンを添えるかも
// 同じ理由で全段そろえる: 片方の段にだけ版が出ると、揃っていないことの方が目に付く。
func dialBands(groups []dialGroup, now time.Time, width, groupH int, colored bool) [][]string {
	cols := dialCols(width)
	cellW := (width - (cols-1)*dialGap) / cols
	for _, withTag := range []bool{true, false} {
		bands := make([][]string, len(groups))
		ok := cols == 2
		for i, g := range groups {
			rowsN := (len(g.cards) + cols - 1) / cols
			b := mergedHead(g, now, width, cellW, withTag, colored)
			// 帯 3 行 + 罫線 1 行を使っても、その段の全カードに盤が残ること
			if b == nil || !fitsFace(groupH-1-len(b), rowsN, 0) {
				ok = false
				break
			}
			bands[i] = b
		}
		if ok {
			return bands
		}
	}
	return make([][]string, len(groups))
}

// renderGroup は 1 つの CLI の段 (見出し + 盤の格子) をちょうど h 行で返す。band は合体帯
// (案 B) の 3 行。空なら肩書き罫線 (案 A) か 1 行見出しへ落ちる。形の選択は RenderDashboard が
// **画面全体で 1 つに揃えて**決める (段ごとに違う形にすると「codex の段だけ見出しが大きい」
// のような非対称になり、同じ画面で読み方が 2 通りになる。ユーザー指摘 2026-09-01)。
func renderGroup(g dialGroup, now time.Time, width, h int, band []string, colored bool) []string {
	cols := dialCols(width)
	rowsN := (len(g.cards) + cols - 1) / cols
	cellW := (width - (cols-1)*dialGap) / cols
	// 見出しは 3 段構えで、盤が残る最初の形を採る (ユーザー選定 2026-09-01)。
	//
	//  1. 合体帯 (案 B): 素の罫線 + 「5H | CLI 名 | 7D」を 1 つの 3 行帯に組む。CLI 名と枠
	//     ラベルが同じ高さに並び、別の帯に積むより 1〜4 行短い
	//  2. 肩書き罫線 (案 A): 罫線の中に CLI 名とバージョンを入れ、カード見出しは各カードの上へ
	//     残す。合体帯が幅で入らないときのフォールバック
	//  3. 素の罫線 + 1 行テキスト: 肩書きすら入らない幅
	//
	// ⚠️ 高さの判定は「その形にしても盤が残るか」で行う。段の高さ h だけで決めると、見出しが
	// 食った行のせいで盤が消え、理由も無い空行だけが残る段ができる (実測 2026-09-01: CLI 名の
	// 桁数で可否が決まるため、同じ画面で Claude 段は盤あり・codex 段は盤なしになっていた)。
	var head []string
	headless := false // true = カードは自分の見出しを描かない (合体帯が既に出している)
	if len(band) > 0 {
		head, headless = append([]string{plainRule(width, colored)}, band...), true
	} else if rule, ok := titledRule(width, g.cli, g.version, colored); ok {
		head = []string{rule}
	} else {
		head = []string{plainRule(width, colored), groupTextHead(g, width, colored)}
	}
	cardsH := max(h-len(head), 1)
	rows := rowsN
	// ⚠️ カードの割り当ては段をまたいで同じにする (等分)。「逼迫している枠を主役にして
	// 幅を 62/38 に振る」を一度実装したが、**段ごとに列幅が変わると上下の段で盤の位置が
	// 揃わず、崩れて見える** (ユーザー確認 2026-09-01 で revert)。格子の縦の整列は、段の中の
	// 強弱より優先する。強弱を付けたいなら、幅ではなく色・枠線など**位置を動かさない手段**で。
	// 参考: 盤の直径は高さで決まるので、幅を配り直しても主役の盤は大きくならない (実測)。
	cellH := cardsH / rows
	out := head
	for r := range rows {
		grid := make([][]string, 0, cols)
		for cc := range cols {
			i := r*cols + cc
			if i >= len(g.cards) {
				grid = append(grid, make([]string, cellH))
				continue
			}
			grid = append(grid, renderCard(g.cards[i], now, cellW, cellH, headless, colored))
		}
		for line := range cellH {
			parts := make([]string, 0, cols)
			for _, cell := range grid {
				parts = append(parts, padRight(termwidth.Truncate(cell[line], cellW, ""), cellW))
			}
			out = append(out, strings.TrimRight(strings.Join(parts, termwidth.PadSpaces(dialGap)), " "))
		}
	}
	for len(out) < h {
		out = append(out, "")
	}
	return out[:h]
}

// fitsFace は段に高さ cardsH を割り当てたとき、各カードに盤が描けるだけの行が残るか。
// renderCard の分岐 (bodyH = cellH - 見出し - foot) と同じ式を使う。headLines はカードが
// 自分の見出しに使う行数 (合体帯を採ったときは 0 = カードは見出しを描かない)。
func fitsFace(cardsH, rows, headLines int) bool {
	if cardsH <= 0 || rows <= 0 {
		return false
	}
	cellH := cardsH / rows
	return cellH-headLines-cardFootLines(cellH) >= 5
}

// plainRule は段の区切りの罫線 (全幅)。段の頭に置くので 1 段目の上にも出る。
func plainRule(width int, colored bool) string {
	if width <= 0 {
		return ""
	}
	return paintIf(strings.Repeat("─", width), sgr.Dim, colored)
}

// titledRule は肩書き付きの罫線 (案 A)。"── codex  v0.144.6 ────…" の形で CLI 名と
// バージョンを罫線の中に入れる。ok=false = 肩書きを入れると線が dialRuleMinTail 未満に
// なる幅 (呼び出し側は素の罫線 + 1 行見出しへ落とす)。
//
// ⚠️ 罫線の長さは肩書きを**予算に入れてから**決める。後付けすると width を超え、狭い端末では
// フレームが自動 OFF でクリップも効かないため折り返して画面全体が崩れる (旧 groupHead が
// バージョンタグで踏んだ罠と同じ形)。
func titledRule(width int, cli, ver string, colored bool) (string, bool) {
	lead, gap := "── ", " "
	fixed := termwidth.Of(lead) + termwidth.Of(cli) + termwidth.Of(gap)
	tag := versionTag(ver, colored)
	used := fixed + termwidth.Of(tag)
	if used+dialRuleMinTail > width {
		tag, used = "", fixed // バージョンは飾りなので先に落とす
	}
	if used+dialRuleMinTail > width {
		return "", false
	}
	return paintIf(lead, sgr.Dim, colored) + paintIf(cli, sgr.Bold, colored) + tag + gap +
		paintIf(strings.Repeat("─", max(width-used, 0)), sgr.Dim, colored), true
}

// groupTextHead は CLI 名の 1 行見出し (肩書き罫線すら入らない幅のときだけ使う)。
func groupTextHead(g dialGroup, width int, colored bool) string {
	return fitLine(width, []string{
		paintIf(g.cli, sgr.Bold, colored) + versionTag(g.version, colored),
		paintIf(g.cli, sgr.Bold, colored),
	})
}

// mergedHead は「左カードの見出し | CLI 名 | 右カードの見出し」を 1 つの 3 行帯にする
// (案 B。ユーザー選定 2026-09-01)。段の大見出しとカード見出しを別の帯に積む形より 1〜4 行
// 短く、CLI 名と枠ラベルが同じ高さに並ぶ。
//
// 左右の見出しは**自分の盤の真上** (セルの中央) に置く。全幅の端へ寄せると、どの盤の見出しか
// の対応が弱くなる。CLI 名は全幅の中央なので、盤 2 枚のあいだの空きへ入る。
//
// nil を返す条件 (呼び出し側は肩書き罫線 = 案 A へ落ちる):
//   - カードが 2 枚でない。codex のプランは枠数が可変で、3 枚以上は「左右」に割れない
//   - ラベル / CLI 名に字形表の外の字が混ざる (bigLines / bannerLines が nil)
//   - 幅が足りず、左右の見出しと中央の CLI 名を最低 2 桁の隙間つきで並べられない
func mergedHead(g dialGroup, now time.Time, width, cellW int, withTag, colored bool) []string {
	if len(g.cards) != 2 || cellW <= 0 {
		return nil
	}
	l, r := g.cards[0], g.cards[1]
	lAA, rAA, cAA := bigLines(l.label), bigLines(r.label), bannerLines(g.cli)
	if lAA == nil || rAA == nil || cAA == nil {
		return nil
	}
	lw, rw, cw := bigWidth(l.label), bigWidth(r.label), bannerWidth(g.cli)
	_, _, lCol, _ := cardPace(l, now)
	_, _, rCol, _ := cardPace(r, now)
	lKind, rKind := kindTag(l.kind, colored), kindTag(r.kind, colored)
	tag := ""
	if withTag {
		tag = versionTag(g.version, colored)
	}
	lAt := max(cellW/2-lw/2, 0)
	rAt := max(cellW+dialGap+cellW/2-rw/2, 0)
	cAt := max((width-cw-termwidth.Of(tag))/2, 0)
	// ⚠️ 種別 ("セッション" / "weekly") は落とさない: 飾りではなく枠の意味そのもので、これを
	// 捨ててまで合体帯に留まるより、種別が残る肩書き罫線 (案 A) の方が読める。落とせるのは
	// バージョンだけで、その判断は画面全体で揃える (呼び出し側の withTag)。
	// 要素どうしの隙間はカード間の空き (dialGap) と同じだけ要求する。2 桁だと隣の語と
	// 地続きに見え、「セッション」が CLI 名の一部のように読める (実測 2026-09-01: 幅 160 で
	// 種別語と CLI 名の AA が 2 桁差で並んだ)。
	if cAt < lAt+lw+termwidth.Of(lKind)+dialGap || rAt < cAt+cw+termwidth.Of(tag)+dialGap ||
		rAt+rw+termwidth.Of(rKind) > width {
		return nil
	}
	out := make([]string, 0, bannerRows)
	for i := range bannerRows {
		line := placeAt("", lAt, paintIf(lAA[i], lCol, colored))
		if i == 1 {
			line += lKind
		}
		line = placeAt(line, cAt, paintIf(cAA[i], sgr.Bold, colored))
		if i == 1 {
			line += tag
		}
		line = placeAt(line, rAt, paintIf(rAA[i], rCol, colored))
		if i == 1 {
			line += rKind
		}
		out = append(out, termwidth.Truncate(line, width, "")) // 最後の砦
	}
	return out
}

// placeAt は line の表示桁 col から s を置く (col まで空白で詰める)。
func placeAt(line string, col int, s string) string {
	return line + termwidth.PadSpaces(max(col-termwidth.Of(line), 0)) + s
}

// kindTag は見出しに添える種別 ("  セッション" / "  weekly")。AA にできない字なので普通の字。
func kindTag(kind string, colored bool) string {
	if kind == "" {
		return ""
	}
	return paintIf("  "+kind, sgr.Dim, colored)
}

// versionTag は " v2.1.216" (取れていなければ空)。
//
// ⚠️ v は `claude --version` / `codex --version` の出力から切り出した**外部由来の文字列**。
// 空白は落ちているが空白を含まない CSI/OSC は残るので、載せる前に無害化する。同じ値を
// 描く他の 2 箇所 (usage_overlay.go / ratelimit_dashboard.go) は無害化しており、ここだけが
// 生だった (セルフレビュー指摘 2026-09-01)。termwidth.Of は ANSI を 0 幅で数えるため、
// 幅の検査でも黙って通ってしまう。
func versionTag(v string, colored bool) string {
	v = termsafe.PlainLine(v)
	if v == "" {
		return ""
	}
	return paintIf("  v"+v, sgr.Dim, colored)
}

// dialCard は 1 枠ぶんのカードの入力。
type dialCard struct {
	cli   string        // "Claude Code" / "codex"
	kind  string        // "セッション" / "weekly" (窓幅から決める。不明なら空)
	win   Window        // 枠そのもの (使用率とリセット時刻)
	span  time.Duration // 枠の長さ (0 = 不明。盤を描かずテキストカードへ落とす)
	label string        // 見出しに出す枠ラベル
}

// dialLabel は見出し用の枠ラベル。codex の "cx" 接頭辞は落とす — 1 行表示では Claude 枠と
// 混ざるため出所を示す必要があるが、カードは見出しに CLI 名を持つので接頭辞が重複する。
func dialLabel(w Window) string {
	if w.Source == SourceCodex {
		return strings.TrimPrefix(w.Label, "cx")
	}
	return w.Label
}

// dialCards は描く枠を順に返す。並びは RenderLine / RenderTable と同じ renderWindows
// (Claude → codex) に従う: 同じ Snapshot を見る 3 つの描画で枠の並びが食い違わないようにする。
func dialCards(s *Snapshot) []dialCard {
	if s == nil {
		return nil
	}
	ws := renderWindows(s)
	cards := make([]dialCard, 0, len(ws))
	for _, w := range ws {
		cli := "Claude Code"
		if w.Source == SourceCodex {
			cli = "codex"
		}
		span := w.Span()
		kind := ""
		switch {
		case span <= 0: // 窓幅不明: 種別を名乗らない
		case span >= 24*time.Hour:
			kind = "weekly"
		default:
			kind = "セッション"
		}
		cards = append(cards, dialCard{cli: cli, kind: kind, win: w, span: span, label: dialLabel(w)})
	}
	return cards
}

// paceBand は「想定どおり」と見なす乖離の幅 (pt)。短い窓ほど作業がバースト的で、weekly と
// 同じ幅にすると常時 "先行" になって信号にならない。
//
// ⚠️ この 2 値は _claude/statusline-command.sh の pace_row と同じ意味・同じ値を持つ
// (statusline は Claude の 5h / 7d を同じ考え方で 1 行に出す)。片方だけ変えると同じ枠が
// 2 か所で違う状態語を出すので、変えるなら両方を揃えること。
func paceBand(span time.Duration) float64 {
	if span <= 6*time.Hour {
		return 25
	}
	return 10
}

// paceState は消費 (used%) と経過 (elapsed%) の乖離から状態色と状態語を決める。語は全て
// 2 文字に揃える (カード間で後続の数値が横にずれないため)。
func paceState(used int, elapsed, band float64) (color, word string) {
	d := float64(used) - elapsed
	switch {
	case used >= 100:
		return sgr.Red, "上限"
	case d >= band*2:
		return sgr.Red, "超過"
	case d >= band:
		return sgr.Yellow, "先行"
	case d >= -band:
		return sgr.Green, "適正"
	case d >= -band*2.5:
		return sgr.BrightBlue, "余裕"
	default:
		return sgr.Magenta, "余剰"
	}
}

// cardFootLines はカード下部に置く行数を高さから決める。狭いカードでは重要度の低い順
// (予算と助言 → ゲージ) に落とす。数値行と「復活まで」は必ず残す。
func cardFootLines(h int) int {
	switch {
	case h >= 18:
		// 空行 / ゲージ / 数値 / 復活まで / 予算と助言。⚠️ 閾値を下げないこと: 盤とゲージの
		// あいだの空行より、盤が 1 行大きいこと (中央に AA の使用率と日本語の残り時間が
		// 両方入る) を優先する。h=17 で 5 行にすると中央が ASCII 表記へ落ちる。
		return 5
	case h >= 15:
		return 4 // ゲージ / 数値 / 復活まで / 予算と助言
	case h >= 10:
		return 3 // ゲージ / 数値 / 復活まで
	default:
		return 2 // 数値 / 復活まで
	}
}

// cardPace はカードの「残り時間 / 経過率 / 状態色 / 状態語」を返す。renderCard と mergedHead が
// 同じ値を使う (色は状態を表すので、見出し帯とカード本体で食い違わせない)。
func cardPace(c dialCard, now time.Time) (remain time.Duration, elapsed float64, col, word string) {
	remain = max(c.win.ResetAt.Sub(now), 0)
	if c.span > 0 {
		elapsed = math.Max(0, math.Min(100, float64(c.span-remain)/float64(c.span)*100))
	}
	col, word = paceState(c.win.Percent, elapsed, paceBand(c.span))
	return remain, elapsed, col, word
}

// renderCard は 1 枚ぶんをちょうど h 行で返す。headless = 見出しを描かない (段の見出し帯が
// 既にこのカードのラベルを出しているとき。renderGroup が決める)。
func renderCard(c dialCard, now time.Time, w, h int, headless, colored bool) []string {
	remain, elapsed, col, word := cardPace(c, now)

	// 見出し・数値行は狭い割り当てでは端から情報を落とす (切り詰めの … で読めなくするより、
	// 落とす順を決めておく方が読める)。落とす順は「重要度の低い順」= 種別 → CLI 名 /
	// リセット絶対時刻 → 見出しの語 / 想定と乖離 → 使用率。
	foot := cardFoot(c, remain, elapsed, col, word, w, cardFootLines(h), colored)
	var head []string
	if !headless {
		head = cardHead(c, col, w, h, len(foot), colored)
	}
	bodyH := max(0, h-len(head)-len(foot))
	var body []string
	if c.span <= 0 || w < dialMinW || bodyH < 5 {
		body = textCardBody(c, col, w, bodyH, colored)
	} else {
		body = renderFace(c, remain, elapsed, col, w, bodyH, colored)
	}
	out := append(append([]string{}, head...), body...)
	for len(out) < h-len(foot) {
		out = append(out, "")
	}
	out = append(out[:max(0, h-len(foot))], foot...)
	for len(out) < h {
		out = append(out, "")
	}
	return out[:h]
}

// cardHead はカード見出し。枠ラベル ("5H" / "7D") は 4 桁幅の AA で大きく出し、種別
// (セッション / weekly) は AA にできないので中段の右へ普通の字で添える。
//
// ⚠️ AA は 3 行あるので、盤が残らないなら 1 行の普通の見出しへ落とす。見出しを大きくして
// 盤が消えたら本末転倒 (段の大見出しと同じ判断。groupHead / fitsFace 参照)。
func cardHead(c dialCard, col string, w, h, footN int, colored bool) []string {
	aa := bigLines(c.label)
	plain := []string{fitLine(w, []string{
		paintIf(strings.TrimSpace(c.label+" "+c.kind), col, colored),
		paintIf(c.label, col, colored),
	})}
	if aa == nil || h-bannerRows-footN < 5 {
		return plain
	}
	// ⚠️ 中央の使用率の AA を犠牲にしてまで見出しを大きくしない。見出しの AA は 3 行あり、
	// 盤が 2 行縮む。優先順位は「中央の数字 > 見出し」— 中央は盤の主役で、見出しは
	// 同じことを小さい字でも言えるため (実測 2026-09-01: 120x44 で見出しを AA にすると
	// 中央が普通の字へ落ちた)。
	if c.span > 0 && centerAAFits(w, h-1-footN, c.win.Percent) && !centerAAFits(w, h-bannerRows-footN, c.win.Percent) {
		return plain
	}
	kind := kindTag(c.kind, colored)
	// ⚠️ 種別は幅の予算に入れてから中央寄せする (段の大見出しと同じ罠。後付けするとその行
	// だけ width をはみ出す)。入らなければ種別だけ落とす。
	bw := bigWidth(c.label)
	if bw+termwidth.Of(kind) > w {
		kind = ""
	}
	if bw > w {
		return plain
	}
	indent := termwidth.PadSpaces(max((w-bw-termwidth.Of(kind))/2, 0))
	out := make([]string, 0, bannerRows)
	for i, row := range aa {
		line := indent + paintIf(row, col, colored)
		if i == 1 {
			line += kind
		}
		out = append(out, termwidth.Truncate(line, w, "")) // 最後の砦
	}
	return out
}

// cardFoot はカード下部の行 (ゲージ / 数値 / 復活まで / 予算と助言) を n 行で返す。
func cardFoot(c dialCard, remain time.Duration, elapsed float64, col, word string, w, n int, colored bool) []string {
	cells := dialDivisions(c.span)
	// いま居るスロット (経過を 1 スロットで割った位置)。窓幅不明のときは印を出さない。
	at := -1
	if c.span > 0 {
		at = min(int(elapsed*float64(cells)/100), cells-1)
	}
	gauge := paceGauge(cells, float64(c.win.Percent), elapsed, at, colored)
	if gauge == "" || c.span <= 0 {
		gauge = paintIf(bar(c.win.Percent, false), col, colored) // 10 等分以上の窓は素のバー
	}
	pctCell := paintIf(fmt.Sprintf("%3d%%", c.win.Percent), col, colored)
	remainCell := paintIf(formatRemain(remain), sgr.Bold, colored)
	// ⚠️ 窓幅が分からない枠では想定・乖離・状態語を出さない。elapsed は 0 固定なので
	// 「想定 0% / +50.0pt 超過」のような、根拠のない断定になる (盤の側は「窓幅が不明」と
	// 断っているのに数値行だけが言い切る形になっていた)。
	numbers := fitLine(w, []string{pctCell})
	if c.span > 0 {
		numbers = fitLine(w, []string{
			pctCell + " " + paintIf(fmt.Sprintf("想定%3.0f%%", elapsed), sgr.Dim, colored) + " " +
				paintIf(fmt.Sprintf("%+5.1fpt %s", float64(c.win.Percent)-elapsed, word), col, colored),
			pctCell + " " + paintIf(word, col, colored),
			pctCell,
		})
	}
	reset := fitLine(w, []string{
		"復活まで " + remainCell + " " + paintIf("("+formatReset(c.win.ResetAt)+")", sgr.Dim, colored),
		"復活まで " + remainCell,
		remainCell,
	})
	// 予算と助言。どちらも空になることがある (適正で残りが 1 スロット未満のとき)。
	parts := make([]string, 0, 2)
	if b := paceBudget(c.win.Percent, remain, c.span, cells); b != "" {
		parts = append(parts, b)
	}
	if a := paceAdvice(c.win.Percent, elapsed, c.span, cells); a != "" {
		parts = append(parts, a)
	}
	pace := ""
	if len(parts) > 0 {
		pace = fitLine(w, []string{
			paintIf(strings.Join(parts, " · "), col, colored),
			paintIf(parts[len(parts)-1], col, colored),
		})
	}
	switch {
	case n >= 5:
		// 盤とゲージが地続きに見えないよう 1 行空ける (ユーザー要望 2026-08-31)
		return []string{"", fitLine(w, []string{gauge}), numbers, reset, pace}
	case n == 4:
		return []string{fitLine(w, []string{gauge}), numbers, reset, pace}
	case n == 3:
		return []string{fitLine(w, []string{gauge}), numbers, reset}
	default:
		return []string{numbers, reset}
	}
}

// renderFace は盤を faceH 行の点描で描く。
func renderFace(c dialCard, remain time.Duration, elapsed float64, col string, w, faceH int, colored bool) []string {
	cv := newBraille(w, faceH)
	cx, cy, rOut, rIn := faceGeom(w, faceH)
	used := math.Max(0, math.Min(100, float64(c.win.Percent))) / 100
	el := elapsed / 100

	// 目盛り = 窓の等分 (5h なら 1 時間ごと、weekly なら 1 日ごと)。
	div := dialDivisions(c.span)
	for k := range div {
		cv.tick(cx, cy, rOut+1, rOut+4, float64(k)/float64(div), sgr.Dim)
	}
	// 外周: 経過ぶんは破線の下地、残りが明るい弧 (これが縮んで 0 になる = 復活)。
	cv.arc(cx, cy, rOut, 0, el, sgr.Dim, 1, 3)
	cv.arc(cx, cy, rOut, el, 1, sgr.BrightWhite, 2, 1)
	// 内周: 枠の消費。
	cv.arc(cx, cy, rIn, 0, 1, sgr.Dim, 1, 3)
	cv.arc(cx, cy, rIn, 0, used, col, 2, 1)
	// 12 時 = リセット点 / 針 = いまの経過位置。どちらも「時間」なので外周の残り弧と同じ色に
	// する — 盤に乗る色を「時間 (白) と消費 (状態色)」の 2 系統だけに保つ。
	cv.tick(cx, cy, rOut+1, rOut+6, 0, sgr.Bold)
	// 針は中央の文字にぶつからない位置から始める (中心から引くと、角度によって残り時間の
	// 数字を横切る)。内周の 3/4 = 中央の文字がだいたい収まる半径。
	cv.ray(cx, cy, rIn*0.75, rOut+1, el, sgr.BrightWhite)

	// 中央は盤の中心 (cy) を挟む行に置く。faceH/2 で数えると 1 行上へずれ、円が細くなる
	// 位置に文字が来てリングへ接する。
	midRow := int(cy) / 4
	drawCenter(cv, c.win.Percent, remain, col, w, faceH, midRow, cy, rIn)
	return cv.lines(colored)
}

// drawCenter は盤の中央に「残り時間 + 使用率」を置く。使用率は入るなら 3 行の AA で大きく
// 出す (ユーザー要望 2026-09-01: 普通の文字より大きく)。内周に入らない盤では従来どおり
// 1 行ずつの文字に落ちる — 盤からはみ出した数字はリングと重なって読めなくなる。
func drawCenter(cv *braille, pct int, remain time.Duration, col string, w, faceH, midRow int, cy, rIn float64) {
	digits := strconv.Itoa(pct)
	// AA は 3 行あり、盤の中心が行の境目に来るとは限らないので上下で弦の長さが違う。
	// いちばん狭い行に合わせないと、下端 (または上端) だけがリングに接する。
	aaAvail := min(innerWidthAt(cy, rIn, midRow-1), innerWidthAt(cy, rIn, midRow), innerWidthAt(cy, rIn, midRow+1))
	// AA は中心行を挟む 3 行。その 1 行上に残り時間を置くので、上下に 2 行ずつの余裕が要る。
	//
	// ⚠️ 入らない盤では「狭い字形」ではなく**普通の字**へ落とす。見出しと同じ 3 桁幅の字形は
	// 0 と 8、5 と 6 の見分けが付かず (ユーザー指摘 2026-09-01)、1 行の "62%" の方が読める。
	// 大きくする目的を果たせないなら大きくしない。
	aa := bigLines(digits)
	if aa != nil && midRow-2 >= 0 && midRow+1 < faceH && bigWidth(digits)+1 <= aaAvail { // +1 は末尾の "%"
		putCentered(cv, midRow-2, w, remainText(remain, innerWidthAt(cy, rIn, midRow-2)), sgr.BrightWhite)
		// ⚠️ 3 行を行ごとに中央寄せしない。"%" を添えた中段だけ 1 桁広く、桁揃えが崩れて
		// 数字が斜めに見える。"%" 込みの塊を中央に置き、起点は 3 行で共有する。
		start := w/2 - (bigWidth(digits)+1)/2
		for i, row := range aa {
			line := row
			if i == 1 {
				line += "%" // 単位は中段に添える (数字と同じ高さの真ん中に来る)
			}
			cv.putText(midRow-1+i, start, line, col)
		}
		return
	}
	putCentered(cv, midRow, w, remainText(remain, innerWidthAt(cy, rIn, midRow)), sgr.BrightWhite)
	putCentered(cv, midRow+1, w, fitText(innerWidthAt(cy, rIn, midRow+1), []string{digits + "%"}), col)
}

// remainText は幅 w に収まる残り時間の表記を選ぶ。狭い盤ほど語を落とす。
func remainText(d time.Duration, w int) string {
	return fitText(w, []string{"残" + formatRemain(d), formatRemain(d), compactRemain(d)})
}

// putCentered は行 row の中央 (盤の中心 w/2) に s を置く。空文字は何も置かない
// (fitText が「入らない」を空で返すので、その結果をそのまま渡せる)。
func putCentered(cv *braille, row, w int, s, col string) {
	if s == "" {
		return
	}
	cv.putText(row, w/2-termwidth.Of(s)/2, s, col)
}

// innerWidthAt は内周リングの内側で、行 row に置ける桁数。
//
// ⚠️ 盤の差し渡し (rIn セル) をどの行にも使うと、中心から離れた行で文字がリングに接する
// (円は上下ほど細い)。その行の弦の長さで測り、左右に 2 セルずつ余白を残す。
// 弦の半分 = sqrt(rIn^2 - dy^2) ドット = そのままセル数 (ドットは横 2 つで 1 セル)。
// ⚠️ 余白 1 セルでは足りない: リングは太さ 2 ドットあり、弦の計算は外側の 1 本しか見ていない。
// さらに 1 セル余分に引く: 中央寄せは w/2 と幅/2 の 2 回切り捨てるので、桁数の偶奇によって
// 文字が最大 1 セル左へずれる (実測 2026-09-01: 余白 2 セルでも 72 通りのサイズで接触した)。
func innerWidthAt(cy, rIn float64, row int) int {
	dy := math.Abs(float64(row*4+2) - cy) // 行の中心のドット座標と盤の中心の差
	if dy >= rIn {
		return 0
	}
	return max(int(math.Sqrt(rIn*rIn-dy*dy))-5, 0)
}

// faceGeom は盤の寸法 (中心・外周・内周)。renderFace と centerAAFits が同じ式を見るための
// 単一の出典 — 片方だけ変えると「入る判定なのに描くと接する」がすぐ起きる。
//
// braille のドットは正方形になる (セルの縦横比 1:2 を横 2 x 縦 4 で割るため)。よって直径は
// 「桁 x2」と「行 x4」の狭い方で決まり、円は縦横どちらにも歪まない。
func faceGeom(w, faceH int) (cx, cy, rOut, rIn float64) {
	d := float64(min(w*2, faceH*4))
	cx, cy = float64(w), float64(faceH*2)
	rOut = d/2 - 3
	rIn = max(rOut-4, rOut/2)
	return cx, cy, rOut, rIn
}

// centerAAFits は faceH 行の盤に、中央の AA (使用率) が入るか。drawCenter の分岐と同じ条件。
func centerAAFits(w, faceH, pct int) bool {
	if faceH <= 0 {
		return false
	}
	_, cy, _, rIn := faceGeom(w, faceH)
	midRow := int(cy) / 4
	if midRow-2 < 0 || midRow+1 >= faceH {
		return false
	}
	avail := min(innerWidthAt(cy, rIn, midRow-1), innerWidthAt(cy, rIn, midRow), innerWidthAt(cy, rIn, midRow+1))
	return bigWidth(strconv.Itoa(pct))+1 <= avail
}

// dialDivisions は盤の目盛り数 (窓を何等分して刻むか)。5h → 5 (1 時間)、7d → 7 (1 日)。
// 割り切れない・多すぎる窓幅は 6 等分の目安にする (正確な値は中央と下段の数値が持つ)。
func dialDivisions(span time.Duration) int {
	switch {
	case span <= 0:
		return 6
	case span%(24*time.Hour) == 0 && span/(24*time.Hour) <= 12:
		return int(span / (24 * time.Hour))
	case span%time.Hour == 0 && span/time.Hour <= 12:
		return int(span / time.Hour)
	default:
		return 6
	}
}

// textCardBody は盤を描けないとき (桁不足 / 窓幅不明) の代替本文。数値は foot が持つので、
// ここはバーと「なぜ盤が無いか」だけを出す (情報は落とさない)。
func textCardBody(c dialCard, _ string, w, h int, colored bool) []string {
	out := make([]string, 0, h)
	if h <= 0 {
		return out
	}
	if c.span <= 0 {
		out = append(out, centerCell(paintIf("窓幅が不明のため盤は省略", sgr.Dim, colored), w))
	}
	for len(out) < h {
		out = append(out, "")
	}
	return out[:h]
}

// fitLine は幅 w に収まる最初の候補を中央寄せで返す。どれも収まらなければ最後の候補を
// 切り詰める (契約「どの行も width を超えない」を候補の選択漏れで破らないための最後の砦)。
func fitLine(w int, candidates []string) string {
	for _, s := range candidates {
		if termwidth.Of(s) <= w {
			return centerCell(s, w)
		}
	}
	last := candidates[len(candidates)-1]
	return termwidth.Truncate(last, w, "…")
}

// fitText は幅 w に収まる最初の候補を返す。
//
// ⚠️ どれも収まらなければ**空**を返す (最後の候補を返さない)。盤の上に置く文字は、
// はみ出すとリングと重なって読めなくなるうえ「文字が盤に接しない」不変条件も壊す。
// 数字は盤の下の数値行にも出るので、極小の盤では中央を空にする方が正しい
// (実測 2026-09-01: 最後の候補を返す実装だと 40〜200 桁 x 12〜60 行のうち 1266 通りで接触)。
func fitText(w int, candidates []string) string {
	for _, s := range candidates {
		if termwidth.Of(s) <= w {
			return s
		}
	}
	return ""
}

// centerCell は幅 w の中で s を中央寄せする (左余白だけ付ける。右端は呼び出し側が埋める)。
func centerCell(s string, w int) string {
	pad := w - termwidth.Of(s)
	if pad <= 0 {
		return s
	}
	return termwidth.PadSpaces(pad/2) + s
}

func paintIf(s, col string, colored bool) string {
	if !colored || col == "" {
		return s
	}
	return col + s + sgr.Reset
}

// compactRemain は盤の中央に置く残り時間の ASCII 表記 ("1:48" / "2d09h")。全角を使わない。
func compactRemain(d time.Duration) string {
	days, hours, minutes := breakdown(d)
	if days > 0 {
		return fmt.Sprintf("%dd%02dh", days, hours)
	}
	return fmt.Sprintf("%d:%02d", hours, minutes)
}
