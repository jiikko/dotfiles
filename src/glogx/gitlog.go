package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// コミット境界の識別は人間向け出力の正規表現ではなく制御文字レコードで行う (issue の設計)。
// %x1e = record separator (コミット先頭)、%x1f = field separator。
// --stat / -p の本文は最後の %x1f 以降に続き、次の %x1e までがそのコミットのレコード。
// %B (フルメッセージ) はセパレータを含みうる唯一のフィールドなので最後に置く。コミット
// メッセージに \x1f/\x1e が入っている病的なケースは解析エラーとして明示的に落ちる。
const (
	recordSep    = "\x1e"
	fieldSep     = "\x1f"
	prettyFormat = "--pretty=format:%x1e%H%x1f%h%x1f%s%x1f%an%x1f%ae%x1f%ad%x1f%ar%x1f%D%x1f%B%x1f"
	numFields    = 9
)

// Commit は git log 1 レコード分。
type Commit struct {
	SHA         string
	ShortSHA    string
	Subject     string
	Author      string
	AuthorEmail string
	Date        string // %ad (git 既定の絶対日時。例: "Thu Jul 16 19:12:47 2026 +0900")
	RelDate     string // %ar (相対日時。--oneline 表示で使う)
	Decoration  string // %D (例: "HEAD -> master, origin/master")、無ければ空
	Message     string // %B (subject を含むフルメッセージ)
	Body        string // --stat / -p の本文 (ヘッダー行以外)。無ければ空
}

// GitExitError は git コマンド自体の失敗。stderr と終了コードをそのまま伝播する。
type GitExitError struct {
	Stderr string
	Code   int
}

func (e *GitExitError) Error() string { return e.Stderr }

// runGit は git を実行して stdout を返す。失敗時は *GitExitError。
func runGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		code := 1
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		return "", &GitExitError{Stderr: stderr.String(), Code: code}
	}
	return stdout.String(), nil
}

// BuildLogArgs は allowlist 済みオプションから git log の引数列を組み立てる (データ解析用。
// prettyFormat の制御文字レコードで機械 parse する)。colored は --stat / -p の本文にのみ効く。
func BuildLogArgs(opts *Options, colored bool) []string {
	return buildLogArgsWith(prettyFormat, opts, colored)
}

// BuildDisplayArgs は表示用 (verbatim) の git log 引数列。format を指定せず git の素の
// 出力 (medium 形式) をそのまま得る = 見た目は git log と機械的に一致する。
func BuildDisplayArgs(opts *Options, colored bool) []string {
	return buildLogArgsWith("", opts, colored)
}

func buildLogArgsWith(format string, opts *Options, colored bool) []string {
	args := []string{"log"}
	if format != "" {
		args = append(args, format)
	}
	args = append(args, fmt.Sprintf("--max-count=%d", opts.MaxCount))
	if colored {
		args = append(args, "--color=always")
	} else {
		args = append(args, "--color=never")
	}
	if opts.Stat {
		args = append(args, "--stat")
	}
	if opts.Patch {
		args = append(args, "--patch")
	}
	args = append(args, opts.Revs...)
	if len(opts.Paths) > 0 {
		args = append(args, "--")
		args = append(args, opts.Paths...)
	}
	return args
}

// LoadLogDisplay は表示用の git log 実出力を行列で返す (verbatim 方式の入力)。
func LoadLogDisplay(opts *Options, colored bool) ([]string, error) {
	out, err := runGit(BuildDisplayArgs(opts, colored)...)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n"), nil
}

