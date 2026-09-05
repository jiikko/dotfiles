package issues

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nextlink.go: 目印 symlink の採用条件と、claim / 解除が symlink の作成 / 削除になること (issue 263)。

func writeIssue(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# "+filepath.Base(path)+"\n\nsecret-free body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func scanOne(t *testing.T, dir string) ([]*Issue, []string) {
	t.Helper()
	found, warns := Scan([]string{dir})
	return found, warns
}

func TestScanAdoptsNextSymlinkAsMarkOnOriginal(t *testing.T) {
	dir := t.TempDir()
	writeIssue(t, filepath.Join(dir, "001-feat-a.md"))
	writeIssue(t, filepath.Join(dir, "002-feat-b.md"))
	symlink(t, "../001-feat-a.md", filepath.Join(dir, NextDirName, "001-feat-a.md"))

	found, warns := scanOne(t, dir)
	if len(warns) != 0 {
		t.Fatalf("正当な目印で警告が出た: %v", warns)
	}
	if len(found) != 2 {
		t.Fatalf("issue が %d 件 (symlink を別 issue として数えた?): %+v", len(found), found)
	}
	var a *Issue
	for _, iss := range found {
		if iss.Number == "001" {
			a = iss
		}
	}
	if a == nil || a.Status != StatusNext {
		t.Fatalf("目印が Status=Next にならない: %+v", a)
	}
	// 🚨 同一性 (Path) は直下のまま。symlink 側のパスを Path にすると本文を symlink 経由で読む
	if a.Path != filepath.Join(dir, "001-feat-a.md") || a.Rel != "001-feat-a.md" {
		t.Errorf("Path/Rel が symlink 側になった: path=%s rel=%s", a.Path, a.Rel)
	}
	if a.NextLink != filepath.Join(dir, NextDirName, "001-feat-a.md") {
		t.Errorf("NextLink が symlink を指さない: %q", a.NextLink)
	}
}

// 採用条件を満たさない symlink は issue にせず、警告にして無視する。
// 🚨 PR 経由で入りうる形 (repo 外を指す絶対パス / 連鎖) を含む。中身が本文として出ないことを
// Scan の結果 (Issue が増えない) で見る。
func TestScanRejectsNonCanonicalNextSymlinks(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "id_rsa.md")
	writeIssue(t, outside)
	cases := []struct {
		name   string
		setup  func(dir string)
		reason string
	}{
		{"repo 外を指す絶対パス", func(dir string) {
			symlink(t, outside, filepath.Join(dir, NextDirName, "009-feat-evil.md"))
		}, "../<同名> でない"},
		{"名前が違う", func(dir string) {
			writeIssue(t, filepath.Join(dir, "001-feat-a.md"))
			symlink(t, "../001-feat-a.md", filepath.Join(dir, NextDirName, "002-feat-b.md"))
		}, "../<同名> でない"},
		{"2 段上", func(dir string) {
			symlink(t, "../../001-feat-a.md", filepath.Join(dir, NextDirName, "001-feat-a.md"))
		}, "../<同名> でない"},
		{"指す先が無い (done へ動いた後)", func(dir string) {
			writeIssue(t, filepath.Join(dir, "done", "001-feat-a.md"))
			symlink(t, "../001-feat-a.md", filepath.Join(dir, NextDirName, "001-feat-a.md"))
		}, "指す先の issue が無い"},
		{"指す先が symlink (連鎖)", func(dir string) {
			symlink(t, outside, filepath.Join(dir, "001-feat-a.md"))
			symlink(t, "../001-feat-a.md", filepath.Join(dir, NextDirName, "001-feat-a.md"))
		}, "通常ファイルでない"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeIssue(t, filepath.Join(dir, "100-feat-anchor.md")) // ディレクトリを issue dir として成立させる
			c.setup(dir)
			found, warns := scanOne(t, dir)
			for _, iss := range found {
				if iss.Status == StatusNext {
					t.Errorf("不正な目印が採用された: %+v", iss)
				}
				if iss.Path == outside || strings.Contains(iss.Path, "id_rsa") {
					t.Errorf("repo 外のファイルが issue になった: %s", iss.Path)
				}
			}
			joined := strings.Join(warns, "\n")
			if !strings.Contains(joined, "next の目印を無視しました") || !strings.Contains(joined, c.reason) {
				t.Errorf("警告が出ない / 理由が違う (want %q): %v", c.reason, warns)
			}
		})
	}
}

