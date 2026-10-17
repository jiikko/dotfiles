package app

import "time"

// FetchTimeout は CI 状態・usage などの外部取得 1 回の上限 (main と画面のパッケージが共有する)。
const FetchTimeout = 10 * time.Second

// UsageRefreshInterval は usage オーバーレイをバックグラウンド再取得する周期 (ユーザー要望
// 2026-07-22)。トークン課金は発生しないが「安価」ではない: 1 回 ≈ 2.0s wall / 1.8s CPU
// (実測 2026-07-25)。この値は「表示の許容陳腐度」の単一の出典で、周期以外に 2 つの派生が
// ぶら下がる: usageCacheTTL (起動時のキャッシュ有効期間) と usageOverlay.stale
// (非表示中に止めたリフレッシュを U の再表示で取り戻す閾値)。どちらも定数を参照しているので
// 値を変えれば自動で追従する — が、下記は追従しないので手で揃えること。
// 🚨 実装で強制できない 2 つの制約 (変更時に再評価すること):
//  1. FetchTimeout より必ず大きく保つ。小さくすると fetch が overlap し、fetchCmd の
//     o.cancel 上書きで前回 fetch の cancel を取りこぼす (現状 10s < 60s で overlap しない)。
//  2. usage_overlay.go のフッター (usageFooter) はこの値ではなく usage.SharedFresh から作る
//     (値が新しくなる間隔はそちらで決まる)。この周期をそれより長くするならフッターも見直すこと。
//
// 🚨 この周期で claude を起こすわけではない: Claude 側の取得は usage.Fetch の全プロセス共有ゲート
//
//	(src/ratelimit/usage/shared.go) を通り、5 分以内の結果があればそれを返す。以前は毎回起こしていて、
//	表示を開いたまま 17 分で /api/oauth/usage が 429 になった (issue 627)。
const UsageRefreshInterval = time.Minute
