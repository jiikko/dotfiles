package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLogKeyHandling(t *testing.T) {
	m := newLogModel([]Commit{{SHA: "a", ShortSHA: "a", Subject: "one"}, {SHA: "b", ShortSHA: "b", Subject: "two"}})
	m.handleKey("j")
	if m.cursor != 1 {
		t.Fatalf("j cursor = %d, want 1", m.cursor)
	}
	m.handleKey("j")
	m.handleKey("k")
	m.handleKey("k")
	if m.cursor != 0 {
		t.Fatalf("clamped cursor = %d, want 0", m.cursor)
	}
	// emacs 流 C-n/C-p でも上下移動できる
	m.handleKey("ctrl+n")
	if m.cursor != 1 {
		t.Fatalf("ctrl+n cursor = %d, want 1", m.cursor)
	}
	m.handleKey("ctrl+p")
	if m.cursor != 0 {
		t.Fatalf("ctrl+p cursor = %d, want 0", m.cursor)
	}
	// → / C-f で詳細モードへ (Enter の別名)、h で一覧へ戻る
	for _, k := range []string{"right", "ctrl+f", "enter"} {
		m.handleKey(k)
		if !m.detailOpen {
			t.Fatalf("%s で詳細モードに入らない", k)
		}
		m.handleKey("h")
		if m.detailOpen {
			t.Fatalf("h で一覧へ戻らない (%s 後)", k)
		}
	}
	m.Update(tea.KeyMsg{})
	m.handleKey("q")
	if !m.done {
		t.Fatal("q did not set done")
	}
}

func TestLogScrollKeepsCursorVisible(t *testing.T) {
	commits := make([]Commit, 5)
	for i := range commits {
		commits[i] = Commit{SHA: string(rune('a' + i)), ShortSHA: "sha", Subject: "subject"}
	}
	m := newLogModel(commits)
	m.height = 5 // paneRows = height-3(footer+border) = 2 行
	m.handleKey("j")
	m.handleKey("j")
	if m.cursor != 2 || m.offset != 1 {
		t.Fatalf("after moving down: cursor=%d offset=%d, want 2/1", m.cursor, m.offset)
	}
	m.handleKey("k")
	m.handleKey("k")
	if m.cursor != 0 || m.offset != 0 {
		t.Fatalf("after moving up: cursor=%d offset=%d, want 0/0", m.cursor, m.offset)
	}
}

func TestLogViewMinSizeGuard(t *testing.T) {
	m := newLogModel([]Commit{{SHA: "a", ShortSHA: "a", Subject: "x"}})
	m.width, m.height = 10, 3 // 最小未満: ボーダー2ペインを出さず単一行に degrade
	got := m.View()
	if strings.Contains(got, "\n") || strings.Contains(got, "╭") {
		t.Fatalf("極小端末でボーダー2ペインを出している (単一行に degrade すべき): %q", got)
	}
	if !strings.HasPrefix(stripANSI(got), "git-popup") {
		t.Fatalf("degrade メッセージでない: %q", got)
	}
	m.width, m.height = 0, 0 // サイズ未確定 (WindowSizeMsg 前) は空
	if m.View() != "" {
		t.Fatalf("サイズ未確定で空を返さない")
	}
}

func TestLogListLinesPushColor(t *testing.T) {
	m := newLogModel([]Commit{{SHA: "p1", ShortSHA: "pushed1", Subject: "s"}, {SHA: "u1", ShortSHA: "unpush1", Subject: "s"}})
	m.unpushed = map[string]bool{"u1": true}
	lines := m.buildListLines(60, 2)
	// カーソル行 (0) は accent の ▌
	if !strings.Contains(lines[0], "▌") {
		t.Errorf("カーソル行に ▌ が無い: %q", lines[0])
	}
	// 未 push は marker_orange・push 済みは cold_gray
	if !strings.Contains(lines[1], fg("marker_orange")) {
		t.Errorf("未 push SHA が marker_orange でない: %q", lines[1])
	}
	if !strings.Contains(lines[0], fg("cold_gray")) {
		t.Errorf("push 済み SHA が cold_gray でない: %q", lines[0])
	}
}

