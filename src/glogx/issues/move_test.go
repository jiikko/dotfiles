package issues

import (
	"glogx/internal/testenv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 目印の宛先ディレクトリ (next/) は無ければ作る (ユーザー指定 2026-08-01)。直下の issue の claim は
// rename ではなく symlink の目印。実ファイルは動かない。
func TestMoveToSubdirCreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte("# 001 feat: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := &Issue{Path: path, Dir: dir, Rel: "001-feat-x.md"}

	dest, err := MoveToSubdir(iss, NextDirName)
	if err != nil {
		t.Fatalf("claim に失敗: %v", err)
	}
	if dest != path {
		t.Fatalf("claim で Path が変わった: %q (want %q)", dest, path)
	}
	link := filepath.Join(dir, NextDirName, "001-feat-x.md")
	if target, err := os.Readlink(link); err != nil || target != "../001-feat-x.md" {
		t.Fatalf("next/ に目印 symlink が無い: target=%q err=%v", target, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("実ファイルが動いた")
	}
}

// 🚨 上書きしない: 同じ basename が 2 箇所にあるのは viewer が警告する異常 (静かな内容喪失)
// なので、移動でそれを作らない。
func TestMoveToSubdirRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	nextDir := filepath.Join(dir, NextDirName)
	if err := os.MkdirAll(nextDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "001-feat-x.md")
	for _, p := range []string{path, filepath.Join(nextDir, "001-feat-x.md")} {
		if err := os.WriteFile(p, []byte("# 001\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	iss := &Issue{Path: path, Dir: dir, Rel: "001-feat-x.md"}

	if _, err := MoveToSubdir(iss, NextDirName); err == nil {
		t.Fatal("同名ファイルがあるのに移動した (静かな内容喪失を作る)")
	} else if !strings.Contains(err.Error(), "同名") {
		t.Fatalf("理由が分かる失敗になっていない: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("失敗したのに元のファイルが消えている")
	}
}

// 既にそこに居るなら何もしない (「変化なし」を呼び出し側が数えられるよう成功で返す)。
func TestMoveToSubdirNoopWhenAlreadyThere(t *testing.T) {
	dir := t.TempDir()
	nextDir := filepath.Join(dir, NextDirName)
	if err := os.MkdirAll(nextDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nextDir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte("# 001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := &Issue{Path: path, Dir: dir, Rel: filepath.Join(NextDirName, "001-feat-x.md")}

	dest, err := MoveToSubdir(iss, NextDirName)
	if err != nil || dest != path {
		t.Fatalf("同じ場所への移動が no-op になっていない: dest=%q err=%v", dest, err)
	}
}

// Epic issue の claim は group の next/ に目印を置き、解除しても group 内で完結する。global の
// `<dir>/next/` に置くと次回 Scan で group から消えるので、symlink の位置を検査する。
func TestMoveToSubdirKeepsEpicIssueInsideGroup(t *testing.T) {
	dir := t.TempDir()
	groupDir := filepath.Join(dir, EpicDirName, "cloud")
	if err := os.MkdirAll(groupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(groupDir, "710-feat-drive.md")
	if err := os.WriteFile(path, []byte("# 710\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := &Issue{
		Path: path, Dir: dir, Rel: filepath.Join(EpicDirName, "cloud", "710-feat-drive.md"),
		Group: "cloud", GroupKind: GroupEpic, GroupKey: groupDir,
	}

	next, err := MoveToSubdir(iss, NextDirName)
	if err != nil || next != path {
		t.Fatalf("Epic issue の claim: dest=%q err=%v", next, err)
	}
	link := filepath.Join(groupDir, NextDirName, "710-feat-drive.md")
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("group 内 next/ に目印が無い: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, NextDirName, "710-feat-drive.md")); !os.IsNotExist(err) {
		t.Fatal("Epic issue の目印が global next/ に置かれている")
	}
	claimed := *iss
	claimed.Status, claimed.NextLink = StatusNext, link
	open, err := MoveToSubdir(&claimed, "")
	if err != nil || open != path {
		t.Fatalf("group 内 next の解除: dest=%q err=%v", open, err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatal("解除で目印が消えない")
	}
}

// next/ は状態として認識し、既定の一覧 (open のみ) でも必ず見せる。
// 🚨 伏せると「目印を付けた issue が既定の一覧から消える」という逆の結果になる。
func TestNextDirIsAStatusAndAlwaysVisible(t *testing.T) {
	dir := t.TempDir()
	nextDir := filepath.Join(dir, NextDirName)
	if err := os.MkdirAll(nextDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nextDir, "001-feat-x.md"), []byte("# 001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	found, _ := Scan([]string{dir})
	if len(found) != 1 {
		t.Fatalf("next/ の issue を拾えていない: %d 件", len(found))
	}
	if found[0].Status != StatusNext {
		t.Fatalf("next/ が状態になっていない: %v", found[0].Status)
	}
	if got := found[0].Status.Badge(); got != "▶" {
		t.Fatalf("next のバッジが違う: %q", got)
	}
	for _, f := range []StatusFilter{FilterOpen, FilterPending, FilterAll} {
		if len(Filter(found, "", f)) != 1 {
			t.Fatalf("filter=%v で next が消えた", f)
		}
	}
}

// TestMoveKeepsStrayGroupChildInsideEpic は group 内の予約外ディレクトリ (`epic/<name>/closed/`) に
// 居る迷子を動かしても epic の外へ出ないことを固定する。
//
// 迷子は GroupKind=Unknown なので、宛先を GroupKind だけで決めると issue ルートへ落ちる。
// 迷子は一覧に出るので、`n` からこの経路へ到達できる。
func TestMoveKeepsStrayGroupChildInsideEpic(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	mkFiles(t, filepath.Join(dir, "epic", "cloud", "closed"), "702-feat-stray.md")
	mkFiles(t, filepath.Join(dir, "epic", "cloud", "done"), "701-feat-done.md")
	all, _ := Scan([]string{dir})

	for _, iss := range all {
		got, err := MoveToSubdir(iss, NextDirName)
		if err != nil {
			t.Fatalf("%s: %v", iss.Rel, err)
		}
		want := filepath.Join(dir, "epic", "cloud", NextDirName, filepath.Base(iss.Rel))
		if got != want {
			t.Errorf("%s (kind=%v) の移動先が epic の外: got %q want %q", iss.Rel, iss.GroupKind, got, want)
		}
	}
}

// TestMoveRejectsGroupIssueWithoutGroupKey は GroupKey を持たない group issue (Scan を通って
// いない手組みの Issue) の移動を拒否することを固定する。通すと destDir が空になり、dest が
// 相対パス (`next/NNN-x.md`) になって **glogx の CWD 配下**へ issue を rename する
// (2026-09-06 に確認: この guard を消しても全テストが緑だった)。
//
// 🚨 このテストで guard を外す変異を当てると、fixture が **repo 内の
// `src/glogx/issues/next/`** に実際に作られる (2026-09-06 に実測)。変異のあとは
// `git status` で残骸を確認すること。
func TestMoveRejectsGroupIssueWithoutGroupKey(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	rel := filepath.Join("epic", "cloud", "007-feat-handmade.md")
	mkFiles(t, filepath.Join(dir, "epic", "cloud"), "007-feat-handmade.md")
	iss := &Issue{
		Path: filepath.Join(dir, rel), Dir: dir, Rel: rel,
		Number: "007", Category: "feat", Status: StatusOpen,
		Group: "cloud", GroupKind: GroupEpic, // GroupKey は空 (Scan を通っていない)
	}
	got, err := MoveToSubdir(iss, NextDirName)
	if err == nil {
		t.Fatalf("GroupKey の無い group issue の移動が通った: %q", got)
	}
	if _, statErr := os.Stat(iss.Path); statErr != nil {
		t.Errorf("拒否したのに元ファイルが動いている: %v", statErr)
	}
	for _, sub := range []string{filepath.Join(dir, NextDirName), filepath.Join(dir, "epic", "cloud", NextDirName)} {
		if entries, _ := os.ReadDir(sub); len(entries) != 0 {
			t.Errorf("%s に何か作られた: %v", sub, entries)
		}
	}
}

// next から出す移動が失敗したら、バナーも目印もファイルも動かない (再実行できる)。
// 同名の確認をバナーと目印を外した後に置くと、失敗しても claim が黙って外れる。
func TestMoveOutOfNextChangesNothingOnFailure(t *testing.T) {
	fixedNow(t)
	claimed := func(t *testing.T) (*Issue, string) {
		t.Helper()
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
		return iss, dir
	}
	unchanged := func(t *testing.T, iss *Issue) {
		t.Helper()
		b, err := os.ReadFile(iss.Path)
		if err != nil {
			t.Fatalf("ファイルが元の場所に無い: %v", err)
		}
		if !strings.Contains(string(b), "**担当中: ") {
			t.Fatalf("失敗したのにバナーが外れた: %q", b)
		}
		if target, err := os.Readlink(iss.NextLink); err != nil || target != "../001-feat-x.md" {
			t.Fatalf("失敗したのに目印が外れた: %q %v", target, err)
		}
	}
	t.Run("宛先に同名", func(t *testing.T) {
		iss, dir := claimed(t)
		if err := os.MkdirAll(filepath.Join(dir, "done"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "done", "001-feat-x.md"), []byte("# 別\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := MoveToSubdir(iss, "done"); err == nil {
			t.Fatal("同名があるのに成功した")
		}
		unchanged(t, iss)
	})
	t.Run("宛先の dir を作れない", func(t *testing.T) {
		iss, dir := claimed(t)
		if err := os.WriteFile(filepath.Join(dir, "done"), []byte("dir ではない\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := MoveToSubdir(iss, "done"); err == nil {
			t.Fatal("宛先の dir を作れないのに成功した")
		}
		unchanged(t, iss)
	})
	// このケースのバナーの検査は常に真 (差し込み口が他人のバナーを書く)。守っているのはファイルと目印の戻し
	t.Run("動かした後にバナーを外せない", func(t *testing.T) {
		iss, _ := claimed(t)
		old := beforeRewrite
		beforeRewrite = func(path string) { // 読んでから書くまでの間に書き換えて、外す処理を失敗させる
			_ = os.WriteFile(path, []byte("# x\n\n> 🚨 **担当中: 他人**\n\n## 概要\n追記\n"), 0o644)
		}
		t.Cleanup(func() { beforeRewrite = old })
		if _, err := MoveToSubdir(iss, "done"); err == nil {
			t.Fatal("バナーを外せないのに成功した")
		}
		beforeRewrite = old
		unchanged(t, iss)
	})
}

// 旧運用 (next/ に実ファイル) でも、同名で失敗したらバナーを外さない。
func TestMoveLegacyNextFileKeepsBannerOnConflict(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, NextDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, NextDirName, "001-feat-x.md")
	body := "# x\n\n" + testBanner + "\n\n## 概要\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "001-feat-x.md"), []byte("# 別\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := &Issue{Path: p, Dir: dir, Rel: "next/001-feat-x.md", Status: StatusNext}
	if _, err := MoveToSubdir(iss, ""); err == nil {
		t.Fatal("直下に同名があるのに成功した")
	}
	if b, _ := os.ReadFile(p); string(b) != body {
		t.Fatalf("失敗したのに本文が変わった: %q", b)
	}
}

// done などから next へ rename で運ぶ経路で、バナーを書けなければファイルを元の場所へ戻す
// (目印だけ・バナー無しの claim を残さない)。
func TestMoveIntoNextByRenameRollsBackWhenBannerFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "done"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "done", "001-feat-x.md")
	if err := os.WriteFile(p, []byte("# x\n\n## 概要\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testenv.Restore(t, &beforeRewrite)
	beforeRewrite = func(path string) { _ = os.WriteFile(path, []byte("# x\n\n## 概要\n追記\n"), 0o644) }
	iss := &Issue{Path: p, Dir: dir, Rel: "done/001-feat-x.md", Status: StatusDone}
	if _, err := MoveToSubdir(iss, NextDirName); err == nil {
		t.Fatal("バナーを書けないのに成功した")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("バナーを書けなかったのにファイルが元の場所へ戻らない: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, NextDirName, "001-feat-x.md")); !os.IsNotExist(err) {
		t.Fatalf("next/ に残骸がある: %v", err)
	}
}

// 失敗した移動の戻しは、自分が外した目印だけを戻す。走査の後に他のホスト (git pull) が外した目印を
// 作り直すと、誰も claim していない issue が Next として共有される。
func TestMoveOutOfNextDoesNotResurrectLinkRemovedByOthers(t *testing.T) {
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
	if err := os.Remove(iss.NextLink); err != nil { // 走査の後に外から外された
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "done"), []byte("dir ではない\n"), 0o644); err != nil {
		t.Fatal(err) // 宛先の dir を作れない = 目印を外した後の失敗
	}
	if _, err := MoveToSubdir(iss, "done"); err == nil {
		t.Fatal("宛先の dir を作れないのに成功した")
	}
	if _, err := os.Lstat(iss.NextLink); !os.IsNotExist(err) {
		t.Fatalf("外から外された目印を、失敗した移動の戻しが作り直した: %v", err)
	}
}

// 宛先への rename だけが失敗したときも、外した目印を戻す (MkdirAll・同名・バナーの
// 失敗に加えて、rename の失敗も注入する。無いと戻しを消す変異が緑のまま)。
func TestMoveOutOfNextRestoresLinkWhenRenameFails(t *testing.T) {
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
	if err := os.Mkdir(filepath.Join(dir, "done"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 元のディレクトリだけ書き込み不可: 目印 (next/ の中) は外せるが、ファイルを外へ rename できない
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := MoveToSubdir(iss, "done"); err == nil {
		t.Fatal("rename できないのに成功した")
	}
	if target, err := os.Readlink(iss.NextLink); err != nil || target != "../001-feat-x.md" {
		t.Fatalf("rename の失敗で目印が外れたまま: %q %v", target, err)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "**担当中: ") {
		t.Fatalf("rename の失敗でバナーが外れた: %q", b)
	}
}
