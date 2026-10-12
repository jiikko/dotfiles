// Package sandbox はテストのための safefs.Guard: 明示的に登録した一時ディレクトリの外への操作を**実行前に**拒否する
// (`~/.claude/rules/sandbox-real-destructive-test-apis.md`)。safefs と treefiler のテストが使う。
// doctor は disk/main_test.go に自前の 4 段構えを持つ (振る舞いを変えないため、こちらへ寄せていない)。
//
// 守ること:
//  1. 既定は全部拒否。Allow で登録して初めて通る (登録漏れは「動かない」側に倒れる)。登録できるのは os.TempDir() の配下だけ
//     (実ホームを登録できるとハーネスが 1 行で無力化する)
//  2. 判定は親ディレクトリを実解決してから行う (sandbox の中から外を指す symlink で抜けられないように)
//  3. 拒否を記録する。呼び出し側は TestMain の最後に Violations を見て、1 件でもあればスイートを落とす
//     (拒否は「ハーネスが助けた」ではなく「テストが壊れている」の証拠)
package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"safefs"
)

// TB は Allow が使う *testing.T の面 (testing を production の import に持ち込まないため)。
type TB interface {
	Helper()
	Fatal(args ...any)
	Cleanup(func())
}

// Sandbox は登録した root と拒否の記録。ゼロ値で使える。
type Sandbox struct {
	mu         sync.Mutex
	roots      []string
	violations []string
}

// Allow はこのテストの間だけ root 配下への操作を許す。os.TempDir() の外は Fatal。
func (s *Sandbox) Allow(t TB, root string) {
	t.Helper()
	r := resolve(root)
	tmp := resolve(filepath.Join(os.TempDir(), "x"))
	tmp = filepath.Dir(tmp)
	if !within(r, tmp) || r == tmp {
		t.Fatal(fmt.Sprintf("sandbox: os.TempDir() (%s) の配下でない場所は登録できません: %s", tmp, r))
		return
	}
	s.mu.Lock()
	s.roots = append(s.roots, r)
	s.mu.Unlock()
	t.Cleanup(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, x := range s.roots {
			if x == r {
				s.roots = append(s.roots[:i], s.roots[i+1:]...)
				return
			}
		}
	})
}

// Guard は safefs.Guard。登録した root の配下なら nil、外なら拒否を記録して error。
func (s *Sandbox) Guard(op, path string) error {
	r := resolve(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, root := range s.roots {
		if within(r, root) {
			return nil
		}
	}
	s.violations = append(s.violations, op+" "+r)
	return fmt.Errorf("sandbox: 登録していない場所への %s を拒否しました: %s", op, r)
}

var _ safefs.Guard = (&Sandbox{}).Guard

// Violations は拒否した記録を取り出して空にする。
func (s *Sandbox) Violations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.violations
	s.violations = nil
	return v
}

// resolve は「カーネルが解決する先」に寄せる: 親を実解決し、対象自身は解決しない (symlink そのものを動かすのが正しいため)。
// 親が無いとき (これから作る深い場所) は、在る祖先まで遡って解決し、残りを繋ぐ。
func resolve(p string) string {
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
	}
	dir, rest := filepath.Dir(p), filepath.Base(p)
	for {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return safefs.NormalizeSystemLinks(filepath.Join(real, rest))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return safefs.NormalizeSystemLinks(p)
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

func within(p, root string) bool { return p == root || strings.HasPrefix(p, root+"/") }
