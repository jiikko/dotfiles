package gitdata

import (
	"errors"
	"fmt"
	"glogx/internal/gitexec"
	"glogx/internal/testenv"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testFmt はテストの git log の区切り (本番は取得ごとに乱数を混ぜる。ここは固定)。
var testFmt = logFormatWith("0123456789abcdef")

// rec はテスト用の git log レコード 1 件分 (9 フィールド + 本文)。
func rec(sha, short, subject, an, ae, ad, ar, deco, message, body string) string {
	return testFmt.rec + strings.Join([]string{sha, short, subject, an, ae, ad, ar, deco, message}, testFmt.field) + testFmt.field + body
}

func TestParseLogPlain(t *testing.T) {
	out := rec(strings.Repeat("a", 40), "aaaaaaa", "Fix invoice calculation", "koji", "koji@example.com",
		"Thu Jul 16 19:12:47 2026 +0900", "2 hours ago", "HEAD -> master, origin/master", "Fix invoice calculation\n\nbody\n", "") +
		rec(strings.Repeat("b", 40), "bbbbbbb", "Update README", "koji", "koji@example.com",
			"Wed Jul 15 10:00:00 2026 +0900", "1 day ago", "", "Update README\n", "")
	commits, err := testFmt.parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("len = %d; want 2", len(commits))
	}
	c := commits[0]
	if c.ShortSHA != "aaaaaaa" || c.Subject != "Fix invoice calculation" || c.Author != "koji" ||
		c.AuthorEmail != "koji@example.com" || c.Date != "Thu Jul 16 19:12:47 2026 +0900" ||
		c.RelDate != "2 hours ago" || c.Decoration != "HEAD -> master, origin/master" ||
		c.Message != "Fix invoice calculation\n\nbody" || c.Body != "" {
		t.Errorf("commits[0] = %+v", c)
	}
	if commits[1].Decoration != "" {
		t.Errorf("commits[1].Decoration = %q; want empty", commits[1].Decoration)
	}
}

func TestParseLogWithBody(t *testing.T) {
	// --stat / -p の本文は最後のフィールドセパレータ以降 (issue のテスト方針: 本文を壊さない)
	body := "\n file.go | 12 ++++--\n 1 file changed\n"
	out := rec(strings.Repeat("a", 40), "aaaaaaa", "subject", "koji", "k@x", "d", "now", "", "subject", body)
	commits, err := testFmt.parse(out)
	if err != nil {
		t.Fatal(err)
	}
	want := " file.go | 12 ++++--\n 1 file changed"
	if commits[0].Body != want {
		t.Errorf("Body = %q; want %q", commits[0].Body, want)
	}
}

// commit message は作者が任意の文字を入れられる外部入力。OSC52 (クリップボード書き込み) 等の
// 端末制御シーケンスが素通りしない (SGR の色だけ残る) こと (2026-07-29)。
func TestParseLogSanitizesControlSequences(t *testing.T) {
	subject := "fix\x1b]52;c;bWFsaWNpb3Vz\x07 \x1b[8mbug"          // OSC52 注入 + 文字を隠す SGR
	message := subject + "\n\n\x1b[2Jbody \x1b[31mred\x1b[0m line" // CSI 画面消去 + SGR
	body := "line1\n\x1b]0;evil-title\x07line2"                    // OSC タイトル変更
	out := rec(strings.Repeat("a", 40), "aaaaaaa", subject, "ko\x1b[2A\x1b[8mji", "k@x", "d", "now", "", message, body)
	commits, err := testFmt.parse(out)
	if err != nil {
		t.Fatal(err)
	}
	c := commits[0]
	if c.Subject != "fix bug" {
		t.Errorf("Subject = %q; want OSC52 が落ちた \"fix bug\"", c.Subject)
	}
	// subject / message は作者が書く欄で git は色を付けないので、SGR も落とす (ESC[8m で文字を隠せる)
	if want := "fix bug\n\nbody red line"; c.Message != want {
		t.Errorf("Message = %q; want %q (CSI 消去も SGR も落ちる)", c.Message, want)
	}
	if want := "line1\nline2"; c.Body != want {
		t.Errorf("Body = %q; want %q (OSC が落ち改行は残る)", c.Body, want)
	}
	if c.Author != "koji" {
		t.Errorf("Author = %q; want 制御シーケンスが落ちた \"koji\"", c.Author)
	}
}

