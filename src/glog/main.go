// glog — GitHub Actions / Checks の結果を非同期で添える git log ラッパー。
// 設計の一次情報: dotfiles の issues/git-log-gha-status-wrapper.md
package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"
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
		return runCached(opts, colored, isTTY)
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
	render := func(statuses map[string]CIState, width int, spinner string) string {
		return RenderCommits(commits, statuses, width, colored, spinner)
	}
	return resolveAndShow(opts, shas, render, isTTY)
}

func runCached(opts *Options, colored, isTTY bool) int {
	head, err := LoadHeadCommit()
	if err != nil {
		return exitGitError(err)
	}
	diff, err := LoadStagedDiff(opts, colored)
	if err != nil {
		return exitGitError(err)
	}
	render := func(statuses map[string]CIState, width int, spinner string) string {
		return RenderCached(head, stateFor(statuses, head.SHA), diff, colored, spinner)
	}
	return resolveAndShow(opts, []string{head.SHA}, render, isTTY)
}

// resolveAndShow は repo 解決 → キャッシュ反映 → (TUI or 静的) 取得 → 最終表示までの共通経路。
// GitHub 側の失敗は警告 1 行 (stderr) に落とし、Git 履歴の表示が成立していれば exit 0 を返す。
func resolveAndShow(opts *Options, shas []string, render renderFunc, isTTY bool) int {
	statuses := map[string]CIState{}
	width, height := terminalSize()

	repo, hasRepo := ResolveRepo()
	if !hasRepo {
		// remote なし / GitHub 以外 → Check は存在しないので全件 `–` (issue のエラー方針)
		for _, sha := range shas {
			statuses[sha] = StateNone
		}
		fmt.Println(render(statuses, width, ""))
		return 0
	}

	cachePath := ""
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
	var toFetch []string
	for _, sha := range shas {
		if _, ok := statuses[sha]; !ok {
			toFetch = append(toFetch, sha)
		}
	}
	if len(toFetch) == 0 {
		fmt.Println(render(statuses, width, ""))
		return 0
	}

	var ghErr *GHError
	var fetched map[string]CIState
	// 動的描画は stdout が TTY のときだけ。パイプ・リダイレクトへは ANSI カーソル制御を
	// 出さず、取得完了後に静的な最終結果を 1 回だけ出力する (issue の設計)。
	// 初期描画が端末に収まらない場合もインライン再描画が乱れるため静的へフォールバック。
	if isTTY && fitsTerminal(render(statuses, width, " "), height) {
		model, err := RunTUI(newTUIModel(render, statuses, toFetch, repo, width))
		if err != nil {
			// TUI 基盤の失敗は静的経路で救済する
			fetched, ghErr = fetchStatic(statuses, toFetch, repo)
			fmt.Println(render(statuses, width, ""))
		} else {
			fetched, ghErr = model.fetched, model.ghErr
			// 最終フレームは TUI が残しているため再出力しない
		}
	} else {
		fetched, ghErr = fetchStatic(statuses, toFetch, repo)
		fmt.Println(render(statuses, width, ""))
	}

	if cachePath != "" && len(fetched) > 0 {
		// キャッシュ保存は best-effort。失敗してもコマンドの成否には影響させない
		_ = SaveCache(cachePath, fetched, time.Now())
	}
	if ghErr != nil {
		fmt.Fprintln(os.Stderr, ghErr.Warning())
	}
	return 0
}

// fetchStatic は同期で CI 状態を取得して statuses へマージする。取得できなかった SHA は
// unknown に落とす (「Check なし」と「取得失敗」を混同しない: issue の懸念点)。
func fetchStatic(statuses map[string]CIState, toFetch []string, repo Repo) (map[string]CIState, *GHError) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	fetched, ghErr := FetchCIStatuses(ctx, ExecRunner, repo, toFetch)
	maps.Copy(statuses, fetched)
	for _, sha := range toFetch {
		if _, ok := statuses[sha]; !ok {
			statuses[sha] = StateUnknown
		}
	}
	return fetched, ghErr
}

func terminalSize() (width, height int) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0, 0
	}
	return w, h
}

// fitsTerminal は初期描画が端末の高さに収まるかを判定する。
// 収まらない場合は動的更新を諦めて静的出力へフォールバックする。
func fitsTerminal(view string, height int) bool {
	if height <= 0 {
		return false
	}
	return strings.Count(view, "\n")+2 <= height
}
