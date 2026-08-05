package main

// lineCache は「キー → 表示行」のメモリ内キャッシュと、その取得の単発化 (single-flight) を
// 1 つにまとめた型。diff / job 詳細ログ / status のプレビューが共有する。
//
// なぜ型にするか: この 3 つは同じ状態 3 点 (entries / order / busy) と同じ手順
//
//	busy を降ろす → 未登録なら order へ足す → entries へ入れる → 上限超過を落とす
//
// を各々が持っていた。上限を落とす部分だけが evictOverlayCache として関数に出ていたが、
// 状態を持たない関数なので呼び出し側が entries と order を引数で渡す形になり、「3 つの
// フィールドを揃って正しく更新する」責任は呼び出し側に残っていた (実際 3 者 + statusView の
// storePreview で 4 通りに書かれていた)。状態と手順を同じ型に置くと、順序を間違える余地が
// 構造的に消える。
//
// ⚠️ アクセス順 (LRU) は追わない: 閲覧は人間律速で、挿入順の粗い evict で十分。
type lineCache struct {
	entries map[string][]string
	order   []string // entries への挿入順 (上限超過分を古い順に落とすため)
	busy    map[string]bool
}

// lineCacheLimit は 1 つの lineCache が保持するエントリ数の上限。
//
// オンディスクの CI キャッシュには maxCacheEntries + TTL prune があるのに対し、メモリ内は
// reloadAfterPull の reset() 以外に縮む契機がなく、pull を挟まない長時間セッションで閲覧した
// SHA/job のぶんだけ無制限に育っていた (issue 029 P2)。diff は 1 エントリ最大 maxDiffLines
// (5000) 行なので、50 エントリでも高々数十 MB に収まる。
const lineCacheLimit = 50

func newLineCache() lineCache {
	return lineCache{entries: map[string][]string{}, busy: map[string]bool{}}
}

// get はキャッシュ済みの行を返す (ok=false = 未取得)。
func (c *lineCache) get(key string) ([]string, bool) {
	lines, ok := c.entries[key]
	return lines, ok
}

// has はキャッシュ済みか。
func (c *lineCache) has(key string) bool {
	_, ok := c.entries[key]
	return ok
}

// loading は取得中か (スピナーを回すかの判定)。
func (c *lineCache) loading(key string) bool { return c.busy[key] }

// fetching は何か 1 つでも取得中か。
func (c *lineCache) fetching() bool { return len(c.busy) > 0 }

// begin は取得を予約する。既にキャッシュ済み / 取得中なら false を返す (= 発行しない)。
// これが single-flight の唯一の入口で、呼び出し側が busy を直接触らないための関門。
func (c *lineCache) begin(key string) bool {
	if c.has(key) || c.busy[key] {
		return false
	}
	c.busy[key] = true
	return true
}

// store は取得結果を格納して取得中を解除し、上限超過分を古い順に落とす。
// keep は「今表示中のキー」で、これだけは落とさない — 表示中エントリを evict すると画面が
// 「(内容なし)」へ突然劣化するため。
func (c *lineCache) store(key string, lines []string, keep string) {
	delete(c.busy, key)
	if !c.has(key) {
		c.order = append(c.order, key)
	}
	c.entries[key] = lines
	c.evict(keep)
}

// abort は結果を格納せずに取得中だけを解除する (取得が失敗した経路)。
func (c *lineCache) abort(key string) { delete(c.busy, key) }

// clearBusy は取得中の札だけを全部降ろす (キャッシュは残す)。
// viewer を閉じたときに使う: 走行中の取得が返らない限り fetching() が true のままになり、
// フレーム tick が回り続けるため。
func (c *lineCache) clearBusy() { c.busy = map[string]bool{} }

// reset は中身を捨てる (pull 後の全面リロード等、キャッシュの前提が変わったとき)。
func (c *lineCache) reset() { *c = newLineCache() }

// evict は上限を超えたぶんを挿入が古い順に落とす (keep は残す)。
func (c *lineCache) evict(keep string) {
	for len(c.order) > lineCacheLimit {
		victim := -1
		for i, k := range c.order {
			if k != keep {
				victim = i
				break
			}
		}
		if victim < 0 {
			return // 全部 keep (起こりえないが無限ループはしない)
		}
		delete(c.entries, c.order[victim])
		c.order = append(c.order[:victim], c.order[victim+1:]...)
	}
}
