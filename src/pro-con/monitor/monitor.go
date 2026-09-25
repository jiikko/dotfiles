// Package monitor は見張り (pro-con monitor。issue 475)。dispatcher とは別のプロセスで、決まった手順で判定できることを見る。
//
//   - 取り込みの衝突: PG の commit 済みの分を git merge-tree で master と、PG どうしで突き合わせる
//   - テストの順番の長さ: テストの係に頼まれてまだ始まっていない本数と、先頭の待ち時間
//
// 見張りは読むだけ (cards.json・各 PG の worktree の git)。見つけたことは受付の箱に置き (依頼の種類 store.KindMonitor)、
// dispatcher が出来事の記録へ書く (書き手は dispatcher 1 つ = 426 の決定 1)。
// 知らせるのは「現れた」「消えた」(と、衝突したファイルが変わった) ときだけ。前に知らせた結果はメモリだけに持つので、
// 見張りを起こし直すと、残っている衝突をもう 1 度だけ知らせる。
// 🚨 git fetch はしない (見張りは ref を動かさない)。origin/master は、取り込みの係が worktree から push するたびに手元で更新される。
package monitor

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"termsafe"

	"pro-con/card"
	"pro-con/dispatcher"
	"pro-con/store"
)

// 「テストの順番が長い」の閾値 (426 のやり方どおり、既定値で始めて動かしながら直す)。
const (
	longQueueLen  = 3                // 待っている本数 (実行中の 1 本は数えない)
	longQueueWait = 30 * time.Minute // 先頭の待ち時間
)

// maxFiles は出来事に並べる衝突したファイルの数 (残りは「ほか N 件」)。
const maxFiles = 5

// Monitor は見張り 1 回ぶんの判定と、前に知らせた結果を持つ。
type Monitor struct {
	Dir    string            // 状態の置き場 (cards.json を読む)
	Repos  map[string]string // repo の名前 → パス (設定の repo)
	Now    func() time.Time
	Git    Git                                   // nil なら ExecGit
	Submit func(r store.Request) (string, error) // nil なら store.Submit(Dir, r)
	Log    func(text string)                     // 1 つの組を見られなかった等 (同じ組では 1 度だけ)。nil なら出さない

	told  map[string]finding    // 知らせた物 (鍵 → 知らせた中身)
	cache map[string]mergeCheck // merge-tree の結果 (repo と 2 つの commit の組 → 結果)
}

// mergeCheck は merge-tree 1 回の結果。err は git が失敗した (unborn・object が無い等。同じ組では git を呼び直さない)。
type mergeCheck struct {
	conflict bool
	files    []string
	err      error
}

// finding は見つけたこと 1 つ。sig が変われば知らせ直す (衝突したファイル)。note は現れたときの文、gone は消えたときの文。
type finding struct {
	card, sig, note, gone string
}

// Check は 1 回見て、変わったことを受付の箱に置く。置いた数を返す。
// 1 つの repo の git が失敗しても他は見る (失敗は errs にまとめて返す。その repo の前の結果は消えたとみなさない)。
func (m *Monitor) Check(ctx context.Context) (int, error) {
	st, err := store.Load(m.Dir)
	if err != nil {
		return 0, err
	}
	now := m.now()
	found := map[string]finding{}
	var errs []string
	keep := map[string]bool{} // 見られなかった repo の鍵の頭 (前の結果を残す)
	for k, f := range queueFindings(st.Cards, now) {
		found[k] = f
	}
	for _, repo := range m.repoNames(st.Cards) {
		fs, err := m.conflicts(ctx, repo, st.Cards)
		if err != nil {
			errs = append(errs, repo+": "+err.Error())
			keep[conflictPrefix(repo)] = true
			continue
		}
		for k, f := range fs {
			found[k] = f
		}
	}
	if m.told == nil {
		m.told = map[string]finding{}
	}
	// 置けた 1 件ごとに told を直す (途中で置けなくなったら、残りは知らせていない = 次の Check で置き直す。置けた分を二重に置かない)
	n := 0
	put := func(cardID, note string) bool {
		if _, err := m.submit(store.Request{Kind: store.KindMonitor, CardID: cardID, Note: note, At: now}); err != nil {
			errs = append(errs, "受付の箱に置けない: "+err.Error())
			return false
		}
		n++
		return true
	}
	for _, k := range sortedKeys(found) {
		f := found[k]
		if t, ok := m.told[k]; ok && t.sig == f.sig {
			m.told[k] = f // 文だけ変わった (待ち時間) は知らせ直さない
			continue
		}
		if !put(f.card, f.note) {
			return n, joinErrs(errs)
		}
		m.told[k] = f
	}
	for _, k := range sortedKeys(m.told) {
		if _, ok := found[k]; ok || kept(k, keep) {
			continue
		}
		if !put(m.told[k].card, m.told[k].gone) {
			return n, joinErrs(errs)
		}
		delete(m.told, k)
	}
	return n, joinErrs(errs)
}

