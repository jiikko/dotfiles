package issues

import (
	"os"
	"regexp"
	"strconv"
	"strings"

	"termsafe"

	"tuikit/markdown"
)

// Body は issue 1 件の本文と、整形結果の幅ごとのキャッシュ。
//
// なぜキャッシュするか: 整形は「ブロック分解 → インライン解析 → 折り返し」を通るので本文
// 1 件で数 ms かかる。TUI は 1 フレームごとに描画を呼ぶため、毎フレーム整形すると popup の
// スクロールが重くなる。同じ (width, colored) なら結果は同じなので、幅が変わったときだけ
// 作り直す (リサイズ 1 回につき 1 回)。
type Body struct {
	src      string
	width    int
	colored  bool
	lines    []string
	srcLines []int // lines と同じ並びのソース行番号 (0 = 出さない)
	renders  int   // 整形した回数 (キャッシュが効いているかのテスト用)

	progress     string // Progress() の結果キャッシュ ("" も有効な値なので下のフラグと対で持つ)
	progressDone bool

	// base は本文中の相対パスの基準。**本文を読んだ時点**の値を ReadBody が入れる (viewer は
	// 移動を追って開いている Issue を差し替えるが、本文は読み直すまで古い場所の書き方のまま。
	// 基準を後から Issue 側で引くと、古い本文を新しい場所で解決してしまう)。Repo は FileLinks が入れる。
	base LinkBase
	// 以下はジャンプモード (FileLinks / JumpLines) のキャッシュ。本文を開いただけでは作らない。
	resolved       map[linkKey]linkTarget // (種類, dest, root) → 解決結果。stat を 1 本文につき 1 回に抑える
	fileLinks      []FileLink
	fileLinksW     int
	fileLinksRepo  string // Repos を連結した鍵
	fileLinksOK    bool
	jumpLines      []string
	jumpW, jumpSel int
	jumpColored    bool
}

type linkKey struct {
	kind       markdown.LinkKind
	dest, repo string
}

type linkTarget struct {
	path string
	line int
	ok   bool
	dir  bool
}

// checkboxRe はチェックボックス行。parse.go の LoadMeta から Body へ移した
// (一覧のために全 issue の全文を読むのを止めるため。下の Progress の doc)。
var checkboxRe = regexp.MustCompile(`^\s*[-*+]\s+\[([ xX])\]`)

// Progress はチェックボックスの生の事実 ("3/7")。チェックボックスが無ければ ""。
//
// 🚨 ここから「着手中」を導出しない。実測でパスと真逆になる: done/ にあるのに 0/N の
// ファイルが 36 件 (dropbox 16 / DualNote 13 / SnapTrim 7)、逆に全チェック済みでも
// 本文が未完を明記している open ファイルがある。チェックボックスは「作業項目の進捗」
// ではなく「将来の実装計画」や「Phase 追跡」に使われていて、意味が repo・ファイルごとに違う。
//
// なぜ Issue ではなく Body が持つか: 数えるには本文を最後まで読む必要があり、Issue 側で
// 持つと**一覧を出すたびに全 issue の全文を読む**ことになっていた (LoadMeta の doc)。
// Body は issue を開いたときだけ作られ、その時点で全文がメモリにあるので追加の I/O が要らない。
//
// 🚨 数える範囲が旧 Issue.Progress と 1 点だけ違う: **front matter 内の行も数える**。
// 旧実装は front matter を別分岐で処理していたため数えなかった (実測差分: 先頭が `---` で
// 始まり front matter 内に `- [x]` がある入力で旧 1/1 に対し新 2/3 等。2026-08-14 の R1
// レビューが 426 件の差分入力を提示)。front matter に checkbox を書く issue は
// この repo に存在しない (51 件すべて front matter 無し) ため実害は無いと判断して
// この差を受け入れる。「先頭 `---` ブロック」の規則を Body 側にも複製すると
// 同じ規則が 2 箇所になるので、そちらは採らない。
//
// 🚨 コードフェンス内の `- [ ]` も数える (checkboxRe はフェンス非対応)。これは旧と同じ挙動。
// フェンス対応が要るなら別 issue で。
func (b *Body) Progress() string {
	if b.progressDone {
		return b.progress
	}
	b.progressDone = true
	boxes, checked := 0, 0
	for line := range strings.Lines(b.src) {
		// LoadMeta が無害化した行に対して数えていたので、移設後も同じ関門を通す
		// (無害化で行頭が変わると一致が変わりうるため、挙動を揃える)
		m := checkboxRe.FindStringSubmatch(
			termsafe.PlainLine(strings.TrimRight(strings.TrimRight(line, "\n"), "\r")))
		if m == nil {
			continue
		}
		boxes++
		if m[1] != " " {
			checked++
		}
	}
	if boxes == 0 {
		return ""
	}
	b.progress = strconv.Itoa(checked) + "/" + strconv.Itoa(boxes)
	return b.progress
}

