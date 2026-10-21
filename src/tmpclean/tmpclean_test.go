package tmpclean

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"tmpclean/internal/runner"
)

func TestProjectKey(t *testing.T) {
	long := "/" + strings.Repeat("a", 250)
	cases := []struct {
		cwd        string
		key        string
		prefix, ok bool
	}{
		{"/Users/me/src/pj_energy_matching", "-Users-me-src-pj-energy-matching", false, true},
		{"/Users/me/.claude/chrome", "-Users-me--claude-chrome", false, true},
		{long, "-" + strings.Repeat("a", 199), true, true},
		{"/Users/me/日本語", "", false, false},          // 本体は UTF-16 の単位で置き換えるのでバイト単位とずれうる
		{`/Users/me/\xe6\x97\xa5`, "", false, false}, // C ロケールの lsof のエスケープ
		{"/Users/me/a\tb", "", false, false},         // 制御文字
		{"", "", false, false},
	}
	for _, c := range cases {
		key, prefix, ok := projectKey(c.cwd)
		if key != c.key || prefix != c.prefix || ok != c.ok {
			t.Errorf("projectKey(%q) = (%q, %v, %v), want (%q, %v, %v)", c.cwd, key, prefix, ok, c.key, c.prefix, c.ok)
		}
	}
}

// 古くて誰も使っていないものはゴミ箱へ移り、元の場所との対応が記録に残る。
func TestApplyMovesOldUnusedToTrash(t *testing.T) {
	f := newFixture(t)
	s := f.deadSession("-dead-proj", deadSID, 30)
	tmp := f.oldTmp("go-build123456", 30)
	id, path := f.plan("session,tmpdir")
	rc, out, errOut := f.apply(id, path)
	if rc != exitOK {
		t.Fatalf("apply rc=%d\n%s\n%s", rc, out, errOut)
	}
	if exists(s) || exists(tmp) {
		t.Fatalf("移っていない: session=%v tmp=%v\n%s", exists(s), exists(tmp), out)
	}
	if !f.trashed("session", filepath.Join("-dead-proj", deadSID)) || !f.trashed("tmpdir", "go-build123456") {
		t.Fatalf("ゴミ箱の決まった場所に無い\n%s", out)
	}
	mf, err := os.Open(filepath.Join(f.out, "moved-"+id+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mf.Close() }()
	var froms []string
	sc := bufio.NewScanner(mf)
	for sc.Scan() {
		var m moved
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		froms = append(froms, m.From)
	}
	if strings.Join(froms, ",") != s+","+tmp {
		t.Fatalf("記録 = %v", froms)
	}
	if !exists(filepath.Join(f.session, "-self-proj", selfSID)) {
		t.Fatal("自分の scratchpad が消えた")
	}
}

// 生きている claude が居るプロジェクトの古いセッションは移さない (ID が外から読めないセッションを守る主な層)。
func TestLiveProjectProtectsSessions(t *testing.T) {
	f := newFixture(t)
	s := f.deadSession("-other-proj", deadSID, 30)
	f.sys.procs = append(f.sys.procs, fproc{pid: 400, ppid: 1, command: "/Applications/Claude.app/Contents/MacOS/claude", ucomm: "claude", cwd: "/other/proj", pgrep: true})
	_, out := mustPlan(t, f, "session")
	if strings.Contains(out, s) && !strings.Contains(out, "生きている claude が居るプロジェクト") {
		t.Fatalf("生きているプロジェクトのセッションが候補になった\n%s", out)
	}
	if countCandidates(out) != 0 {
		t.Fatalf("候補が出た\n%s", out)
	}
}

// 200 字を超える cwd の claude は、本体の切り詰め (先頭 200 字 + -hash) に合わせて前方一致で守る。
func TestLongCwdProtectsTruncatedProject(t *testing.T) {
	f := newFixture(t)
	cwd := "/w/" + strings.Repeat("a", 260)
	key, _, _ := projectKey(cwd)
	s := f.deadSession(key[:200]+"-1abcde", deadSID, 30)
	f.sys.procs = append(f.sys.procs, fproc{pid: 400, ppid: 1, command: "claude", ucomm: "claude", cwd: cwd, pgrep: true})
	_, out := mustPlan(t, f, "session")
	if countCandidates(out) != 0 {
		t.Fatalf("切り詰められた project のセッションが候補になった: %s\n%s", s, out)
	}
}

// claude を拾う 3 つの口 (pgrep・UCOMM の版番号・command の語) は、それぞれ単独で効く。
func TestClaudeDetectionPaths(t *testing.T) {
	cases := map[string]fproc{
		"pgrep (argv[0] が claude)":         {pid: 400, ppid: 1, command: "/odd/path with space/x --flag", ucomm: "x", cwd: "/other/proj", pgrep: true},
		"UCOMM が版番号 (版の実体を直接起動)":           {pid: 400, ppid: 1, command: "/opt/x/run", ucomm: "2.1.306", cwd: "/other/proj"},
		"command が npm の形 (node …/claude)": {pid: 400, ppid: 1, command: "node /opt/homebrew/bin/claude", ucomm: "node", cwd: "/other/proj"},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.deadSession("-other-proj", deadSID, 30)
			f.sys.procs = append(f.sys.procs, p)
			_, out := mustPlan(t, f, "session")
			if countCandidates(out) != 0 {
				t.Fatalf("この口で拾えず、生きているプロジェクトのセッションが候補になった\n%s", out)
			}
		})
	}
}

