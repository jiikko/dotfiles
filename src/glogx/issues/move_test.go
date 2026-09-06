package issues

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 目印の宛先ディレクトリ (next/) は無ければ作る (ユーザー指定 2026-08-01)。直下の issue の claim は
// rename ではなく symlink の目印 (issue 263)。実ファイルは動かない。
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
// 居る迷子を動かしても epic の外へ出ないことを固定する (2026-09-06 の敵対的レビュー P2-1)。
//
// 迷子は GroupKind=Unknown なので、宛先を GroupKind だけで決めると issue ルートへ落ちる。
// issue 291 で迷子を一覧に出すようにしたことで、`n` からこの経路へ到達できるようになった。
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
// (2026-09-06 の敵対的レビュー 2 周目: この guard を消しても全テストが緑だった)。
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
