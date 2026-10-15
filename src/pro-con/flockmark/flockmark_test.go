package flockmark

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Create は印 <id>.lock を flock (EX) を持ったまま置き、中身を書き、作りかけ (.tmp-*) を残さない。
func TestCreateHoldsLockAndWritesBody(t *testing.T) {
	d := t.TempDir()
	f, id, p, err := Create(d, 4, func(id string) ([]byte, error) { return []byte("id=" + id), nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if len(id) != 8 || p != filepath.Join(d, id+".lock") {
		t.Fatalf("id=%q p=%q", id, p)
	}
	if b, err := os.ReadFile(p); err != nil || string(b) != "id="+id {
		t.Fatalf("中身 %q %v", b, err)
	}
	g, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	if err := syscall.Flock(int(g.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err == nil {
		t.Fatal("印の flock が持たれていない (ほかの画面から「落ちた画面の残り」に見える)")
	}
	if ms, _ := filepath.Glob(filepath.Join(d, ".tmp-*")); len(ms) != 0 {
		t.Fatalf("作りかけが残った: %v", ms)
	}
}

// 中身を作れなかったら印を置かず、作りかけも残さない。
func TestCreateBodyErrorLeavesNothing(t *testing.T) {
	d := t.TempDir()
	if _, _, _, err := Create(d, 4, func(string) ([]byte, error) { return nil, os.ErrInvalid }); err == nil {
		t.Fatal("中身の失敗が返らない")
	}
	if es, _ := os.ReadDir(d); len(es) != 0 {
		t.Fatalf("何か残った: %v", es)
	}
}

// 古い作りかけだけが「落ちた残り」。新しいもの (作っている最中)・普通のファイルでないもの・作りかけでない名前は違う。
func TestIsStaleTmp(t *testing.T) {
	d := t.TempDir()
	past := time.Now().Add(-2 * StaleTmp)
	mk := func(name string, old bool) {
		p := filepath.Join(d, name)
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if old {
			if err := os.Chtimes(p, past, past); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk(".tmp-old", true)
	mk(".tmp-new", false)
	mk("abc.lock", true)
	if err := syscall.Mkfifo(filepath.Join(d, ".tmp-fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(d, ".tmp-fifo"), past, past); err != nil {
		t.Fatal(err)
	}
	es, err := os.ReadDir(d)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{".tmp-old": true, ".tmp-new": false, "abc.lock": false, ".tmp-fifo": false}
	for _, e := range es {
		if got := IsStaleTmp(e); got != want[e.Name()] {
			t.Errorf("IsStaleTmp(%s) = %v, want %v", e.Name(), got, want[e.Name()])
		}
	}
	if len(es) != len(want) {
		t.Fatalf("作った数と違う: %d", len(es))
	}
}

// 印の名前にする直前には、flock を持ち、中身を書き終えている (読む側に「誰も持っていない印」「中身の無い印」が見える窓を作らない)。
// 順序の退行 (rename を先にする・中身を後で書く) は並行のテストでは確率的にしか出ないので、直前の状態を直に見る。
func TestCreateLocksAndWritesBeforeRename(t *testing.T) {
	d := t.TempDir()
	checked := false
	beforeRename = func(tmp, lock string) {
		checked = true
		if _, err := os.Stat(lock); err == nil {
			t.Error("flock と中身より先に印の名前ができている")
		}
		if b, err := os.ReadFile(tmp); err != nil || string(b) != "body" {
			t.Errorf("rename の前に中身が書かれていない: %q %v", b, err)
		}
		g, err := os.Open(tmp)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = g.Close() }()
		if err := syscall.Flock(int(g.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err == nil {
			t.Error("rename の前に flock を持っていない")
		}
	}
	defer func() { beforeRename = nil }()
	f, _, _, err := Create(d, 4, func(string) ([]byte, error) { return []byte("body"), nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if !checked {
		t.Fatal("差し込み口が呼ばれていない")
	}
}
