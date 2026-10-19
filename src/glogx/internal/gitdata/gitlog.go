// Package gitdata は glogx が描くデータの取得: git log (制御文字レコードでコミット境界を識別)・CI 状態の GraphQL 取得と
// ローカルキャッシュ。取得の条件は CLI の Options ではなく LogQuery で受け取る。
package gitdata

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"

	"subproc"

	"glogx/internal/gitexec"
	"glogx/internal/term"

	"github.com/jiikko/dotfiles/src/tuikit/highlight"
)

// コミット境界の識別は人間向け出力の正規表現ではなく制御文字レコードで行う (issue の設計)。
// 区切りは logFormat が 1 回の取得ごとに作る (下の doc)。--stat / -p の本文は最後のフィールド区切りの後に続き、
// 次のレコード区切りまでがそのコミットのレコード。
const (
	// FieldSep は指紋 (fingerprintFormat) の区切り。%H と %D は作者が書けない欄 (SHA と ref 名) なので、固定の制御文字でよい。
	FieldSep  = "\x1f"
	numFields = 9
	// fingerprintFormat は「表示が変わったか」だけを測るための最小フォーマット (gitlog_watch.go)。
	// SHA と decoration しか出さないので、-p の巨大 patch を持つ表示でも測る側は軽い。
	fingerprintFormat = "--pretty=format:%H%x1f%D"
)

// logFormat は git log のレコードの区切りとフィールドの区切り。
// 🚨 固定の制御文字だけで区切らない: %B と本文 (--stat / -p) はコミットの作者が書けるので、本文に区切りを書くと
// 本物のレコードの途中から偽のコミット行を作れる (偽の SHA に `--output=<path>` を書くと、その行の d で git show が
// ファイルを上書きした)。区切りに取得ごとの乱数 (64 bit) を混ぜ、作者が知りえない形にする。
// 🚨 区切りを書けなくなるわけではない (作者が当てれば書ける。1 回の取得につき 2^-64)。偶然・推測の一致は受け入れる。
type logFormat struct {
	rec, field string // 出力の中の区切り
	nonce      string
}

// newLogFormat は取得ごとに新しい区切りを作る。crypto/rand.Read は失敗しない (Go 1.24 以降は error を返さない)。
func newLogFormat() logFormat {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return logFormatWith(hex.EncodeToString(b[:]))
}

func logFormatWith(nonce string) logFormat {
	return logFormat{rec: "\x1e" + nonce + "\x1e", field: "\x1f" + nonce + "\x1f", nonce: nonce}
}

// pretty は git log に渡す --pretty。%B (フルメッセージ) は本文の直前に置く。
func (f logFormat) pretty() string {
	rec, field := "%x1e"+f.nonce+"%x1e", "%x1f"+f.nonce+"%x1f"
	return "--pretty=format:" + rec + strings.Join([]string{"%H", "%h", "%s", "%an", "%ae", "%ad", "%ar", "%D", "%B"}, field) + field
}

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

// colorArg は git の --color フラグを colored に応じて返す。
func colorArg(colored bool) string {
	if colored {
		return "--color=always"
	}
	return "--color=never"
}

// runGit は git を実行して stdout を返す。失敗時は *GitExitError。
// 起動時の同期経路用 (ハングしてもユーザーの Ctrl-C がプロセスごと落とせる)。
// TUI 対話中に非同期発行される経路は runGitTimeout を使うこと (下記)。
// 例外は gitlog_watch.go の追従経路 (LoadCommits)。起動時と同じ関数を共有する理由は同ファイルの注記。
func runGit(args ...string) (string, error) {
	// waitdelay-in: WaitDelay は runGitCmd が張る (ctx 有無の両経路が通るので 1 箇所に寄せてある)。
	// 🚨 no-waitdelay ではない。ここで「不要」と書くと嘘になる。
	return gitexec.RunGitCmd(exec.Command("git", subproc.GitArgs(args...)...)) // subproc: waitdelay-in runGitCmd
}

// buildLogArgs は allowlist 済みオプションから git log の引数列を組み立てる (データ解析用。
// f の区切りで機械 parse する)。colored は --stat / -p の本文にのみ効く。
func buildLogArgs(f logFormat, q LogQuery, colored bool) []string {
	return buildLogArgsWith(f.pretty(), q, colored, true)
}