// ParseLog は prettyFormat 付き git log の出力をコミット列へ解析する。
func ParseLog(out string) ([]Commit, error) {
	if out == "" {
		return nil, nil
	}
	var commits []Commit
	for rec := range strings.SplitSeq(out, recordSep) {
		if rec == "" {
			continue // 先頭レコード前の空文字列
		}
		parts := strings.SplitN(rec, fieldSep, numFields+1)
		if len(parts) != numFields+1 {
			return nil, fmt.Errorf("glog: git log の出力を解析できません (フィールド数 %d)", len(parts))
		}
		body := strings.TrimPrefix(parts[9], "\n")
		body = strings.TrimRight(body, "\n")
		commits = append(commits, Commit{
			SHA:         parts[0],
			ShortSHA:    parts[1],
			Subject:     parts[2],
			Author:      parts[3],
			AuthorEmail: parts[4],
			Date:        parts[5],
			RelDate:     parts[6],
			Decoration:  parts[7],
			Message:     strings.TrimRight(parts[8], "\n"),
			Body:        body,
		})
	}
	return commits, nil
}

// LoadCommits は git log を実行して解析まで行う。
func LoadCommits(opts *Options, colored bool) ([]Commit, error) {
	out, err := runGit(BuildLogArgs(opts, colored)...)
	if err != nil {
		return nil, err
	}
	return ParseLog(out)
}

// LoadHeadCommit は --cached モード用に HEAD 1 件を取得する。
func LoadHeadCommit() (*Commit, error) {
	out, err := runGit("log", prettyFormat, "--max-count=1", "--color=never")
	if err != nil {
		return nil, err
	}
	commits, err := ParseLog(out)
	if err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return nil, errors.New("glog: HEAD コミットがありません")
	}
	return &commits[0], nil
}

// UnpushedSHAs は revs (空なら HEAD) の履歴のうち、どの remote ref にも到達していない
// = GitHub 上にまだ存在しないコミットの集合を返す。これらは API に問い合わせても必ず
// 「無い」と返るため取得対象から外し、表示も「push 済みだが Check なし (–)」と
// 区別して ↑ にする。rev-list の失敗 (特殊な ref 状態) は「未 push なし」へ落とす
// (その場合は従来どおり API 側の判定に任される)。
//
// limit (>0) は列挙の上限。表示対象は rev-list 先頭の MaxCount 件で、フィルタは相対順序を
// 保つため「表示範囲内の未 push」は必ずフィルタ後の先頭 limit 件に含まれる = 上限を
// かけても表示対象の判定は欠けない。remote ref が無い巨大 repo で全履歴を列挙する劣化の
// ガード。仮に取りこぼしても API 問い合わせが none を返すだけの fail-soft。
//
// paths は表示側 (BuildLogArgs の `-- <paths>`) と同じ pathspec。渡さないと表示は
// 「path 該当の先頭 N 件」・未 push 集合は「path 無視の先頭 N 件」と対象ドメインが食い違い、
// path を触らない未 push が N 件超あると `glogx -- <path>` で path 該当の未 push が
// rev-list から溢れて ↑ でなく – と誤表示される (上の subsequence 保証が path 付きで
// 崩れる)。表示と同じ path フィルタを rev-list にも掛けて集合を揃える。
func UnpushedSHAs(revs []string, limit int, paths []string) map[string]bool {
	args := []string{"rev-list"}
	if len(revs) == 0 {
		args = append(args, "HEAD")
	} else {
		args = append(args, revs...)
	}
	args = append(args, "--not", "--remotes")
	if limit > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", limit))
	}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	out, err := runGit(args...)
	if err != nil {
		return nil
	}
	unpushed := map[string]bool{}
	for sha := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if sha != "" {
			unpushed[sha] = true
		}
	}
	return unpushed
}

// DecorColors は decoration (ブランチ名等) の色。git log の見た目を尊重するため、
// git 本体の既定色と git config の color.decorate.* 上書きをそのまま使う。
type DecorColors struct {
	HEAD         string   // 既定 bold cyan
	Branch       string   // 既定 bold green
	RemoteBranch string   // 既定 bold red
	Tag          string   // 既定 bold yellow
	Remotes      []string // remote 名 (remote branch 判定用)
}