// 生きているセッションの ID は、子孫のシェルの環境変数と argv の -r / --resume から拾う (本体の環境変数には無い)。
func TestSessionIDSources(t *testing.T) {
	cases := map[string]fproc{
		"子のシェルの環境変数":       {pid: 401, ppid: 1, command: "/bin/zsh", ucomm: "zsh", env: "CLAUDE_CODE_SESSION_ID=" + deadSID},
		"argv の -r":        {pid: 401, ppid: 1, command: "sh -c claude-wrapper -r " + deadSID, ucomm: "sh"},
		"argv の --resume=": {pid: 401, ppid: 1, command: "sh -c run --resume=" + deadSID, ucomm: "sh"},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.deadSession("-gone-proj", deadSID, 30)
			f.sys.procs = append(f.sys.procs, p)
			_, out := mustPlan(t, f, "session")
			if countCandidates(out) != 0 || !strings.Contains(out, "生きているセッション") {
				t.Fatalf("ID が生きているセッションが候補になった\n%s", out)
			}
		})
	}
}

// 生きているかを確実に判定できないときは、session の種類ごと止める (「生きていない」に丸めない)。
func TestUndecidableStops(t *testing.T) {
	cases := map[string]func(f *fixture){
		"cwd が ASCII 以外": func(f *fixture) {
			f.sys.procs = append(f.sys.procs, fproc{pid: 400, ppid: 1, command: "claude", ucomm: "claude", cwd: "/w/日本語", pgrep: true})
		},
		"cwd が lsof のエスケープ": func(f *fixture) {
			f.sys.procs = append(f.sys.procs, fproc{pid: 400, ppid: 1, command: "claude", ucomm: "claude", cwd: `/w/a\tb`, pgrep: true})
		},
		"cwd の project が scratchpad に無い (起動後に cwd を移した恐れ)": func(f *fixture) {
			f.sys.procs = append(f.sys.procs, fproc{pid: 400, ppid: 1, command: "claude", ucomm: "claude", cwd: "/nowhere", pgrep: true})
		},
		"cwd が起動時の PWD と違う": func(f *fixture) {
			f.mkdir(filepath.Join(f.session, "-moved-to", "cccccccc-0000-0000-0000-000000000009"))
			f.sys.procs = append(f.sys.procs, fproc{pid: 400, ppid: 1, command: "claude", ucomm: "claude", env: "PWD=/started/here", cwd: "/moved/to", pgrep: true})
		},
		"生きている claude の cwd を読めない": func(f *fixture) {
			f.sys.procs = append(f.sys.procs, fproc{pid: 400, ppid: 1, command: "claude", ucomm: "claude", pgrep: true})
		},
		"祖先に claude が居ない (拾い方の失敗)": func(f *fixture) {
			f.sys.procs[0].pgrep, f.sys.procs[0].ucomm, f.sys.procs[0].command = false, "x", "/opt/other"
		},
		"自分の claude の project と自分の scratchpad が違う": func(f *fixture) {
			f.mkdir(filepath.Join(f.session, "-elsewhere", "dddddddd-0000-0000-0000-000000000009"))
			f.sys.procs[0].cwd = "/elsewhere"
		},
		"自分の ID が無い": func(f *fixture) { f.d.selfSID = "" },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.deadSession("-dead-proj", deadSID, 30)
			setup(f)
			rc, out, errOut := f.runCLI("plan", "-kinds", "session", "-out", f.out)
			if rc != exitStopped {
				t.Fatalf("rc=%d, want %d (止める)\n%s\n%s", rc, exitStopped, out, errOut)
			}
		})
	}
}

