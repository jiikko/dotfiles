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
var (
	sanitizeDetailLine   = termsafe.DetailLine
	sanitizeLineKeepTabs = termsafe.LineKeepTabs
	sanitizePlainLine    = termsafe.PlainLine
	dropEmojiVS16        = termsafe.DropEmojiVS16
)
