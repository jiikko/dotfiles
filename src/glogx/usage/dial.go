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
	"strings"
	"time"

	"glogx/sgr"
	"glogx/termwidth"
)

const (
	// dialMinW / dialMinH は 1 枚のカードに盤を描く最小の桁数・行数。これを下回る割り当てでは
	// 盤が潰れて読めないので、同じ情報をテキストカード (バー + 数値) で出す。
	dialMinW = 26
	dialMinH = 9
	// dialGap はカード間の空き桁。dialRowGap は段と段の間に挟む空行。どちらも「盤が隣の
	// カードと地続きに見える」のを防ぐための余白 (ユーザー要望 2026-08-31)。
	dialGap    = 4
	dialRowGap = 1
	// dialBannerMinH は CLI 名を AA の大見出しで出す最小の段の高さ。これを下回ると盤が
	// 潰れるので、1 行のテキスト見出しへ落とす。
	dialBannerMinH = 15
)

// RenderDashboard は Snapshot の全枠を格子状のアナログ盤にして描く。返す行数はちょうど
// height。s が nil / 枠が 1 つも無いときは nil を返す (呼び出し側が取得中・失敗を出す)。
func RenderDashboard(s *Snapshot, now time.Time, width, height int, colored bool) []string {
	groups := dialGroups(s)
	if len(groups) == 0 || width <= 0 || height <= 0 {
		return nil
	}
	out := make([]string, 0, height)
	groupH := (height - (len(groups)-1)*dialRowGap) / len(groups)
	for gi, g := range groups {
		if gi > 0 {
			for range dialRowGap {
				out = append(out, "")
			}
		}
		out = append(out, renderGroup(g, now, width, groupH, colored)...)
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
	var groups []dialGroup
	for _, c := range dialCards(s) {
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

// renderGroup は 1 つの CLI の段 (大見出し + 盤の格子) をちょうど h 行で返す。
func renderGroup(g dialGroup, now time.Time, width, h int, colored bool) []string {
	head := groupHead(g, width, h, colored)
	cardsH := max(h-len(head), 1)
	cols := 2
	if width < dialMinW*2+dialGap {
		cols = 1
	}
	rows := (len(g.cards) + cols - 1) / cols
	cellW := (width - (cols-1)*dialGap) / cols
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
			grid = append(grid, renderCard(g.cards[i], now, cellW, cellH, colored))
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

// groupHead は段の大見出し。幅と高さが足りれば CLI 名をブロック文字の AA で出し、足りなければ
// 1 行のテキストへ落とす。バージョンは AA の中段の右に dim で添える (ロゴのバージョンタグの形)。
func groupHead(g dialGroup, width, h int, colored bool) []string {
	aa := bannerLines(g.cli)
	bw := bannerWidth(g.cli)
	if aa == nil || h < dialBannerMinH || bw > width {
		return []string{centerCell(paintIf(g.cli, sgr.Bold, colored)+versionTag(g.version, colored), width)}
	}
	indent := termwidth.PadSpaces(max((width-bw)/2, 0))
	out := make([]string, 0, bannerRows+1)
	for i, row := range aa {
		line := indent + paintIf(row, sgr.Bold, colored)
		if i == 1 {
			line += versionTag(g.version, colored)
		}
		out = append(out, line)
	}
	return append(out, "") // 見出しと盤のあいだを 1 行空ける
}

// versionTag は " v2.1.216" (取れていなければ空)。
func versionTag(v string, colored bool) string {
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
	case h >= 16:
		return 5 // 空行 / ゲージ / 数値 / 復活まで / 予算と助言
	case h >= 15:
		return 4 // ゲージ / 数値 / 復活まで / 予算と助言
	case h >= 10:
		return 3 // ゲージ / 数値 / 復活まで
	default:
		return 2 // 数値 / 復活まで
	}
}

// renderCard は 1 枚ぶんをちょうど h 行で返す。
func renderCard(c dialCard, now time.Time, w, h int, colored bool) []string {
	remain := max(c.win.ResetAt.Sub(now), 0)
	elapsed := 0.0
	if c.span > 0 {
		elapsed = math.Max(0, math.Min(100, float64(c.span-remain)/float64(c.span)*100))
	}
	col, word := paceState(c.win.Percent, elapsed, paceBand(c.span))

	// 見出し・数値行は狭い割り当てでは端から情報を落とす (切り詰めの … で読めなくするより、
	// 落とす順を決めておく方が読める)。落とす順は「重要度の低い順」= 種別 → CLI 名 /
	// リセット絶対時刻 → 見出しの語 / 想定と乖離 → 使用率。
	head := fitLine(w, []string{
		paintIf(strings.TrimSpace(c.label+" "+c.kind), col, colored),
		paintIf(c.label, col, colored),
	})
	foot := cardFoot(c, remain, elapsed, col, word, w, cardFootLines(h), colored)
	bodyH := max(0, h-1-len(foot))
	var body []string
	if c.span <= 0 || w < dialMinW || bodyH < 5 {
		body = textCardBody(c, col, w, bodyH, colored)
	} else {
		body = renderFace(c, remain, elapsed, col, w, bodyH, colored)
	}
	out := append([]string{head}, body...)
	for len(out) < h-len(foot) {
		out = append(out, "")
	}
	out = append(out[:max(0, h-len(foot))], foot...)
	for len(out) < h {
		out = append(out, "")
	}
	return out[:h]
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
	numbers := fitLine(w, []string{
		pctCell + " " + paintIf(fmt.Sprintf("想定%3.0f%%", elapsed), sgr.Dim, colored) + " " +
			paintIf(fmt.Sprintf("%+5.1fpt %s", float64(c.win.Percent)-elapsed, word), col, colored),
		pctCell + " " + paintIf(word, col, colored),
		pctCell,
	})
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
	// braille のドットは正方形になる (セルの縦横比 1:2 を横 2 x 縦 4 で割るため)。よって
	// 直径は「桁 x2」と「行 x4」の狭い方で決まり、円は縦横どちらにも歪まない。
	d := float64(min(w*2, faceH*4))
	cx, cy := float64(w), float64(faceH*2)
	rOut := d/2 - 3
	rIn := max(rOut-4, rOut/2)
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

	// 盤の中央。内周に収まる範囲でいちばん読みやすい表記を選ぶ (狭い盤では語を落とす)。
	// ⚠️ 内周の差し渡しは 2*rIn ドット = rIn セル (ドットは横 2 つで 1 セル)。ドット数のまま
	// 桁数として使うと 2 倍に見積もり、文字がリングに接する。左右に 1 セルずつ余白を残す。
	inner := max(int(rIn)-4, 1)
	mid := fitText(inner, []string{"残" + formatRemain(remain), formatRemain(remain), compactRemain(remain)})
	// 中央 2 行は盤の中心 (cy) を挟む行に置く。faceH/2 で数えると 1 行上へずれ、円が細く
	// なる位置に文字が来てリングへ接する。
	midRow := int(cy) / 4
	cv.putText(midRow, w/2-termwidth.Of(mid)/2, mid, sgr.BrightWhite)
	pct := fmt.Sprintf("%d%%", c.win.Percent)
	cv.putText(midRow+1, w/2-termwidth.Of(pct)/2, pct, col)
	return cv.lines(colored)
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

// fitText は幅 w に収まる最初の候補を返す (どれも収まらなければ最後の候補)。
func fitText(w int, candidates []string) string {
	for _, s := range candidates {
		if termwidth.Of(s) <= w {
			return s
		}
	}
	return candidates[len(candidates)-1]
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
