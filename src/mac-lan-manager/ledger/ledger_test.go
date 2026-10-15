package ledger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) FileStore {
	t.Helper()
	dir, err := os.MkdirTemp(sandbox, "case-") //nolint:usetesting // 書き込みは関門の中 (sandbox) に限る。t.TempDir は関門の外に作られる
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	return FileStore{Path: filepath.Join(dir, "sub", "hosts.toml"), Now: func() time.Time {
		n++
		return time.Date(2026, 10, 15, 12, 0, n, 0, time.UTC)
	}}
}

func TestDefaultPathUsesEnvDir(t *testing.T) {
	p, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p, sandbox) {
		t.Fatalf("DefaultPath() = %s: テスト中に本物の置き場所を指している", p)
	}
}

func TestDefaultPathHome(t *testing.T) {
	t.Setenv(EnvDir, "")
	t.Setenv("HOME", "/Users/someone")
	p, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if p != "/Users/someone/.config/glogx/mac-lan-manager/hosts.toml" {
		t.Errorf("DefaultPath() = %s", p)
	}
}

func TestUpsertLoadRemove(t *testing.T) {
	s := newStore(t)
	if hs, err := s.Load(); err != nil || len(hs) != 0 {
		t.Fatalf("空の台帳: %v %v", hs, err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.Upsert(Host{Host: "M2MacMini.local.", User: "koji", LAN: "192.168.100.13", Alias: "mini"}))
	must(s.Upsert(Host{Host: "studio.local", User: "koji", LAN: "192.168.100.12"}))
	// 同じ鍵 (大文字小文字・末尾の . が違うだけ) は置き換える
	must(s.Upsert(Host{Host: "m2macmini.local", User: "koji", LAN: "192.168.100.14", Alias: "mini"}))
	hs, err := s.Load()
	must(err)
	if len(hs) != 2 {
		t.Fatalf("hosts = %+v, want 2", hs)
	}
	if hs[0].Host != "m2macmini.local" || hs[0].LAN != "192.168.100.14" || hs[0].UpdatedAt.IsZero() {
		t.Errorf("hosts[0] = %+v", hs[0])
	}
	must(s.Remove("M2MACMINI.local"))
	must(s.Remove("nothing.local")) // 無いものを消しても失敗しない
	hs, err = s.Load()
	must(err)
	if len(hs) != 1 || hs[0].Host != "studio.local" {
		t.Errorf("after remove: %+v", hs)
	}
	b, err := os.ReadFile(s.Path)
	must(err)
	if !strings.Contains(string(b), "version = 1\n") {
		t.Errorf("版の行が無い:\n%s", b)
	}
}

func TestUpsertRejectsEmptyHost(t *testing.T) {
	s := newStore(t)
	if err := s.Upsert(Host{Host: " . "}); err == nil {
		t.Fatal("空のホスト名を受け付けた")
	}
}

// 新しい版の台帳を古い版で上書きすると、知らない項目が消える。書き込みを拒んで、ファイルを変えないこと。
func TestRefusesNewerFormat(t *testing.T) {
	s := newStore(t)
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := "version = 2\n\n[[host]]\nhost = \"a.local\"\nshared_with = \"team\"\n"
	if err := os.WriteFile(s.Path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	err := s.Upsert(Host{Host: "b.local"})
	if !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("err = %v, want ErrNewerFormat", err)
	}
	b, _ := os.ReadFile(s.Path)
	if string(b) != orig {
		t.Errorf("新しい版の台帳が書き換わった:\n%s", b)
	}
	// 読むことはできる (知らない鍵は読み飛ばす)
	hs, err := s.Load()
	if err != nil || len(hs) != 1 || hs[0].Host != "a.local" {
		t.Errorf("Load = %+v %v", hs, err)
	}
}

// 壊れた台帳を空として扱って上書きしない。
func TestRefusesBrokenLedger(t *testing.T) {
	s := newStore(t)
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := "version = 1\n[[host]]\nhost = koji-no-quote\n"
	if err := os.WriteFile(s.Path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(Host{Host: "b.local"}); err == nil {
		t.Fatal("壊れた台帳に書き込んだ")
	}
	b, _ := os.ReadFile(s.Path)
	if string(b) != orig {
		t.Errorf("壊れた台帳が書き換わった:\n%s", b)
	}
}

func TestFormatParseRoundTrip(t *testing.T) {
	in := []Host{
		{Host: "z.local", User: "u", LAN: "192.168.100.2", Alias: "名前 \"引用\"", UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
		{Host: "a.local", User: "u2"},
	}
	ver, out, err := Parse(Format(in))
	if err != nil || ver != Version || len(out) != 2 {
		t.Fatalf("Parse(Format) = %d %+v %v", ver, out, err)
	}
	if out[0].Host != "a.local" || out[1].Alias != "名前 \"引用\"" || !out[1].UpdatedAt.Equal(in[0].UpdatedAt) {
		t.Errorf("round trip = %+v", out)
	}
}

// 関門そのものの検査: 一時ディレクトリの外 (本物の置き場所) への書き込みは、書く前に拒否されてファイルが作られないこと。
func TestWriteGuardRejectsOutsideSandbox(t *testing.T) {
	// 外の場所は毎回別の名前にする (関門を外した変異が実際に作った残骸で、元に戻した後の検査が誤って赤にならないように)
	outside := filepath.Join(os.TempDir(), fmt.Sprintf("mac-lan-manager-guard-%d-%d", os.Getpid(), time.Now().UnixNano()), "hosts.toml")
	s := FileStore{Path: outside}
	if err := s.Upsert(Host{Host: "a.local"}); err == nil {
		t.Fatal("一時ディレクトリの外への書き込みを拒否しなかった")
	}
	if _, err := os.Stat(filepath.Dir(outside)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("拒否したのにディレクトリが作られた: %v", err)
	}
	// symlink で外へ出る形も拒否する
	link := filepath.Join(sandbox, "escape")
	if err := os.Symlink(os.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if err := (FileStore{Path: filepath.Join(link, "x", "hosts.toml")}).Upsert(Host{Host: "a.local"}); err == nil {
		t.Fatal("symlink で外へ出る書き込みを拒否しなかった")
	}
}

// 同時に書いても片方の登録が消えないこと (ロックが無いと、読み直しから書き込みの間に割り込まれて最後に書いた方だけが残る)。
func TestConcurrentUpsertKeepsAll(t *testing.T) {
	s := newStore(t)
	s.Now = nil
	const n = 30
	errs := make(chan error, n)
	for i := range n {
		go func() {
			errs <- FileStore{Path: s.Path}.Upsert(Host{Host: fmt.Sprintf("h%02d.local", i)})
		}()
	}
	for range n {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	hs, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != n {
		t.Fatalf("hosts = %d, want %d (同時の書き込みで登録が消えた)", len(hs), n)
	}
}

// red team の P2-1: ロックファイルが外を指す symlink でも、外に何も作らない。
func TestLockFileSymlinkDoesNotEscape(t *testing.T) {
	s := newStore(t)
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(os.TempDir(), fmt.Sprintf("mac-lan-manager-lock-escape-%d-%d", os.Getpid(), time.Now().UnixNano()))
	if err := os.Symlink(outside, s.Path+".lock"); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(Host{Host: "a.local"}); err == nil {
		t.Error("外を指すロックファイルのまま書き込んだ")
	}
	if _, err := os.Lstat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("sandbox の外にファイルができた: %s (%v)", outside, err)
		_ = os.Remove(outside)
	}
}

// red team の P2-2: Path が sandbox そのものでも、sandbox の外 (隣) にロックを作らない。
func TestGuardRejectsSandboxItself(t *testing.T) {
	if err := (FileStore{Path: sandbox}).Upsert(Host{Host: "a.local"}); err == nil {
		t.Fatal("sandbox そのものへの書き込みを拒否しなかった")
	}
	if _, err := os.Lstat(sandbox + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("sandbox の外にロックができた: %v", err)
		_ = os.Remove(sandbox + ".lock")
	}
}

// red team の P2-3 / P3-1: 書き直すと黙って消える中身がある台帳には書き込まない (ファイルを変えない)。一覧の読み取りは緩く読む。
func TestRefusesLossyRewrite(t *testing.T) {
	cases := map[string]string{
		"知らない鍵":         "version = 1\n[[host]]\nhost = \"a.local\"\nnote = \"keep me\"\n",
		"節の外の鍵":         "version = 1\nshared_repo = \"git@example\"\n[[host]]\nhost = \"a.local\"\n",
		"host の無い節":     "version = 1\n[[host]]\nuser = \"u\"\nlan = \"192.168.100.9\"\n",
		"読めない時刻":        "version = 1\n[[host]]\nhost = \"a.local\"\nupdated_at = \"yesterday\"\n",
		"version の重複":   "version = 2\nversion = 1\n[[host]]\nhost = \"a.local\"\n",
		"鍵の重複 (正規化で同じ)": "version = 1\n[[host]]\nhost = \"X.local\"\n[[host]]\nhost = \"x.local.\"\n",
		"節の中の鍵の重複":      "version = 1\n[[host]]\nhost = \"a.local\"\nuser = \"u\"\nuser = \"v\"\n",
	}
	for name, orig := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(s.Path, []byte(orig), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := s.Upsert(Host{Host: "b.local"}); err == nil {
				t.Error("書き直すと中身が消える台帳に書き込んだ")
			}
			if b, _ := os.ReadFile(s.Path); string(b) != orig {
				t.Errorf("台帳が書き換わった:\n%s", b)
			}
			if _, err := s.Load(); err != nil {
				t.Errorf("一覧のための読み取りまで失敗した: %v", err)
			}
		})
	}
}
