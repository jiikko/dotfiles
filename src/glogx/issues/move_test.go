package issues

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 移動先ディレクトリは無ければ作る (ユーザー指定 2026-08-01)。
func TestMoveToSubdirCreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte("# 001 feat: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := &Issue{Path: path, Dir: dir, Rel: "001-feat-x.md"}

	dest, err := MoveToSubdir(iss, NextDirName)
	if err != nil {
		t.Fatalf("移動に失敗: %v", err)
	}
	if want := filepath.Join(dir, NextDirName, "001-feat-x.md"); dest != want {
		t.Fatalf("移動先が違う: %q (want %q)", dest, want)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("移動先にファイルが無い: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("元の場所にファイルが残っている (コピーになっている)")
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

// Epic issue の claim は group の next/ に入り、解除しても group 直下へ戻る。global issue の
// `<dir>/next/` と混ぜると次回 Scan で group から消えるので、戻り値の新 path も合わせて検査する。
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
	if err != nil || next != filepath.Join(groupDir, NextDirName, "710-feat-drive.md") {
		t.Fatalf("Epic issue の移動先が違う: dest=%q err=%v", next, err)
	}
	claimed := *iss
	claimed.Path, claimed.Rel, claimed.Status = next,
		filepath.Join(EpicDirName, "cloud", NextDirName, "710-feat-drive.md"), StatusNext
	open, err := MoveToSubdir(&claimed, "")
	if err != nil || open != path {
		t.Fatalf("group 内 next の解除先が違う: dest=%q err=%v", open, err)
	}
	if _, err := os.Stat(filepath.Join(dir, NextDirName, "710-feat-drive.md")); !os.IsNotExist(err) {
		t.Fatal("Epic issue が global next/ へ移動している")
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
