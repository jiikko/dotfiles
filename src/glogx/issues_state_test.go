package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"glogx/issues"
)

// 保存 → 読み出しの往復。パスで指すので、番号や basename が重複していても取り違えない。
func TestIssuesScreenRoundTrip(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	now := time.Unix(1000, 0)
	want := issuesScreen{
		Root: "/repo", SavedAt: now, Tab: "refactor", Filter: uint8(issues.FilterPending),
		Cursor: "/repo/issues/028-refactor-c.md", Open: "/repo/issues/029-feat-b.md", BodyOff: 12,
	}
	if err := saveIssuesScreen(want); err != nil {
		t.Fatal(err)
	}
	got, ok := loadIssuesScreen(now.Add(time.Minute))
	if !ok {
		t.Fatal("保存した画面が読めない")
	}
	if got.Root != want.Root || got.Tab != want.Tab || got.Filter != want.Filter ||
		got.Cursor != want.Cursor || got.Open != want.Open || got.BodyOff != want.BodyOff {
		t.Fatalf("往復で内容が変わった:\n got=%+v\nwant=%+v", got, want)
	}
}

// TTL 切れ・未来の時刻・壊れたファイル・不在は、すべて「記憶なし」に落とす
// (記憶の都合で起動を失敗させない)。
func TestIssuesScreenIgnoresStaleAndBroken(t *testing.T) {
	now := time.Unix(100000, 0)
	cases := []struct {
		name  string
		write func(t *testing.T, path string)
		at    time.Time
	}{
		{"不在", func(*testing.T, string) {}, now},
		{"TTL 切れ", func(t *testing.T, string2 string) {
			t.Helper()
			if err := saveIssuesScreen(issuesScreen{Root: "/repo", SavedAt: now}); err != nil {
				t.Fatal(err)
			}
		}, now.Add(issuesStateTTL)},
		{"未来の時刻 (時計のずれ)", func(t *testing.T, _ string) {
			t.Helper()
			if err := saveIssuesScreen(issuesScreen{Root: "/repo", SavedAt: now.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
		}, now},
		{"壊れた JSON", func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, now},
		{"root なし", func(t *testing.T, _ string) {
			t.Helper()
			if err := saveIssuesScreen(issuesScreen{SavedAt: now}); err != nil {
				t.Fatal(err)
			}
		}, now},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			t.Setenv("XDG_CACHE_HOME", base)
			path := filepath.Join(base, "glog", "issues-last-screen.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			tc.write(t, path)
			if _, ok := loadIssuesScreen(tc.at); ok {
				t.Fatal("使ってはいけない記憶が返った")
			}
		})
	}
}

// 外部ファイル由来の壊れた段階値は既定 (open のみ) へ倒す。
func TestIssuesScreenClampsBrokenFilter(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	now := time.Unix(1000, 0)
	if err := saveIssuesScreen(issuesScreen{Root: "/repo", SavedAt: now, Filter: 99, BodyOff: -5}); err != nil {
		t.Fatal(err)
	}
	got, ok := loadIssuesScreen(now)
	if !ok {
		t.Fatal("読めない")
	}
	if issues.StatusFilter(got.Filter) != issues.FilterOpen || got.BodyOff != 0 {
		t.Fatalf("壊れた値が正規化されていない: filter=%d bodyOff=%d", got.Filter, got.BodyOff)
	}
}

// 終了時に覚えるのは viewer を出していたときだけ。一覧 (git log) から終了したら記憶を消す
// (残すと「一覧を見て閉じたのに次の起動で viewer が出る」)。
func TestQuitRemembersOnlyWhenViewerVisible(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.issuesOv = *loadedView(sampleIssues()...)
	m.issuesOv.root = "/repo"
	m.quit()
	// 読むのは保存より後の時刻で (SavedAt が読み取り時刻より未来なら時計のずれとして捨てる仕様)
	s, ok := loadIssuesScreen(timeNow())
	if !ok {
		t.Fatal("viewer を出したまま終了したのに記憶が無い")
	}
	if s.Root != "/repo" {
		t.Fatalf("記憶の root が違う: %q", s.Root)
	}

	m2 := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m2.quit() // 一覧から終了
	if _, ok := loadIssuesScreen(timeNow()); ok {
		t.Fatal("一覧から終了したのに記憶が残っている")
	}
}

// スキャン前 (root 未確定) は覚えない: 照合キーの無い記憶は別 repo で当たってしまう。
func TestScreenNotSavedBeforeScan(t *testing.T) {
	v := newIssuesView()
	v.shown = true
	if _, ok := v.screen(timeNow()); ok {
		t.Fatal("スキャン前の viewer を覚えてしまった")
	}
}

// 閉じる演出の途中 (h を押した直後に C-g) は「開いている」として覚えない。
func TestScreenDoesNotRememberClosingBody(t *testing.T) {
	v := loadedView(sampleIssues()...)
	v.root = "/repo"
	v.open = v.rows[0]
	v.drawer.open(timeNow())
	v.closeBody()
	s, ok := v.screen(timeNow())
	if !ok {
		t.Fatal("覚えられていない")
	}
	if s.Open != "" {
		t.Fatalf("閉じかけの本文を開いた状態で覚えた: %q", s.Open)
	}
}

// 復元の本体: タブ・フィルタ・カーソル・本文・スクロール位置が戻り、開く演出は出ない。
func TestIssuesViewRestoreAppliesScreen(t *testing.T) {
	dir := t.TempDir()
	body := "# 029 feat: 復元の対象\n\n" + strings.Repeat("段落。\n\n", 40)
	path := filepath.Join(dir, "029-feat-b.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	target := &issues.Issue{Path: path, Dir: dir, Rel: "029-feat-b.md", Number: "029", Category: "feat", Slug: "b"}
	// feat を独立したタブにするには TabMinCount (2) 件要る。1 件だと other へ寄せられ、
	// 「復元したいタブがそもそも存在しない」テストになってしまう
	sibling := &issues.Issue{Path: filepath.Join(dir, "030-feat-a.md"), Dir: dir,
		Rel: "030-feat-a.md", Number: "030", Category: "feat", Slug: "a"}
	other := &issues.Issue{Path: filepath.Join(dir, "028-refactor-c.md"), Dir: dir,
		Rel: "028-refactor-c.md", Number: "028", Category: "refactor", Slug: "c"}

	v := newIssuesView()
	cmd := v.restore(dir, issuesScreen{
		Root: dir, SavedAt: timeNow(), Tab: "feat", Filter: uint8(issues.FilterAll),
		Cursor: target.Path, Open: target.Path, BodyOff: 7,
	})
	if cmd == nil || !v.visible() {
		t.Fatal("復元でスキャンが始まらない / viewer が開いていない")
	}
	if v.animating() {
		t.Fatal("復元で開く演出が始まっている (閉じたところから再開のはず)")
	}
	v.receive(issuesScanMsg{root: dir, dirs: []string{dir}, issues: []*issues.Issue{target, sibling, other}})

	if v.filter != issues.FilterAll {
		t.Fatalf("フィルタが戻っていない: %d", v.filter)
	}
	if v.currentTab() != "feat" {
		t.Fatalf("タブが戻っていない: %q", v.currentTab())
	}
	if v.current() == nil || v.current().Path != target.Path {
		t.Fatalf("カーソルが戻っていない: %+v", v.current())
	}
	if v.open == nil || v.open.Path != target.Path || v.body == nil {
		t.Fatal("本文が開いていない")
	}
	if v.drawer.phase != drawerOpen {
		t.Fatalf("引き出しが開き切っていない (演出が残っている): phase=%d", v.drawer.phase)
	}
	out := strings.Join(v.lines(renderOpts(40)), "\n")
	if !strings.Contains(out, "029-feat-b.md") {
		t.Fatalf("復元した本文が描かれていない:\n%s", out)
	}
	if v.bodyOff != 7 {
		t.Fatalf("本文のスクロール位置が戻っていない: %d", v.bodyOff)
	}
	// 復元は 1 度だけ: 以降の再スキャン (r) は「今見ている場所」を引き継ぐ。カーソルを記憶と
	// 別の行 (feat タブの先頭 = 030) へ動かしてから取り直し、記憶の 029 へ戻らないことを見る
	v.cursor = 1
	v.receive(issuesScanMsg{root: dir, dirs: []string{dir}, issues: []*issues.Issue{target, sibling, other}})
	if v.current() == nil || v.current().Path != sibling.Path {
		t.Fatalf("再スキャンで復元がもう一度当たった: %+v", v.current())
	}
}

// 記憶した issue が消えていても (rename / done へ移動)、黙って別物を出さない。
func TestIssuesViewRestoreMissingIssueFallsBack(t *testing.T) {
	dir := t.TempDir()
	alive := &issues.Issue{Path: filepath.Join(dir, "028-refactor-c.md"), Dir: dir,
		Rel: "028-refactor-c.md", Number: "028", Category: "refactor", Slug: "c"}
	v := newIssuesView()
	v.restore(dir, issuesScreen{
		Root: dir, SavedAt: timeNow(), Cursor: filepath.Join(dir, "消えた.md"),
		Open: filepath.Join(dir, "消えた.md"),
	})
	v.receive(issuesScanMsg{root: dir, dirs: []string{dir}, issues: []*issues.Issue{alive}})
	if v.open != nil {
		t.Fatalf("消えた issue の本文を開いた: %+v", v.open)
	}
	if v.cursor != 0 {
		t.Fatalf("カーソルが先頭に落ちていない: %d", v.cursor)
	}
}

// 復元の照合中に i が押されていたら、開いている viewer を上書きしない。
func TestIssuesViewRestoreDoesNotClobberOpenViewer(t *testing.T) {
	v := loadedView(sampleIssues()...)
	before := v.currentTab()
	if cmd := v.restore("/repo", issuesScreen{Root: "/repo", SavedAt: timeNow(), Tab: "feat"}); cmd != nil {
		t.Fatal("既に開いている viewer に復元のスキャンを走らせた")
	}
	if v.pending != nil || v.currentTab() != before {
		t.Fatal("既に開いている viewer の状態を復元が書き換えた")
	}
}
