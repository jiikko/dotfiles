package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

type fakeT struct {
	fatal    bool
	cleanups []func()
}

func (f *fakeT) Helper()          {}
func (f *fakeT) Fatal(...any)     { f.fatal = true }
func (f *fakeT) Cleanup(c func()) { f.cleanups = append(f.cleanups, c) }

func TestAllowRefusesOutsideTempDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{home, "/", os.TempDir(), filepath.Join(os.TempDir(), "..")} {
		var s Sandbox
		ft := &fakeT{}
		s.Allow(ft, root)
		if !ft.fatal || len(s.roots) != 0 {
			t.Errorf("%s を登録できた", root)
		}
	}
}

func TestGuardOnlyAllowsRegistered(t *testing.T) {
	var s Sandbox
	in, out := t.TempDir(), t.TempDir()
	s.Allow(t, in)
	if err := s.Guard("create", filepath.Join(in, "a")); err != nil {
		t.Errorf("登録した場所を拒否した: %v", err)
	}
	if err := s.Guard("create", filepath.Join(out, "a")); err == nil {
		t.Error("登録していない場所を通した")
	}
	if err := s.Guard("create", in+"-sibling/a"); err == nil {
		t.Error("名前の前方一致だけで通した")
	}
	if v := s.Violations(); len(v) != 2 {
		t.Errorf("記録 = %v, want 2 件", v)
	}
}

// sandbox の中から外を指す symlink の先は、親を実解決して外として扱う。
func TestGuardResolvesSymlinkedParent(t *testing.T) {
	var s Sandbox
	in, out := t.TempDir(), t.TempDir()
	s.Allow(t, in)
	link := filepath.Join(in, "escape")
	if err := os.Symlink(out, link); err != nil {
		t.Fatal(err)
	}
	if err := s.Guard("rename", filepath.Join(link, "victim")); err == nil {
		t.Error("symlink 越しに外を通した")
	}
	// symlink そのもの (最後の要素) を動かすのは中の操作
	if err := s.Guard("trash", link); err != nil {
		t.Errorf("symlink 自身の操作を拒否した: %v", err)
	}
	s.Violations()
}

func TestRootIsReleasedOnCleanup(t *testing.T) {
	var s Sandbox
	in := t.TempDir()
	ft := &fakeT{}
	s.Allow(ft, in)
	if err := s.Guard("create", filepath.Join(in, "a")); err != nil {
		t.Fatal(err)
	}
	for _, c := range ft.cleanups {
		c()
	}
	if err := s.Guard("create", filepath.Join(in, "a")); err == nil {
		t.Error("テストが終わった後も登録が残った")
	}
	s.Violations()
}
