package main

import "termsafe"

// 端末描画に対して外部由来の文字列を無害化する関数の、main パッケージ側の別名。
//
// 実体は termsafe パッケージにある: 同じ無害化を issues パッケージ (issue markdown) も
// 通す必要があり、main の非公開関数のままだと呼べずコピーが生まれるため切り出した。
// 仕様・トレードオフの一次情報は termsafe のドキュメントコメント。
//
// ここに別名を置いているのは callsite の量が多い (git / CI / diff の各入口) ためで、
// 意味は完全に同一。新しい呼び出しはどちらの名前で書いてもよい。
//
// 🚨 var でなく func で持つ: sanitizePlainLine は毎フレーム全行から呼ばれる経路
// (worktreeRow.dispPath) にあり、変数だと間接呼び出しが 1 段挟まる。差し替え点にする意図も無い。
// (訂正: 7d60537 のコミットメッセージは「fast path がインライン化されない」と書いたが誤り。
// fast path は termsafe.sanitize の中にあり、sanitize はループを持つため var / func どちらでも
// インライン化されない — `go build -gcflags=-m ./...` (src/termsafe) の can inline 一覧に載らない。
// var → func で消えるのは間接呼び出しだけで、効果自体は実在するが理由が違っていた)
func sanitizeDetailLine(s string) string   { return termsafe.DetailLine(s) }
func sanitizeLineKeepTabs(s string) string { return termsafe.LineKeepTabs(s) }
func sanitizePlainLine(s string) string    { return termsafe.PlainLine(s) }
func dropEmojiVS16(s string) string        { return termsafe.DropEmojiVS16(s) }
