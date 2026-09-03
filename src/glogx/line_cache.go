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
// 🚨 アクセス順 (LRU) は追わない: 閲覧は人間律速で、挿入順の粗い evict で十分。
//
// 🚨 **キーの契約**: キーは内容を一意に決めること。決められない caller は、
// **内容が変わりうる契機で自分で clearEntries すること** (この型は無効化の契機を知らない)。
//
// 3 者のうち diff (キー=SHA) と job 詳細 (キー=SHA/cursor) は内容が不変なので契約が成立する。
// statusView.preview は **キー (section+XY+path) が内容を一意に決めない** —
// 同じ ` M` のまま中身だけ変わる保存し直しでキーが動かない。キーに内容の指紋 (mtime+size 等) を
// 混ぜる案は採らなかった: previewKey は描画経路でも呼ばれるので毎フレーム syscall になる。
// 代わりに statusView 側が **r / 閉じ** の 2 契機で clearEntries する (issue 114)。
// 自動更新 (1.5 秒ポーリング) でも `receive` が clearEntries を呼ぶが、それは
// **rows が動いたとき (section/XY/path の変化) だけ**で、上に書いた「同じ ` M` のまま中身だけ
// 変わる保存し直し」では発火しない。そこを据え置くのは意図的
// (毎 1.5 秒 git diff を走らせないため。TestStatusReceiveSchedulesPreviewRefetchOnChange が pin)。
//
// 🚨 caller が clearEntries を呼んでも、**飛んでいる取得が後から着地すれば復活する**。
// statusView はメッセージに gen を載せて世代違いを捨てることで塞いでいる (この型は
// 無効化の契機も世代も知らないので、型だけでは守れない)。
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

// clearEntries は中身だけを捨てる (取得中の札は残す)。
//
// 🚨 reset と使い分けること。「キャッシュした内容はもう当てにならないが、走行中の取得は
// 走行中のまま」という状況で使う (外部編集で作業ツリーが変わった等)。ここで札まで降ろすと、
// 直後に張り直した取得予約が「誰も取っていない」と判断して同じキーを二重に取りに行く
// (statusView の外部編集検知で実際に起きた)。
func (c *lineCache) clearEntries() {
	c.entries = map[string][]string{}
	c.order = nil
}

// cancel は 1 件の取得中の札だけを降ろす (結果を捨てるとき用)。
// 🚨 降ろさないと fetching() が true のまま残り、そのキーは begin() に永久に弾かれる。
func (c *lineCache) cancel(key string) { delete(c.busy, key) }

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