func TestLogDetailCIOverlay(t *testing.T) {
	m := newLogModel([]Commit{{SHA: "a"}})
	m.preview = "diff-a\ndiff-b\ndiff-c\ndiff-d\ndiff-e"
	m.ciJobs = []CIJob{{State: "success", Name: "job1"}}
	// 一覧モード: CI は上部にオーバーレイし diff を押し下げない
	// (render は ヘッダ + job1 + フッタ の 3 行。その下は diff の続き = 4 行目)
	lines := m.buildDetailLines(40, 5)
	if !strings.Contains(stripANSI(lines[0]), "── CI ──") || !strings.Contains(stripANSI(lines[1]), "✓ job1") {
		t.Fatalf("CI が上部オーバーレイになっていない: %q", lines)
	}
	if stripANSI(lines[3]) != "diff-d" || stripANSI(lines[4]) != "diff-e" {
		t.Fatalf("diff の下部が押し下げられている (占有 overlay のはず): %q", lines)
	}
	// 詳細モード (ジョブ選択なし): オーバーレイせず detailOffset から diff を出す
	m.detailOpen = true
	m.detailOffset = 1
	d := m.buildDetailLines(40, 3)
	if stripANSI(d[0]) != "diff-b" {
		t.Fatalf("詳細スクロールが offset どおりでない: %q", d)
	}
	// ジョブ選択モード: 詳細中でも CI オーバーレイを出し選択行に ▌
	m.jobSelect = true
	m.jobCursor = 0
	js := m.buildDetailLines(40, 3)
	if !strings.Contains(stripANSI(js[1]), "▌") || !strings.Contains(stripANSI(js[1]), "job1") {
		t.Fatalf("ジョブ選択中のカーソルが出ていない: %q", js)
	}
}

func TestLogListTopBottom(t *testing.T) {
	m := newLogModel([]Commit{{SHA: "a"}, {SHA: "b"}, {SHA: "c"}})
	m.handleKey("G")
	if m.cursor != 2 {
		t.Fatalf("G で末尾に行かない: %d", m.cursor)
	}
	m.handleKey("g")
	if m.cursor != 0 {
		t.Fatalf("g で先頭に戻らない: %d", m.cursor)
	}
}

func TestLogJobsSwapClampsCursor(t *testing.T) {
	// ジョブ選択中に同一 sha の ciJobsMsg が空へ差し替わっても panic せず選択モードを解除する
	m := newLogModel([]Commit{{SHA: "a"}})
	m.ciJobs = []CIJob{{State: "success", Name: "j1"}, {State: "success", Name: "j2"}}
	m.handleKey("enter") // 詳細へ
	m.handleKey("enter") // ジョブ選択へ (Enter 二段)
	m.handleKey("j")     // jobCursor=1
	m.Update(ciJobsMsg{sha: "a", jobs: nil})
	if m.jobSelect {
		t.Fatalf("空差し替えでジョブ選択が解除されない")
	}
	m.handleKey("j") // panic しない (負 index に落ちない)
	// 1 件へ縮む差し替えではカーソルがクランプされる
	m.ciJobs = []CIJob{{Name: "j1"}, {Name: "j2"}}
	m.jobSelect, m.jobCursor = true, 1
	m.Update(ciJobsMsg{sha: "a", jobs: []CIJob{{Name: "only"}}})
	if m.jobCursor != 0 {
		t.Fatalf("縮小差し替えで jobCursor がクランプされない: %d", m.jobCursor)
	}
}

func TestLogStatusShownInFooter(t *testing.T) {
	// 詳細/ジョブ選択中でも status (opened:/エラー) が footer に出る (操作説明より優先)
	m := newLogModel([]Commit{{SHA: strings.Repeat("a", 40), ShortSHA: "abc", Subject: "s"}})
	m.width, m.height = 80, 10
	m.handleKey("enter")
	m.status = "opened: build"
	if v := m.View(); !strings.Contains(stripANSI(v), "opened: build") {
		t.Fatalf("詳細中の status が footer に出ない")
	}
	// 詳細へ入り直すと status はクリアされ操作説明に戻る
	m.handleKey("esc")
	m.handleKey("enter")
	if v := m.View(); strings.Contains(stripANSI(v), "opened: build") {
		t.Fatalf("詳細入場で status がクリアされない")
	}
}