// 開かれているもの・新しいもの・socket を含むもの・symlink・システムの置き場は候補にしない。
func TestTmpdirKeeps(t *testing.T) {
	f := newFixture(t)
	opened := f.oldTmp("opened-dir", 30)
	f.sys.open = []string{filepath.Join(opened, "f.txt")}
	fresh := f.oldTmp("has-new-file", 30)
	if err := os.WriteFile(filepath.Join(fresh, "new.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	newer := testNow.Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(fresh, "new.txt"), newer, newer)
	emptyNew := filepath.Join(f.tmpdir, "tmp.just-made")
	if err := os.Mkdir(emptyNew, 0o700); err != nil { // mktemp -d で作ったばかりの空の dir
		t.Fatal(err)
	}
	sock := f.oldTmp("nvim.me", 30)
	if err := syscall.Mkfifo(filepath.Join(sock, "s"), 0o600); err != nil { // socket はパスの長さの上限で作れないことがあるので FIFO で同じ判定を通す
		t.Fatal(err)
	}
	f.age(sock, 30)
	if err := os.Symlink(f.trash, filepath.Join(f.tmpdir, "link")); err != nil {
		t.Fatal(err)
	}
	f.oldTmp("com.apple.something", 30)
	_, out := mustPlan(t, f, "tmpdir")
	for _, want := range []string{"kept (開かれている): 1", "kept (新しい): 2", "kept (socket / FIFO を含む): 1", "kept (symlink): 1", "kept (システム): 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が無い", want)
		}
	}
	if countCandidates(out) != 0 {
		t.Fatalf("候補が出た\n%s", out)
	}
}

