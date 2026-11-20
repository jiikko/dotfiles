package check

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
	"ja-writing/visibility"
)

// runSim は run の記録の、本家の driver が固定した値 (時刻・実行 ID の連番・time.monotonic) と子の代わりを、Go の run の Runtime に写す。
// driver は全部の case を 1 つのプロセスで走らせるので、時刻と連番は case をまたいで持つ。
type runSim struct {
	t       *testing.T
	clock   string
	counter int
	shim    string
	// restore は op snapshot を比べずに作業フォルダへ戻す (verify --replay の記録。本家の run が書いた木に Go の verify を当てる)。
	restore bool
	// temporary は restore のとき、verify の前後で変わらないことを確かめる一時フォルダ (TMPDIR。作業フォルダも一緒に確かめる)。
	temporary string
}

// watchedTrees は restore のとき (verify を呼ぶ記録)、作業フォルダ (状態フォルダを含む) と一時フォルダの木。ほかの記録では nil。
// verify は読むだけ: 本家の verify は状態フォルダにも一時フォルダにも書かないので、記録の stdout・stderr が合っても、Go が書いていれば食い違い。
func (e *interp) watchedTrees() map[string]string {
	if e.sim == nil || !e.sim.restore {
		return nil
	}
	trees := map[string]string{}
	for _, top := range []string{e.fx.Root, e.sim.temporary} {
		if err := treeFingerprint(top, trees); err != nil {
			e.t.Fatalf("%s の木を読めない: %v", top, err)
		}
	}
	return trees
}

