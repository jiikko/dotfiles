package main

// overlay のメモリ内キャッシュ (diff / job 詳細ログ) の成長上限。オンディスクの CI
// キャッシュには maxCacheEntries + TTL prune があるのに対し、メモリ内キャッシュは
// reloadAfterPull の reset() 以外に縮む契機がなく、pull を挟まない長時間セッションで
// 閲覧した SHA/job のぶんだけ無制限に育っていた (issue 029 P2)。

// overlayCacheLimit は 1 オーバーレイが保持するエントリ数の上限。diff は 1 エントリ最大
// maxDiffLines (5000) 行なので、50 エントリでも高々数十 MB に収まる。
const overlayCacheLimit = 50

// evictOverlayCache は cache が上限を超えたぶんを挿入が古い順に落とす。keep (表示中の
// キー) は消さない — 表示中エントリを evict すると画面が「(内容なし)」へ突然劣化する。
// order は挿入順のキー列 (呼び出し側が挿入時に append して渡す)。戻り値は更新後の order。
// アクセス順 (LRU) は追わない: 閲覧は人間律速で、挿入順の粗い evict で十分。
func evictOverlayCache(cache map[string][]string, order []string, keep string) []string {
	for len(order) > overlayCacheLimit {
		victim := -1
		for i, k := range order {
			if k != keep {
				victim = i
				break
			}
		}
		if victim < 0 {
			return order // 全部 keep (起こりえないが無限ループはしない)
		}
		delete(cache, order[victim])
		order = append(order[:victim], order[victim+1:]...)
	}
	return order
}