// plan を書き換えても、移すものは増やせない: 根そのもの・project dir・根の外・システムの置き場を足して ID を計算し直しても、
// apply は今の候補との交わりしか移さない。
func TestTamperedPlanCannotAddTargets(t *testing.T) {
	f := newFixture(t)
	f.deadSession("-dead-proj", deadSID, 30)
	outside := f.mkdir(filepath.Join(filepath.Dir(f.session), "outside"))
	f.age(outside, 30)
	sys := f.oldTmp("com.apple.precious", 30)
	_, path := f.plan("session,tmpdir")
	var p planFile
	body, _ := os.ReadFile(path)
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	for _, extra := range []struct{ kind, path string }{
		{kindSession, filepath.Join(f.session, "-dead-proj")},
		{kindTmpdir, f.tmpdir},
		{kindTmpdir, outside},
		{kindTmpdir, sys},
		{kindSession, filepath.Join(f.session, "-self-proj", selfSID)},
	} {
		e, ok := newEntry(extra.kind, extra.path)
		if !ok {
			t.Fatalf("同一性を取れない: %s", extra.path)
		}
		p.Entries = append(p.Entries, e)
	}
	p.ID = planID(p.CreatedAt, p.Selection, p.Entries)
	body, _ = json.Marshal(p)
	tampered := filepath.Join(f.out, "plan-"+p.ID+".json")
	if err := os.WriteFile(tampered, body, 0o600); err != nil {
		t.Fatal(err)
	}
	rc, out, errOut := f.apply(p.ID, tampered)
	if rc != exitOK {
		t.Fatalf("rc=%d\n%s\n%s", rc, out, errOut)
	}
	for _, keep := range []string{filepath.Join(f.session, "-dead-proj"), f.tmpdir, outside, sys, filepath.Join(f.session, "-self-proj", selfSID)} {
		if !exists(keep) {
			t.Fatalf("書き換えで足したものが移った: %s\n%s", keep, out)
		}
	}
	if strings.Count(out, "kept (今は候補でない)") != 5 {
		t.Fatalf("足した 5 件が「今は候補でない」で残っていない\n%s", out)
	}
}

// ID と中身・承認・古さが合わない plan では 1 件も動かさない。
func TestPlanChecksStop(t *testing.T) {
	cases := map[string]func(f *fixture, id, path string) (string, string){
		"中身を書き換えて ID はそのまま": func(f *fixture, id, path string) (string, string) {
			body, _ := os.ReadFile(path)
			body = []byte(strings.Replace(string(body), `"session_days": 7`, `"session_days": 1`, 1))
			_ = os.WriteFile(path, body, 0o600)
			return id, path
		},
		"承認した ID が違う": func(f *fixture, id, path string) (string, string) { return "0000000000000000", path },
		"1 時間を超えた plan": func(f *fixture, id, path string) (string, string) {
			f.d.now = func() time.Time { return testNow.Add(2 * time.Hour) }
			return id, path
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			s := f.deadSession("-dead-proj", deadSID, 30)
			id, path := f.plan("session")
			id, path = mutate(f, id, path)
			rc, out, errOut := f.apply(id, path)
			if rc != exitStopped || !exists(s) {
				t.Fatalf("rc=%d (want %d) exists=%v\n%s\n%s", rc, exitStopped, exists(s), out, errOut)
			}
		})
	}
}

// 移す直前の取り直し: plan の後に差し替わった・開かれた・判定不能・claude が再開した、どれも移さない。
func TestApplyRechecksBeforeMove(t *testing.T) {
	cases := map[string]func(f *fixture, s string){
		"差し替わった (同じ名前の別物)": func(f *fixture, s string) {
			if err := os.RemoveAll(s); err != nil {
				t.Fatal(err)
			}
			f.mkdir(filepath.Join(s, "scratchpad"))
			f.age(s, 30)
		},
		// 本物の lsof +D は開いているものを見つけても rc=1 で stdout に出す (2026-10-21 実測)
		"移す直前に開かれた": func(f *fixture, s string) { f.sys.openNow[s] = [3]any{"p1\nf7\nn" + s + "/f\n", "", 1} },
		// 次の 2 つは apply の冒頭の判定 (1 回目の ps) の後、1 件目を移す直前の取り直し (2 回目の ps) で状況を変える。
		// 冒頭で変えると候補の作り直しの段で外れ、移す直前の取り直しを検査しない
		"移す直前に claude -r で再開された": func(f *fixture, s string) {
			f.sys.nPS = 0
			f.sys.onPS = func(n int) {
				if n == 2 {
					f.sys.procs = append(f.sys.procs, fproc{pid: 402, ppid: 1, command: "claude -r " + deadSID, ucomm: "claude"})
				}
			}
		},
		"移す直前に中が更新された": func(f *fixture, s string) {
			f.sys.nPS = 0
			f.sys.onPS = func(n int) {
				if n == 2 {
					_ = os.WriteFile(filepath.Join(s, "scratchpad", "new.txt"), []byte("x"), 0o600)
				}
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			s := f.deadSession("-dead-proj", deadSID, 30)
			id, path := f.plan("session")
			mutate(f, s)
			rc, out, errOut := f.apply(id, path)
			if rc != exitOK || !exists(s) {
				t.Fatalf("rc=%d exists=%v\n%s\n%s", rc, exists(s), out, errOut)
			}
		})
	}
}

