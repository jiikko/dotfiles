// Package app は glogx の画面のパッケージと main が共有する状態・道具を持つ: 現在時刻のシーム (TimeNow)・
// 全画面ビューアの ID と横断キーの表・全画面を閉じたときの記憶 (ScreenMemory)・終了前の完了待ち (CleanupLatch)・
// キャッシュの置き場と atomic 書き込み。main の型は import できないので、ここへ持ち込まない (issue 742)。
package app

import "time"

// TimeNow は経過時間・ETA 算出の現在時刻。テストで固定するため差し替え可能にしている。
// main の tui.go では time.Now / time.Since の直呼びを forbidigo が禁じ、ここを通させる (.golangci.yml)。
var TimeNow = time.Now