// 旧運用 (ファイルそのものが next/ に居る) は引き続き読める。移行途中の repo が壊れない。
func TestScanStillReadsLegacyFileInNextDir(t *testing.T) {
	dir := t.TempDir()
	writeIssue(t, filepath.Join(dir, "002-feat-b.md"))
	writeIssue(t, filepath.Join(dir, NextDirName, "001-feat-a.md"))
	found, warns := scanOne(t, dir)
	if len(warns) != 0 || len(found) != 2 {
		t.Fatalf("旧運用の next/ が読めない: found=%d warns=%v", len(found), warns)
	}
	for _, iss := range found {
		if iss.Number == "001" && (iss.Status != StatusNext || iss.NextLink != "" || iss.Rel != filepath.Join(NextDirName, "001-feat-a.md")) {
			t.Errorf("旧運用の issue の読み方が変わった: %+v", iss)
		}
	}
}

func TestMoveToSubdirClaimPlacesSymlinkAndUnclaimRemovesIt(t *testing.T) {
	dir := t.TempDir()
	writeIssue(t, filepath.Join(dir, "001-feat-a.md"))
	found, _ := scanOne(t, dir)
	iss := found[0]

	got, err := MoveToSubdir(iss, NextDirName)
	if err != nil {
		t.Fatal(err)
	}
	if got != iss.Path {
		t.Errorf("claim で同一性 (Path) が変わった: %s → %s", iss.Path, got)
	}
	if _, err := os.Stat(iss.Path); err != nil {
		t.Fatalf("claim で実ファイルが動いた: %v", err)
	}
	link := filepath.Join(dir, NextDirName, "001-feat-a.md")
	if target, err := os.Readlink(link); err != nil || target != "../001-feat-a.md" {
		t.Fatalf("symlink の形が違う: target=%q err=%v", target, err)
	}
	after, warns := scanOne(t, dir)
	if len(warns) != 0 || len(after) != 1 || after[0].Status != StatusNext || after[0].NextLink != link {
		t.Fatalf("claim 後の再走査が Next にならない: %+v warns=%v", after, warns)
	}
	// 2 度目の claim は no-op (viewer は「変化なし」と数える)
	if got, err := MoveToSubdir(after[0], NextDirName); err != nil || got != iss.Path {
		t.Errorf("既に目印つきの claim が no-op でない: got=%s err=%v", got, err)
	}

	got, err = MoveToSubdir(after[0], "")
	if err != nil || got != iss.Path {
		t.Fatalf("解除が失敗 / Path が変わった: got=%s err=%v", got, err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("解除で symlink が消えない: %v", err)
	}
	final, _ := scanOne(t, dir)
	if len(final) != 1 || final[0].Status != StatusOpen {
		t.Errorf("解除後に open へ戻らない: %+v", final)
	}
}

func TestMoveToSubdirClaimRefusesToReplaceExistingEntry(t *testing.T) {
	dir := t.TempDir()
	writeIssue(t, filepath.Join(dir, "001-feat-a.md"))
	writeIssue(t, filepath.Join(dir, NextDirName, "001-feat-a.md")) // 旧運用の実ファイル (別内容) が既にある
	found, _ := scanOne(t, dir)
	var open *Issue
	for _, iss := range found {
		if iss.Status == StatusOpen {
			open = iss
		}
	}
	if open == nil {
		t.Fatal("前提: 直下の issue が見えない")
	}
	if _, err := MoveToSubdir(open, NextDirName); err == nil {
		t.Fatal("next/ に既にあるものを黙って置き換えた")
	}
	if fi, err := os.Lstat(filepath.Join(dir, NextDirName, "001-feat-a.md")); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("既存の実ファイルが壊れた: %v", err)
	}
}