// -p / --stat の本文はコミットごとに maxDiffLines で打ち切られる (単一 diff ポップアップと
// 同じ安全弁。-p × 無制限件数で際限なく保持しない)。
func TestParseLogCapsHugeBody(t *testing.T) {
	huge := strings.Repeat("+ line\n", MaxDiffLines+100)
	out := rec(strings.Repeat("a", 40), "aaaaaaa", "s", "a", "e", "d", "r", "", "s", "\n"+huge)
	commits, err := testFmt.parse(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(commits[0].Body, "\n")
	if len(lines) != MaxDiffLines+1 {
		t.Fatalf("Body 行数 = %d; want %d (上限 + 省略注記)", len(lines), MaxDiffLines+1)
	}
	if !strings.Contains(lines[MaxDiffLines], "省略") || !strings.Contains(lines[MaxDiffLines], "aaaaaaa") {
		t.Errorf("末尾が省略注記でない: %q", lines[MaxDiffLines])
	}
}

func TestParseLogEmpty(t *testing.T) {
	commits, err := testFmt.parse("")
	if err != nil || commits != nil {
		t.Errorf("ParseLog(\"\") = %v, %v", commits, err)
	}
}

func TestParseLogMalformed(t *testing.T) {
	if _, err := testFmt.parse(testFmt.rec + "only-one-field"); err == nil {
		t.Errorf("フィールド不足の出力がエラーにならない")
	}
}

func TestIntegrationStagedDiff(t *testing.T) {
	dir := testenv.NewTempRepo(t, []string{"first"})
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testenv.GitInRepo(t, dir, "add", ".")
	diff, err := LoadStagedDiff(LogQuery{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "staged.txt") {
		t.Errorf("staged diff に staged.txt がありません: %q", diff)
	}
	head, err := LoadHeadCommit()
	if err != nil {
		t.Fatal(err)
	}
	if head.Subject != "first" {
		t.Errorf("HEAD subject = %q", head.Subject)
	}
}

func TestIntegrationMediumFields(t *testing.T) {
	testenv.NewTempRepo(t, []string{"multi line\n\nmessage body here"})
	commits, err := LoadCommits(LogQuery{MaxCount: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	c := commits[0]
	if c.AuthorEmail != "t@example.com" {
		t.Errorf("AuthorEmail = %q", c.AuthorEmail)
	}
	if c.Date == "" || c.Message == "" {
		t.Errorf("Date/Message が空: %+v", c)
	}
	if !strings.Contains(c.Message, "message body here") {
		t.Errorf("フルメッセージが取れていない: %q", c.Message)
	}
}

// ResolveRepo の優先順 (upstream remote → origin) と投機的 origin 並列取得が、
// 並列化リファクタ後も元の意味論を保つことを検証する (github.go の ResolveRepo は
// runGit 直呼びで unit test 不可なので実 repo で確認)。
func TestIntegrationResolveRepoPriority(t *testing.T) {
	testenv.NewTempRepo(t, []string{"c1"})
	git := func(args ...string) { t.Helper(); testenv.GitInRepo(t, ".", args...) }
	head := testenv.HeadSHA(t)
	git("remote", "add", "origin", "git@github.com:o/r.git")
	git("remote", "add", "up", "git@github.com:up/r.git")
	git("update-ref", "refs/remotes/up/main", head)

	// upstream = 非 origin remote (up) → up を最優先で解決する
	git("branch", "--set-upstream-to=up/main", "main")
	if repo, ok := ResolveRepo(); !ok || repo != (Repo{Owner: "up", Name: "r"}) {
		t.Fatalf("upstream 優先が効かない: %+v ok=%v; want {up r}", repo, ok)
	}

	// upstream 未設定 → 投機取得した origin へ fallback
	git("branch", "--unset-upstream", "main")
	if repo, ok := ResolveRepo(); !ok || repo != (Repo{Owner: "o", Name: "r"}) {
		t.Fatalf("origin fallback が効かない: %+v ok=%v; want {o r}", repo, ok)
	}
}

// 非 GitHub の remote だけなら ok=false (誤って GitHub 扱いしない)。
func TestIntegrationResolveRepoNonGitHub(t *testing.T) {
	testenv.NewTempRepo(t, []string{"c1"})
	git := func(args ...string) { t.Helper(); testenv.GitInRepo(t, ".", args...) }
	git("remote", "add", "origin", "https://gitlab.com/x/y.git")
	if repo, ok := ResolveRepo(); ok {
		t.Fatalf("非 GitHub remote を解決した: %+v ok=%v; want ok=false", repo, ok)
	}
}

func TestIntegrationOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	_, err := LoadCommits(LogQuery{MaxCount: 1}, false)
	if err == nil {
		t.Fatal("リポジトリ外でエラーになっていない")
	}
	var gitErr *gitexec.GitExitError
	if !errors.As(err, &gitErr) {
		t.Fatalf("GitExitError ではない: %T %v", err, err)
	}
	if gitErr.Code == 0 || gitErr.Stderr == "" {
		t.Errorf("git の終了コード/stderr が伝播していない: %+v", gitErr)
	}
}

// LoadCommitDiff は実 git に対する薄い皮なので、実リポジトリで flag の妥当性ごと検証する。
func TestLoadCommitDiffRealRepo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	run := func(args ...string) {
		t.Helper()
		if _, err := runGit(args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if err := os.WriteFile("a.txt", []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-q", "-m", "add a")
	sha, err := runGit("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	lines, err := LoadCommitDiff(strings.TrimSpace(sha), false)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "+hello") {
		t.Errorf("patch 本文が含まれない:\n%s", joined)
	}
	if !strings.Contains(joined, "a.txt") {
		t.Errorf("stat のファイル名が含まれない:\n%s", joined)
	}
	if _, err := LoadCommitDiff("no-such-sha", false); err == nil {
		t.Error("不正 SHA でエラーが返らない")
	}
}

// 区切りは作者が知りえない乱数入りなので、本文に固定の制御文字の区切り (\x1e / \x1f) を書いても
// レコードは割れず、本文の一部として読まれる (偽のコミット行を作れない・起動不能にならない)。
func TestParseLogIgnoresSeparatorsWrittenByAuthor(t *testing.T) {
	forged := "\x1f\x1e--output=/tmp/pwned\x1fabcdef0\x1fForged\x1fMallory\x1fm@x\x1fd\x1fr\x1f\x1fForged\x1f"
	out := rec(strings.Repeat("a", 40), "aaaaaaa", "innocent", "a", "e", "d", "r", "", "innocent\n\nbefore\x1eafter"+forged, "")
	commits, err := testFmt.parse(out)
	if err != nil {
		t.Fatalf("本文の区切りの文字で解析が失敗した: %v", err)
	}
	if len(commits) != 1 || commits[0].SHA != strings.Repeat("a", 40) {
		t.Fatalf("本文から偽のレコードが生まれた: %#v", commits)
	}
	if !strings.Contains(commits[0].Message, "Forged") {
		t.Errorf("本文が途中で切れた: %q", commits[0].Message)
	}
}

// VS16 の正規化は区切った後に欄ごとに掛ける。先に出力全体へ掛けると、VS16 を挟んだ断片が連結されて、
// 元の出力に無かった区切りや SHA の形の値ができる。
func TestParseLogNormalizesVS16AfterSplitting(t *testing.T) {
	split := strings.Replace(testFmt.field, "01234567", "01234567\uFE0F", 1) // VS16 を落とすと区切りになる
	out := rec(strings.Repeat("a", 40), "aaaaaaa", "s", "a", "e", "before"+split+"after", "r", "", "msg ⚠\uFE0F end", "")
	commits, err := testFmt.parse(out)
	if err != nil {
		t.Fatal(err)
	}
	c := commits[0]
	if c.Date != "before"+strings.ReplaceAll(split, "\uFE0F", "")+"after" || c.RelDate != "r" {
		t.Errorf("VS16 を落とした値で区切った: Date=%q RelDate=%q", c.Date, c.RelDate)
	}
	if c.Message != "msg ⚠ end" {
		t.Errorf("欄の VS16 が落ちていない: %q", c.Message)
	}
}

// 区切りが合っていても SHA の欄が SHA の形でなければ受け取らない (SHA は git show の argv に渡る)。
func TestParseLogRejectsNonHexSHA(t *testing.T) {
	for _, sha := range []string{"--output=/tmp/x", strings.Repeat("A", 40), strings.Repeat("a", 39)} {
		if _, err := testFmt.parse(rec(sha, "aaaaaaa", "s", "a", "e", "d", "r", "", "s", "")); err == nil {
			t.Errorf("SHA の欄 %q を受け取った", sha)
		}
	}
	if _, err := testFmt.parse(rec(strings.Repeat("a", 40), "-x", "s", "a", "e", "d", "r", "", "s", "")); err == nil {
		t.Error("短縮 SHA の欄 -x を受け取った")
	}
}

// 本物の git で、作者が本文に区切りの文字を書いたコミットを読んでも偽の行は出ず、偽の SHA を d に渡しても
// ファイルは作られない。git show の 2 つの段 (SHA の形の検査・--end-of-options) はそれぞれ単独で止める。
func TestIntegrationForgedRecordInMessage(t *testing.T) {
	dir := testenv.NewTempRepo(t, []string{"first"})
	pwned := filepath.Join(dir, "pwned.txt")
	forged := "\x1f\x1e--output=" + pwned + "\x1fabcdef0\x1fForged subject\x1fMallory\x1fm@x\x1fd\x1fr\x1f\x1fForged\x1f"
	msg := filepath.Join(t.TempDir(), "msg")
	if err := os.WriteFile(msg, []byte("innocent subject\n\n"+forged+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testenv.GitInRepo(t, dir, "commit", "-q", "--allow-empty", "--cleanup=verbatim", "-F", msg)

	commits, err := LoadCommits(LogQuery{MaxCount: 10}, false)
	if err != nil {
		t.Fatalf("本文に区切りの文字を持つコミットで読めない: %v", err)
	}
	subjects := make([]string, 0, len(commits))
	for _, c := range commits {
		subjects = append(subjects, c.Subject)
	}
	if want := []string{"innocent subject", "first"}; !slices.Equal(subjects, want) {
		t.Fatalf("subjects = %q; want %q (偽の行が出た)", subjects, want)
	}
	if !strings.Contains(commits[0].Message, "Forged subject") || !strings.Contains(commits[0].Message, "Mallory") {
		t.Errorf("本文が途中で切れた: %q", commits[0].Message)
	}
	// HEAD は git show が受け取れる rev なので、SHA の形の検査が無いと err=nil で返る (検査の段だけを見る)
	for _, notSHA := range []string{"--output=" + pwned, "HEAD"} {
		if _, err := LoadCommitDiff(notSHA, false); err == nil {
			t.Errorf("SHA でない値 %q を LoadCommitDiff が受け取った", notSHA)
		}
	}
	if _, err := showCommit("--output=" + pwned); err == nil {
		t.Error("--output= を git show がオプションとして読んだ (--end-of-options が効いていない)")
	}
	if _, err := os.Stat(pwned); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("git show がファイルを書いた: %v", err)
	}
}

// capGitBody / truncatedNotice の回帰ガード (打ち切り文言は capGitBody と LoadCommitDiff で
// 共有される truncatedNotice が単一の出典。文言が壊れると「全文を見る手段」の案内が消える)。
func TestCapGitBody(t *testing.T) {
	short := "a\nb\nc"
	if got := capGitBody(short, "abc1234"); got != short {
		t.Errorf("上限内の本文が変更された: %q", got)
	}
	long := strings.TrimRight(strings.Repeat("x\n", MaxDiffLines+10), "\n")
	got := strings.Split(capGitBody(long, "abc1234"), "\n")
	if len(got) != MaxDiffLines+1 {
		t.Fatalf("打ち切り後の行数 = %d; want %d (本文 maxDiffLines + 案内 1 行)", len(got), MaxDiffLines+1)
	}
	last := got[len(got)-1]
	if !strings.Contains(last, "省略") || !strings.Contains(last, "git show abc1234") {
		t.Errorf("案内行に省略の説明と全文の見方が無い: %q", last)
	}
	if !strings.Contains(last, fmt.Sprintf("%d 行", MaxDiffLines)) {
		t.Errorf("案内行に打ち切り行数が無い: %q", last)
	}
}

func TestBuildLogArgs(t *testing.T) {
	q := LogQuery{MaxCount: 5, Stat: true, Revs: []string{"main"}, Paths: []string{"src/"}}
	args := buildLogArgs(testFmt, q, false)
	joined := strings.Join(args, " ")
	testenv.ContainsAll(t, "args", joined, "--max-count=5", "--stat", "--color=never", "main", "-- src/")
	if strings.Contains(joined, "--patch") {
		t.Errorf("指定していない --patch が入っている: %v", args)
	}
}

func TestBuildFingerprintArgsSkipsBodyAndColor(t *testing.T) {
	q := LogQuery{MaxCount: 7, Stat: true, Patch: true, Revs: []string{"HEAD"}, Paths: []string{"a.go"}}
	args := BuildFingerprintArgs(q)
	for _, bad := range []string{"--stat", "--patch", "--color=always"} {
		if slices.Contains(args, bad) {
			t.Errorf("指紋の問い合わせに %s が混ざっている (本文まで読むと -p の巨大 patch を毎分取る): %v", bad, args)
		}
	}
	for _, want := range []string{fingerprintFormat, "--max-count=7", "--color=never", "HEAD", "--", "a.go"} {
		if !slices.Contains(args, want) {
			t.Errorf("指紋の問い合わせに %s が無い: %v", want, args)
		}
	}
}

// PlanPush は今のブランチを 1 本だけ送る設定で push 先を解決できたときだけ件数を確かと言う。upstream が無い・push の
// refspec が設定されているときは件数を確かと言わない (0 件でも push が要りうる)。
func TestPlanPushRealRepo(t *testing.T) {
	// 開発者の ~/.gitconfig の push.default (current 等) で判定が変わらないように、global / system の設定を読ませない
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	dir := testenv.NewTempRepo(t, []string{"c1"})
	bare := filepath.Join(t.TempDir(), "origin.git")
	testenv.GitInRepo(t, dir, "init", "-q", "--bare", bare)
	testenv.GitInRepo(t, dir, "remote", "add", "origin", bare)
	testenv.GitInRepo(t, dir, "push", "-q", "origin", "main") // upstream は張らない
	testenv.CommitLines(t, dir, 2, "c2")
	if p := PlanPush(); p.Exact {
		t.Fatalf("upstream なしで件数を確かと言った: %+v", p)
	}
	testenv.GitInRepo(t, dir, "push", "-q", "-u", "origin", "main")
	testenv.CommitLines(t, dir, 3, "c3")
	if p := PlanPush(); !p.Exact || p.N != 1 {
		t.Fatalf("upstream あり: PlanPush = %+v (@{push} からの 1 件のはず)", p)
	}
	// push.default=matching は今のブランチ以外も送る (@{push} は解決できても件数は確かでない)
	testenv.GitInRepo(t, dir, "config", "push.default", "matching")
	if p := PlanPush(); p.Exact {
		t.Fatalf("push.default=matching なのに件数を確かと言った: %+v", p)
	}
	testenv.GitInRepo(t, dir, "config", "--unset", "push.default")
	// @{push} は main を指したまま解決できるが、別のブランチも送る refspec (件数は main の分しか数えられない)
	testenv.GitInRepo(t, dir, "config", "--add", "remote.origin.push", "refs/heads/main:refs/heads/main")
	testenv.GitInRepo(t, dir, "config", "--add", "remote.origin.push", "refs/heads/side:refs/heads/side")
	if p := PlanPush(); p.Exact {
		t.Fatalf("push の refspec があるのに件数を確かと言った: %+v", p)
	}
	testenv.GitInRepo(t, dir, "config", "--unset-all", "remote.origin.push")
	if p := PlanPush(); !p.Exact {
		t.Fatalf("前提: 設定を戻しても件数が確かにならない: %+v", p)
	}
	// 今のブランチ以外も送る / 追跡 ref と別の送り先へ送る / git が設定の誤りで断る (どれもコミット数では決まらない)
	for _, kv := range [][2]string{
		{"push.followTags", "true"}, {"remote.origin.mirror", "yes"},
		{"remote.origin.pushurl", filepath.Join(t.TempDir(), "other.git")},
		{"push.default", "upstream"},
	} {
		testenv.GitInRepo(t, dir, "config", kv[0], kv[1])
		if p := PlanPush(); p.Exact {
			t.Errorf("%s=%s なのに件数を確かと言った: %+v", kv[0], kv[1], p)
		}
		testenv.GitInRepo(t, dir, "config", "--unset", kv[0])
	}
	for _, kv := range [][2]string{{"remote.origin.url", "x"}, {"branch.main.merge", "x"}, {"branch.main.merge", ""}} { // 2 つ目の値 (空も 1 件)
		testenv.GitInRepo(t, dir, "config", "--add", kv[0], kv[1])
		if p := PlanPush(); p.Exact {
			t.Errorf("%s が複数 (2 つ目 %q) なのに件数を確かと言った: %+v", kv[0], kv[1], p)
		}
		testenv.GitInRepo(t, dir, "config", "--unset", kv[0], "^"+kv[1]+"$")
	}
	if p := PlanPush(); !p.Exact || p.N != 1 {
		t.Fatalf("前提: 設定を戻したら件数が確かに戻る: %+v", p)
	}
}

// 行数の上限ちょうどは何も省略しない (省略の案内も付けない)。1 行超えたら上限で切って案内を付ける。
func TestDiffLinesCapBoundary(t *testing.T) {
	body := func(n int) string { return strings.TrimSuffix(strings.Repeat("+ line\n", n), "\n") }
	if got := DiffDisplayLines(body(MaxDiffLines), false, "省略"); len(got) != MaxDiffLines || got[len(got)-1] == "省略" {
		t.Errorf("ちょうど上限: %d 行 / 末尾 %q (省略しないはず)", len(got), got[len(got)-1])
	}
	if got := DiffDisplayLines(body(MaxDiffLines+1), false, "省略"); len(got) != MaxDiffLines+1 || got[MaxDiffLines] != "省略" {
		t.Errorf("上限 + 1: %d 行 / 末尾 %q (上限で切って案内を付けるはず)", len(got), got[len(got)-1])
	}
	if got := capGitBody(body(MaxDiffLines), "aaaaaaa"); got != body(MaxDiffLines) {
		t.Error("-p の本文: ちょうど上限なのに書き換えた")
	}
	if got := capGitBody(body(MaxDiffLines+1), "aaaaaaa"); !strings.Contains(got, "省略") {
		t.Error("-p の本文: 上限 + 1 なのに省略していない")
	}
}