// buildDisplayArgs は表示用 (verbatim) の git log 引数列。format を指定せず git の素の
// 出力 (medium 形式) をそのまま得る = 見た目は git log と機械的に一致する。
// --decorate=short は明示が要る: log.decorate の既定 (auto) は出力先が端末のときだけ
// ブランチ名を付けるので、パイプで受けるここでは消える (端末の git log とずれる)。
func buildDisplayArgs(q LogQuery, colored bool) []string {
	return buildLogArgsWith("--decorate=short", q, colored, true)
}

// BuildFingerprintArgs は表示中の git log が変わったかを測る引数列 (gitlog_watch.go)。
// 表示と同じ revs / max-count / paths のまま --stat / -p を外す: 本文は SHA で決まるので、
// SHA と decoration が同じなら表示も同じ = 空振りの再読込 (CI の再取得を伴う) を出さない。
// 色は常に付けない (比較しかしないので ANSI は無駄。装飾の揺れを偽の変化にしない)。
func BuildFingerprintArgs(q LogQuery) []string {
	return buildLogArgsWith(fingerprintFormat, q, false, false)
}

// buildLogArgsWith は 3 つの入口の共通部 (revs / max-count / paths の組み立ての単一の出典)。
// shape は出力の形を決める引数 (--pretty か --decorate)。withBody は --stat / -p を通すか
// (指紋は本文を読まないので false)。
func buildLogArgsWith(shape string, q LogQuery, colored, withBody bool) []string {
	args := []string{"log", shape}
	args = append(args, fmt.Sprintf("--max-count=%d", q.MaxCount))
	args = append(args, colorArg(colored))
	if withBody {
		if q.Stat {
			args = append(args, "--stat")
		}
		if q.Patch {
			// --no-textconv --no-ext-diff: 開いた repo の設定のコマンドを差分の表示で走らせない (subproc.GitArgs の doc)
			args = append(args, "--patch", "--no-textconv", "--no-ext-diff")
		}
	}
	args = append(args, q.Revs...)
	if len(q.Paths) > 0 {
		args = append(args, "--")
		args = append(args, q.Paths...)
	}
	return args
}

// LoadLogDisplay は表示用の git log 実出力を行列で返す (verbatim 方式の入力)。
func LoadLogDisplay(q LogQuery, colored bool) ([]string, error) {
	out, err := runGit(buildDisplayArgs(q, colored)...)
	if err != nil {
		return nil, err
	}
	// verbatim 表示は git 出力をそのまま流すので、ここで行ごとに無害化する:
	// VS16 付き絵文字の正規化 (幅ガタつき防止) + 端末制御シーケンス注入の除去
	// (SGR の焼き込み色は残す)。ParseLog 側のフィールド無害化と対になる。タブは残す
	// (静的出力のパリティ契約。TUI では decorateVerbatim が展開する)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, l := range lines {
		lines[i] = term.SanitizeLineKeepTabs(l)
	}
	return lines, nil
}

