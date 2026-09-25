package eventlog

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)

func evN(i int) Event {
	return Event{At: t0.Add(time.Duration(i) * time.Second), Kind: KindLaunch, Card: "C-001", Session: "s1", Reason: fmt.Sprintf("出来事 %03d", i)}
}

func reasons(evs []Event) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Reason
	}
	return out
}

// 置き場のディレクトリは 0700、記録は 0600 (前から緩い権限で在っても 0600 に直す)。
func TestAppendPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := Append(dir, []Event{evN(1)}); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("ディレクトリの権限: %v", st.Mode().Perm())
	}
	p := filepath.Join(dir, File)
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Append(dir, []Event{evN(2)}); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("記録の権限: %v", st.Mode().Perm())
	}
	got, err := Read(dir)
	if err != nil || len(got) != 2 || got[0] != evN(1) || got[1] != evN(2) {
		t.Fatalf("読み戻し: %+v %v", got, err)
	}
}

// 前の書き手が行の途中で落ちて末尾が改行で終わっていなくても、次に足す出来事は壊れた行につながらずに読める。
func TestAppendAfterTruncatedLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, File), []byte(`{"kind":"apply","reason":"trunc`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Append(dir, []Event{evN(1)}); err != nil {
		t.Fatal(err)
	}
	if got, err := Read(dir); err != nil || len(got) != 1 || got[0] != evN(1) {
		t.Fatalf("壊れた行の後に足した出来事が読めない: %v %v", reasons(got), err)
	}
}

// 上限を超えそうになったら 1 つ前へ回す。読むと回した分から古い順に並び、置き場は上限の 2 倍を超えない。
func TestAppendRotates(t *testing.T) {
	old := MaxBytes
	MaxBytes = 400
	t.Cleanup(func() { MaxBytes = old })
	dir := t.TempDir()
	for i := range 20 {
		if err := Append(dir, []Event{evN(i)}); err != nil {
			t.Fatal(err)
		}
	}
	cur, _ := os.Stat(filepath.Join(dir, File))
	prev, err := os.Stat(filepath.Join(dir, OldFile))
	if err != nil || cur.Size() > MaxBytes || prev.Size() > MaxBytes {
		t.Fatalf("回していない / 上限を超えた: cur=%d old=%v err=%v", cur.Size(), prev, err)
	}
	got, err := Read(dir)
	if err != nil || len(got) < 2 || got[len(got)-1] != evN(19) {
		t.Fatalf("最後の出来事が読めない: %v %v", reasons(got), err)
	}
	for i := 1; i < len(got); i++ {
		if !got[i].At.After(got[i-1].At) {
			t.Fatalf("古い順でない: %v", reasons(got))
		}
	}
}

// 最初に読んだときは何も無く、次に読むまでに回った: 回した分 (まだ読んでいない) も返す。
func TestFollowerFirstEmptyThenRotated(t *testing.T) {
	old := MaxBytes
	MaxBytes = 400
	t.Cleanup(func() { MaxBytes = old })
	dir := t.TempDir()
	f := NewFollower(dir)
	if got, _ := f.Next(); len(got) != 0 {
		t.Fatalf("まだ無い記録: %v", got)
	}
	for i := range 4 {
		if err := Append(dir, []Event{evN(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, OldFile)); err != nil {
		t.Fatalf("前提: 回っていない: %v", err)
	}
	if got, _ := f.Next(); len(got) != 4 {
		t.Fatalf("回した分を取りこぼした: %v", reasons(got))
	}
}

// 読み進める側は、読んでいる間に回されても、回される前に書かれた残りを取りこぼさず、同じ出来事を 2 度返さない。
// 書きかけ (改行で終わっていない) の行は次に回す。
func TestFollowerAcrossRotation(t *testing.T) {
	old := MaxBytes
	MaxBytes = 400 // 1 ファイルに 3 行。2 件ごとに読めば、読む間に回るのは高々 1 回
	t.Cleanup(func() { MaxBytes = old })
	dir := t.TempDir()
	f := NewFollower(dir)
	if got, err := f.Next(); err != nil || len(got) != 0 {
		t.Fatalf("まだ無い記録: %v %v", got, err)
	}
	var seen []string
	for i := range 12 {
		if err := Append(dir, []Event{evN(i)}); err != nil {
			t.Fatal(err)
		}
		if i%2 == 1 { // 2 件ごとに読む (その間に回ることがある)
			got, err := f.Next()
			if err != nil {
				t.Fatal(err)
			}
			seen = append(seen, reasons(got)...)
		}
	}
	if len(seen) != 12 {
		t.Fatalf("取りこぼした / 2 度返した: %v", seen)
	}
	for i, r := range seen {
		if r != evN(i).Reason {
			t.Fatalf("%d 件目が違う: %v", i, seen)
		}
	}
	fh, err := os.OpenFile(filepath.Join(dir, File), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fh.WriteString(`{"at":"2026-09-25T01:00:00Z","kind":"x","reason":"書きかけ`)
	_ = fh.Close()
	if got, _ := f.Next(); len(got) != 0 {
		t.Fatalf("書きかけの行を読んだ: %v", got)
	}
	fh, _ = os.OpenFile(filepath.Join(dir, File), os.O_WRONLY|os.O_APPEND, 0o600)
	_, _ = fh.WriteString("\"}\n")
	_ = fh.Close()
	if got, _ := f.Next(); len(got) != 1 || got[0].Reason != "書きかけ" {
		t.Fatalf("書き終えた行を読まない: %v", got)
	}
}
