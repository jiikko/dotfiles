package issues

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testBanner = "> 🚨 **担当中: host (glogx)**（2026-09-19〜）"

func TestAddBannerPlacement(t *testing.T) {
	cases := []struct{ name, src, want, back string }{
		// back = 解除後の本文。標準形は元どおり、H1 直後に本文が続く形だけ空行が 1 つ増える (害なし)
		{"標準形", "# t\n\n## 概要\n", "# t\n\n" + testBanner + "\n\n## 概要\n", "# t\n\n## 概要\n"},
		{"H1 の直後が本文", "# t\n本文\n", "# t\n\n" + testBanner + "\n\n本文\n", "# t\n\n本文\n"},
		{"H1 が無い", "本文\n", testBanner + "\n\n本文\n", "本文\n"},
		{"空ファイル", "", testBanner + "\n", ""},
	}
	for _, c := range cases {
		got, changed := addBanner(c.src, testBanner)
		if !changed || got != c.want {
			t.Errorf("%s: got %q changed=%v, want %q", c.name, got, changed, c.want)
		}
		if back, ok := removeBanner(got); !ok || back != c.back {
			t.Errorf("%s: 解除後 %q, want %q", c.name, back, c.back)
		}
	}
}

// 既に冒頭にバナーがあれば二重に書かない。冒頭ではない (フェンス内・見出しの後・散文中) ものは数えない。
func TestBannerDetectionMatchesCIRules(t *testing.T) {
	has := []string{
		"# t\n\n> 🚨 **担当中: x**（2026-09-19〜）\n",
		"# t\n\n**着手中 (2026-09-19 / s)**\n",
		"# t\r\n\r\n> 🚨 **担当中: x**\r\n",
	}
	for _, s := range has {
		if _, changed := addBanner(s, testBanner); changed {
			t.Errorf("既存のバナーを見落として二重に書いた: %q", s)
		}
	}
	not := []string{
		"# t\n\n## 概要\n\n> 🚨 **担当中: x**\n",
		"# t\n\n```\n**担当中: x**\n```\n",
		"# t\n\n  ~~~\n**担当中: x**\n  ~~~\n",
		"# t\n\n過去は**担当中**だった\n",
		"# t **担当中じゃない**\n",
	}
	for _, s := range not {
		if _, changed := addBanner(s, testBanner); !changed {
			t.Errorf("冒頭のバナーでないものをバナーと読んだ: %q", s)
		}
	}
}

func fixedNow(t *testing.T) {
	t.Helper()
	old := bannerNow
	bannerNow = func() time.Time { return time.Date(2026, 9, 19, 0, 0, 0, 0, time.Local) }
	t.Cleanup(func() { bannerNow = old })
}

