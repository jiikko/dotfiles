// Package issues は repo 内の issue markdown ファイルの探索・分類と、端末表示用の整形を担う。
//
// glogx 本体 (package main) には依存しない: 幅計算・ANSI・markdown 整形をこのパッケージ内で
// 完結させる (usage パッケージと同じ方針。将来の切り出しで依存を残さないため)。
//
// 層の分担:
//   - discover.go / parse.go — ファイルシステム側 (探索・ファイル名/本文からのメタデータ抽出)
//   - markdown.go / wrap.go — 「本文 → 意味付きスパン列 → 折り返した行」への変換 (ANSI を含まない)
//   - render.go — スパン列に ANSI を塗る純粋描画層 (I/O 禁止。depguard の render-pure ルール)
package issues
