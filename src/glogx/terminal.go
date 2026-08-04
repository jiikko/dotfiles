package main

import "glogx/termsafe"

// 端末描画に対して外部由来の文字列を無害化する関数の、main パッケージ側の別名。
//
// 実体は termsafe パッケージにある: 同じ無害化を issues パッケージ (issue markdown) も
// 通す必要があり、main の非公開関数のままだと呼べずコピーが生まれるため切り出した。
// 仕様・トレードオフの一次情報は termsafe のドキュメントコメント。
//
// ここに別名を置いているのは callsite の量が多い (git / CI / diff の各入口) ためで、
// 意味は完全に同一。新しい呼び出しはどちらの名前で書いてもよい。
//
// ⚠️ var でなく func で持つ: sanitizePlainLine は毎フレーム全行から呼ばれる経路
// (worktreeRow.dispPath) にあり、変数だと間接呼び出しになって「制御文字なしなら素通り」の
// fast path がインライン化されない。差し替え点にする意図も無い。
func sanitizeDetailLine(s string) string   { return termsafe.DetailLine(s) }
func sanitizeLineKeepTabs(s string) string { return termsafe.LineKeepTabs(s) }
func sanitizePlainLine(s string) string    { return termsafe.PlainLine(s) }
func dropEmojiVS16(s string) string        { return termsafe.DropEmojiVS16(s) }
