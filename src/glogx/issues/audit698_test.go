package issues

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// 同名の二重化 (内容の喪失) の警告は、壊れた目印の警告より前に来る (viewer のヘッダーは先頭の 1 本を出す。issue 698 の 1)。
func TestScanPutsConflictsFirst(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "issues")
	mkFiles(t, dir, "001-a.md")
	mkFiles(t, filepath.Join(dir, "done"), "001-a.md")
	symlink(t, "../999-gone.md", filepath.Join(dir, "next", "999-gone.md")) // 指す先の無い目印 (警告になる)
	_, warns := Scan([]string{dir})
	if len(warns) < 2 {
		t.Fatalf("前提: 警告が 2 本以上出る: %q", warns)
	}
	if !strings.Contains(warns[0], "同じファイル名") {
		t.Fatalf("先頭の警告が二重化でない: %q", warns)
	}
}

// 読めない状態のフォルダは警告に出す (黙って空にすると「全部消えた」と見分けられない。issue 698 の 2)。
func TestScanWarnsUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root は権限で読めなくならない")
	}
	dir := filepath.Join(t.TempDir(), "issues")
	mkFiles(t, dir, "001-a.md")
	mkFiles(t, filepath.Join(dir, "done"), "002-b.md")
	mkFiles(t, filepath.Join(dir, "epic", "g", "done"), "003-c.md")
	for _, d := range []string{filepath.Join(dir, "done"), filepath.Join(dir, "epic", "g", "done")} {
		if err := os.Chmod(d, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(d, 0o755) })
	}
	_, warns := Scan([]string{dir})
	n := 0
	for _, w := range warns {
		if strings.Contains(w, "読めないフォルダ") {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("読めないフォルダの警告が %d 本 (done と epic の done の 2 本のはず): %q", n, warns)
	}
}

// URL は文字の向きを変える制御文字・行の区切りで切る (混ざると一覧の見た目と開く先が食い違う。issue 698 の 3)。
func TestURLsStopAtBidiControls(t *testing.T) {
	for _, c := range []string{string(rune(0x202e)), string(rune(0x2066)), string(rune(0x2028)), string(rune(0x2029))} {
		b := &Body{src: "see https://example.com/a" + c + "b/c done"}
		got := b.URLs()
		if len(got) != 1 || got[0] != "https://example.com/a" {
			t.Errorf("%U: URLs = %q (制御文字の手前で切るはず)", []rune(c)[0], got)
		}
	}
}

// FIFO の NNN-x.md は issue として拾わない (読むと戻らない。issue 698 の 4)。
func TestScanSkipsFIFO(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "issues")
	mkFiles(t, dir, "001-a.md")
	if err := syscall.Mkfifo(filepath.Join(dir, "002-fifo.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := Scan([]string{dir})
	if len(got) != 1 || filepath.Base(got[0].Rel) != "001-a.md" {
		t.Fatalf("FIFO を拾った: %d 件", len(got))
	}
}

// front matter のある本文では、バナーを front matter と H1 の後ろに入れる (前に入れると front matter と見なくなり、
// status の食い違いの 🚨 が消える)。外すと元に戻る。CRLF の本文には CRLF で足す (issue 698 の 5・6)。
func TestAddBannerAfterFrontMatterAndKeepsCRLF(t *testing.T) {
	const banner = "> 🚨 **担当中: h (glogx)**（2026-10-09〜）"
	for _, eol := range []string{"\n", "\r\n"} {
		src := strings.ReplaceAll("---\nstatus: pending\n---\n\n# T\n\nbody\n", "\n", eol)
		got, changed := addBanner(src, banner)
		if !changed {
			t.Fatal("バナーが入らない")
		}
		want := strings.ReplaceAll("---\nstatus: pending\n---\n\n# T\n\n"+banner+"\n\nbody\n", "\n", eol)
		if got != want {
			t.Fatalf("eol %q: 入れた形\n%q\nwant\n%q", eol, got, want)
		}
		path := filepath.Join(t.TempDir(), "001-x.md")
		writeFileContent(t, path, got)
		iss := newIssue(filepath.Dir(path), filepath.Base(path))
		if err := iss.LoadMeta(); err != nil || iss.Declared != "pending" {
			t.Fatalf("eol %q: バナーを入れた後に front matter の status を読めない (%q, %v)", eol, iss.Declared, err)
		}
		if back, _ := removeBanner(got); back != src {
			t.Fatalf("eol %q: 外しても元に戻らない\n%q", eol, back)
		}
	}
}

// 日付の名前 (20260101-notes.md) や桁あふれする数字は採番の番号と見なさない (issue 698 の 7)。
func TestNextNumberIgnoresDateLikeNames(t *testing.T) {
	list := []*Issue{{Number: "041"}, {Number: "20260101"}, {Number: "9223372036854775807"}, {Number: "123456"}}
	if got := NextNumber(list); got != "123457" {
		t.Fatalf("次番号 = %q (6 桁までを番号と見て 123457 のはず)", got)
	}
	if got := NextNumber([]*Issue{{Number: "041"}, {Number: "20260101"}}); got != "042" {
		t.Fatalf("次番号 = %q (日付を番号と読んでいる)", got)
	}
}