// NewBody は本文から Body を作る。整形はまだ行わない (最初の Lines で遅延実行)。
func NewBody(src string) *Body { return &Body{src: src, width: -1} }

// Lines は width 桁へ整形した行を返す。前回と同じ条件ならキャッシュを返す。
func (b *Body) Lines(width int, colored bool) []string {
	if b.lines != nil && b.width == width && b.colored == colored {
		return b.lines
	}
	b.width, b.colored = width, colored
	b.lines, b.srcLines = markdown.Render(b.src, width, colored)
	b.renders++
	return b.lines
}

// SrcLines は Lines と同じ並びのソース (.md) 行番号 (0 = その行には出さない)。
// 🚨 Lines を呼ぶ前は空。整形しないと行の対応が決まらないため。
func (b *Body) SrcLines() []int { return b.srcLines }

// SrcLineCount は本文のソース行数。行番号の溝を何桁取るかを整形前に決めるのに使う
// (溝幅が決まらないと整形幅が決まらず、整形しないと行番号が決まらない循環を切る)。
func (b *Body) SrcLineCount() int { return strings.Count(b.src, "\n") + 1 }

// FileLinks は width 桁で整形したとき、開けるファイルへ解決できたリンク (画面の出現順)。
// repos は「repo の中」とみなす範囲 (LinkBase.Repos)。
//
// 解決 (stat) は (種類, dest) ごとに 1 回だけ行い、結果は Body が生きている間持つ。本文は
// 取り直し (見張り・エディタから戻ったとき) で Body ごと作り直されるので、そこで新しくなる。
// 位置は幅で変わるので幅ごとに作り直す (整形は Lines と同じだが、色の有無に依らず位置は同じなので
// 色なしで 1 回整形する)。
func (b *Body) FileLinks(width int, repos []string) []FileLink {
	repo := strings.Join(repos, "\x00")
	if b.fileLinksOK && b.fileLinksW == width && b.fileLinksRepo == repo {
		return b.fileLinks
	}
	_, _, links := markdown.RenderLinks(b.src, width, false, nil)
	if b.resolved == nil {
		b.resolved = map[linkKey]linkTarget{}
	}
	out := make([]FileLink, 0, len(links))
	for i, l := range links {
		if len(l.Segs) == 0 {
			continue
		}
		dests := []string{l.Dest}
		if l.Kind == markdown.LinkCode {
			dests = ExpandBraces(l.Dest) // `x.{sql,tsv}` は展開したそれぞれに止まる (同じ強調を共有する)
		}
		for _, d := range dests {
			k := linkKey{l.Kind, d, repo}
			t, seen := b.resolved[k]
			if !seen {
				base := b.base
				base.Repos = repos
				p, line, ok := ResolveLink(l.Kind, d, base)
				t = linkTarget{path: p, line: line, ok: ok}
				if ok {
					if fi, err := os.Stat(p); err == nil && fi.IsDir() {
						t.dir = true
					}
				}
				b.resolved[k] = t
			}
			if !t.ok {
				continue
			}
			out = append(out, FileLink{Index: i, Kind: l.Kind, Dest: d, Path: t.path, Line: t.line, Dir: t.dir, Top: l.Segs[0].Line})
		}
	}
	b.fileLinks, b.fileLinksW, b.fileLinksRepo, b.fileLinksOK = out, width, repo, true
	return out
}