// parse は f.pretty() 付き git log の出力をコミット列へ解析する。
func (f logFormat) parse(out string) ([]Commit, error) {
	if out == "" {
		return nil, nil
	}
	commits := make([]Commit, 0, strings.Count(out, f.rec))
	for rec := range strings.SplitSeq(out, f.rec) {
		if rec == "" {
			continue // 先頭レコード前の空文字列
		}
		parts := strings.SplitN(rec, f.field, numFields+1)
		if len(parts) != numFields+1 {
			return nil, fmt.Errorf("glogx: git log の出力を解析できません (フィールド数 %d)", len(parts))
		}
		// 区切りは作者が知りえないので、ここに来るのは git の出力の形が想定と違うときだけ。SHA の欄は git の argv
		// (LoadCommitDiff) に渡るので、形を確かめてから受け取る
		if !isHexSHA(parts[0], fullSHALens...) || !isHexSHA(parts[1]) {
			return nil, fmt.Errorf("glogx: git log の SHA の欄を解析できません (%q)", term.SanitizePlainLine(parts[1]))
		}
		// VS16 付き絵文字が残ると描画エンジンと端末で幅が食い違いガタつくため、欄ごとに正規化する (term.DropEmojiVS16)。
		// 🚨 区切る前に出力全体へ掛けない: VS16 を落とすと前後の文字が連結され、元の出力に無かった区切りや
		// SHA の形の値ができる (区切りと SHA の検査は git が出した生の値で行う)
		for i := 2; i < len(parts); i++ {
			parts[i] = term.DropEmojiVS16(parts[i])
		}
		body := strings.TrimPrefix(parts[9], "\n")
		body = strings.TrimRight(body, "\n")
		// subject/message/body/author はコミット作者が任意の文字を入れられる外部入力。
		// CI ログと同じ端末制御シーケンス注入 (OSC52 のクリップボード書き込み等) の経路に
		// なるため、ここ (解析の単一ファネル) で無害化する。タブは残す
		// (静的出力の git log パリティ契約。タブ展開は TUI 描画側 — term.SanitizeLineKeepTabs の doc)。
		// SGR (色) を残すのは git が --color=always で色を付ける欄 (decoration の %D と、--stat / -p の
		// body) だけ。subject / author / email / message (%B) に git は色を付けないので、そこにある SGR は
		// 作者が書いたもので、ESC[8m で文字を隠せる。色も落として平文にする。
		// 🚨 これが効くのは Commit のフィールドを描く --oneline と予備の mediumLines だけ。既定の verbatim
		// 表示は git の出力を行ごとに LoadLogDisplay で無害化していて、1 行の中の git の色と作者の SGR を
		// 区別できないので SGR を残している
		commits = append(commits, Commit{
			SHA:         parts[0],
			ShortSHA:    parts[1],
			Subject:     term.SanitizePlainKeepTabs(parts[2]),
			Author:      term.SanitizePlainKeepTabs(parts[3]),
			AuthorEmail: term.SanitizePlainKeepTabs(parts[4]),
			Date:        parts[5],
			RelDate:     parts[6],
			Decoration:  term.SanitizeLineKeepTabs(parts[7]),
			Message:     sanitizeAuthorText(strings.TrimRight(parts[8], "\n")),
			Body:        sanitizeGitText(capGitBody(body, parts[1])),
		})
	}
	return commits, nil
}

// capGitBody は --stat / -p の本文行数をコミットごとに maxDiffLines で打ち切る (単一 diff
// ポップアップと同じ安全弁)。-p × 無制限件数 (-n -1) で巨大 patch が全コミット分
// メモリと描画行数に載り続けるのを防ぐ。sanitize より先に呼び、落とす行の
// 無害化コストを払わない。
func capGitBody(body, shortSHA string) string {
	lines := strings.Split(body, "\n")
	if len(lines) <= MaxDiffLines {
		return body
	}
	lines = append(lines[:MaxDiffLines], truncatedNotice(shortSHA))
	return strings.Join(lines, "\n")
}

// truncatedNotice は maxDiffLines 打ち切りの案内行 (capGitBody と LoadCommitDiff で文言まで
// 同一だったのを一本化)。sha は呼び出し側の手元にある形 (short / full) をそのまま受ける
// (git show はどちらでも解決できる)。
func truncatedNotice(sha string) string {
	return fmt.Sprintf("... (%d 行を超えるため省略。全文: git show %s)", MaxDiffLines, sha)
}

// sanitizeGitText は複数行の git 由来テキストを行ごとに term.SanitizeLineKeepTabs へ通す
// (\n は構造なので保持。サニタイザは制御文字として \n を落とすため行単位で呼ぶ)。
// sanitizeAuthorText は作者が書いた複数行 (%B) を行ごとに平文にする (色も落とす。sanitizeGitText は色を残す)。
func sanitizeAuthorText(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = term.SanitizePlainKeepTabs(l)
	}
	return strings.Join(lines, "\n")
}

func sanitizeGitText(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = term.SanitizeLineKeepTabs(l)
	}
	return strings.Join(lines, "\n")
}

// LoadCommits は git log を実行して解析まで行う。
func LoadCommits(q LogQuery, colored bool) ([]Commit, error) {
	f := newLogFormat()
	out, err := runGit(buildLogArgs(f, q, colored)...)
	if err != nil {
		return nil, err
	}
	return f.parse(out)
}