// 1 件移した後に止まったら 4 (途中で止めた)。止まる前に移したものは記録に残る。
func TestStopAfterMoveIsExit4(t *testing.T) {
	f := newFixture(t)
	a := f.deadSession("-dead-a", deadSID, 30)
	b := f.deadSession("-dead-b", deadSID2, 30)
	id, path := f.plan("session")
	// apply の中で ps が呼ばれるのは、全体の判定で 1 回 + 1 件目の取り直しで 1 回。2 件目の取り直しで判定不能を起こす
	f.sys.nPS = 0
	f.sys.onPS = func(n int) {
		if n == 3 {
			f.sys.procs = append(f.sys.procs, fproc{pid: 409, ppid: 1, command: "claude", ucomm: "claude", cwd: "/w/日本語", pgrep: true})
		}
	}
	rc, out, errOut := f.apply(id, path)
	if rc != exitStoppedMoved {
		t.Fatalf("rc=%d, want %d\n%s\n%s", rc, exitStoppedMoved, out, errOut)
	}
	if exists(a) || !exists(b) {
		t.Fatalf("1 件目だけが移るはず: a=%v b=%v", exists(a), exists(b))
	}
	body, _ := os.ReadFile(filepath.Join(f.out, "moved-"+id+".jsonl"))
	if !strings.Contains(string(body), a) {
		t.Fatalf("止まる前に移したものが記録に無い: %s", body)
	}
}

// 前提が崩れていたら 1 件も動かさずに 2: TMPDIR が空で tmpdir を選んだ・lsof が失敗した・ゴミ箱の経路に symlink がある。
func TestPreconditionsStop(t *testing.T) {
	t.Run("TMPDIR が空", func(t *testing.T) {
		f := newFixture(t)
		f.d.tmpDirEnv = ""
		rc, _, _ := f.runCLI("plan", "-kinds", "tmpdir", "-out", f.out)
		if rc != exitStopped {
			t.Fatalf("rc=%d", rc)
		}
	})
	t.Run("全体の lsof が失敗", func(t *testing.T) {
		f := newFixture(t)
		f.sys.lsofAll = func() (string, int) { return "p1\nn/x\n", 1 }
		rc, _, _ := f.runCLI("plan", "-kinds", "tmpdir", "-out", f.out)
		if rc != exitStopped {
			t.Fatalf("rc=%d", rc)
		}
	})
	t.Run("ゴミ箱の経路に symlink", func(t *testing.T) {
		f := newFixture(t)
		s := f.deadSession("-dead-proj", deadSID, 30)
		id, path := f.plan("session")
		real := filepath.Join(filepath.Dir(f.trash), "real-trash")
		if err := os.Mkdir(real, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(f.trash); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, f.trash); err != nil {
			t.Fatal(err)
		}
		rc, out, errOut := f.apply(id, path)
		if rc != exitStopped || !exists(s) {
			t.Fatalf("rc=%d exists=%v\n%s\n%s", rc, exists(s), out, errOut)
		}
		if ents, _ := os.ReadDir(real); len(ents) != 0 {
			t.Fatalf("symlink の先に dir ができた: %v", ents)
		}
	})
}

