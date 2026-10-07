// Package issues は repo 内の issue markdown ファイルの探索・分類と、端末表示用の整形を担う。
//
// glogx 本体 (package main) には依存しない (将来の切り出しで依存を残さないため)。markdown の
// 整形 (本文 → 折り返した行) は共有の tuikit/markdown に任せ、このパッケージは issue 固有の
// 前処理とキャッシュだけを持つ。
//
// 層の分担:
//   - discover.go / parse.go — ファイルシステム側 (探索・ファイル名/本文からのメタデータ抽出・
//     一覧の絞り込みと並び順・バッジ)
//   - body.go — 本文の読み込みと、幅ごとの整形結果のキャッシュ (整形は tuikit/markdown)
//   - move.go / nextlink.go / banner.go — 状態ディレクトリへの移動・next/ の目印・担当者バナー
//   - filelink.go — 本文中のファイルパスの抽出 (viewer のジャンプモード)
package issues
