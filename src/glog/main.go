// glog — GitHub Actions / Checks の結果を非同期で添える git log ラッパー。
// 設計の一次情報: dotfiles の issues/done/015-feat-git-log-gha-status-wrapper.md
// (対話ブラウズは元 issue の非目標だったが 2026-07-16 のユーザー指示で解禁)
package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	opts, err := ParseArgs(argv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 2
	}
	if opts.Help {
		fmt.Println(Usage())
		return 0
	}

	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	colored := isTTY && os.Getenv("NO_COLOR") == ""

	if opts.Mode == ModeCached {
		return runCached(opts, colored)
	}
	return runLog(opts, colored, isTTY)
}

// exitGitError は git 自体の失敗。stderr と終了コードをそのまま返す (issue のエラー方針)。
func exitGitError(err error) int {
	var gitErr *GitExitError
	if errors.As(err, &gitErr) {
		fmt.Fprint(os.Stderr, gitErr.Stderr)
		return gitErr.Code
	}
	fmt.Fprintln(os.Stderr, "glog:", err)
	return 1
}

func runLog(opts *Options, colored, isTTY bool) int {
	// 起動の律速は git fork の直列連鎖 (実測 1 fork ≈ 6ms、直列だと最大 9 本 ≈ 55ms)。
	// 互いに独立な「repo 解決 + キャッシュ」「decoration 色解決」を git log と並列に
	// 走らせ、最長チェーン (fork 2 本 ≈ 12ms) まで縮める。goroutine は read-only の
	// git 実行のみで、エラーで先に return してもプロセス終了で無害に片付く
	planCh := make(chan repoPlan, 1)
	go func() { planCh <- gatherRepoPlan(opts) }()
	var decorCh chan DecorColors
	if colored {
		// decoration の配色は git log を尊重する (color.decorate.* + git 既定色)
		decorCh = make(chan DecorColors, 1)
		go func() { decorCh <- LoadDecorColors() }()
	}

	commits, err := LoadCommits(opts, colored)
	if err != nil {
		return exitGitError(err)
	}
	if len(commits) == 0 {
		return 0
	}
	shas := make([]string, len(commits))
	for i, c := range commits {
		shas[i] = c.SHA
	}

	statuses, toFetch, repo, hasRepo, cachePath := mergePlan(<-planCh, shas)
	renderOpts := RenderOpts{Oneline: opts.Oneline, Colored: colored}
	var decor *DecorColors
	if colored {
		dc := <-decorCh
		decor = &dc
		renderOpts.Decor = decor
	}
	width, height := terminalSize()

	// 対話ブラウズ (less 風) は TTY のみ。パイプ・リダイレクトへは ANSI カーソル制御を
	// 出さず、取得完了後に静的な最終結果を 1 回だけ出力する (issue の設計)。
	// less -F 相当のショートカット: 取得不要 (全部キャッシュ) かつ 1 画面に収まるなら
	// ブラウズを開かずそのまま出力して終了する。
	interactive := isTTY && !opts.NoPager
	if interactive && len(toFetch) == 0 && fitsTerminal(len(RenderLines(commits, statuses, renderOpts)), height) {
		interactive = false
	}

	if !interactive {
		return showStatic(commits, statuses, toFetch, repo, hasRepo, cachePath, opts, renderOpts)
	}

	browse := newBrowseModel(commits, statuses, toFetch, repo, hasRepo, opts, colored, width, height)
	// quit 経路 (q/Ctrl-C) 以外 — RunBrowse のエラーや fetch 無しでの即終了 — でも
	// context の timer を解放する (cancel は冪等)
	defer browse.cancel()
	browse.decor = decor
	model, err := RunBrowse(browse)
	if err != nil {
		// TUI 基盤の失敗は静的経路で救済する
		return showStatic(commits, statuses, toFetch, repo, hasRepo, cachePath, opts, renderOpts)
	}
	// Alt Screen なので終了時に表示は消える (git log の pager と同じ)。
	// 静的な最終出力はしない (ユーザー要望 2026-07-17。残したいものは
	// y / o / --no-pager で)。取得結果のキャッシュ保存と警告だけ行う
	saveFetched(cachePath, model.fetched, opts)
	if model.ghErr != nil {
		fmt.Fprintln(os.Stderr, model.ghErr.Warning())
	}
	if model.switchToChanges {
		// exit 20 は呼び出し元の tmux popup に changes 画面への切替を伝える契約。
		return 20
	}
	return 0
}

func runCached(opts *Options, colored bool) int {
	// runLog と同じく、独立な repo 解決 + キャッシュ読みを HEAD/diff の取得と並列に走らせる
	planCh := make(chan repoPlan, 1)
	go func() { planCh <- gatherRepoPlan(opts) }()
	head, err := LoadHeadCommit()
	if err != nil {
		return exitGitError(err)
	}
	diff, err := LoadStagedDiff(opts, colored)
	if err != nil {
		return exitGitError(err)
	}
	statuses, toFetch, repo, hasRepo, cachePath := mergePlan(<-planCh, []string{head.SHA})
	_, ghErr := fetchStatic(statuses, toFetch, repo, hasRepo, cachePath, opts)
	fmt.Println(RenderCached(head, stateFor(statuses, head.SHA), diff, colored, ""))
	if ghErr != nil {
		fmt.Fprintln(os.Stderr, ghErr.Warning())
	}
	return 0
}