// tmpclean 自身の検査点: 根の配下の決まった深さだけを移し、今回の移し先の外へは移さない (候補の選定が壊れても効く 2 段目)。
func TestTrashGuard(t *testing.T) {
	r := roots{session: "/r/claude-501", tmpdir: "/r/T"}
	g := trashGuard(r, "/h/.Trash/tmpclean-x", func(string, string) error { return nil })
	ok := [][2]string{
		{"trash", "/r/claude-501/-p/" + deadSID},
		{"trash", "/r/T/go-build1"},
		{"trash-dest", "/h/.Trash/tmpclean-x/session/-p"},
	}
	ng := [][2]string{
		{"trash", "/r/claude-501"},
		{"trash", "/r/claude-501/-p"},
		{"trash", "/r/claude-501/-p/" + deadSID + "/scratchpad"},
		{"trash", "/r/T"},
		{"trash", "/r/T/a/b"},
		{"trash", "/r/other/x"},
		{"trash", "/r/claude-501/-p/../../x"},
		{"trash-dest", "/h/.Trash"},
		{"trash-dest", "/h/.Trash/tmpclean-xy"},
		{"rename", "/r/T/go-build1"},
	}
	for _, c := range ok {
		if err := g(c[0], c[1]); err != nil {
			t.Errorf("通るはず: %v: %v", c, err)
		}
	}
	for _, c := range ng {
		if err := g(c[0], c[1]); err == nil {
			t.Errorf("拒否するはず: %v", c)
		}
	}
}

func TestCount(t *testing.T) {
	f := newFixture(t)
	f.oldTmp("treefiler-config-123456", 30)
	f.oldTmp("treefiler-config-654321", 30)
	if err := os.Mkdir(filepath.Join(f.tmpdir, "tmp.Abc123"), 0o700); err != nil {
		t.Fatal(err)
	}
	rc, out, errOut := f.runCLI("count", "-json")
	if rc != exitOK {
		t.Fatalf("rc=%d %s", rc, errOut)
	}
	var rep Counts
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Tmpdir == nil || rep.Tmpdir.TopLevel != 3 || rep.Tmpdir.EmptyDirs != 1 || rep.Tmpdir.Total != 5 {
		t.Fatalf("tmpdir の集計が違う: %+v", rep.Tmpdir)
	}
	if len(rep.Tmpdir.Prefixes) == 0 || rep.Tmpdir.Prefixes[0] != (Part{Name: "treefiler-config-", Count: 2}) {
		t.Fatalf("接頭辞の集計が違う: %+v", rep.Tmpdir.Prefixes)
	}
}

func mustPlan(t *testing.T, f *fixture, kinds string) (string, string) {
	t.Helper()
	rc, out, errOut := f.runCLI("plan", "-kinds", kinds, "-out", f.out)
	if rc != exitOK {
		t.Fatalf("plan rc=%d\n%s\n%s", rc, out, errOut)
	}
	return errOut, out
}

