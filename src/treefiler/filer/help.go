package filer

import (
	"strings"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// help.go は ? のキー一覧の板。分類ごとの節に分け、幅があれば 2 段に組む (2026-11-04 ユーザー依頼「カテゴリごとにセクションを作って
// 分類して表示してほしい。help のモーダルをもっと広く使っていいよ」。2 段組の見本を見て選んだ)。spec §8.1 の 1 列・幅 64 から変えた所は spec §0.1。

type helpSection struct {
	title string
	keys  [][2]string
}

// helpSections はキー一覧 (treebeard の KEYS から、写さないもの (マウス・音声・画像) を除いて日本語にした)。
// 2 段に組むときは節の並びのまま前後に割るので、読む順に並べる。
var helpSections = []helpSection{
	{"移動", [][2]string{
		{"j k ↓ ↑ ^N ^P", "同じ階層の中で移動"},
		{"J K / ^D ^U", "10 件 / 半ページ"},
		{"g G", "兄弟の先頭 / 末尾"},
		{"l → ^F enter", "フォルダへ潜る · ファイルを開く"},
		{"h ← ^B", "親へ (root なら 1 段上へ)"},
		{"- backspace ^H", "root を 1 段上へ"},
		{":", "パスへ移動 (外なら root を上げる)"},
		{"'", "起動時のカーソルへ戻る"},
	}},
	{"開閉", [][2]string{
		{"space tab", "フォルダの開閉"},
		{"c C", "このフォルダを畳む · 経路以外を畳む"},
		{">", "配下を全部開く (100 件以上は確認)"},
		{"esc", "> で開いている途中を止める"},
		{"<", "配下を全部畳む"},
		{"Z", "全部畳んで root 直下へ戻る"},
	}},
	{"検索", [][2]string{
		{"/", "開いた木の全体を検索"},
		{"n N", "次 · 前の一致"},
	}},
	{"表示", [][2]string{
		{". I", "dotfile の表示を切り替え"},
		{"p", "右のプレビューの欄を開閉"},
		{"S", "並びを巡回 (名前 → 更新 → 大きさ)"},
		{"r", "読み直す"},
		{",", "設定"},
	}},
	{"ファイル", [][2]string{
		{"a", "新規作成 (末尾 / でフォルダ)"},
		{"m", "名前の変更"},
		{"d", "ゴミ箱へ移す"},
		{"y Y", "パスをコピー (絶対 · root から)"},
		{"e", "エディタで開く (フォルダも)"},
		{"o", "外のアプリで開く (open)"},
		{"O", "Finder で表示"},
	}},
	{"タイルの中", [][2]string{
		{"j k ^D ^U space", "スクロール"},
		{"g G", "先頭 · 末尾"},
		{"tab", "パスを選ぶ (e で選んだ先を編集)"},
		{"J K", "隣のタイルへ"},
		{"d", "diff と本文を切り替え"},
		{"e o O", "エディタ · 外のアプリ · Finder"},
		{"q esc", "1 枚閉じる"},
	}},
	{"外へ出る", [][2]string{
		{"!", "コマンドを 1 行 ($f = 選んだパス)"},
		{"s", "シェルを開く (抜けると戻る)"},
	}},
	{"その他", [][2]string{
		{"q esc", "終了 (タイルの上では 1 枚閉じる)"},
		{"?", "このキー一覧"},
	}},
}

const (
	helpKeyW    = 15 // キーの欄の桁 (いちばん長いキーの並び "j k ^D ^U space")
	helpColMax  = 52 // 1 段の最大の桁 (広い端末でも説明の欄を伸ばしすぎない)
	helpColMin  = 40 // 2 段に組む 1 段の最小の桁。これを切る端末では 1 段 (幅 64。spec §8.1)
	helpColGap  = 4  // 段の間
	helpOneColW = 60 // 1 段のときの段の桁 (枠を入れて 64)
)

// helpRow は板の 1 行。title があれば節の見出し、title も key も無ければ節の間の空行。
type helpRow struct{ title, key, desc string }

// helpColumns は節を ncol 段 (1 か 2) の行に組む。2 段は節の並びのまま前後に割り、高い方の段が最も低くなる所で割る。
func helpColumns(secs []helpSection, ncol int) [][]helpRow {
	rows := func(ss []helpSection) []helpRow {
		var out []helpRow
		for i, s := range ss {
			if i > 0 {
				out = append(out, helpRow{})
			}
			out = append(out, helpRow{title: s.title})
			for _, kv := range s.keys {
				out = append(out, helpRow{key: kv[0], desc: kv[1]})
			}
		}
		return out
	}
	if ncol < 2 || len(secs) < 2 {
		return [][]helpRow{rows(secs)}
	}
	best := 1
	for k := 2; k < len(secs); k++ {
		if max(len(rows(secs[:k])), len(rows(secs[k:]))) < max(len(rows(secs[:best])), len(rows(secs[best:]))) {
			best = k
		}
	}
	return [][]helpRow{rows(secs[:best]), rows(secs[best:])}
}

// drawHelp はキー一覧の板 (中央)。幅があれば 2 段、無ければ 1 段 (幅 64)。
func (m *Model) drawHelp(c *canvas) {
	inner := m.w - 2 - 4 // 板の外に左右 1 桁ずつ・枠と内側の余白で左右 2 桁ずつ
	ncol, colW := 1, min(helpOneColW, inner)
	if cw := min(helpColMax, (inner-helpColGap)/2); cw >= helpColMin {
		ncol, colW = 2, cw
	}
	cols := helpColumns(helpSections, ncol)
	need := 0
	for _, col := range cols {
		need = max(need, len(col))
	}
	w := ncol*colW + (ncol-1)*helpColGap + 4
	h := min(need+4, m.h-1)
	if colW < 16 || h < 4 {
		return
	}
	x0, y0 := (m.w-w)/2, max((m.h-1-h)/2, 0)
	c.frame(rect{x0, y0, w, h}, cAccRoute)
	c.put(x0+2, y0, " treefiler · キー一覧 ", cText, true)
	rows := h - 3 // 枠の上下と題の下の空行を除いた行
	for i, col := range cols {
		x := x0 + 2 + i*(colW+helpColGap)
		shown := col
		if len(shown) > rows { // 収まらないなら段の最後の行に残りの数を出す (黙って切ると、切れたことが分からない。issue 731)
			shown = shown[:max(rows-1, 0)]
		}
		for j, r := range shown {
			m.drawHelpRow(c, x, y0+2+j, colW, r)
		}
		if rest := helpKeyCount(col[len(shown):]); rest > 0 {
			c.put(x, y0+2+len(shown), termwidth.Truncate("… 残り "+itoa(rest)+" 行 (端末を高くすると全部出る)", colW, "…"), cMuted, false)
		}
	}
}

func (m *Model) drawHelpRow(c *canvas, x, y, colW int, r helpRow) {
	switch {
	case r.title != "":
		head := "── " + r.title + " "
		c.put(x, y, termwidth.Truncate(head, colW, ""), cText, true)
		if rest := colW - termwidth.Of(head); rest > 0 {
			c.put(x+termwidth.Of(head), y, strings.Repeat("─", rest), cAccDim, false)
		}
	case r.key != "":
		c.put(x, y, termwidth.FillRight(r.key, helpKeyW), cAccRoute, true)
		c.put(x+helpKeyW+1, y, termwidth.Truncate(r.desc, max(colW-helpKeyW-1, 1), "…"), cText, false)
	}
}

// helpKeyCount は rows のうちキーの行の数 (残りの行数として出す。見出しと空行は数えない)。
func helpKeyCount(rows []helpRow) int {
	n := 0
	for _, r := range rows {
		if r.key != "" {
			n++
		}
	}
	return n
}