// repoPlan は表示前に必要な repo まわりの情報の束 (gatherRepoPlan が並列に集める)。
type repoPlan struct {
	repo      Repo
	hasRepo   bool
	cachePath string
	unpushed  map[string]bool
	cached    map[string]CIState
}

// gatherRepoPlan は repo 解決・未 push 判定・キャッシュ読みを並列に行う。
// ResolveRepo (fork 最大 2 本) と UnpushedSHAs (fork 1 本) は互いに独立で、直列に
// つなぐと fork 遅延が足し算になる (起動ボトルネックの実測より)。
// UnpushedSHAs は hasRepo の結果を待たず投機実行する (remote 無し repo では 1 fork
// 無駄になるが、--max-count 上限があるため軽い)。
func gatherRepoPlan(opts *Options) repoPlan {
	var plan repoPlan
	var wg sync.WaitGroup
	wg.Go(func() {
		plan.unpushed = UnpushedSHAs(opts.Revs, opts.MaxCount)
	})
	plan.repo, plan.hasRepo = ResolveRepo()
	if plan.hasRepo && !opts.NoCache {
		if p, err := CachePath(plan.repo); err == nil {
			plan.cachePath = p
			if !opts.Refresh {
				plan.cached = LoadCache(plan.cachePath, time.Now())
			}
		}
	}
	wg.Wait()
	return plan
}

// planStatuses は repo 解決とキャッシュ反映を行い、表示初期状態と未取得 SHA を返す
// (gather と merge の合成。runLog は gather を git log と並列化するため別々に呼ぶ)。
func planStatuses(opts *Options, shas []string) (statuses map[string]CIState, toFetch []string, repo Repo, hasRepo bool, cachePath string) {
	return mergePlan(gatherRepoPlan(opts), shas)
}

// mergePlan は集めた repoPlan を表示対象 shas へ適用する純関数部分。
func mergePlan(plan repoPlan, shas []string) (statuses map[string]CIState, toFetch []string, repo Repo, hasRepo bool, cachePath string) {
	statuses = map[string]CIState{}
	if !plan.hasRepo {
		// remote なし / GitHub 以外 → Check は存在しないので全件 `–` (issue のエラー方針)
		for _, sha := range shas {
			statuses[sha] = StateNone
		}
		return statuses, nil, plan.repo, false, ""
	}
	// 未 push の SHA は GitHub 上に存在せず、問い合わせても必ず「無い」と返るため
	// ローカル判定で確定させて取得対象から外す (キャッシュより優先。push 直後に
	// 古い none キャッシュが当たって「Check なし」に見える混同も防ぐ)
	for _, sha := range shas {
		if plan.unpushed[sha] {
			statuses[sha] = StateUnpushed
		}
	}
	for _, sha := range shas {
		if _, ok := statuses[sha]; ok {
			continue
		}
		if state, ok := plan.cached[sha]; ok {
			statuses[sha] = state
		}
	}
	for _, sha := range shas {
		if _, ok := statuses[sha]; !ok {
			toFetch = append(toFetch, sha)
		}
	}
	return statuses, toFetch, plan.repo, true, plan.cachePath
}

// showStatic は静的経路の共通処理: 同期取得 → 最終出力 → 警告 → exit 0。
// 非 TTY / --no-pager / less -F 相当のショートカット / TUI 基盤失敗の救済、の
// すべてがここへ落ちる (出力契約の変更はこの 1 箇所で済む)。
func showStatic(commits []Commit, statuses map[string]CIState, toFetch []string, repo Repo, hasRepo bool, cachePath string, opts *Options, renderOpts RenderOpts) int {
	prs, ghErr := fetchStatic(statuses, toFetch, repo, hasRepo, cachePath, opts)
	renderOpts.PRs = prs
	fmt.Println(RenderStatic(commits, statuses, renderOpts))
	if ghErr != nil {
		fmt.Fprintln(os.Stderr, ghErr.Warning())
	}
	return 0
}

// fetchStatic は同期で CI 状態を取得して statuses へマージし、キャッシュへ保存する。
// 取得できなかった SHA は unknown に落とす (「Check なし」と「取得失敗」を混同しない:
// issue の懸念点)。GitHub 側の失敗は警告として返し、コマンドの成否には影響させない。
// 返り値の prs はコミット行の PR バッジ用。
func fetchStatic(statuses map[string]CIState, toFetch []string, repo Repo, hasRepo bool, cachePath string, opts *Options) (map[string]*PRRef, *GHError) {
	if !hasRepo || len(toFetch) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	batch, ghErr := FetchCIStatuses(ctx, ExecRunner, repo, toFetch)
	fetched := fillUnknownFetched(batch.Statuses, toFetch)
	maps.Copy(statuses, fetched)
	saveFetched(cachePath, fetched, opts)
	return batch.PRs, ghErr
}

// saveFetched は取得結果をキャッシュへ書く。best-effort で失敗してもコマンドの成否に
// 影響させない。
func saveFetched(cachePath string, fetched map[string]CIState, opts *Options) {
	if opts.NoCache || cachePath == "" || len(fetched) == 0 {
		return
	}
	_ = SaveCache(cachePath, fetched, time.Now())
}

func terminalSize() (width, height int) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0, 0
	}
	return w, h
}

// fitsTerminal は初期描画が端末の高さに収まるかを判定する (less -F 相当の判定)。
func fitsTerminal(lineCount, height int) bool {
	if height <= 0 {
		return false
	}
	return lineCount+1 <= height
}
