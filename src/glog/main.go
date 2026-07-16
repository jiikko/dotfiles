// glog — GitHub Actions / Checks の結果を非同期で添える git log ラッパー。
// 設計の一次情報: dotfiles の issues/done/git-log-gha-status-wrapper.md
// (対話ブラウズは元 issue の非目標だったが 2026-07-16 のユーザー指示で解禁)
package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
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

	statuses, toFetch, repo, hasRepo, cachePath := planStatuses(opts, shas)
	renderOpts := RenderOpts{Oneline: opts.Oneline, Colored: colored}
	var decor *DecorColors
	if colored {
		// decoration の配色は git log を尊重する (color.decorate.* + git 既定色)
		dc := LoadDecorColors()
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
		ghErr := fetchStatic(statuses, toFetch, repo, hasRepo, cachePath, opts)
		fmt.Println(RenderStatic(commits, statuses, renderOpts))
		if ghErr != nil {
			fmt.Fprintln(os.Stderr, ghErr.Warning())
		}
		return 0
	}

	browse := newBrowseModel(commits, statuses, toFetch, repo, hasRepo, opts, colored, width, height)
	browse.decor = decor
	model, err := RunBrowse(browse)
	if err != nil {
		// TUI 基盤の失敗は静的経路で救済する
		ghErr := fetchStatic(statuses, toFetch, repo, hasRepo, cachePath, opts)
		fmt.Println(RenderStatic(commits, statuses, renderOpts))
		if ghErr != nil {
			fmt.Fprintln(os.Stderr, ghErr.Warning())
		}
		return 0
	}
	// 終了時に TUI 領域は消えているので、最終結果を静的出力してターミナル履歴に残す
	// (issue の完了条件)。job パネルを開いたまま終了した場合は、その内容も
	// インライン展開の形で残す
	if model.panelSHA != "" {
		renderOpts.Expanded = map[string]bool{model.panelSHA: true}
		renderOpts.Details = model.details
	}
	fmt.Println(RenderStatic(commits, model.statuses, renderOpts))
	saveFetched(cachePath, model.fetched, opts)
	if model.ghErr != nil {
		fmt.Fprintln(os.Stderr, model.ghErr.Warning())
	}
	return 0
}

func runCached(opts *Options, colored bool) int {
	head, err := LoadHeadCommit()
	if err != nil {
		return exitGitError(err)
	}
	diff, err := LoadStagedDiff(opts, colored)
	if err != nil {
		return exitGitError(err)
	}
	statuses, toFetch, repo, hasRepo, cachePath := planStatuses(opts, []string{head.SHA})
	ghErr := fetchStatic(statuses, toFetch, repo, hasRepo, cachePath, opts)
	fmt.Println(RenderCached(head, stateFor(statuses, head.SHA), diff, colored, ""))
	if ghErr != nil {
		fmt.Fprintln(os.Stderr, ghErr.Warning())
	}
	return 0
}

// planStatuses は repo 解決とキャッシュ反映を行い、表示初期状態と未取得 SHA を返す。
func planStatuses(opts *Options, shas []string) (statuses map[string]CIState, toFetch []string, repo Repo, hasRepo bool, cachePath string) {
	statuses = map[string]CIState{}
	repo, hasRepo = ResolveRepo()
	if !hasRepo {
		// remote なし / GitHub 以外 → Check は存在しないので全件 `–` (issue のエラー方針)
		for _, sha := range shas {
			statuses[sha] = StateNone
		}
		return statuses, nil, repo, false, ""
	}
	if !opts.NoCache {
		if p, err := CachePath(repo); err == nil {
			cachePath = p
		}
	}
	if cachePath != "" && !opts.Refresh {
		cached := LoadCache(cachePath, time.Now())
		for _, sha := range shas {
			if state, ok := cached[sha]; ok {
				statuses[sha] = state
			}
		}
	}
	for _, sha := range shas {
		if _, ok := statuses[sha]; !ok {
			toFetch = append(toFetch, sha)
		}
	}
	return statuses, toFetch, repo, true, cachePath
}

// fetchStatic は同期で CI 状態を取得して statuses へマージし、キャッシュへ保存する。
// 取得できなかった SHA は unknown に落とす (「Check なし」と「取得失敗」を混同しない:
// issue の懸念点)。GitHub 側の失敗は警告として返し、コマンドの成否には影響させない。
func fetchStatic(statuses map[string]CIState, toFetch []string, repo Repo, hasRepo bool, cachePath string, opts *Options) *GHError {
	if !hasRepo || len(toFetch) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	fetched, _, ghErr := FetchCIStatuses(ctx, ExecRunner, repo, toFetch)
	maps.Copy(statuses, fetched)
	for _, sha := range toFetch {
		if _, ok := statuses[sha]; !ok {
			statuses[sha] = StateUnknown
		}
	}
	saveFetched(cachePath, fetched, opts)
	return ghErr
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
