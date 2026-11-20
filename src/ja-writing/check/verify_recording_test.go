package check

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"ja-writing/cmdenv"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
)

// driverOnly は driver (本家) だけが動かす op。Go のテストは何もしない (状態フォルダは後の op snapshot の記録から戻す)。
var driverOnly = map[string]bool{"pycli": true, "pyedit": true, "clock": true}

// 状態フォルダの木の記録の印 (call_function.py の op snapshot)。
const (
	digestToken     = "sha256:<<DIGEST>>"
	internalErrHead = "yomiyasu_check.py: 内部エラー"
)

// shaToken は記録の中の、作業フォルダのパスを中身に持つファイルの SHA-256 の印。
var shaToken = regexp.MustCompile(`<<SHA:([^>]*)>>`)

type snapshotEntry struct {
	rel, kind string
	mode      uint32
	mtime     int64
	ref       *string
	extra     any
}

func (e *interp) decodeSnapshot(kept oracle.VisKept) []snapshotEntry {
	e.t.Helper()
	var raw struct {
		Entries [][]json.RawMessage `json:"entries"`
		Blobs   map[string]string   `json:"blobs"`
	}
	if err := json.Unmarshal(kept.Result, &raw); err != nil {
		e.t.Fatalf("snapshot の記録を読めない: %v", err)
	}
	for key, b64 := range raw.Blobs {
		packed, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			e.t.Fatal(err)
		}
		r, err := zlib.NewReader(bytes.NewReader(packed))
		if err != nil {
			e.t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		if err != nil {
			e.t.Fatal(err)
		}
		e.blobs[key] = data
	}
	out := make([]snapshotEntry, len(raw.Entries))
	for i, row := range raw.Entries {
		var en snapshotEntry
		for j, dst := range []any{&en.rel, &en.kind, &en.mode, &en.mtime, &en.ref, &en.extra} {
			if err := json.Unmarshal(row[j], dst); err != nil {
				e.t.Fatalf("snapshot の行 %d を読めない: %v", i, err)
			}
		}
		out[i] = en
	}
	return out
}

// materialize は op snapshot の記録を作業フォルダに戻す。記録の中の作業フォルダの印は自分の作業フォルダにし、by-path の鍵は自分のパスの
// _place から作り直し、中身と合っていた record_digest は自分の式で求め直す (どれも作業フォルダのパスで値が変わる)。
func (e *interp) materialize(op oracle.Op, kept oracle.VisKept) {
	e.t.Helper()
	top := e.fx.Sub(op.Path)
	if !strings.HasPrefix(top, e.fx.Root+"/") {
		e.t.Fatalf("snapshot の置き場 %s が作業フォルダ %s の外", top, e.fx.Root)
	}
	entries := e.decodeSnapshot(kept)
	// 消す前に、読めない・書けないフォルダを開ける (消すのは作業フォルダの下だけ)
	_ = filepath.WalkDir(top, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(path, 0o755)
		}
		return nil
	})
	cwd, err := os.Getwd()
	if err != nil {
		e.t.Fatal(err)
	}
	children, err := os.ReadDir(top)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, child := range children {
		if err := os.RemoveAll(top + "/" + child.Name()); err != nil {
			e.t.Fatal(err)
		}
	}
	// cwd が消したフォルダの中なら、作り直したフォルダへパスで入り直す (消した inode を指したままにしない)
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			e.t.Fatal(err)
		}
	}()
	folded := pyunicode.Casefold(pyunicode.Normalize(pyunicode.NFC, e.fx.Root))
	renamed := map[string]string{}
	for _, en := range entries {
		if place, ok := en.extra.(string); ok && strings.Contains(place, oracle.RootToken) {
			dir := en.rel[:strings.LastIndex(en.rel, "/")+1]
			renamed[en.rel] = dir + SHA256([]byte(strings.ReplaceAll(place, oracle.RootToken, folded)))
		}
	}
	relOf := func(rel string) string {
		for from, to := range renamed {
			if rel == from || strings.HasPrefix(rel, from+"/") {
				return to + rel[len(from):]
			}
		}
		return rel
	}
	type mode struct {
		path string
		mode uint32
		dir  bool
	}
	var modes []mode
	// 記録は、中で SHA-256 を印にしたファイル (<<SHA:相対パス>>) を書き終えてから書く
	isRecord := func(en snapshotEntry) bool { _, ok := en.extra.(bool); return ok }
	ordered := slices.Clone(entries)
	sort.SliceStable(ordered, func(i, j int) bool { return !isRecord(ordered[i]) && isRecord(ordered[j]) })
	for _, en := range ordered {
		path := top + "/" + relOf(en.rel)
		switch en.kind {
		case "dir":
			if err := os.MkdirAll(path, 0o755); err != nil {
				e.t.Fatal(err)
			}
		case "file":
			data := bytes.ReplaceAll(e.blobs[*en.ref], []byte(oracle.RootToken), []byte(e.fx.Root))
			data = shaToken.ReplaceAllFunc(data, func(m []byte) []byte {
				written, err := os.ReadFile(top + "/" + relOf(string(shaToken.FindSubmatch(m)[1])))
				if err != nil {
					e.t.Fatalf("SHA-256 の印のファイルを読めない: %v", err)
				}
				return []byte(SHA256(written))
			})
			if en.extra == true {
				data = e.redigest(data)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				e.t.Fatal(err)
			}
			stamp := time.Unix(0, en.mtime)
			if err := os.Chtimes(path, stamp, stamp); err != nil {
				e.t.Fatal(err)
			}
		case "link":
			if err := os.Symlink(strings.ReplaceAll(*en.ref, oracle.RootToken, e.fx.Root), path); err != nil {
				e.t.Fatal(err)
			}
			continue
		default:
			e.t.Fatalf("snapshot の種類 %s", en.kind)
		}
		modes = append(modes, mode{path, en.mode, en.kind == "dir"})
	}
	// mode はファイルを書き終えてから、深いものから当てる (読めないフォルダの中にも書けるように)
	sort.SliceStable(modes, func(i, j int) bool { return len(modes[i].path) > len(modes[j].path) })
	for _, m := range modes {
		if err := os.Chmod(m.path, fs.FileMode(m.mode&0o777)|unixBits(m.mode)); err != nil {
			e.t.Fatal(err)
		}
	}
}

