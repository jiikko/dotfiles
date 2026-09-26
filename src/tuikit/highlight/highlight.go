// Package highlight は diff とコードの行へシンタックスハイライト (chroma) を付ける。glogx の diff の板と、
// pro-con の詳細の差分の板 (dotfiles issue 508)、markdown のフェンスコード (tuikit/markdown) が同じ色付けを使う
// (glogx の highlight.go から移した)。
//
// 方式: git は --color=never で受け、diff の構造色 (メタ行/hunk/追加/削除の記号) は
// 自前で付け、コード本文だけを chroma でファイル拡張子ベースにハイライトする。
// トークナイズは行単位 (複数行コメントなどの状態は行を跨いで持たない)。delta と同じ
// 割り切りで、行単位でも実用上の見た目は十分。
//
// 入力は使う側で無害化 (termsafe) してから渡す。ここは色を足すだけで、制御文字は落とさない。
package highlight

import (
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"tuikit/sgr"
)

// 256色主環境 (docs/theme-colors.md) なので formatter は terminal256、スタイルは
// テーマの基調と同じ gruvbox。truecolor 端末でも 256 色出力は問題なく表示される。
var (
	hlFormatter = formatters.Get("terminal256")
	hlStyle     = styles.Get("gruvbox")
)

// Diff は git show / git diff (--color=never) の出力へ diff 構造色 + シンタックス
// ハイライトを付ける。失敗した行は素のまま返す (ハイライトは常に best-effort)。行数は変えない。
func Diff(lines []string) []string {
	out := make([]string, 0, len(lines))
	var lex chroma.Lexer // 現在のファイルの lexer (nil = 言語不明で素通し)
	inDiff := false      // 最初の "diff --git" 以降か (それ以前は commit ヘッダー/メッセージ)
	inHunk := false      // "@@" 以降のコード本文か。ヘッダー系の判定は hunk 外に限定する:
	// hunk 内の "+++ x" / "--- x" は「先頭が ++ / -- のコード行の追加/削除」であって
	// ファイルヘッダーではない (誤ってヘッダー扱いすると ± マーカーを失う上、
	// lexerForDiffPath が偽パスで lexer を潰し以降のハイライトが消える。セルフレビューで検出)
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git"):
			inDiff = true
			inHunk = false
			lex = nil // 次の +++ で確定するまでリセット
			out = append(out, sgr.Bold+line+sgr.Reset)
		case inDiff && !inHunk && strings.HasPrefix(line, "+++ "):
			lex = lexerForDiffPath(strings.TrimPrefix(line, "+++ "))
			out = append(out, sgr.Bold+line+sgr.Reset)
		case inDiff && !inHunk && strings.HasPrefix(line, "--- "):
			out = append(out, sgr.Bold+line+sgr.Reset)
		case inDiff && strings.HasPrefix(line, "@@"):
			// hunk ヘッダー。"@@ ... @@ 関数名" の関数名部分も含めてまとめてシアン
			inHunk = true
			out = append(out, sgr.Cyan+line+sgr.Reset)
		case inDiff && !inHunk && (strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "new file mode") ||
			strings.HasPrefix(line, "deleted file mode") || strings.HasPrefix(line, "old mode") ||
			strings.HasPrefix(line, "new mode") || strings.HasPrefix(line, "similarity index") ||
			strings.HasPrefix(line, "rename from") || strings.HasPrefix(line, "rename to") ||
			strings.HasPrefix(line, "Binary files")):
			out = append(out, sgr.Dim+line+sgr.Reset)
		case inDiff && strings.HasPrefix(line, "+"):
			out = append(out, sgr.Green+"+"+sgr.Reset+Code(lex, line[1:]))
		case inDiff && strings.HasPrefix(line, "-"):
			out = append(out, sgr.Red+"-"+sgr.Reset+Code(lex, line[1:]))
		case inDiff && strings.HasPrefix(line, " "):
			out = append(out, " "+Code(lex, line[1:]))
		case !inDiff && strings.HasPrefix(line, "commit "):
			out = append(out, sgr.Yellow+line+sgr.Reset)
		default:
			// commit メッセージ本文・--stat 部分・空行などは素のまま
			out = append(out, line)
		}
	}
	return out
}