// treeChanges は watchedTrees を before と比べ、増えた・消えた・変わったものを並べる (変わらなければ空)。
func (e *interp) treeChanges(before map[string]string) string {
	if before == nil {
		return ""
	}
	after := e.watchedTrees()
	var lines []string
	for path, was := range before {
		if now, ok := after[path]; !ok {
			lines = append(lines, "  消えた "+path)
		} else if now != was {
			lines = append(lines, "  変わった "+path+" ("+was+" → "+now+")")
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			lines = append(lines, "  増えた "+path)
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// treeFingerprint は top の下の全部 (top を含む) を、パス → 種類・mode・中身の SHA-256 (リンクは先) にして trees へ入れる。
// mtime と atime は入れない (読むだけで atime が変わる)。読めないフォルダは、その誤りを値にして中へ入らない。
func treeFingerprint(top string, trees map[string]string) error {
	return filepath.WalkDir(top, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == top {
				return err
			}
			trees[path] = "読めない: " + err.Error()
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		mode := fmt.Sprintf("%o", info.Sys().(*syscall.Stat_t).Mode)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			trees[path] = "link " + mode + " " + target
		case d.Type().IsRegular():
			// 読めないファイル (fixture の mode 000) は中身の代わりに大きさで持つ
			content := fmt.Sprintf("読めない %d", info.Size())
			if data, err := os.ReadFile(path); err == nil {
				content = SHA256(data)
			}
			trees[path] = "file " + mode + " " + content
		case d.IsDir():
			trees[path] = "dir " + mode
		default:
			trees[path] = "other " + mode
		}
		return nil
	})
}

func newRunSim(t *testing.T) *runSim {
	return &runSim{t: t, clock: "2026-01-01T00:00:00Z"}
}

// handles は run の記録の op のうち、Go の run の Runtime に写すもの (clock・pyexe) と driver だけが動かすもの。
func (s *runSim) handles(e *interp, op oracle.Op) bool {
	switch op.Kind {
	case "clock":
		s.clock = op.Value
	case "pyexe":
		s.shim = ""
		if op.Path != "" {
			s.shim = e.fx.Sub(op.Path)
		}
	case "pycli", "pyedit", "rename":
	default:
		return false
	}
	return true
}

// runtime は driver と同じ値を返す Runtime (time.monotonic は 1000 で止め、新しい実行 ID の乱数は 8 桁の連番)。
func (s *runSim) runtime() Runtime {
	exe := testExecutable(s.t)
	rt := DefaultRuntime()
	rt.Executable = func() (string, error) {
		if s.shim != "" {
			return s.shim, nil
		}
		return exe, nil
	}
	rt.ChildEnv = childEnv(exe)
	rt.Now = func() time.Time {
		moment, err := time.Parse("2006-01-02T15:04:05Z", s.clock)
		if err != nil {
			s.t.Fatal(err)
		}
		return moment
	}
	rt.Monotonic = func() float64 { return 1000 }
	random := rt.RandomHex
	rt.RandomHex = func(n int) (string, error) {
		if n != 4 {
			return random(n)
		}
		s.counter++
		return fmt.Sprintf("%08x", s.counter), nil
	}
	// 一時フォルダの親は本番と同じ gettempdir で選ぶ (run-tempdir の case が TMPDIR を変えて、候補の移り方を本家と比べる)
	return rt
}

// treeEntry は木の 1 つ (op snapshot の行を、比べられる形にしたもの)。
type treeEntry struct {
	kind  string
	mode  uint32
	mtime int64
	data  string // ファイルの中身 (正規化した後) かリンクの先
	extra any
}

const pythonFieldToken = "<<PYTHON>>"

var digestField = regexp.MustCompile(`("record_digest"[^"]*)"sha256:[0-9a-f]{64}"`)

// normalizeRecordFields は記録の python.version と python.implementation を印にする (Go と本家で値が違う欄)。読めない記録はそのまま。
func normalizeRecordFields(data []byte) []byte {
	value, err := pyjson.LoadsBytes(data)
	if err != nil {
		return data
	}
	record, ok := value.(*pyjson.Object)
	if !ok {
		return data
	}
	if python, ok := record.Field("python").(*pyjson.Object); ok {
		python.Set("version", pythonFieldToken)
		python.Set("implementation", pythonFieldToken)
	}
	out, err := pyjson.Dumps(record, pyjson.Indent1())
	if err != nil {
		return data
	}
	return []byte(out + "\n")
}

func isRecordRel(rel string) bool {
	parts := strings.Split(rel, "/")
	n := len(parts)
	return n >= 3 && parts[n-3] == "runs" && attemptFilePattern.MustMatchAt0(parts[n-1])
}

// goTree は Go が書いた木を op snapshot と同じ形にする: 通常のファイルの mtime を元の順を保った決まった値にそろえ (本家の op snapshot と同じく
// ファイルにも当てる)、中身の作業フォルダ・作業フォルダを中身に持つファイルの SHA-256・合っていた record_digest を印にする。
func (e *interp) goTree(top string) map[string]treeEntry {
	e.t.Helper()
	type file struct {
		mtime int64
		path  string
	}
	var files []file
	if err := filepath.WalkDir(top, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			files = append(files, file{info.ModTime().UnixNano(), path})
		}
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].mtime != files[j].mtime {
			return files[i].mtime < files[j].mtime
		}
		return files[i].path < files[j].path
	})
	const base = int64(1767225600) * 1000000000
	stamps := map[string]int64{}
	for rank, f := range files {
		stamp := base + int64(rank)*1000000
		stamps[f.path] = stamp
		if err := os.Chtimes(f.path, time.Unix(0, stamp), time.Unix(0, stamp)); err != nil {
			e.t.Fatal(err)
		}
	}
	root := []byte(e.fx.Root)
	out := map[string]treeEntry{}
	type pending struct {
		rel  string
		data []byte
	}
	var waiting []pending
	if err := filepath.WalkDir(top, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == top {
			return nil
		}
		rel := path[len(top)+1:]
		var st syscall.Stat_t
		if err := syscall.Lstat(path, &st); err != nil {
			return err
		}
		mode := uint32(st.Mode) & 0o7777
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			out[rel] = treeEntry{kind: "link", mode: mode, data: strings.ReplaceAll(target, e.fx.Root, oracle.RootToken)}
		case d.IsDir():
			out[rel] = treeEntry{kind: "dir", mode: mode}
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[rel] = treeEntry{kind: "file", mode: mode, mtime: stamps[path]}
			waiting = append(waiting, pending{rel, data})
		}
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
	hashes := map[string]string{}
	for _, p := range waiting {
		if bytes.Contains(p.data, root) && !isRecordRel(p.rel) {
			hashes[SHA256(p.data)] = "<<SHA:" + p.rel + ">>"
		}
	}
	for _, p := range waiting {
		data := p.data
		en := out[p.rel]
		if isRecordRel(p.rel) {
			record, _ := ReadRecord(top + "/" + p.rel)
			en.extra = false
			if record != nil {
				digest, _ := record.Field("record_digest").(string)
				computed, err := RecordDigest(record)
				valid := err == nil && digest == computed
				en.extra = valid
				if python, ok := record.Field("python").(*pyjson.Object); ok {
					if python.Field("version") != runtime.Version() || python.Field("implementation") != "Go ja-writing "+visibility.Version {
						e.t.Errorf("Go の記録 %s の python の欄が Go の値でない: %v %v", p.rel, python.Field("version"), python.Field("implementation"))
					}
				}
				if strings.HasPrefix(digest, "sha256:") && len(digest) == 71 {
					token := digestToken
					if !valid {
						token = "sha256:" + strings.Repeat("0", 64)
					}
					data = bytes.ReplaceAll(data, []byte(digest), []byte(token))
				}
			} else {
				data = digestField.ReplaceAll(data, []byte(`$1"sha256:`+strings.Repeat("0", 64)+`"`))
			}
			for real, token := range hashes {
				data = bytes.ReplaceAll(data, []byte(real), []byte(token))
			}
		}
		data = bytes.ReplaceAll(data, root, []byte(oracle.RootToken))
		if isRecordRel(p.rel) {
			data = normalizeRecordFields(data)
		}
		en.data = string(data)
		out[p.rel] = en
	}
	return out
}