func unixBits(mode uint32) fs.FileMode {
	var out fs.FileMode
	if mode&syscall.S_ISUID != 0 {
		out |= fs.ModeSetuid
	}
	if mode&syscall.S_ISGID != 0 {
		out |= fs.ModeSetgid
	}
	if mode&syscall.S_ISVTX != 0 {
		out |= fs.ModeSticky
	}
	return out
}

// redigest は record_digest の印を、作業フォルダを入れた中身から求め直した値にする。
func (e *interp) redigest(data []byte) []byte {
	e.t.Helper()
	value, err := pyjson.LoadsBytes(data)
	if err != nil {
		e.t.Fatalf("digest の合っていた記録を読めない: %v", err)
	}
	digest, err := RecordDigest(value.(*pyjson.Object))
	if err != nil {
		e.t.Fatal(err)
	}
	if bytes.Count(data, []byte(digestToken)) != 1 {
		e.t.Fatalf("記録に digest の印が 1 つではない")
	}
	return bytes.Replace(data, []byte(digestToken), []byte(digest), 1)
}

// normalizeInternal は標準エラーの内部エラーの行から後ろを 1 行にする (call_function.py の capture と同じ)。
func normalizeInternal(stderr string) string {
	at := strings.Index(stderr, internalErrHead+": ")
	if at >= 0 && (at == 0 || stderr[at-1] == '\n') {
		return stderr[:at] + internalErrHead + "\n"
	}
	return stderr
}

// capture は本家の main を capture で呼んだ記録の代わりに、check の Main を呼んで {rc, stdout, stderr} を返す。
func (e *interp) capture(c oracle.Call) outcome {
	e.t.Helper()
	var argv []string
	for _, a := range c.Args[0].([]string) {
		argv = append(argv, e.sub(a))
	}
	cwd, err := os.Getwd()
	if err != nil {
		e.t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	// 標準入力は op stdin が決めたもの (op stdin none の後は無い)。記録の case は、標準入力を読む呼び出しの前に op stdin を置く
	rc := Main(context.Background(), cmdenv.Env{Args: argv, Stdin: e.stdin, Stdout: &stdout, Stderr: &stderr, Cwd: cwd, Getenv: os.Getenv})
	o := pyjson.NewObject()
	o.Set("rc", int64(rc))
	o.Set("stdout", stdout.String())
	o.Set("stderr", normalizeInternal(stderr.String()))
	return outcome{value: o}
}

func loadVerify(t *testing.T) (oracle.VisibilityGolden, []oracle.VisCase) {
	t.Helper()
	var g oracle.VisibilityGolden
	if err := oracle.ReadGolden(goldenDir, oracle.CheckVerifyFile, &g); err != nil {
		t.Fatalf("make golden で記録を作る: %v", err)
	}
	cases := oracle.CheckVerifyCases()
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

// 本家の run が書いた状態フォルダを戻し、check の verify が本家の verify と同じ stdout・stderr・終了コードを返す。
func TestVerifyMatchesPythonRecording(t *testing.T) {
	withUmask(t, 0o022)
	t.Setenv(stateDirEnv, "")
	if err := os.Unsetenv(stateDirEnv); err != nil {
		t.Fatal(err)
	}
	g, cases := loadVerify(t)
	stats := map[string]*stat{}
	failed, total := 0, 0
	for i, c := range cases {
		for _, m := range runCase(t, c, g.Cases[i], stats) {
			failed++
			if failed <= 15 {
				t.Error(m)
			}
		}
		total += len(c.Calls)
	}
	if failed > 15 {
		t.Errorf("ほかにも %d 件の食い違いがあります", failed-15)
	}
	for _, f := range g.Functions {
		s := stats[f.Name]
		if s == nil || s.calls != f.Calls || s.raised != f.Raised {
			t.Errorf("%s の呼び出し数・例外の数が記録と違う: Go %+v、記録 %+v", f.Name, s, f)
		}
	}
	t.Logf("verify の記録 %d case・呼び出し %d 件を全部比べた", len(cases), total)
}

// 空振りを止める: verify の呼び出しが記録にあり、終了コード 0・1・2・3 のどれもが記録に出る。
func TestVerifyRecordingCoversExitCodes(t *testing.T) {
	g, _ := loadVerify(t)
	codes := map[int]int{}
	for _, c := range g.Cases {
		for _, k := range c.Kept {
			var r struct {
				RC *int `json:"rc"`
			}
			if json.Unmarshal(k.Result, &r) == nil && r.RC != nil {
				codes[*r.RC]++
			}
		}
	}
	for _, rc := range []int{0, 1, 2, 3} {
		if codes[rc] == 0 {
			t.Errorf("終了コード %d の verify が記録に無い (%v)", rc, codes)
		}
	}
	if !slices.ContainsFunc(g.Functions, func(f oracle.VisFunctionRecord) bool { return f.Name == "main" && f.Calls > 100 }) {
		t.Errorf("main の呼び出しが 100 件を超えていない: %+v", g.Functions)
	}
}