// lexerForDiffPath は "+++ b/path/to/file" のパス部分から lexer を解決する。
// 見つからない言語・/dev/null (削除ファイル) は nil (素通し)。空白等を含むパスは
// git が "b/pa th" と quote するため Match に失敗するが、その場合も素通しに
// 落ちるだけで害はない (unquote 対応は実需要が出たら)。
func lexerForDiffPath(path string) chroma.Lexer {
	path = strings.TrimPrefix(path, "b/")
	if path == "/dev/null" {
		return nil
	}
	return lexers.Match(path)
}

// Lang はフェンスの言語名 (sh / zsh / golang 等。エイリアスの解決は chroma の表に委ねる) でコード 1 行を色付けする。
// 未知の言語・空の言語名は素のまま返す。
func Lang(lang, code string) string {
	if lang == "" {
		return code
	}
	return Code(lexers.Get(lang), code)
}

// hlEscCache はトークン種別 → ANSI エスケープ列 ("" = 装飾なし) のメモ。
//
// chroma の Format はスタイル→エスケープ表 (256 色の Lab 距離最近傍探索込み) を **呼び出しの
// たびに** 丸ごと再計算する。glogx は行単位で Format していたため 5000 行の diff で同じ表を
// 5000 回作り直し、プロファイルで CPU の ~3 割が findClosest に落ちていた (実測 2026-08-13)。
// スタイル (gruvbox) と formatter (terminal256) は固定なので、種別ごとの結果は不変 = メモ化
// できる。取り出しは Format をトークン 1 個で呼んで前置エスケープを切り出す形にし、fallback
// 連鎖 (SubCategory → Category → Text) や色写像のロジックを chroma 側と二重実装しない。
var hlEscCache sync.Map // chroma.TokenType -> string

// hlEscapeFor は種別 t の前置エスケープを返す。sentinel の \x01 は ANSI エスケープ列
// (ESC・数字・';'・'['・'m') に決して現れないため、出力の切り出しが誤爆しない。
func hlEscapeFor(t chroma.TokenType) string {
	if v, ok := hlEscCache.Load(t); ok {
		return v.(string)
	}
	esc := ""
	var b strings.Builder
	if err := hlFormatter.Format(&b, hlStyle, chroma.Literator(chroma.Token{Type: t, Value: "\x01"})); err == nil {
		if i := strings.IndexByte(b.String(), '\x01'); i > 0 {
			esc = b.String()[:i]
		}
	}
	hlEscCache.Store(t, esc)
	return esc
}

// Code はコード 1 行を chroma でハイライトする。lexer 不明・トークナイズ
// 失敗時は素のまま返す。
//
// トークンの整形は Format を使わず自前で行う (hlEscCache の doc)。出力形式は Format
// (terminal256) と同一: 装飾ありトークンは esc + 本文 + リセット、なしは素のまま。
// 入力は 1 行 (改行を含まない) なので、chroma が補う改行はトークン末尾にしか現れない。
// Format は改行の手前でリセットするため、末尾で改行を落としてから閉じれば等価になる。
func Code(lex chroma.Lexer, code string) string {
	if lex == nil || code == "" {
		return code
	}
	it, err := lex.Tokenise(nil, code)
	if err != nil {
		return code
	}
	var b strings.Builder
	b.Grow(len(code) * 2)
	for token := it(); token != chroma.EOF; token = it() {
		v := strings.TrimRight(token.Value, "\n")
		if v == "" {
			continue
		}
		if esc := hlEscapeFor(token.Type); esc != "" {
			b.WriteString(esc)
			b.WriteString(v)
			b.WriteString(sgr.Reset)
			continue
		}
		b.WriteString(v)
	}
	return b.String()
}