// queueFindings はテストの順番の長さ。dispatcher (テストの係) が列に並べて順番を書いたカード (Wait.Resource が store.RunResource) を数える
// (列の組み方を真似ない。runner.go の列は削除中のカードを外す・pro-con の外が lock を持っている 1 本は別の名前で待たせる)。
func queueFindings(cards []card.Card, now time.Time) map[string]finding {
	var waiting []card.Card
	for _, c := range cards {
		if !c.Archived && c.State == card.Running && c.Wait.Kind == card.WaitResource && c.Wait.Resource == store.RunResource {
			waiting = append(waiting, c)
		}
	}
	if len(waiting) == 0 {
		return nil
	}
	sort.SliceStable(waiting, func(i, j int) bool { return waiting[i].Wait.Position < waiting[j].Wait.Position })
	head := waiting[0]
	wait := now.Sub(head.RunAt)
	if len(waiting) < longQueueLen && (head.RunAt.IsZero() || wait < longQueueWait) {
		return nil
	}
	note := fmt.Sprintf("テストの順番が長い: %d 本が待っている", len(waiting))
	if !head.RunAt.IsZero() {
		note += fmt.Sprintf("。先頭の %s は頼んでから %s", head.ID, wait.Round(time.Minute))
	}
	return map[string]finding{"queue": {
		note: note + " (重いコマンドを頼む PG が多いか、実行中の 1 本が長い。pro-con ps で実行中の 1 本を見る)",
		gone: "テストの順番の長さが戻った",
	}}
}

// target は衝突を見るカード 1 枚 (PG の worktree の HEAD)。
type target struct {
	id, head string
}

// conflicts は repo の、衝突の見つけたこと (master と / PG どうし)。
func (m *Monitor) conflicts(ctx context.Context, repo string, cards []card.Card) (map[string]finding, error) {
	path := m.Repos[repo]
	base, baseName, err := m.git().Base(ctx, path)
	if err != nil {
		return nil, err
	}
	var ts []target
	for _, c := range cards {
		if c.Repo != repo || !watched(c) {
			continue
		}
		wt := dispatcher.WorktreePath(path, c)
		if _, err := os.Stat(wt); err != nil { // まだ起動していない / 片付けた
			continue
		}
		head, err := m.git().Head(ctx, wt)
		if err != nil {
			continue // worktree が壊れている・消える途中。PG の側の問題なので見張りの失敗にしない
		}
		merged, err := m.git().IsAncestor(ctx, path, head, base)
		if err != nil {
			return nil, err
		}
		if !merged { // まだ commit が無い / 取り込み済みなら見ない
			ts = append(ts, target{c.ID, head})
		}
	}
	out := map[string]finding{}
	for _, t := range ts {
		r := m.merge(ctx, path, base, t.head, t.id+" と "+baseName)
		if r.conflict {
			files := fileList(r.files)
			out[conflictPrefix(repo)+t.id] = finding{card: t.id, sig: files,
				note: fmt.Sprintf("%s の commit 済みの分が %s と衝突する (%s)。取り込む前に、PG が %s を取り込んで直す", t.id, baseName, files, baseName),
				gone: fmt.Sprintf("%s と %s の衝突は見えなくなった (直したか、カードが見張りの対象を離れた)", t.id, baseName)}
		}
	}
	for i, a := range ts {
		for _, b := range ts[i+1:] {
			if _, ok := out[conflictPrefix(repo)+a.id]; ok {
				continue // master と衝突するカードは、master を取り込んで直すと組の結果も変わる (同じ衝突を二重に知らせない)
			}
			if _, ok := out[conflictPrefix(repo)+b.id]; ok {
				continue
			}
			r := m.merge(ctx, path, a.head, b.head, a.id+" と "+b.id)
			if r.conflict {
				files := fileList(r.files)
				out[conflictPrefix(repo)+a.id+"+"+b.id] = finding{card: a.id, sig: files,
					note: fmt.Sprintf("%s と %s の commit 済みの分どうしが衝突する (%s)。後に取り込む方が、先の取り込みの後に直す", a.id, b.id, files),
					gone: fmt.Sprintf("%s と %s の衝突は見えなくなった (直したか、カードが見張りの対象を離れた)", a.id, b.id)}
			}
		}
	}
	return out, nil
}