// LoadHeadCommit は --cached モード用に HEAD 1 件を取得する。
func LoadHeadCommit() (*Commit, error) {
	f := newLogFormat()
	out, err := runGit("log", f.pretty(), "--max-count=1", "--color=never")
	if err != nil {
		return nil, err
	}
	commits, err := f.parse(out)
	if err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return nil, errors.New("glogx: HEAD コミットがありません")
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

// PushPlan は b の確認に出す「git push が送るもの」の見積もり。
// Exact は N が確かなとき (今のブランチを 1 本だけ送る設定で、push 先 @{push} を解決できた) だけ真。
// 🚨 Exact でないときの 0 件を「push 不要」と読まない: ref の作成だけの push (送るコミットは 0 件) や、今のブランチ以外・
// 追跡 ref と別の送り先へ送る設定がある (コミットの数では push の要否を決められない。pushPlanUncertain)。
// 🚨 Exact でも N は remote-tracking ref (最後に fetch した時点の @{push}) からの数: remote でブランチが消えた後に
// prune していなければ 0 件と出るが、push はブランチを作り直す。remote に問い合わせない (b の確認を待たせない) ための限界。
type PushPlan struct {
	N     int
	Exact bool
}

// PlanPush は b の確認と件数の出典 (表示中のコミット・path filter・-n・CI の状態に依存させない)。
// ローカルの git だけを読む (remote には問い合わせない)。数えられないときは Exact=false で返す (err にしない:
// push 自体は git が判断する)。
func PlanPush() PushPlan {
	if pushPlanUncertain() {
		return PushPlan{}
	}
	out, err := gitexec.RunGitTimeout("rev-list", "--count", "@{push}..HEAD")
	if err != nil {
		return PushPlan{} // push 先を解決できない (upstream 未設定・triangular の未作成のブランチなど)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return PushPlan{}
	}
	return PushPlan{N: n, Exact: true}
}

// pushPlanUncertain は「@{push} からのコミット数」が素の git push の送るもの・送れるかを表さない設定か:
//   - 今のブランチ以外も送る: push の refspec・remote の mirror (全 ref)・push.followTags (届く annotated tag)・
//     push.default=matching (同名のブランチ全部)
//   - 送り先が @{push} の追跡 ref と別: pushurl・1 つの remote に url が複数 (全部へ送る)
//   - git が設定の誤りで断る: push.default=upstream (push 先の remote が upstream と違うと断る)・branch.<名前>.merge が複数
//     (0 件と出すと、断られる理由が「未 push のコミットはありません」に隠れる)
//   - detached HEAD (@{push} も解決しない)
func pushPlanUncertain() bool {
	// 鍵の名前は git が小文字に正規化して照合・出力する (push.followTags も push.followtags で当たる)
	if out, err := gitexec.RunGitTimeout("config", "--type=bool", "--get-regexp", `^(remote\..*\.mirror|push\.followtags)$`); err == nil {
		for line := range strings.SplitSeq(out, "\n") {
			if strings.HasSuffix(strings.TrimSpace(line), " true") {
				return true
			}
		}
	}
	if out, err := gitexec.RunGitTimeout("config", "--get-regexp", `^(remote\..*\.(push|pushurl|url)|push\.default)$`); err == nil {
		urls := map[string]int{}
		for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
			key, val, _ := strings.Cut(line, " ")
			switch {
			case strings.HasSuffix(key, ".push"), strings.HasSuffix(key, ".pushurl"):
				return true
			case strings.HasSuffix(key, ".url"):
				if urls[key]++; urls[key] > 1 {
					return true
				}
			case key == "push.default":
				if val != "simple" && val != "current" {
					return true
				}
			}
		}
	}
	branch, err := gitexec.RunGitTimeout("symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return true
	}
	merges, err := gitexec.RunGitTimeout("config", "--get-all", "branch."+strings.TrimSuffix(branch, "\n")+".merge")
	// 行の数で数える (TrimSpace にしない: 空の値 `merge = ""` も 1 件で、git は複数の upstream として断る)
	return err == nil && strings.Count(strings.TrimSuffix(merges, "\n"), "\n") > 0
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

// LoadCommitDiff は d キーの diff ポップアップ本文 (git show --stat --patch) を取得する。
// 行は term.SanitizeDetailLine で無害化する (SGR 色は残しタブ/制御文字は枠描画を壊すため潰す)。
// 色は git に任せず --color=never で受けて github.com/jiikko/dotfiles/src/tuikit/highlight.Diff が付ける (diff 構造色 +
// chroma のシンタックスハイライト。方式は github.com/jiikko/dotfiles/src/tuikit/highlight の冒頭)。
func LoadCommitDiff(sha string, colored bool) ([]string, error) {
	if !isHexSHA(sha, fullSHALens...) {
		return nil, fmt.Errorf("glogx: コミットの SHA ではありません (%q)", term.SanitizePlainLine(sha))
	}
	out, err := showCommit(sha)
	if err != nil {
		return nil, err
	}
	return DiffDisplayLines(out, colored, truncatedNotice(sha)), nil
}

// MaxDiffLines は diff (コミットの diff・status viewer のプレビュー・-p の本文) に保持する行数の上限。巨大コミット (自動生成物等) で
// 描画・保持行数が際限なく増えないための安全弁で、超過時は末尾に省略注記を足す。
// 🚨 これがバウンドするのは「保持する行数」だけで、ピークメモリは制限しない: runGit は
// git show の全出力を bytes.Buffer + String() で一旦フルに展開する (diff 全長 × 約2) ため、
// 数万行 diff の一時スパイクは起きる (popup クローズ後に GC 解放される一時的なもの)。
// メモリも真にバウンドしたければ git 出力を行ストリームで読み上限で打ち切る必要がある。
const MaxDiffLines = 5000

// DiffDisplayLines は git の diff / show の生出力を表示行へ整える: MaxDiffLines で打ち切って notice の行を足し、各行を
// 無害化し、colored なら色を付ける (コミットの diff と status viewer のプレビューが同じ整形を通る)。空の出力は nil。
func DiffDisplayLines(out string, colored bool, notice string) []string {
	trimmed := strings.TrimRight(out, "\n")
	if trimmed == "" {
		return nil
	}
	lines := make([]string, 0, min(strings.Count(trimmed, "\n")+1, MaxDiffLines+1))
	for line := range strings.SplitSeq(trimmed, "\n") {
		if len(lines) >= MaxDiffLines {
			lines = append(lines, notice)
			break
		}
		lines = append(lines, term.SanitizeDetailLine(line))
	}
	if colored {
		lines = highlight.Diff(lines)
	}
	return lines
}

// showCommit は git show の本体。--end-of-options で sha をオプションとして読ませない (`--output=<path>` を渡されても
// ファイルを書かない)。LoadCommitDiff の SHA の形の検査とは別の段: 片方が外れても、もう片方で止まる。
func showCommit(sha string) (string, error) {
	// TUI 対話中の非同期経路 (d キー) なので timeout 付き (runGitTimeout の doc 参照)
	return gitexec.RunGitTimeout("show", "--stat", "--patch", "--no-textconv", "--no-ext-diff", "--color=never", "--end-of-options", sha)
}

// fullSHALens は %H の長さ (SHA-1 / SHA-256)。
var fullSHALens = []int{40, 64}

// isHexSHA は s が小文字の 16 進数だけで、長さが lens のどれかか (lens が空なら 4〜64 文字) を返す。
// 短縮形 (%h) は core.abbrev と衝突の回避で長さが決まるので、長さを渡さずに見る。
func isHexSHA(s string, lens ...int) bool {
	if len(lens) == 0 {
		if len(s) < 4 || len(s) > 64 {
			return false
		}
	} else if !slices.Contains(lens, len(s)) {
		return false
	}
	for i := range len(s) {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// LoadStagedDiff は --cached モードの本文 (git diff --cached) を取得する。
// フラグ未指定時は --stat 相当を出す (staged 変更の一覧が目的で、既定でフル patch は過剰なため)。
func LoadStagedDiff(q LogQuery, colored bool) (string, error) {
	args := []string{"diff", "--cached", "--no-textconv", "--no-ext-diff"}
	args = append(args, colorArg(colored))
	if q.Patch {
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

// LogQuery は git log の取得の条件 (gitlog.go の取得の関数が読むものだけ)。CLI の Options を丸ごと渡さないのは、
// 取得の層を internal/gitdata へ出すときに CLI の型を持ち込まないため。
type LogQuery struct {
	MaxCount int // 負数 = 無制限 (git log と同じ)
	Stat     bool
	Patch    bool
	Revs     []string // revision 指定
	Paths    []string // "--" 以降の pathspec
}
