package main

// diff ポップアップのシンタックスハイライト (chroma)。
//
// ⚠️ 実験的機能・切り捨てやすさ優先の構造: chroma への依存とハイライトのロジックは
// このファイルに完全に閉じている。呼び出しは LoadCommitDiff (gitlog.go) の 1 箇所だけ
// なので、遅い/表示が好みでない場合は「その 1 行を素通しに戻し、本ファイルと go.mod の
// chroma を消す」だけで従来の git 配色相当へ戻せる (ユーザー了承済みの前提 2026-07-19)。
//
// 方式: git show は --color=never で受け、diff の構造色 (メタ行/hunk/追加/削除の記号) は
// 自前で付け、コード本文だけを chroma でファイル拡張子ベースにハイライトする。
// トークナイズは行単位 (複数行コメントなどの状態は行を跨いで持たない)。delta と同じ
// 割り切りで、行単位でも実用上の見た目は十分。

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// 256色主環境 (docs/theme-colors.md) なので formatter は terminal256、スタイルは
// テーマの基調と同じ gruvbox。truecolor 端末でも 256 色出力は問題なく表示される。
var (
	hlFormatter = formatters.Get("terminal256")
	hlStyle     = styles.Get("gruvbox")
)

// HighlightDiff は git show --color=never の出力へ diff 構造色 + シンタックス
// ハイライトを付ける。失敗した行は素のまま返す (ハイライトは常に best-effort)。
func HighlightDiff(lines []string) []string {
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
			out = append(out, ansiBold+line+ansiReset)
		case inDiff && !inHunk && strings.HasPrefix(line, "+++ "):
			lex = lexerForDiffPath(strings.TrimPrefix(line, "+++ "))
			out = append(out, ansiBold+line+ansiReset)
		case inDiff && !inHunk && strings.HasPrefix(line, "--- "):
			out = append(out, ansiBold+line+ansiReset)
		case inDiff && strings.HasPrefix(line, "@@"):
			// hunk ヘッダー。"@@ ... @@ 関数名" の関数名部分も含めてまとめてシアン
			inHunk = true
			out = append(out, ansiCyan+line+ansiReset)
		case inDiff && !inHunk && (strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "new file mode") ||
			strings.HasPrefix(line, "deleted file mode") || strings.HasPrefix(line, "old mode") ||
			strings.HasPrefix(line, "new mode") || strings.HasPrefix(line, "similarity index") ||
			strings.HasPrefix(line, "rename from") || strings.HasPrefix(line, "rename to") ||
			strings.HasPrefix(line, "Binary files")):
			out = append(out, ansiDim+line+ansiReset)
		case inDiff && strings.HasPrefix(line, "+"):
			out = append(out, ansiGreen+"+"+ansiReset+highlightCode(lex, line[1:]))
		case inDiff && strings.HasPrefix(line, "-"):
			out = append(out, ansiRed+"-"+ansiReset+highlightCode(lex, line[1:]))
		case inDiff && strings.HasPrefix(line, " "):
			out = append(out, " "+highlightCode(lex, line[1:]))
		case !inDiff && strings.HasPrefix(line, "commit "):
			out = append(out, ansiYellow+line+ansiReset)
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

// highlightCode はコード 1 行を chroma でハイライトする。lexer 不明・トークナイズ
// 失敗時は素のまま返す。
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
	// chroma は行末に改行を足すことがあるため落とす (呼び出し側は行単位で管理)
	return strings.TrimRight(b.String(), "\n")
}
