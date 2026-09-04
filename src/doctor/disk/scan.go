package disk

import (
	"doctor/runner"

	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Status は 1 エントリの結果の種類。「候補 0 件」(ok で Items 空) と「走査できず」(failed) を分ける。
type Status string

const (
	StatusOK      Status = "ok"      // 走査できた (Items が空なら候補なし)
	StatusBlocked Status = "blocked" // guard により今は対象外 (理由あり)。合計に足さない
	StatusFailed  Status = "failed"  // 走査 / 判定できなかった (理由あり)。合計に足さない
)

// Result は 1 エントリの結果。
type Result struct {
	Entry    Entry         `json:"entry"`
	Status   Status        `json:"status"`
	Reason   string        `json:"reason,omitempty"` // blocked / failed の理由
	Items    []Item        `json:"items"`
	Size     int64         `json:"size"`
	Failures []string      `json:"failures,omitempty"` // 走査できなかった Item (理由つき)。Size に入っていない
	Contents []string      `json:"contents,omitempty"` // Inspect のとき: 各 Item 直下の名前
	Elapsed  time.Duration `json:"elapsed"`
	// MeasuredAt は実際に走査した時刻。Reused は今回走査せず前回の結果をそのまま返した (Options.Reuse)。
	// 再利用時も Elapsed / MeasuredAt は元の計測のまま (次回「重い」と判定し続けるため)。
	MeasuredAt time.Time `json:"measured_at"`
	Reused     bool      `json:"reused,omitempty"`
	// FromSnapshot は「今回走査しておらず、保存された結果をそのまま復元した」印。**Reused とは別物**:
	// Reused は「重いエントリの計測値だけを前回から引き継いだ (行に『N 分前の計測を再利用』と出る)」で、
	// こちらは「画面ごと snapshot から再現した」。前者を後者にも流用すると、普通の開き直しで
	// 「-1113 分前の計測を再利用」のような嘘の注記が全行に出る (issue 178 の敵対レビューで実測)。
	// ④ (削除) は**どちらの印が立っていても再スキャンを通す** (issues/148 の不変条件)。
	FromSnapshot bool `json:"from_snapshot,omitempty"`
}

// Report は全エントリの結果。
type Report struct {
	Results   []Result  `json:"results"`
	Total     int64     `json:"total"` // 今消せる量 (ok のみ。blocked / failed は足さない)
	ScannedAt time.Time `json:"scannedAt"`
	Partial   bool      `json:"partial"` // ctx が途中で切れた
}

// Options は Scan の入力。テストは Env / Run / Catalog / BootTime を差す。
type Options struct {
	Env         Env
	Run         runner.Runner
	Catalog     []Entry
	BootTime    func() (time.Time, error)
	Concurrency int           // 既定 4 (ディスク I/O が競合する)
	PerEntry    time.Duration // 1 エントリの上限。既定 60 秒
	// OnResult は完了したエントリを順次受ける (UI のインクリメンタル表示用)。nil 可。
	// 🚨 走査 goroutine から並行に呼ばれる (呼び出し側で直列化する。bubbletea なら Msg に載せる)
	OnResult func(Result)
	// Reuse は「このエントリは走査せず、この前回結果を使え」を返す (nil = 走査する)。走査はディスク I/O を
	// 使うので、重いエントリを短い間隔で何度も測り直さないための口。判定 (何を重いとみなし、いつまで使うか)
	// は呼び出し側 (glogx の doctor) が持つ。返した Result は Reused=true を立てて OnResult / Results に載る
	Reuse func(Entry) *Result
}

// Scan は全エントリを走査して Report を返す。削除はしない (その経路はこのパッケージに無い)。
func Scan(ctx context.Context, opt Options) Report {
	if opt.Concurrency <= 0 {
		opt.Concurrency = 4
	}
	if opt.PerEntry <= 0 {
		opt.PerEntry = 60 * time.Second
	}
	if opt.Catalog == nil {
		opt.Catalog = catalog
	}
	if opt.BootTime == nil {
		opt.BootTime = bootTime
	}
	g := &guards{opt: opt}
	results := make([]Result, len(opt.Catalog))
	sem := make(chan struct{}, opt.Concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i, e := range opt.Catalog {
		wg.Add(1)
		go func(i int, e Entry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ectx, cancel := context.WithTimeout(ctx, opt.PerEntry)
			defer cancel()
			start := time.Now()
			var r Result
			if prev := reusable(opt, e); prev != nil {
				r = *prev
				r.Reused = true
			} else {
				r = scanEntry(ectx, opt, g, e)
				r.Elapsed = time.Since(start)
				r.MeasuredAt = start
			}
			mu.Lock()
			results[i] = r
			mu.Unlock()
			if opt.OnResult != nil {
				opt.OnResult(r)
			}
		}(i, e)
	}
	wg.Wait()
	rep := Report{Results: results, ScannedAt: time.Now(), Partial: ctx.Err() != nil, Total: SumDeletable(results)}
	sort.SliceStable(rep.Results, func(a, b int) bool { return rep.Results[a].Size > rep.Results[b].Size })
	return rep
}

// guards は複数エントリで共有する判定材料 (simctl / brew / boottime / Applications) を 1 回だけ取る。
type guards struct {
	opt  Options
	once map[string]*sync.Once
	mu   sync.Mutex

	boot        time.Time
	bootErr     error
	udids       map[string]bool
	udidsErr    error
	apps        map[string]bool
	appsErr     error
	formulae    map[string]bool
	formulaeErr error
	cleanup     []string
	cleanupErr  error
	prefix      string
	prefixErr   error
	modcache    string
	modcacheErr error
}

func (g *guards) do(key string, f func()) {
	g.mu.Lock()
	if g.once == nil {
		g.once = map[string]*sync.Once{}
	}
	o := g.once[key]
	if o == nil {
		o = &sync.Once{}
		g.once[key] = o
	}
	g.mu.Unlock()
	o.Do(f)
}

func failed(e Entry, reason string) Result {
	return Result{Entry: e, Status: StatusFailed, Reason: reason}
}

func scanEntry(ctx context.Context, opt Options, g *guards, e Entry) Result {
	switch e.Guard {
	case GuardSimRuntime:
		return scanSimRuntimes(ctx, opt, e)
	case GuardBrewCleanup:
		g.do("cleanup", func() { g.cleanup, g.cleanupErr = brewCleanupTargets(ctx, opt.Run) })
		if g.cleanupErr != nil {
			return failed(e, "brew cleanup --dry-run を実行できず: "+g.cleanupErr.Error())
		}
		return sizePaths(ctx, opt, e, g.cleanup)
	case GuardProcessAbsent:
		for _, name := range e.Processes {
			running, err := processRunning(ctx, opt.Run, name)
			if err != nil {
				return failed(e, "プロセスの有無を判定できず: "+err.Error())
			}
			if running {
				return Result{Entry: e, Status: StatusBlocked, Reason: name + " 起動中のため対象外"}
			}
		}
	// 🚨 残りの Guard は**パス展開の後**の switch (下の「guard による Item の絞り込み」) が受ける。
	// 全 Guard をどちらか一方で必ず受けるのが不変条件。default を書かず全 case を並べることで
	// exhaustive (.golangci.yml) がそれを強制する: 新しい Guard をどちらにも書き忘れると
	// guard が 1 つも適用されないまま候補になる (fail-open)。
	case GuardNone, GuardBoottime, GuardSimDevice, GuardOrphanApp, GuardBrewOrphan, GuardVMRoot,
		GuardGoModcacheCurrent, GuardGoModcacheOld, GuardChromiumCache:
	}
	// $BREW_PREFIX を使うエントリは brew --prefix を実測してから展開する (直書きしない: issue 176)。
	// 取れなければ fail-closed (候補 0 件に畳まない)。
	env := opt.Env
	for _, tmpl := range e.Paths {
		if !strings.Contains(tmpl, "$BREW_PREFIX") {
			continue
		}
		g.do("prefix", func() { g.prefix, g.prefixErr = brewPrefix(ctx, opt.Run) })
		if g.prefixErr != nil {
			return failed(e, "brew --prefix を取得できず (候補にしない): "+g.prefixErr.Error())
		}
		env.BrewPrefix = g.prefix
		break
	}
	var paths []string
	for _, tmpl := range e.Paths {
		ps, err := expand(env, tmpl)
		if err != nil {
			return failed(e, err.Error())
		}
		paths = append(paths, ps...)
	}
	// guard による Item の絞り込み
	switch e.Guard {
	case GuardBoottime:
		g.do("boot", func() { g.boot, g.bootErr = opt.BootTime() })
		if g.bootErr != nil {
			return failed(e, "起動時刻を取得できず (候補にしない): "+g.bootErr.Error())
		}
		paths = filterPaths(paths, func(p string) bool {
			fi, err := os.Lstat(p)
			return err == nil && fi.ModTime().Before(g.boot)
		})
	case GuardSimDevice:
		g.do("udids", func() { g.udids, g.udidsErr = simDeviceUDIDs(ctx, opt.Run, opt.Env) })
		if g.udidsErr != nil {
			return failed(e, "simctl で現存デバイスを取れず (孤児判定をしない): "+g.udidsErr.Error())
		}
		paths = filterPaths(paths, func(p string) bool {
			m := simDeviceDirRe.FindStringSubmatch(filepath.Base(p))
			return m != nil && !g.udids[strings.ToUpper(m[1])]
		})
	case GuardOrphanApp:
		g.do("apps", func() { g.apps, g.appsErr = installedBundleIDs(opt.Env) })
		if g.appsErr != nil {
			return failed(e, "/Applications を走査できず (孤児判定をしない): "+g.appsErr.Error())
		}
		paths = filterPaths(paths, func(p string) bool {
			id := filepath.Base(p)
			// 名前から bundle id を決められないコンテナ (UUID 名) は判定できないので候補にしない。
			// 突合は「ディレクトリ名 = bundle id」を前提にしており、UUID 名は構造的に素通りする
			// (issue 167 (a))
			if containerIsUndiagnosable(id) {
				return false
			}
			for _, pre := range containerExcludePrefixes {
				if strings.HasPrefix(id, pre) {
					return false
				}
			}
			return !containerOwnedByInstalled(id, g.apps)
		})
	case GuardBrewOrphan:
		g.do("formulae", func() { g.formulae, g.formulaeErr = brewFormulae(ctx, opt.Run) })
		if g.formulaeErr != nil {
			return failed(e, "brew list を実行できず (孤児判定をしない): "+g.formulaeErr.Error())
		}
		paths = filterPaths(paths, func(p string) bool {
			name := filepath.Base(p)
			if fi, err := os.Lstat(p); err != nil || !fi.IsDir() || strings.Contains(name, ".") || brewSharedVarDirs[name] {
				return false
			}
			return !g.formulae[name]
		})
	case GuardGoModcacheCurrent, GuardGoModcacheOld:
		// 🚨 解決できないなら**候補 0 件ではなく診断できず**へ倒す (GuardVMRoot と同じ理由)。
		// 「解決できない = 現行でない = 古い世代」と読むと、現役のキャッシュが候補に出る
		g.do("modcache", func() { g.modcache, g.modcacheErr = goModcache(ctx, opt.Run) })
		if g.modcacheErr != nil {
			return failed(e, "go env GOMODCACHE を解決できず (世代を分けられない): "+g.modcacheErr.Error())
		}
		want, err := canonicalPath(opt.Env, g.modcache)
		if err != nil {
			return failed(e, "GOMODCACHE を正規化できず (世代を分けられない): "+err.Error())
		}
		kept := paths[:0]
		for _, p := range paths {
			got, err := canonicalPath(opt.Env, p)
			if err != nil {
				return failed(e, "対象パスを正規化できず (世代を分けられない): "+err.Error())
			}
			if (got == want) == (e.Guard == GuardGoModcacheCurrent) {
				kept = append(kept, p)
			}
		}
		paths = kept
	case GuardVMRoot:
		// 🚨 比較に使う値が解決できないなら**候補 0 件ではなく診断できず**へ倒す。
		// 「解決できない = 一致しない = 孤児」と読むと、HOME が空の環境で現役の root が
		// 全部削除候補に出る (ユーザー要求 2026-09-03)
		kept := paths[:0]
		for _, p := range paths {
			tool := strings.TrimPrefix(filepath.Base(p), ".")
			got, err := canonicalPath(opt.Env, p)
			if err != nil {
				return failed(e, "対象パスを正規化できず (孤児判定をしない): "+err.Error())
			}
			want, err := effectiveVMRoot(opt.Env, tool)
			if err != nil {
				return failed(e, "実効 root を決められず (孤児判定をしない): "+err.Error())
			}
			if got != want { // 両側を同じ正規化で比べる
				kept = append(kept, p)
			}
		}
		paths = kept
	case GuardChromiumCache:
		// 🚨 この guard は **2 つ**を同時に見る。片方だけでは足りない理由は catalog.go に書いた。
		// pgrep が失敗したら fail-closed (「起動中かもしれない」ので候補にしない)。
		kept := paths[:0]
		live := map[string]bool{}  // アプリ 1 つにつき 1 回だけ判定する (lsof は 1 回 0.15 秒)
		known := map[string]bool{} // 判定済みか (live[app] が false のときと区別する)
		for _, p := range paths {
			app, ok := appSupportChild(opt.Env, p)
			if !ok {
				// Application Support 配下でないものがこの guard に来るのは配線の誤り。
				// 候補にせず落とす (推測で消さない)
				continue
			}
			if !isChromiumProfile(app) {
				continue
			}
			if !known[app] {
				l, err := appIsLive(ctx, opt.Run, app)
				if err != nil {
					return failed(e, "起動中かどうかを判定できず (候補にしない): "+err.Error())
				}
				live[app], known[app] = l, true
			}
			if !live[app] {
				kept = append(kept, p)
			}
		}
		// 🚨 **全部が「起動中」で落ちたら blocked にする** (敵対レビュー 2026-09-04)。
		// 黙って候補 0 件にすると `Foldable` が行ごと畳み、2.7GB が「候補なし = きれい」と
		// 同じ見え方になる (Slack と Dropbox を常駐させていれば普通に起きる)。
		// 起動中で触らないことを見せる形は `chrome-tmp` (GuardProcessAbsent) と揃える。
		if len(kept) == 0 && len(live) > 0 {
			names := make([]string, 0, len(live))
			for app, l := range live {
				if l {
					names = append(names, filepath.Base(app))
				}
			}
			if len(names) > 0 {
				sort.Strings(names)
				return Result{Entry: e, Status: StatusBlocked,
					Reason: strings.Join(names, " / ") + " 起動中のため対象外 (終了して r で再スキャン)"}
			}
		}
		paths = kept
	// 絞り込みが無い (GuardNone) か、上の switch で処理済み。理由は上の case 群のコメント
	case GuardNone, GuardSimRuntime, GuardBrewCleanup, GuardProcessAbsent:
	}
	return sizePaths(ctx, opt, e, paths)
}

func filterPaths(paths []string, keep func(string) bool) []string {
	out := paths[:0]
	for _, p := range paths {
		if keep(p) {
			out = append(out, p)
		}
	}
	return out
}

// sizePaths は各パスを検証してから du 相当で測る。検証 / 走査に失敗した Item は Failures に残して
// 他の Item は続ける (TCC で読めない 1 コンテナのために全部を隠さない)。全 Item が失敗、または
// ctx が切れたらエントリを failed にする。Failures の分は Size に入らない (合計に足さない)。
func sizePaths(ctx context.Context, opt Options, e Entry, paths []string) Result {
	r := Result{Entry: e, Status: StatusOK, Items: []Item{}}
	seen := map[[2]uint64]struct{}{}
	for _, p := range paths {
		vp, err := validateTarget(opt.Env, p)
		if err != nil {
			r.Failures = append(r.Failures, "対象パスを拒否: "+err.Error())
			continue
		}
		// 🚨 **除外リストは実行時にも効かせる** (2026-09-04)。以前は `excludedRoots` を
		// テストが**テンプレート文字列**に対してだけ照合していたので、glob を張った瞬間に
		// 素通りする: `~/Library/Application Support/*/Cache` は静的には
		// `~/Library/Application Support/Google` で始まらないが、**展開すると中に入りうる**。
		// 展開後のパスをここで落とす (エントリ全体を止めない: 他のアプリのぶんは正当な候補)。
		if ex, err := excludedRootFor(opt.Env, vp); err != nil {
			r.Failures = append(r.Failures, "除外判定ができず: "+err.Error())
			continue
		} else if ex != "" {
			r.Failures = append(r.Failures, "除外リスト ("+ex+") に踏み込むので対象外: "+vp)
			continue
		}
		it, err := duSize(ctx, vp, seen)
		if err != nil {
			if ctx.Err() != nil {
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return failed(e, "走査が時間内に終わらなかった: "+vp)
				}
				return failed(e, "走査を中断した: "+vp)
			}
			r.Failures = append(r.Failures, "走査できず: "+err.Error())
			continue
		}
		r.Items = append(r.Items, it)
		r.Size += it.Size
		if e.Inspect {
			r.Contents = append(r.Contents, listContents(vp)...)
		}
	}
	if len(r.Items) == 0 && len(r.Failures) > 0 {
		return failed(e, strings.Join(r.Failures, " / "))
	}
	return r
}

// listContents は Inspect 用に Item 直下の名前を挙げる (ユーザーファイルかを人が見るため)。
func listContents(p string) []string {
	entries, err := os.ReadDir(p)
	if err != nil {
		return []string{filepath.Base(p)}
	}
	out := make([]string, 0, len(entries))
	for _, en := range entries {
		out = append(out, filepath.Join(filepath.Base(p), en.Name()))
	}
	return out
}

// scanSimRuntimes は走査せず simctl の申告値を使う (実体は SIP 配下で、rm ではなく simctl で消す)。
func scanSimRuntimes(ctx context.Context, opt Options, e Entry) Result {
	rts, err := simRuntimes(ctx, opt.Run)
	if err != nil {
		return failed(e, "simctl runtime list を実行できず: "+err.Error())
	}
	r := Result{Entry: e, Status: StatusOK, Items: []Item{}}
	for _, rt := range rts {
		r.Items = append(r.Items, Item{Path: rt.Path, Size: rt.SizeBytes, Mtime: rt.LastUsedAt, Ref: rt.Identifier})
		r.Size += rt.SizeBytes
		used := "未使用 (lastUsedAt なし)"
		if !rt.LastUsedAt.IsZero() {
			used = "最終使用 " + rt.LastUsedAt.Format("2006-01-02")
		}
		r.Contents = append(r.Contents, fmt.Sprintf("%s  id=%s  %s", rt.Name, rt.Identifier, used))
	}
	return r
}

// SumDeletable は「今消せる量」。blocked (guard で対象外) と failed (走査できず) は行として出すが
// 合計には足さない (トーストの発火判定も同じ合計を使う。issue 148)。UI の途中集計もこれを使う (計算を 2 つ持たない)。
func SumDeletable(results []Result) int64 {
	var total int64
	for _, r := range results {
		// NotFreeable は「測れるが、その手順で同じ量が返るとは限らない」対象なので足さない
		// (行にはサイズを出す)。この合計は見出しの「解放可能」と起動時トーストの閾値になる
		if r.Status == StatusOK && !r.Entry.NotFreeable {
			total += r.Size
		}
	}
	return total
}

// reusable は Options.Reuse が前回結果を返せばそれ (Entry の ID が一致するものだけ。別エントリの結果を混ぜない)。
func reusable(opt Options, e Entry) *Result {
	if opt.Reuse == nil {
		return nil
	}
	prev := opt.Reuse(e)
	if prev == nil || prev.Entry.ID != e.ID {
		return nil
	}
	return prev
}