// 直下に無い issue (done/) の claim は ../<base> が成立しないので従来の rename に倒す。
func TestMoveToSubdirClaimFromDoneStillRenames(t *testing.T) {
	dir := t.TempDir()
	writeIssue(t, filepath.Join(dir, "done", "001-feat-a.md"))
	writeIssue(t, filepath.Join(dir, "002-feat-b.md"))
	found, _ := scanOne(t, dir)
	var done *Issue
	for _, iss := range found {
		if iss.Status == StatusDone {
			done = iss
		}
	}
	got, err := MoveToSubdir(done, NextDirName)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, NextDirName, "001-feat-a.md") {
		t.Errorf("done からの claim が rename にならない: %s", got)
	}
	if fi, err := os.Lstat(got); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("rename 先が実ファイルでない: %v", err)
	}
}

func TestEpicChildClaimPlacesSymlinkInsideGroup(t *testing.T) {
	dir := t.TempDir()
	writeIssue(t, filepath.Join(dir, "100-feat-anchor.md"))
	child := filepath.Join(dir, EpicDirName, "cloud", "001-feat-a.md")
	writeIssue(t, child)
	found, _ := scanOne(t, dir)
	var iss *Issue
	for _, f := range found {
		if f.Path == child {
			iss = f
		}
	}
	if iss == nil || iss.GroupKind != GroupEpic {
		t.Fatalf("前提: epic の子が読めていない: %+v", iss)
	}
	if got, err := MoveToSubdir(iss, NextDirName); err != nil || got != child {
		t.Fatalf("epic の子の claim: got=%s err=%v", got, err)
	}
	link := filepath.Join(dir, EpicDirName, "cloud", NextDirName, "001-feat-a.md")
	if target, err := os.Readlink(link); err != nil || target != "../001-feat-a.md" {
		t.Fatalf("group 内の symlink の形が違う: target=%q err=%v", target, err)
	}
	after, warns := scanOne(t, dir)
	var claimed *Issue
	for _, f := range after {
		if f.Path == child {
			claimed = f
			if f.Status != StatusNext || f.GroupKind != GroupEpic || f.Group != "cloud" || f.NextLink != link {
				t.Errorf("claim 後の epic の子の読み方が違う: %+v", f)
			}
		}
	}
	if len(warns) != 0 || claimed == nil {
		t.Fatalf("警告が出た / 再走査で子が消えた: %v", warns)
	}
	// 解除は再走査後の Issue (NextLink を持つ) で行う。claim 前の値で呼ぶと目印を知らない
	if got, err := MoveToSubdir(claimed, ""); err != nil || got != child {
		t.Fatalf("epic の子の解除: got=%s err=%v", got, err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("解除で group 内の symlink が消えない: %v", err)
	}
}

// 解除は「今も目印 (symlink) か」を取り直してから消す。NextLink は前回走査時の値なので、
// git pull で next/<base> が旧運用の実ファイルに差し替わっていると、無条件の Remove は
// issue 本体 (直下に同名が無ければ唯一のコピー) を消す (敵対レビュー 2026-09-05 P1)。
func TestUnclaimRefusesToRemoveRegularFileAtNextLink(t *testing.T) {
	dir := t.TempDir()
	writeIssue(t, filepath.Join(dir, "001-feat-a.md"))
	symlink(t, "../001-feat-a.md", filepath.Join(dir, NextDirName, "001-feat-a.md"))
	found, _ := scanOne(t, dir)
	claimed := found[0]
	if claimed.NextLink == "" {
		t.Fatal("前提: 目印が読めていない")
	}
	// git pull 相当: symlink が実ファイルに差し替わる
	if err := os.Remove(claimed.NextLink); err != nil {
		t.Fatal(err)
	}
	writeIssue(t, claimed.NextLink)

	if _, err := MoveToSubdir(claimed, ""); err == nil {
		t.Fatal("実ファイルに差し替わった next/<base> を解除で消した")
	}
	if fi, err := os.Lstat(claimed.NextLink); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("実ファイルが消えた / 壊れた: %v", err)
	}
	// 目印が既に無い解除は成功 (意図は満たされている)
	if err := os.Remove(claimed.NextLink); err != nil {
		t.Fatal(err)
	}
	if _, err := MoveToSubdir(claimed, ""); err != nil {
		t.Errorf("目印が既に無い解除が失敗: %v", err)
	}
}