// n (claim) は目印とバナーを 1 組で作り、もう一度 n (解除) で両方を消す。mode は保つ。
func TestClaimWritesAndUnclaimClearsBanner(t *testing.T) {
	fixedNow(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-x.md")
	orig := "# 001 feat: x\n\n## 概要\n"
	if err := os.WriteFile(path, []byte(orig), 0o640); err != nil {
		t.Fatal(err)
	}
	iss := &Issue{Path: path, Dir: dir, Rel: "001-feat-x.md"}
	if _, err := MoveToSubdir(iss, NextDirName); err != nil {
		t.Fatalf("claim に失敗: %v", err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "**担当中: ") || !strings.Contains(string(b), "（2026-09-19〜）") {
		t.Fatalf("claim でバナーが書かれていない: %q", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode が変わった: %v", fi.Mode().Perm())
	}
	iss.Status, iss.NextLink = StatusNext, NextLinkPath(iss)
	if _, err := MoveToSubdir(iss, ""); err != nil {
		t.Fatalf("解除に失敗: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != orig {
		t.Fatalf("解除で元の本文に戻らない: %q", b)
	}
	if _, err := os.Lstat(iss.NextLink); !os.IsNotExist(err) {
		t.Fatalf("解除で目印が残った: %v", err)
	}
}

// バナーを書けなければ目印も残さない (目印だけの claim は CI が落とす半端な状態)。
func TestClaimRollsBackLinkWhenBannerFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, NextDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	// issue ディレクトリだけ書き込み不可にする: next/ への symlink は作れるが、本文の temp は作れない
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	iss := &Issue{Path: path, Dir: dir, Rel: "001-feat-x.md"}
	if _, err := MoveToSubdir(iss, NextDirName); err == nil {
		t.Fatal("バナーを書けないのに claim が成功した")
	}
	if _, err := os.Lstat(NextLinkPath(iss)); !os.IsNotExist(err) {
		t.Fatalf("バナーを書けなかったのに目印が残った: %v", err)
	}
}

// Go 側で作った claim を CI 側の検査 (bash + awk の別実装) に通し、2 実装が同じ結論を出すことを見る。
func TestClaimPassesCIBannerCheck(t *testing.T) {
	script, err := filepath.Abs("../../../tests/issues/test_next_claims_have_banner.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("CI 側の検査が見つからない (repo の配置が変わった?): %v", err)
	}
	fixedNow(t)
	dir := t.TempDir()
	for _, name := range []string{"001-feat-a.md", "002-bug-b.md"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("# "+name+"\n\n## 概要\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := MoveToSubdir(&Issue{Path: p, Dir: dir, Rel: name}, NextDirName); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command("bash", script, dir).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "2 件を検査") {
		t.Fatalf("glogx が書いた claim を CI の検査が受け付けない: err=%v\n%s", err, out)
	}
	// 対照: バナーを消すと同じ検査が落ちる (検査が何も見ていない緑でないこと)
	p := filepath.Join(dir, "002-bug-b.md")
	if err := clearClaimBanner(p); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", script, dir).CombinedOutput(); err == nil {
		t.Fatalf("バナーを消しても CI の検査が通った:\n%s", out)
	}
}

// 直下以外 (pending/) から next へ運ぶ rename の経路でもバナーを書き、next から done へ出すときは外す。
func TestRenamePathsWriteAndClearBanner(t *testing.T) {
	fixedNow(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "pending"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "pending", "001-feat-x.md")
	if err := os.WriteFile(p, []byte("# x\n\n## 概要\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest, err := MoveToSubdir(&Issue{Path: p, Dir: dir, Rel: "pending/001-feat-x.md", Status: StatusPending}, NextDirName)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); !strings.Contains(string(b), "**担当中: ") {
		t.Fatalf("pending から next へ運んだのにバナーが無い: %q", b)
	}
	done, err := MoveToSubdir(&Issue{Path: dest, Dir: dir, Rel: "next/001-feat-x.md", Status: StatusNext}, "done")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(done); strings.Contains(string(b), "担当中") {
		t.Fatalf("next から done へ出したのにバナーが残った: %q", b)
	}
}

// 読んでから書くまでの間に本文が変わったら上書きしない (エディタの保存を消さない)。
func TestRewriteAbortsWhenFileChangedMeanwhile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(p, []byte("# x\n\n## 概要\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	edited := "# x\n\n## 概要\n\nエディタで足した行\n"
	old := beforeRewrite
	beforeRewrite = func(path string) {
		if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeRewrite = old })
	if err := writeClaimBanner(p); err == nil {
		t.Fatal("並行編集があったのに書き換えた")
	}
	if b, _ := os.ReadFile(p); string(b) != edited {
		t.Fatalf("並行編集が消えた: %q", b)
	}
}

// issue ファイル自体が symlink / ハードリンクなら書き換えない (rename でリンクを壊すため)。
func TestRewriteRefusesLinkedIssueFiles(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.md")
	body := "# x\n\n## 概要\n"
	if err := os.WriteFile(real, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sym := filepath.Join(dir, "001-feat-sym.md")
	if err := os.Symlink("real.md", sym); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(dir, "002-feat-hard.md")
	if err := os.Link(real, hard); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{sym, hard} {
		if err := writeClaimBanner(p); err == nil {
			t.Errorf("%s を書き換えた", filepath.Base(p))
		}
	}
	if fi, _ := os.Lstat(sym); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink が通常ファイルに置き換わった")
	}
	if b, _ := os.ReadFile(real); string(b) != body {
		t.Fatalf("リンク先が書き換わった: %q", b)
	}
}

// 解除でバナーを外せないときは目印も外さない (何も変えない)。外すと Status が Next でなくなり、
// もう一度 n を押しても解除が skip されて古いバナーを UI から外せなくなる。
func TestUnclaimChangesNothingWhenBannerClearFails(t *testing.T) {
	fixedNow(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(p, []byte("# x\n\n## 概要\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := &Issue{Path: p, Dir: dir, Rel: "001-feat-x.md"}
	if _, err := MoveToSubdir(iss, NextDirName); err != nil {
		t.Fatal(err)
	}
	iss.Status, iss.NextLink = StatusNext, NextLinkPath(iss)
	if err := os.Chmod(dir, 0o555); err != nil { // next/ は書ける、本文の temp は作れない
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	for _, sub := range []string{"", "done"} {
		if _, err := MoveToSubdir(iss, sub); err == nil {
			t.Fatalf("%q: バナーを外せないのに成功を返した", sub)
		}
		if _, err := os.Lstat(iss.NextLink); err != nil {
			t.Fatalf("%q: バナーを外せないのに目印を外した: %v", sub, err)
		}
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%q: バナーを外せないのにファイルを動かした: %v", sub, err)
		}
	}
}