func TestLogDetailKeys(t *testing.T) {
	m := newLogModel([]Commit{{SHA: "a"}})
	m.ciJobs = []CIJob{{State: "success", Name: "job1", URL: "https://x/1"}}
	m.handleKey("enter") // 詳細へ
	// C-g はモーダル (詳細) を閉じて一覧へ (quit しない)
	m.handleKey("ctrl+g")
	if m.detailOpen || m.done {
		t.Fatalf("詳細の C-g で一覧へ戻るべき: detailOpen=%v done=%v", m.detailOpen, m.done)
	}
	// 詳細で q はプログラム終了
	m.handleKey("enter")
	if _, cmd := m.handleKey("q"); cmd == nil || !m.done {
		t.Fatalf("詳細の q で終了しない")
	}
	// ジョブ選択 → Enter でブラウザ (差し替えた opener が呼ばれる)
	m2 := newLogModel([]Commit{{SHA: "a"}})
	m2.ciJobs = []CIJob{{State: "success", Name: "job1", URL: "https://x/1"}}
	var opened string
	orig := openInBrowser
	openInBrowser = func(url string) error { opened = url; return nil }
	t.Cleanup(func() { openInBrowser = orig })
	m2.handleKey("enter") // 詳細へ
	m2.handleKey("enter") // ジョブ選択へ (Enter 二段)
	if !m2.jobSelect {
		t.Fatalf("詳細の Enter でジョブ選択に入らない")
	}
	m2.handleKey("enter")
	if opened != "https://x/1" {
		t.Fatalf("Enter でブラウザを開かない: %q", opened)
	}
	m2.handleKey("ctrl+g") // C-g でジョブ選択を閉じる (詳細に留まる)
	if m2.jobSelect || !m2.detailOpen {
		t.Fatalf("ジョブ選択の C-g でサブモードだけ閉じるべき")
	}
}

func TestLogDetailEmptyPreview(t *testing.T) {
	m := newLogModel([]Commit{{SHA: "a"}})
	m.preview = "" // preview 未着
	lines := m.buildDetailLines(40, 3)
	if !strings.Contains(stripANSI(lines[0]), "no preview") {
		t.Errorf("空 preview で (no preview) を出さない: %q", lines[0])
	}
}

func TestClipANSIAware(t *testing.T) {
	// 色エスケープは可視幅に数えない: "✓ hello" (可視 7 桁) は width 7 でそのまま返る
	colored := "\x1b[38;5;2m✓\x1b[0m hello"
	if got := clip(colored, 7); got != colored {
		t.Errorf("色付きで幅内なのに truncate された: %q", got)
	}
	// 幅を超えたら可視幅で切り、… と reset を付ける (エスケープ途中で切らない)
	got := clip(colored, 4)
	if displayWidth(got) > 4 {
		t.Errorf("clip 後の可視幅 %d > 4: %q", displayWidth(got), got)
	}
	if !strings.HasSuffix(got, "…\x1b[0m") {
		t.Errorf("truncate 時に …+reset が付いていない: %q", got)
	}
	if strings.Count(got, "\x1b[38;5;2m") != 1 {
		t.Errorf("先頭の色エスケープが保持されていない: %q", got)
	}
}

func TestLogCIMark(t *testing.T) {
	m := newLogModel([]Commit{{SHA: "sha"}})
	if got := m.ciMark("sha"); got != ansiDim+"·"+ansiReset {
		t.Fatalf("loading mark = %q", got)
	}
	// 色は theme (active_green) から引く。直値でなく paintFg で期待値を作り theme に追従させる
	m.ci = map[string]CIState{"sha": CISuccess}
	if got := m.ciMark("sha"); got != paintFg("active_green", "✓") {
		t.Fatalf("success mark = %q", got)
	}
	if stripANSI(m.ciMark("sha")) != "✓" {
		t.Fatalf("success mark 可視文字 = %q", stripANSI(m.ciMark("sha")))
	}
}