func countCandidates(out string) int {
	return strings.Count(out, "\ncandidate (") + boolInt(strings.HasPrefix(out, "candidate ("))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// セッションが終わった後も、そこで起こした背景のプロセスが scratchpad の中を開いていれば移さない。
func TestOpenSessionIsKept(t *testing.T) {
	f := newFixture(t)
	s := f.deadSession("-dead-proj", deadSID, 30)
	f.sys.open = []string{filepath.Join(s, "scratchpad", "f.txt")}
	_, out := mustPlan(t, f, "session")
	if countCandidates(out) != 0 || !strings.Contains(out, "kept (開かれている): 1") {
		t.Fatalf("開かれているセッションが候補になった\n%s", out)
	}
}

// lsof が返す 3 つの形を、根と同じ /private 付きの形に揃える (揃えないと開かれているものを見落とす)。
func TestNormalizeLsofPath(t *testing.T) {
	cases := map[string]string{
		"/var/folders/x/y/T/a":                             "/private/var/folders/x/y/T/a",
		"/private/var/folders/x/y/T/a":                     "/private/var/folders/x/y/T/a",
		"/System/Volumes/Data/private/var/folders/x/y/T/a": "/private/var/folders/x/y/T/a",
		"/tmp/claude-501/p":                                "/private/tmp/claude-501/p",
		"/Users/me/x":                                      "/Users/me/x",
	}
	for in, want := range cases {
		if got := normalizeLsofPath(in); got != want {
			t.Errorf("normalizeLsofPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// lsof で使用中かを判定できなければ残し、「移せなかった」(rc=1) に数える (全件が判定不能でも成功に見せない)。
func TestOpenUnknownIsFailure(t *testing.T) {
	f := newFixture(t)
	s := f.deadSession("-dead-proj", deadSID, 30)
	id, path := f.plan("session")
	f.sys.openNow[s] = [3]any{"", "lsof: status error on " + s + ": Resource temporarily unavailable", 1}
	rc, out, errOut := f.apply(id, path)
	if rc != exitFailed || !exists(s) || !strings.Contains(out, "failed (lsof で使用中かを判定できない)") {
		t.Fatalf("rc=%d exists=%v\n%s\n%s", rc, exists(s), out, errOut)
	}
}

// 本物の lsof を使い捨ての dir に当てる canary: fake の応答の形が本物と同じであることを、ここで固定する
// (fake が本物と違う rc を返していて、本物では毎回「判定不能」になっていたことがある)。
func TestOpenNowRealLsof(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{run: runner.Exec}
	fh, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := openNow(context.Background(), d, dir, true); got != isOpen {
		t.Errorf("開いている dir: %v, want isOpen", got)
	}
	if got := openNow(context.Background(), d, file, false); got != isOpen {
		t.Errorf("開いているファイル: %v, want isOpen", got)
	}
	_ = fh.Close()
	if got := openNow(context.Background(), d, dir, true); got != notOpen {
		t.Errorf("閉じた dir: %v, want notOpen", got)
	}
	if got := openNow(context.Background(), d, file, false); got != notOpen {
		t.Errorf("閉じたファイル: %v, want notOpen", got)
	}
}

// 長く動く daemon の一時 dir (中が空で mtime は起動時刻のまま) は、名前が動いているプロセスと同じなら残す。
func TestRunningDaemonDirIsKept(t *testing.T) {
	f := newFixture(t)
	d := filepath.Join(f.tmpdir, "contentlinkingd")
	if err := os.Mkdir(d, 0o700); err != nil {
		t.Fatal(err)
	}
	f.age(d, 30)
	f.sys.procs = append(f.sys.procs, fproc{pid: 500, ppid: 1, command: "/usr/libexec/contentlinkingd", ucomm: "contentlinkingd"})
	_, out := mustPlan(t, f, "tmpdir")
	if countCandidates(out) != 0 || !strings.Contains(out, "kept (動いているプロセスと同じ名前): 1") {
		t.Fatalf("動いている daemon の dir が候補になった\n%s", out)
	}
}

// ブラウザ連携の常駐 (claude --chrome-native-host) は除外するが、引数に同じ語を含む普通のセッションは守る。
func TestChromeNativeHostExclusion(t *testing.T) {
	f := newFixture(t)
	f.deadSession("-other-proj", deadSID, 30)
	f.sys.procs = append(f.sys.procs,
		fproc{pid: 600, ppid: 1, command: "/Users/me/.local/bin/claude --chrome-native-host", ucomm: "claude", cwd: "/Users/me/.claude/chrome", pgrep: true},
		fproc{pid: 601, ppid: 1, command: "/Users/me/.local/bin/claude --chrome-native-host を直して", ucomm: "claude", cwd: "/other/proj", pgrep: true},
		// ps の command は引用符を落とすので、末尾がその語のプロンプトも同じ形に見える
		fproc{pid: 602, ppid: 1, command: "/Users/me/.local/bin/claude 直して --chrome-native-host", ucomm: "claude", cwd: "/other2/proj", pgrep: true},
	)
	f.deadSession("-other2-proj", deadSID2, 30)
	_, out := mustPlan(t, f, "session")
	if countCandidates(out) != 0 {
		t.Fatalf("引数に --chrome-native-host を含むセッションが守られていない\n%s", out)
	}
}

// 移し先 (~/.Trash/tmpclean-<日時>-<ID>) に先に symlink を置かれていたら、その先に何も作らずに止める。
func TestTrashBaseSymlinkStops(t *testing.T) {
	f := newFixture(t)
	s := f.deadSession("-dead-proj", deadSID, 30)
	id, path := f.plan("session")
	elsewhere := filepath.Join(filepath.Dir(f.trash), "elsewhere")
	if err := os.Mkdir(elsewhere, 0o700); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(f.trash, "tmpclean-"+testNow.Format("20060102-150405")+"-"+id)
	if err := os.Symlink(elsewhere, base); err != nil {
		t.Fatal(err)
	}
	rc, out, errOut := f.apply(id, path)
	if rc != exitStopped || !exists(s) {
		t.Fatalf("rc=%d exists=%v\n%s\n%s", rc, exists(s), out, errOut)
	}
	if ents, _ := os.ReadDir(elsewhere); len(ents) != 0 {
		t.Fatalf("symlink の先に作った: %v", ents)
	}
}

// scratchpad はプロジェクトごとの配下の件数を多い順に出す (doctor のタブの「最多」)。
func TestCountSessionProjects(t *testing.T) {
	f := newFixture(t)
	f.deadSession("-big-proj", deadSID, 30)
	f.mkdir(filepath.Join(f.session, "-big-proj", deadSID2, "a", "b"))
	rep, err := count(context.Background(), f.d)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Session.Projects) < 2 || rep.Session.Projects[0].Name != "-big-proj" || rep.Session.Projects[0].Count != 8 { // project 自身・セッション 2・scratchpad・a・b・ファイル 2
		t.Fatalf("内訳が違う: %+v", rep.Session.Projects)
	}
}

// scratchpad の根が無いのは 0 件として数え、$TMPDIR は数える (Claude Code を使っていない機械で全体を判定不能にしない)。
// plan / apply は根が無ければ止める (別の経路)。
func TestCountWithoutSessionRoot(t *testing.T) {
	f := newFixture(t)
	f.oldTmp("go-build123456", 30)
	f.d.sessionRoot = filepath.Join(filepath.Dir(f.session), "claude-missing")
	rep, err := count(context.Background(), f.d)
	if err != nil {
		t.Fatalf("根が無いだけで止まった: %v", err)
	}
	if rep.Session.Total != 0 || rep.Tmpdir == nil || rep.Tmpdir.TopLevel != 1 {
		t.Fatalf("集計が違う: session=%+v tmpdir=%+v", rep.Session, rep.Tmpdir)
	}
	if rc, _, _ := f.runCLI("plan", "-kinds", "session", "-out", f.out); rc != exitStopped {
		t.Fatalf("plan は根が無ければ止めるはず: rc=%d", rc)
	}
}

// ctx が終わったら走査を打ち切り、ctx の error を返す (glogx の doctor が閉じるときに数え終わるのを待たない)。
func TestCountStopsOnCancel(t *testing.T) {
	f := newFixture(t)
	f.oldTmp("a123456", 30)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := count(ctx, f.d); !errors.Is(err, context.Canceled) {
		t.Fatalf("打ち切られていない: %v", err)
	}
}

func TestCountDir(t *testing.T) {
	f := newFixture(t)
	d := f.mkdir(filepath.Join(f.out, "x", "y"))
	_ = d
	c, err := CountDir(context.Background(), f.out)
	if err != nil {
		t.Fatal(err)
	}
	if c.Total != 3 || c.TopLevel != 1 || c.EmptyDirs != 0 || len(c.Projects)+len(c.Prefixes) != 0 {
		t.Fatalf("CountDir が違う: %+v", c)
	}
}