// watched は衝突を見るカードか (PG が commit しうる列。片付けたカード・完了・依頼は見ない)。
func watched(c card.Card) bool {
	if c.Archived {
		return false
	}
	switch c.State {
	case card.Planned, card.Running, card.Waiting, card.Review:
		return true
	case card.Requested, card.Done:
		return false
	}
	return false
}

// merge は merge-tree の結果を、同じ組なら覚えた値で返す (commit の id で引くので、HEAD が動けば引き直す)。
// git が失敗した組は衝突なしとして飛ばし、Log に 1 度だけ出す (repo の他の組は見続ける。who は出す文の主語)。
func (m *Monitor) merge(ctx context.Context, repo, a, b, who string) mergeCheck {
	key := repo + "\x00" + a + "\x00" + b
	if r, ok := m.cache[key]; ok {
		return r
	}
	conflict, files, err := m.git().MergeTree(ctx, repo, a, b)
	if err != nil && ctx.Err() != nil {
		return mergeCheck{} // 止める途中。覚えない (次に起きた見張りが見直す)
	}
	if m.cache == nil || len(m.cache) > 4096 { // 古い組は使わないので、溜まったら捨てて引き直す
		m.cache = map[string]mergeCheck{}
	}
	r := mergeCheck{conflict: conflict, files: files, err: err}
	m.cache[key] = r
	if err != nil && m.Log != nil {
		m.Log(fmt.Sprintf("%s の衝突を見られない (この組は HEAD が動くまで見ない): %v", who, err))
	}
	return r
}

// repoNames は、衝突を見るカードのある、設定にある repo (名前の順)。
func (m *Monitor) repoNames(cards []card.Card) []string {
	var out []string
	for _, c := range cards {
		if _, ok := m.Repos[c.Repo]; ok && watched(c) && !slices.Contains(out, c.Repo) {
			out = append(out, c.Repo)
		}
	}
	sort.Strings(out)
	return out
}

func conflictPrefix(repo string) string { return "conflict:" + repo + ":" }

// kept は鍵が、見られなかった repo のものか。
func kept(key string, keep map[string]bool) bool {
	for p := range keep {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// fileList は出来事に書くファイルの並び (端末に出すので制御文字を落とす)。
func fileList(files []string) string {
	var out []string
	for _, f := range files {
		out = append(out, termsafe.PlainLine(f))
	}
	if len(out) > maxFiles {
		return strings.Join(out[:maxFiles], ", ") + fmt.Sprintf(" ほか %d 件", len(out)-maxFiles)
	}
	return strings.Join(out, ", ")
}

func sortedKeys(m map[string]finding) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func joinErrs(errs []string) error {
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(errs, " / "))
}

func (m *Monitor) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Monitor) git() Git {
	if m.Git != nil {
		return m.Git
	}
	return ExecGit{}
}

func (m *Monitor) submit(r store.Request) (string, error) {
	if m.Submit != nil {
		return m.Submit(r)
	}
	return store.Submit(m.Dir, r)
}