// Recheck は l を開く直前に、キャッシュを使わずもう一度解決する (ResolveLink と同じ判定を通す)。
// 一覧を作った後に実体が symlink へ差し替わった (git pull 等) ものを、stat だけの近似で通さないため。
func (b *Body) Recheck(l FileLink, repos []string) bool {
	base := b.base
	base.Repos = repos
	p, _, ok := ResolveLink(l.Kind, l.Dest, base)
	return ok && p == l.Path
}

// JumpLines は FileLinks のリンクを強調した整形結果 (sel = FileLinks 内の選択中の添字)。
// 行数・桁・行番号は Lines と同じ (markdown.RenderLinks の doc)。
func (b *Body) JumpLines(width int, colored bool, repos []string, sel int) []string {
	if b.jumpLines != nil && b.jumpW == width && b.jumpColored == colored && b.jumpSel == sel &&
		b.fileLinksOK && b.fileLinksW == width && b.fileLinksRepo == strings.Join(repos, "\x00") {
		return b.jumpLines
	}
	fl := b.FileLinks(width, repos)
	marks := make(map[int]markdown.LinkMark, len(fl))
	for i, l := range fl {
		// 🚨 Selected を Marked で上書きしない: 波括弧の展開は 1 つのリンク (Index) に複数の止まり先を持つ
		if i == sel {
			marks[l.Index] = markdown.LinkSelected
		} else if marks[l.Index] != markdown.LinkSelected {
			marks[l.Index] = markdown.LinkMarked
		}
	}
	b.jumpLines, _, _ = markdown.RenderLinks(b.src, width, colored, func(i int) markdown.LinkMark { return marks[i] })
	b.jumpW, b.jumpColored, b.jumpSel = width, colored, sel
	return b.jumpLines
}

// Len は最後に整形した行数 (未整形なら 0)。pager のスクロール上限に使う。
func (b *Body) Len() int { return len(b.lines) }

// urlRe は本文から拾う http(s) URL。終端は空白か、URL の外側に来やすい記号で切る。
//
// なぜ記号を除くか: issue 本文の URL は markdown リンク `[text](url)`、括弧書き `(url)`、
// 文末の句点つき `url。` のいずれの形でも現れる。貪欲に拾うと `)` や `。` が URL に混ざり、
// ブラウザが 404 を開く。逆に `?` `#` `=` `&` は query / fragment の一部なので残す。
//
// 🚨 制御文字 (C0 \x00-\x1f / DEL \x7f / C1 \u0080-\u009f) も終端に含める。URL に生の制御文字は
// 入らないので実用上の損失は
// 無く、含めないと `https://example.com/<ESC>]0;…<BEL>` が 1 本の URL として抽出され、URL
// ピッカーの一覧描画で端末へ素通りする (URLs は整形経路を通らず生ソースから拾うため、
// tuikit/markdown 側の無害化では守れない)。\s は \t\n\r\f\v しか外さない。
// C1 を落とすのは hasTerminalControl (同パッケージ) が C1 を制御文字と定義しているのと揃えるため。
// U+009B (CSI) / U+009D (OSC) は端末によっては ESC[ / ESC] と同義に解釈されるので、負クラスから
// 漏れると URL ピッカーの行 (url_picker.go) まで到達する。
var urlRe = regexp.MustCompile(`https?://[^\s)\]>"'` + "`" + `\x00-\x1f\x7f\x{80}-\x{9f}]+`)

// URLs は本文に現れる http(s) URL を出現順で返す (重複は最初の 1 つだけ)。
//
// 重複を落とすのは、同じ URL が「参考」節と本文の両方に出る issue が実在し、順に開く操作
// (viewer の u キー) で同じページを 2 回開かされるのを避けるため。整形 (Lines) とは独立に
// 生ソースから拾う: 折り返しで URL が行をまたいで割れても、元のソースには 1 本で入っている。
func (b *Body) URLs() []string {
	found := urlRe.FindAllString(b.src, -1)
	seen := make(map[string]bool, len(found))
	out := make([]string, 0, len(found))
	for _, u := range found {
		// 末尾の句読点は URL の一部でないことが多い (英文の "…/foo." と日本語の "…/foo。")。
		// 記号だけを削り、パス区切りの / は残す。
		u = strings.TrimRight(u, ".,;:。、")
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}