// compareSnapshot は本家の op snapshot の木と、Go が同じ場所に書いた木を比べる。by-path の鍵は、記録の印の文字列を Go の作業フォルダで
// 作り直した名前にそろえる。食い違いの文を返す。
func (e *interp) compareSnapshot(name string, index int, op oracle.Op, kept oracle.VisKept) []string {
	e.t.Helper()
	if op.Value != "" {
		e.t.Fatalf("run の記録の snapshot に ties は使わない (%s の %d 番目)", name, index)
	}
	entries := e.decodeSnapshot(kept)
	relOf := byPathRenamer(entries, e.fx.Root)
	want := map[string]treeEntry{}
	for _, en := range entries {
		t := treeEntry{kind: en.kind, mode: en.mode, mtime: en.mtime}
		switch en.kind {
		case "file":
			data := e.blobs[*en.ref]
			if isRecordRel(en.rel) {
				data = normalizeRecordFields(data)
				t.extra = en.extra
			}
			t.data = string(data)
		case "link":
			t.data = *en.ref
		}
		want[relOf(en.rel)] = t
	}
	got := e.goTree(e.fx.Sub(op.Path))
	var out []string
	where := fmt.Sprintf("case %s の %d 番目の木", name, index)
	keys := map[string]bool{}
	for k := range want {
		keys[k] = true
	}
	for k := range got {
		keys[k] = true
	}
	var rels []string
	for k := range keys {
		rels = append(rels, k)
	}
	slices.Sort(rels)
	for _, rel := range rels {
		w, inWant := want[rel]
		g, inGot := got[rel]
		switch {
		case !inGot:
			out = append(out, fmt.Sprintf("%s: Go に %s が無い (%s)", where, rel, w.kind))
		case !inWant:
			out = append(out, fmt.Sprintf("%s: Go だけに %s がある (%s)", where, rel, g.kind))
		case w.kind != g.kind || w.mode != g.mode:
			out = append(out, fmt.Sprintf("%s: %s の種類か mode が違う: Go %s %o、記録 %s %o", where, rel, g.kind, g.mode, w.kind, w.mode))
		case w.data != g.data:
			out = append(out, fmt.Sprintf("%s: %s の中身が違う\n%s", where, rel, firstDifference(g.data, w.data)))
		case w.mtime != g.mtime:
			out = append(out, fmt.Sprintf("%s: %s の書いた順 (mtime) が違う: Go %d、記録 %d", where, rel, g.mtime, w.mtime))
		case w.kind == "file" && isRecordRel(rel) && w.extra != g.extra:
			out = append(out, fmt.Sprintf("%s: %s の record_digest の合い方が違う: Go %v、記録 %v", where, rel, g.extra, w.extra))
		}
	}
	return out
}

// firstDifference は 2 つの文の最初に違う行を並べる。
func firstDifference(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range max(len(g), len(w)) {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return fmt.Sprintf("  %d 行目\n  Go:   %s\n  記録: %s", i+1, clip(gl, 300), clip(wl, 300))
		}
	}
	return "  (行の数だけが違う)"
}

func loadRun(t *testing.T) (oracle.VisibilityGolden, []oracle.VisCase) {
	t.Helper()
	var g oracle.VisibilityGolden
	if err := oracle.ReadGolden(goldenDir, oracle.CheckRunFile, &g); err != nil {
		t.Fatalf("make golden で記録を作る: %v", err)
	}
	cases := oracle.CheckRunCases()
	if len(cases) != len(g.Cases) {
		t.Fatalf("case %d 件に対して記録は %d 件。make golden で作り直す", len(cases), len(g.Cases))
	}
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name || rec.Calls != len(c.Calls) || len(rec.Digests) != rec.Calls*oracle.DigestWidth {
			t.Fatalf("case %d が記録と違う (%s %d 件 / 記録 %s %d 件)。make golden で作り直す", i, c.Name, len(c.Calls), rec.Name, rec.Calls)
		}
	}
	return g, cases
}
