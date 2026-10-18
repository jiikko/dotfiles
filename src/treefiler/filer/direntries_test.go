//go:build darwin

package filer

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// nastyDir は読み方で食い違いやすい項目を混ぜたフォルダ。
func nastyDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	mustWrite(t, filepath.Join(d, "file"), "hello")
	mustWrite(t, filepath.Join(d, ".hidden"), "h")
	if err := os.Mkdir(filepath.Join(d, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(d, "sub", "x"), "x")
	if err := os.Mkdir(filepath.Join(d, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(d, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(locked, "in"), "in")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	for name, target := range map[string]string{"rel": "file", "abs": filepath.Join(d, "sub"), "dangling": "/nonexistent/" + strings.Repeat("t", 200)} {
		if err := os.Symlink(target, filepath.Join(d, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Mkfifo(filepath.Join(d, "fifo"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"café", "caféx", "日本語の名前", strings.Repeat("あ", 85)} { // 85 × 3 = 255 バイト
		mustWrite(t, filepath.Join(d, n), n)
	}
	if err := os.WriteFile(filepath.Join(d, "big"), make([]byte, 300<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

func byName(es []dirEntry) map[string]entryAttr {
	m := make(map[string]entryAttr, len(es))
	for _, e := range es {
		m[e.name] = e.entryAttr
	}
	return m
}

func sameEntries(t *testing.T, what string, got, want []dirEntry) {
	t.Helper()
	g, w := byName(got), byName(want)
	if len(got) != len(g) || len(want) != len(w) {
		t.Fatalf("%s: 同じ名前が 2 回出た (got %d / %d, want %d / %d)", what, len(got), len(g), len(want), len(w))
	}
	for name, wa := range w { // 両方の側の名前を回す (片方にだけ在る項目を件数でなく名前で拾う)
		ga, ok := g[name]
		if !ok {
			t.Errorf("%s: %q が無い", what, name)
			continue
		}
		if ga.dir != wa.dir || ga.size != wa.size || !ga.mtime.Equal(wa.mtime) {
			t.Errorf("%s: %q got %+v want %+v", what, name, ga, wa)
		}
	}
	for name := range g {
		if _, ok := w[name]; !ok {
			t.Errorf("%s: 余分な %q", what, name)
		}
	}
}

// まとめて読む経路は、今の形 (ReadDir + lstat) と同じ項目・同じ属性を返す。macOS では本当にまとめて読めていること (戻っていないこと) も見る。
func TestBulkMatchesReadDirLstat(t *testing.T) {
	d := nastyDir(t)
	want, err := readEntriesSlow(d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readEntriesBulk(d)
	if err != nil {
		t.Fatalf("まとめて読めない (今の形へ戻る経路しか試せていない): %v", err)
	}
	sameEntries(t, "bulk", got, want)
	if len(want) < 14 {
		t.Fatalf("前提: 項目が少ない (%d)", len(want))
	}
}

// 受け皿 (64KB) に入りきらない件数でも、続きを読んで全部返す。
func TestBulkReadsAcrossBuffers(t *testing.T) {
	d := t.TempDir()
	long := strings.Repeat("n", 200)
	for i := range 1500 { // 1 件 約 270 バイト × 1500 ≒ 400KB
		mustWrite(t, filepath.Join(d, long+strconv.Itoa(i)), "")
	}
	want, err := readEntriesSlow(d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readEntriesBulk(d)
	if err != nil {
		t.Fatal(err)
	}
	sameEntries(t, "1500 件", got, want)
}

// まとめて読めないときは今の形で読み、同じ結果を返す。フォルダ自体が読めない error は os.ReadDir と同じ。
func TestReadEntriesFallsBack(t *testing.T) {
	d := nastyDir(t)
	old := readBulk
	t.Cleanup(func() { readBulk = old })
	calls := 0
	readBulk = func(string) ([]dirEntry, error) { calls++; return nil, errNoBulk }
	got, err := readEntries(d)
	if err != nil || calls == 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	want, _ := readEntriesSlow(d)
	sameEntries(t, "戻った経路", got, want)
	readBulk = old
	for _, p := range []string{filepath.Join(d, "nope"), filepath.Join(d, "file"), filepath.Join(d, "locked")} {
		_, werr := os.ReadDir(p)
		_, gerr := readEntries(p)
		if (werr == nil) != (gerr == nil) || (werr != nil && !errors.Is(gerr, errors.Unwrap(werr))) {
			t.Errorf("%s: err = %v, want %v", filepath.Base(p), gerr, werr)
		}
	}
}

// 壊れた記録は読み違えずに errNoBulk (今の形へ戻る)。
func TestParseBulkRecordRejectsMalformed(t *testing.T) {
	rec := make([]byte, 72)
	le := binary.LittleEndian
	le.PutUint32(rec, 72)
	le.PutUint32(rec[4:], attrCmnName|attrCmnObjType|attrCmnModTime|attrCmnError|attrCmnReturnedAttrs)
	le.PutUint32(rec[16:], attrFileDataLength)
	le.PutUint32(rec[28:], uint32(64-28)) // 名前は 64 バイト目から
	le.PutUint32(rec[32:], 2)
	copy(rec[64:], "a\x00")
	le.PutUint64(rec[56:], 5)
	if e, l, err := parseBulkRecord("/", rec); err != nil || l != 72 || e.name != "a" || e.size != 5 {
		t.Fatalf("正しい記録を読めない: %+v %d %v", e, l, err)
	}
	cases := map[string]func([]byte) []byte{
		"短い":       func(b []byte) []byte { return b[:3] },
		"長さが大きすぎる": func(b []byte) []byte { le.PutUint32(b, 1000); return b },
		"長さが小さすぎる": func(b []byte) []byte { le.PutUint32(b, 20); return b },
		"名前が外":     func(b []byte) []byte { le.PutUint32(b[28:], 1000); return b },
		"名前が前":     func(b []byte) []byte { le.PutUint32(b[28:], 0xfffffff0); return b },
		"名前の長さ 0":  func(b []byte) []byte { le.PutUint32(b[32:], 0); return b },
		"名前が返らない":  func(b []byte) []byte { le.PutUint32(b[4:], attrCmnObjType|attrCmnModTime); return b },
	}
	for what, f := range cases {
		b := f(slices.Clone(rec))
		if _, _, err := parseBulkRecord("/", b); !errors.Is(err, errNoBulk) {
			t.Errorf("%s: err = %v (errNoBulk で今の形へ戻ること)", what, err)
		}
	}
}

// 大きさが返らない・ERROR が立った項目は、その項目だけ lstat で読み直す (並びの位置を推測しない)。
func TestParseBulkRecordFallsBackPerEntry(t *testing.T) {
	d := t.TempDir()
	mustWrite(t, filepath.Join(d, "a"), "12345")
	info, err := os.Lstat(filepath.Join(d, "a"))
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	mk := func(fileA, errCode uint32) []byte {
		rec := make([]byte, 72)
		le.PutUint32(rec, 72)
		le.PutUint32(rec[4:], attrCmnName|attrCmnObjType|attrCmnModTime|attrCmnError|attrCmnReturnedAttrs)
		le.PutUint32(rec[16:], fileA)
		le.PutUint32(rec[24:], errCode)
		le.PutUint32(rec[28:], uint32(64-28))
		le.PutUint32(rec[32:], 2)
		copy(rec[64:], "a\x00")
		le.PutUint64(rec[56:], 999) // lstat に戻ったなら使われない値
		return rec
	}
	for what, rec := range map[string][]byte{"大きさが返らない": mk(0, 0), "ERROR": mk(attrFileDataLength, 13)} {
		e, _, err := parseBulkRecord(d, rec)
		if err != nil || e.name != "a" || e.size != info.Size() || !e.mtime.Equal(info.ModTime()) {
			t.Errorf("%s: %+v %v (lstat の値 %d で読み直すこと)", what, e, err, info.Size())
		}
	}
}

// / はファームリンク (Users・Applications 等) とマウントポイントを持つ。bulk はそこで覆われた側の値を返すので、フォルダは lstat の値で揃える
// (どの macOS の機械・CI の runner にも在る実物で確かめる)。
func TestBulkMatchesAtFirmlinksAndMountPoints(t *testing.T) {
	for _, d := range []string{"/", "/System/Volumes", "/Volumes"} {
		want, err := readEntriesSlow(d)
		if err != nil {
			t.Fatal(err)
		}
		got, err := readEntriesBulk(d)
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		sameEntries(t, d, got, want)
	}
}
