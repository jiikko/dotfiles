package main

// diff プレビューのシンタックスハイライト (chroma)。src/glog/highlight.go を参照した移植。
// glog は read-only ツールとして触らない方針のため、ロジックはこのファイルに self-contained
// にコピーしてある。
//
// ⚠️ 切り捨てやすさ優先: chroma 依存とハイライトのロジックはこのファイルに閉じており、
// 呼び出しは loadPreview / loadChangePreview (gitcmd.go) だけ。遅い/好みでなければ
// 「その呼び出しを git show --color=always の素通しに戻し、本ファイルと go.mod の chroma を
// 消す」だけで従来の git 配色へ戻せる。
//
// 方式: git を --color=never で受け、diff の構造色 (メタ/hunk/±) は theme 色で自前で付け、
// コード本文だけを chroma でファイル拡張子ベースにハイライトする。トークナイズは行単位
// (複数行コメント等の状態は行を跨がない。delta と同じ割り切り)。

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// 256 色主環境なので formatter は terminal256、スタイルはテーマ基調と同じ gruvbox。
var (
	hlFormatter = formatters.Get("terminal256")
	hlStyle     = styles.Get("gruvbox")
)

const ansiBold = "\x1b[1m"

// diff の構造色は theme/colors.yml から引く (単一ソース)。
var (
	hlGreen  = fg("active_green")    // 追加 +
	hlRed    = fg("error_red")       // 削除 -
	hlCyan   = fg("info_cyan")       // hunk @@
	hlYellow = fg("quantity_yellow") // commit 行
)

// highlightDiff は git show/diff --color=never の出力へ diff 構造色 + シンタックス
// ハイライトを付ける。失敗した行は素のまま返す (常に best-effort)。
func highlightDiff(lines []string) []string {
	out := make([]string, 0, len(lines))
	var lex chroma.Lexer // 現在のファイルの lexer (nil = 言語不明で素通し)
	inDiff := false      // 最初の "diff --git" 以降か
	inHunk := false      // "@@" 以降のコード本文か。ヘッダー系判定は hunk 外に限定する:
	// hunk 内の "+++ x" / "--- x" は「先頭が ++ / -- のコード行」であってファイルヘッダー
	// ではない (誤判定すると ± マーカーを失い lexer も潰れる。glog のセルフレビューで検出済)
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git"):
			inDiff = true
			inHunk = false
			lex = nil
			out = append(out, ansiBold+line+ansiReset)
		case inDiff && !inHunk && strings.HasPrefix(line, "+++ "):
			lex = lexerForDiffPath(strings.TrimPrefix(line, "+++ "))
			out = append(out, ansiBold+line+ansiReset)
		case inDiff && !inHunk && strings.HasPrefix(line, "--- "):
			out = append(out, ansiBold+line+ansiReset)
		case inDiff && strings.HasPrefix(line, "@@"):
			inHunk = true
			out = append(out, hlCyan+line+ansiReset)
		case inDiff && !inHunk && (strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "new file mode") ||
			strings.HasPrefix(line, "deleted file mode") || strings.HasPrefix(line, "old mode") ||
			strings.HasPrefix(line, "new mode") || strings.HasPrefix(line, "similarity index") ||
			strings.HasPrefix(line, "rename from") || strings.HasPrefix(line, "rename to") ||
			strings.HasPrefix(line, "Binary files")):
			out = append(out, ansiDim+line+ansiReset)
		case inDiff && strings.HasPrefix(line, "+"):
			out = append(out, hlGreen+"+"+ansiReset+highlightCode(lex, line[1:]))
		case inDiff && strings.HasPrefix(line, "-"):
			out = append(out, hlRed+"-"+ansiReset+highlightCode(lex, line[1:]))
		case inDiff && strings.HasPrefix(line, " "):
			out = append(out, " "+highlightCode(lex, line[1:]))
		case !inDiff && strings.HasPrefix(line, "commit "):
			out = append(out, hlYellow+line+ansiReset)
		default:
			// commit メッセージ本文・--stat・空行などは素のまま
			out = append(out, line)
		}
	}
	return out
}

// lexerForDiffPath は "+++ b/path/to/file" のパス部分から lexer を解決する。
// 見つからない言語・/dev/null (削除) は nil (素通し)。
func lexerForDiffPath(path string) chroma.Lexer {
	path = strings.TrimPrefix(path, "b/")
	if path == "/dev/null" {
		return nil
	}
	return lexers.Match(path)
}

// highlightCode はコード 1 行を chroma でハイライトする。lexer 不明・失敗時は素通し。
func highlightCode(lex chroma.Lexer, code string) string {
	if lex == nil || code == "" {
		return code
	}
	it, err := lex.Tokenise(nil, code)
	if err != nil {
		return code
	}
	var b strings.Builder
	if err := hlFormatter.Format(&b, hlStyle, it); err != nil {
		return code
	}
	return strings.TrimRight(b.String(), "\n")
}

// highlightDiffText は複数行文字列をハイライトして返す (末尾改行は保つ)。
func highlightDiffText(s string) string {
	if s == "" {
		return s
	}
	return strings.Join(highlightDiff(strings.Split(s, "\n")), "\n")
}