// next/ ディレクトリ自体が symlink なら丸ごと不採用 (hasMarkdown のディレクトリ symlink 拒否と同じ方針。
// 追うと PR で目印状態を捏造でき、解除の Remove が repo 外を消す先になる)。
func TestScanRejectsNextDirThatIsASymlink(t *testing.T) {
	dir := t.TempDir()
	evil := t.TempDir()
	writeIssue(t, filepath.Join(dir, "001-feat-a.md"))
	symlink(t, "../001-feat-a.md", filepath.Join(evil, "001-feat-a.md"))
	symlink(t, evil, filepath.Join(dir, NextDirName))

	found, warns := scanOne(t, dir)
	if len(found) != 1 || found[0].Status != StatusOpen || found[0].NextLink != "" {
		t.Fatalf("symlink の next/ 経由で目印が採用された: %+v", found)
	}
	if !strings.Contains(strings.Join(warns, "\n"), "next/ 自体が symlink") {
		t.Errorf("警告が出ない: %v", warns)
	}
}

// 3 条件を通っても直下のエントリ名と完全一致しない目印 (大文字小文字違い。APFS で手で張ると起きる)
// は、黙って捨てずに警告にする。
func TestScanWarnsOnNextLinkWithoutMatchingEntry(t *testing.T) {
	dir := t.TempDir()
	writeIssue(t, filepath.Join(dir, "001-feat-a.md"))
	// case-insensitive FS では ../001-FEAT-A.md は実在の 001-feat-a.md を指し、3 条件を通る
	symlink(t, "../001-FEAT-A.md", filepath.Join(dir, NextDirName, "001-FEAT-A.md"))
	found, warns := scanOne(t, dir)
	for _, iss := range found {
		if iss.Status == StatusNext {
			t.Errorf("名前が一致しない目印が採用された: %+v", iss)
		}
	}
	joined := strings.Join(warns, "\n")
	if !strings.Contains(joined, "next の目印を無視しました") {
		t.Errorf("名前が一致しない目印が黙って捨てられた: %v", warns)
	}
}

// meta ファイル (README.md 等) は直下でも issue に数えないので、目印にもしない。数えると照合相手が
// 無く「直下に同じ名前の issue が無い」という偽警告が毎スキャン出続ける (敵対レビュー 2 周目 P2-1)。
func TestNextLinkToMetaFileIsIgnoredSilently(t *testing.T) {
	dir := t.TempDir()
	writeIssue(t, filepath.Join(dir, "001-feat-a.md"))
	writeIssue(t, filepath.Join(dir, "README.md"))
	symlink(t, "../README.md", filepath.Join(dir, NextDirName, "README.md"))
	found, warns := scanOne(t, dir)
	if len(warns) != 0 {
		t.Errorf("meta ファイルの目印で警告が出た: %v", warns)
	}
	if len(found) != 1 {
		t.Errorf("README が issue に数えられた: %+v", found)
	}
}

// done/ pending/ へ運ぶときは目印を先に消す。残すと dangling になり、同じ base の issue が直下へ
// 戻った瞬間に古い目印が成立して偽の claim が共有される (敵対レビュー 2 周目 P3-2)。
func TestMoveToDoneRemovesNextLinkFirst(t *testing.T) {
	dir := t.TempDir()
	writeIssue(t, filepath.Join(dir, "001-feat-a.md"))
	symlink(t, "../001-feat-a.md", filepath.Join(dir, NextDirName, "001-feat-a.md"))
	found, _ := scanOne(t, dir)
	claimed := found[0]
	got, err := MoveToSubdir(claimed, "done")
	if err != nil || got != filepath.Join(dir, "done", "001-feat-a.md") {
		t.Fatalf("done への移動: got=%s err=%v", got, err)
	}
	if _, err := os.Lstat(claimed.NextLink); !os.IsNotExist(err) {
		t.Fatalf("done へ運んだ後も目印が残っている (dangling): %v", err)
	}
	// 直下へ戻しても Next に復活しない
	if err := os.Rename(got, claimed.Path); err != nil {
		t.Fatal(err)
	}
	after, warns := scanOne(t, dir)
	if len(after) != 1 || after[0].Status != StatusOpen || len(warns) != 0 {
		t.Errorf("再 open で偽の claim が復活した: %+v warns=%v", after, warns)
	}
}
