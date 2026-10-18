package gitdata

import (
	"errors"
	"fmt"
	"glogx/internal/extcmd"
	"glogx/internal/testenv"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// rec はテスト用の git log レコード 1 件分 (9 フィールド + 本文)。
func rec(sha, short, subject, an, ae, ad, ar, deco, message, body string) string {
	return "\x1e" + strings.Join([]string{sha, short, subject, an, ae, ad, ar, deco, message}, "\x1f") + "\x1f" + body
}

func TestParseLogPlain(t *testing.T) {
	out := rec(strings.Repeat("a", 40), "aaaaaaa", "Fix invoice calculation", "koji", "koji@example.com",
		"Thu Jul 16 19:12:47 2026 +0900", "2 hours ago", "HEAD -> master, origin/master", "Fix invoice calculation\n\nbody\n", "") +
		rec(strings.Repeat("b", 40), "bbbbbbb", "Update README", "koji", "koji@example.com",
			"Wed Jul 15 10:00:00 2026 +0900", "1 day ago", "", "Update README\n", "")
	commits, err := parseLog(out)
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
	commits, err := parseLog(out)
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
	commits, err := parseLog(out)
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
	huge := strings.Repeat("+ line\n", extcmd.MaxDiffLines+100)
	out := rec(strings.Repeat("a", 40), "aaaaaaa", "s", "a", "e", "d", "r", "", "s", "\n"+huge)
	commits, err := parseLog(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(commits[0].Body, "\n")
	if len(lines) != extcmd.MaxDiffLines+1 {
		t.Fatalf("Body 行数 = %d; want %d (上限 + 省略注記)", len(lines), extcmd.MaxDiffLines+1)
	}
	if !strings.Contains(lines[extcmd.MaxDiffLines], "省略") || !strings.Contains(lines[extcmd.MaxDiffLines], "aaaaaaa") {
		t.Errorf("末尾が省略注記でない: %q", lines[extcmd.MaxDiffLines])
	}
}

func TestParseLogEmpty(t *testing.T) {
	commits, err := parseLog("")
	if err != nil || commits != nil {
		t.Errorf("ParseLog(\"\") = %v, %v", commits, err)
	}
}

func TestParseLogMalformed(t *testing.T) {
	if _, err := parseLog("\x1eonly-one-field"); err == nil {
		t.Errorf("フィールド不足の出力がエラーにならない")
	}
}

func TestIntegrationStagedDiff(t *testing.T) {
	dir := testenv.NewTempRepo(t, []string{"first"})
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
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
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
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
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
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
	var gitErr *extcmd.GitExitError
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

// メッセージに record separator \x1e が入ると record が割れフィールド数不足になり解析エラーで
// 落ちる (gitlog.go の comment が宣言する不変条件)。
func TestParseLogRecordSepInMessageErrors(t *testing.T) {
	fields := []string{"H", "h", "subj", "an", "ae", "ad", "ar", "deco", "before\x1eafter"}
	out := recordSep + strings.Join(fields, FieldSep) + FieldSep + "body"
	if _, err := parseLog(out); err == nil {
		t.Error("メッセージ内の record separator (\\x1e) が解析エラーにならない")
	}
}

// 既知の制限 (gitlog.go の comment と一致): メッセージに field separator \x1f が入ると、単一
// デリミタ方式では本文の \x1f と区別できず検出できないため、最初の \x1f で無言に切り詰められる。
// この挙動を golden で固定し、将来「\x1f をエラーにする」変更が本文の \x1f 耐性を壊さないよう見張る。
func TestParseLogFieldSepInMessageTruncates(t *testing.T) {
	fields := []string{"H", "h", "subj", "an", "ae", "ad", "ar", "deco", "msgPart1\x1fmsgPart2"}
	out := recordSep + strings.Join(fields, FieldSep) + FieldSep + "body"
	commits, err := parseLog(out)
	if err != nil {
		t.Fatalf("既知の制限: エラーでなく切り詰めのはず: %v", err)
	}
	if len(commits) != 1 || commits[0].Message != "msgPart1" {
		t.Errorf("メッセージが最初の \\x1f で切り詰められていない: %#v", commits)
	}
}

// capGitBody / truncatedNotice の回帰ガード (打ち切り文言は capGitBody と LoadCommitDiff で
// 共有される truncatedNotice が単一の出典。文言が壊れると「全文を見る手段」の案内が消える)。
func TestCapGitBody(t *testing.T) {
	short := "a\nb\nc"
	if got := capGitBody(short, "abc1234"); got != short {
		t.Errorf("上限内の本文が変更された: %q", got)
	}
	long := strings.TrimRight(strings.Repeat("x\n", extcmd.MaxDiffLines+10), "\n")
	got := strings.Split(capGitBody(long, "abc1234"), "\n")
	if len(got) != extcmd.MaxDiffLines+1 {
		t.Fatalf("打ち切り後の行数 = %d; want %d (本文 maxDiffLines + 案内 1 行)", len(got), extcmd.MaxDiffLines+1)
	}
	last := got[len(got)-1]
	if !strings.Contains(last, "省略") || !strings.Contains(last, "git show abc1234") {
		t.Errorf("案内行に省略の説明と全文の見方が無い: %q", last)
	}
	if !strings.Contains(last, fmt.Sprintf("%d 行", extcmd.MaxDiffLines)) {
		t.Errorf("案内行に打ち切り行数が無い: %q", last)
	}
}

func TestBuildLogArgs(t *testing.T) {
	q := LogQuery{MaxCount: 5, Stat: true, Revs: []string{"main"}, Paths: []string{"src/"}}
	args := buildLogArgs(q, false)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--max-count=5", "--stat", "--color=never", "main", "-- src/"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args に %q がありません: %v", want, args)
		}
	}
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