// DefaultDecorColors は git の組み込み既定色 (git config が読めない環境の fallback)。
func DefaultDecorColors() DecorColors {
	return DecorColors{
		HEAD:         "\x1b[1;36m",
		Branch:       "\x1b[1;32m",
		RemoteBranch: "\x1b[1;31m",
		Tag:          "\x1b[1;33m",
		Remotes:      []string{"origin"},
	}
}

// LoadDecorColors は git config --get-color で decoration 色を解決する
// (ユーザーが color.decorate.* を設定していればそれ、無ければ git と同じ既定色が返る)。
// 5 本の git fork (config ×4 + remote) は互いに独立なので並列に走らせる
// (直列だと fork 遅延 ≈ 6ms × 5 が起動にそのまま乗る。各 goroutine は別フィールドに
// 書くためロック不要)。
func LoadDecorColors() DecorColors {
	dc := DefaultDecorColors()
	var wg sync.WaitGroup
	slots := []struct {
		dst  *string
		slot string
		def  string
	}{
		{&dc.HEAD, "HEAD", "bold cyan"},
		{&dc.Branch, "branch", "bold green"},
		{&dc.RemoteBranch, "remoteBranch", "bold red"},
		{&dc.Tag, "tag", "bold yellow"},
	}
	for _, s := range slots {
		wg.Go(func() {
			if out, err := runGit("config", "--get-color", "color.decorate."+s.slot, s.def); err == nil && out != "" {
				*s.dst = out
			}
		})
	}
	wg.Go(func() {
		out, err := runGit("remote")
		if err != nil {
			return
		}
		var remotes []string
		for name := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
			if name != "" {
				remotes = append(remotes, name)
			}
		}
		if len(remotes) > 0 {
			dc.Remotes = remotes
		}
	})
	wg.Wait()
	return dc
}

// maxDiffLines は diff ポップアップに保持する行数の上限。巨大コミット (自動生成物等) で
// 描画・保持行数が際限なく増えないための安全弁で、超過時は末尾に省略注記を足す。
// ⚠️ これがバウンドするのは「保持する行数」だけで、ピークメモリは制限しない: runGit は
// git show の全出力を bytes.Buffer + String() で一旦フルに展開する (diff 全長 × 約2) ため、
// 数万行 diff の一時スパイクは起きる (popup クローズ後に GC 解放される一時的なもの)。
// メモリも真にバウンドしたければ git 出力を行ストリームで読み上限で打ち切る必要がある。
const maxDiffLines = 5000

// LoadCommitDiff は d キーの diff ポップアップ本文 (git show --stat --patch) を取得する。
// 行は sanitizeDetailLine で無害化する (SGR 色は残しタブ/制御文字は枠描画を壊すため潰す)。
// 色は git に任せず --color=never で受けて HighlightDiff が付ける (diff 構造色 +
// chroma のシンタックスハイライト。切り捨て方は highlight.go 冒頭コメント参照)。
func LoadCommitDiff(sha string, colored bool) ([]string, error) {
	out, err := runGit("show", "--stat", "--patch", "--color=never", sha)
	if err != nil {
		return nil, err
	}
	var lines []string
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if len(lines) >= maxDiffLines {
			lines = append(lines, fmt.Sprintf("... (%d 行を超えるため省略。全文: git show %s)", maxDiffLines, sha))
			break
		}
		lines = append(lines, sanitizeDetailLine(line))
	}
	if colored {
		lines = HighlightDiff(lines)
	}
	return lines, nil
}

// LoadStagedDiff は --cached モードの本文 (git diff --cached) を取得する。
// フラグ未指定時は --stat 相当を出す (staged 変更の一覧が目的で、既定でフル patch は過剰なため)。
func LoadStagedDiff(opts *Options, colored bool) (string, error) {
	args := []string{"diff", "--cached"}
	if colored {
		args = append(args, "--color=always")
	} else {
		args = append(args, "--color=never")
	}
	if opts.Patch {
		// -p: フル patch (git diff --cached の既定出力)
	} else {
		args = append(args, "--stat")
	}
	out, err := runGit(args...)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}
