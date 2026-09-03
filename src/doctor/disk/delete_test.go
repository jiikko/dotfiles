package disk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// deleteRunner は stdout / stderr / rc / err を**別々に**返す fake。scan_test.go の fakeRunner は
// 一致した応答の stderr を固定文字列にしてしまうので、削除側 (stderr にしか真相が無い形を再現する)
// では使えない。onCall で「コマンドが実際にディスクを変える」も模せる。
type deleteRunner struct {
	mu     sync.Mutex
	calls  [][]string
	resp   map[string]cmdResp // key: コマンド行の prefix
	onCall func(args []string)
}

type cmdResp struct {
	stdout, stderr string
	rc             int
	err            error
}

func (f *deleteRunner) run(_ context.Context, name string, args ...string) (string, string, int, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	f.mu.Lock()
	f.calls = append(f.calls, append([]string{name}, args...))
	f.mu.Unlock()
	if f.onCall != nil {
		f.onCall(append([]string{name}, args...))
	}
	best := ""
	for k := range f.resp {
		if !strings.HasPrefix(line, k) {
			continue
		}
		if len(line) > len(k) && line[len(k)] != ' ' {
			continue
		}
		if len(k) > len(best) {
			best = k
		}
	}
	if best == "" {
		return "", "unexpected: " + line, 1, nil
	}
	r := f.resp[best]
	return r.stdout, r.stderr, r.rc, r.err
}

func (f *deleteRunner) callLines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

// deleteFixture は「HOME の下にツリーがあり、それを指すカタログ 1 件」を用意して走査まで通す。
type deleteFixture struct {
	env    Env
	entry  Entry
	target string
	run    *deleteRunner
	opt    DeleteOptions
}

func newDeleteFixture(t *testing.T, e Entry, kb int) *deleteFixture {
	t.Helper()
	env := testEnv(t)
	target := filepath.Join(env.Home, "Library", "Caches", "testcache")
	mkfile(t, filepath.Join(target, "blob"), kb)
	e.Paths = []string{"~/Library/Caches/testcache"}
	run := &deleteRunner{}
	return &deleteFixture{env: env, entry: e, target: target, run: run,
		opt: DeleteOptions{Env: env, Run: run.run, BootTime: okBoot, Catalog: []Entry{e},
			HistoryDir: filepath.Join(t.TempDir(), "history"),
			TrashDir:   filepath.Join(env.Home, ".Trash"), Now: time.Now}}
}

func (f *deleteFixture) scan(t *testing.T) Result {
	t.Helper()
	rep := Scan(context.Background(), Options{Env: f.env, Run: f.run.run, Catalog: []Entry{f.entry}, BootTime: okBoot})
	if rep.Results[0].Status != StatusOK {
		t.Fatalf("前提が崩れている: 走査が ok でない: %+v", rep.Results[0])
	}
	return rep.Results[0]
}

func (f *deleteFixture) delete(t *testing.T, r Result) DeleteReport {
	t.Helper()
	rep, err := Delete(context.Background(), []Result{r}, f.opt)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	return rep
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

var rmEntry = Entry{ID: "testcache", Label: "テストキャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm", Recover: "再生成されます"}

// 🚨 走査していない結果 (Reused / FromSnapshot) と、走査に失敗した結果は削除しない。
// これが崩れると、ユーザー権限で書き換えられる doctor-snapshot.json の任意パスが削除対象になる。
func TestDeleteRefusesUnscannedResults(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*Result)
		want string
	}{
		{"Reused", func(r *Result) { r.Reused = true }, "再利用"},
		{"FromSnapshot", func(r *Result) { r.FromSnapshot = true }, "復元"},
		{"blocked", func(r *Result) { r.Status = StatusBlocked }, "走査できていない"},
		{"failed", func(r *Result) { r.Status = StatusFailed }, "走査できていない"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDeleteFixture(t, rmEntry, 64)
			r := f.scan(t)
			tc.mut(&r)
			rep := f.delete(t, r)
			got := rep.Entries[0]
			if got.Outcome != OutcomeFailed {
				t.Fatalf("outcome = %s (failed であるべき): %+v", got.Outcome, got)
			}
			if !strings.Contains(got.Reason, tc.want) {
				t.Errorf("理由に %q が無い: %q", tc.want, got.Reason)
			}
			if !exists(f.target) {
				t.Fatal("拒否したのに対象が消えている")
			}
		})
	}
}

// 削除の作法と危険度は、渡された Result.Entry ではなく**カタログ**から引き直す
// (Result は保存経路を通ってくるので、DeleteVia を差し替えられる)。
func TestDeleteResolvesMethodFromCatalogNotResult(t *testing.T) {
	f := newDeleteFixture(t, Entry{ID: "testcache", Label: "cli のもの", Tier: 1, Risk: RiskSafe,
		DeleteVia: "cli:faketool purge", Recover: "x"}, 64)
	f.run.resp = map[string]cmdResp{"faketool purge": {rc: 0}}
	r := f.scan(t)
	r.Entry.DeleteVia = "rm" // 細工: rm に差し替える
	rep := f.delete(t, r)
	if !exists(f.target) {
		t.Fatal("Result 側の DeleteVia を信じて rm した (カタログを正本にすること)")
	}
	if got := rep.Entries[0].Method; got != methodCLI {
		t.Errorf("method = %q, want cli", got)
	}
}

func TestDeleteRefusesUnknownCatalogID(t *testing.T) {
	f := newDeleteFixture(t, rmEntry, 64)
	r := f.scan(t)
	r.Entry.ID = "not-in-catalog"
	rep := f.delete(t, r)
	if rep.Entries[0].Outcome != OutcomeFailed {
		t.Fatalf("outcome = %s", rep.Entries[0].Outcome)
	}
	if !exists(f.target) {
		t.Fatal("カタログにない ID なのに消した")
	}
}

// risk: confirm はユーザーのファイルでありうるので、ゴミ箱以外の経路では削除しない。
func TestDeleteRiskConfirmRefusesNonTrash(t *testing.T) {
	f := newDeleteFixture(t, Entry{ID: "testcache", Label: "confirm なのに rm", Tier: 3,
		Risk: RiskConfirm, DeleteVia: "rm", Recover: "x", Inspect: true}, 64)
	rep := f.delete(t, f.scan(t))
	if rep.Entries[0].Outcome != OutcomeFailed {
		t.Fatalf("outcome = %s (failed であるべき)", rep.Entries[0].Outcome)
	}
	if !exists(f.target) {
		t.Fatal("risk: confirm を rm した")
	}
}

// カタログの不変条件: risk: confirm は必ずゴミ箱経路。
func TestCatalogConfirmEntriesUseTrash(t *testing.T) {
	n := 0
	for _, e := range catalog {
		if e.Risk != RiskConfirm {
			continue
		}
		n++
		if e.DeleteVia != "trash" {
			t.Errorf("%s: risk confirm なのに deleteVia=%q (trash にすること)", e.ID, e.DeleteVia)
		}
	}
	if n == 0 {
		t.Fatal("risk: confirm のエントリが 1 件も無い (検査が空振りしている)")
	}
}

// カタログの全エントリが解釈できる作法を持つ (未知の綴りを混ぜたら落ちる)。
func TestCatalogDeleteViaIsParsable(t *testing.T) {
	if len(catalog) == 0 {
		t.Fatal("カタログが空")
	}
	for _, e := range catalog {
		if _, _, err := parseDeleteVia(e.DeleteVia); err != nil {
			t.Errorf("%s: %v", e.ID, err)
		}
	}
}

func TestDeleteRmRemovesAndCountsFreedFromRescan(t *testing.T) {
	f := newDeleteFixture(t, rmEntry, 256)
	r := f.scan(t)
	rep := f.delete(t, r)
	got := rep.Entries[0]
	if got.Outcome != OutcomeDeleted {
		t.Fatalf("outcome = %s: %+v", got.Outcome, got)
	}
	if exists(f.target) {
		t.Fatal("削除されていない")
	}
	if got.AfterSize != 0 || got.Freed != got.BeforeSize || got.Freed <= 0 {
		t.Errorf("before=%d after=%d freed=%d", got.BeforeSize, got.AfterSize, got.Freed)
	}
	if rep.Freed != got.Freed {
		t.Errorf("合計 freed = %d, entry = %d", rep.Freed, got.Freed)
	}
}

// TOCTOU: 走査後に実体が差し替わっていたら触らない。
func TestDeleteSkipsWhenIdentityChanged(t *testing.T) {
	f := newDeleteFixture(t, rmEntry, 64)
	r := f.scan(t)
	if err := os.RemoveAll(f.target); err != nil {
		t.Fatal(err)
	}
	mkfile(t, filepath.Join(f.target, "other"), 8) // 同じパス・別 inode
	rep := f.delete(t, r)
	got := rep.Entries[0]
	if !exists(filepath.Join(f.target, "other")) {
		t.Fatal("差し替わった実体を消した (走査時の (dev, ino) と照合すること)")
	}
	if got.Items[0].Outcome != OutcomeSkipped || !strings.Contains(got.Items[0].Reason, "別の実体") {
		t.Errorf("item = %+v", got.Items[0])
	}
}

var cliEntry = Entry{ID: "testcache", Label: "cli で消すもの", Tier: 1, Risk: RiskSafe,
	DeleteVia: "cli:faketool purge", Recover: "x"}

// `deleteVia: cli:` の対象を rm しない。
func TestDeleteCLIDoesNotRemovePath(t *testing.T) {
	f := newDeleteFixture(t, cliEntry, 64)
	f.run.resp = map[string]cmdResp{"faketool purge": {rc: 0}}
	rep := f.delete(t, f.scan(t))
	if !exists(f.target) {
		t.Fatal("cli: の対象を rm した")
	}
	if want := "faketool purge"; len(f.run.callLines()) == 0 || f.run.callLines()[0] != want {
		t.Fatalf("実行されたコマンド = %v, want %q", f.run.callLines(), want)
	}
	// コマンドは成功したが対象は残っている = 第 3 の状態
	if got := rep.Entries[0].Outcome; got != OutcomeIncomplete {
		t.Fatalf("outcome = %s (incomplete であるべき)", got)
	}
	if rep.Entries[0].Freed != 0 {
		t.Errorf("消えていないのに freed=%d", rep.Entries[0].Freed)
	}
}

// cli が実際に消したときは、再走査で確認してから deleted にする。
func TestDeleteCLIVerifiedByRescan(t *testing.T) {
	f := newDeleteFixture(t, cliEntry, 128)
	f.run.resp = map[string]cmdResp{"faketool purge": {rc: 0}}
	f.run.onCall = func(args []string) {
		if strings.Join(args, " ") == "faketool purge" {
			_ = os.RemoveAll(f.target)
		}
	}
	rep := f.delete(t, f.scan(t))
	got := rep.Entries[0]
	if got.Outcome != OutcomeDeleted || got.Freed != got.BeforeSize {
		t.Fatalf("outcome=%s freed=%d before=%d", got.Outcome, got.Freed, got.BeforeSize)
	}
}

// 外部コマンドの stdout / stderr / exit code を分けて扱う (simctl は rc≠0 + stderr のみを返す)。
func TestDeleteCLIKeepsStreamsSeparate(t *testing.T) {
	f := newDeleteFixture(t, cliEntry, 64)
	f.run.resp = map[string]cmdResp{"faketool purge": {stdout: "", stderr: "Please retry in 48s", rc: 24}}
	rep := f.delete(t, f.scan(t))
	got := rep.Entries[0]
	if len(got.Commands) != 1 {
		t.Fatalf("コマンドの記録が %d 件", len(got.Commands))
	}
	rec := got.Commands[0]
	if rec.RC != 24 || rec.Stderr != "Please retry in 48s" || rec.Stdout != "" {
		t.Errorf("記録 = %+v (rc / stdout / stderr を分けて残すこと)", rec)
	}
	if got.Outcome != OutcomeFailed || !strings.Contains(got.Items[0].Reason, "Please retry in 48s") {
		t.Errorf("outcome=%s item=%+v (stderr にしか真相が無い形)", got.Outcome, got.Items[0])
	}
	if !exists(f.target) {
		t.Fatal("失敗したのに対象が消えている")
	}
}

// simctl 経路: `<id>` にランタイム識別子が入り、SIP 配下のパスには触らない。
func TestDeleteSimRuntimeUsesIdentifierNotPath(t *testing.T) {
	env := testEnv(t)
	rtPath := filepath.Join(env.Home, "fake-runtime")
	mkfile(t, filepath.Join(rtPath, "blob"), 32)
	e := Entry{ID: "simulator-runtimes", Label: "ランタイム", Tier: 1, Risk: RiskCaution,
		DeleteVia: "cli:xcrun simctl runtime delete <id>", Recover: "x", Guard: GuardSimRuntime}
	list := `{"a":{"identifier":"ABC-123.def_x","runtimeIdentifier":"com.apple.iOS-18-0","version":"18.0","build":"22A","sizeBytes":1024,"lastUsedAt":"2026-01-01T00:00:00Z","path":"` + rtPath + `"}}`
	run := &deleteRunner{resp: map[string]cmdResp{
		"xcrun simctl runtime list":   {stdout: list},
		"xcrun simctl runtime delete": {stderr: "Please retry in 48s", rc: 24},
	}}
	opt := DeleteOptions{Env: env, Run: run.run, BootTime: okBoot, Catalog: []Entry{e},
		HistoryDir: filepath.Join(t.TempDir(), "h"), TrashDir: filepath.Join(env.Home, ".Trash"), Now: time.Now}
	rep := Scan(context.Background(), Options{Env: env, Run: run.run, Catalog: []Entry{e}, BootTime: okBoot})
	out, err := Delete(context.Background(), rep.Results, opt)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range run.callLines() {
		if l == "xcrun simctl runtime delete ABC-123.def_x" {
			found = true
		}
	}
	if !found {
		t.Fatalf("識別子を渡していない: %v", run.callLines())
	}
	if !exists(rtPath) {
		t.Fatal("simctl 経路のパスを rm した (SIP 配下なので触ってはいけない)")
	}
	if out.Entries[0].Outcome != OutcomeFailed {
		t.Errorf("outcome = %s (rc=24 は失敗)", out.Entries[0].Outcome)
	}
}

func TestDeleteProposeExecutesNothing(t *testing.T) {
	f := newDeleteFixture(t, Entry{ID: "testcache", Label: "提示だけ", Tier: 4, Risk: RiskCaution,
		DeleteVia: "propose", Recover: "x"}, 64)
	rep := f.delete(t, f.scan(t))
	if rep.Entries[0].Outcome != OutcomeProposed {
		t.Fatalf("outcome = %s", rep.Entries[0].Outcome)
	}
	if n := len(f.run.callLines()); n != 0 {
		t.Errorf("コマンドを %d 回実行した (propose は実行しない)", n)
	}
	if !exists(f.target) {
		t.Fatal("propose なのに消した")
	}
}

func TestParseDeleteViaRejectsSudoAndUnknown(t *testing.T) {
	for _, s := range []string{"cli:sudo rm -rf /x", "cli:", "rm -rf", "", "trash it"} {
		if _, _, err := parseDeleteVia(s); err == nil {
			t.Errorf("%q を受け入れた", s)
		}
	}
	for _, s := range []string{"rm", "trash", "propose", "cli:go clean -modcache"} {
		if _, _, err := parseDeleteVia(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
}

func TestValidateRefRejectsArgumentInjection(t *testing.T) {
	for _, s := range []string{"", "-f", "--all", "a b", "a;b", "a/b", "$(x)", strings.Repeat("a", 129)} {
		if err := validateRef(s); err == nil {
			t.Errorf("%q を受け入れた", s)
		}
	}
	for _, s := range []string{"ABC-123", "com.apple.CoreSimulator.SimRuntime.iOS-18-0", "a_b:c"} {
		if err := validateRef(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
}

// ゴミ箱: 移動先が埋まっていても**上書きしない**。
func TestTrashMoveDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	trash := filepath.Join(dir, "Trash")
	mkfile(t, filepath.Join(trash, "victim"), 1)
	keep, err := os.ReadFile(filepath.Join(trash, "victim"))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "src", "victim")
	mkfile(t, src, 2)
	dst, err := trashMove(src, trash)
	if err != nil {
		t.Fatal(err)
	}
	if dst != filepath.Join(trash, "victim 2") {
		t.Errorf("移動先 = %q (連番にすること)", dst)
	}
	got, err := os.ReadFile(filepath.Join(trash, "victim"))
	if err != nil || len(got) != len(keep) {
		t.Fatalf("既にゴミ箱にあった同名を上書きした (err=%v len=%d/%d)", err, len(got), len(keep))
	}
	if exists(src) {
		t.Error("移動元が残っている")
	}
}

func TestDeleteTrashMovesAndDoesNotCountAsFreed(t *testing.T) {
	f := newDeleteFixture(t, Entry{ID: "testcache", Label: "ユーザーデータかも", Tier: 3,
		Risk: RiskConfirm, DeleteVia: "trash", Recover: "ゴミ箱から戻せます", Inspect: true}, 128)
	rep := f.delete(t, f.scan(t))
	got := rep.Entries[0]
	if got.Outcome != OutcomeTrashed {
		t.Fatalf("outcome = %s: %+v", got.Outcome, got)
	}
	if exists(f.target) {
		t.Error("移動元が残っている")
	}
	if !exists(filepath.Join(f.opt.TrashDir, "testcache")) {
		t.Error("ゴミ箱に無い")
	}
	if got.Freed != 0 || got.Trashed <= 0 {
		t.Errorf("freed=%d trashed=%d (ゴミ箱移動では容量は戻らない)", got.Freed, got.Trashed)
	}
}

func TestDeleteWritesInventory(t *testing.T) {
	f := newDeleteFixture(t, rmEntry, 64)
	rep := f.delete(t, f.scan(t))
	if rep.HistoryPath == "" || rep.HistoryError != "" {
		t.Fatalf("history=%q err=%q", rep.HistoryPath, rep.HistoryError)
	}
	b, err := os.ReadFile(rep.HistoryPath)
	if err != nil {
		t.Fatal(err)
	}
	var inv inventory
	if err := json.Unmarshal(b, &inv); err != nil {
		t.Fatal(err)
	}
	if inv.Phase != "done" {
		t.Errorf("phase = %q", inv.Phase)
	}
	if len(inv.Report.Entries) != 1 || inv.Report.Entries[0].ID != "testcache" {
		t.Fatalf("記録の中身 = %+v", inv.Report.Entries)
	}
	// パスは validateTarget の正規化 (/var → /private/var) を通っているので末尾で見る
	if len(inv.Report.Entries[0].Items) == 0 || !strings.HasSuffix(inv.Report.Entries[0].Items[0].Path, "/Library/Caches/testcache") {
		t.Errorf("消したパスが記録に無い: %+v", inv.Report.Entries[0].Items)
	}
	if exists(rep.HistoryPath + ".tmp") {
		t.Error("一時ファイルが残っている")
	}
}

// 記録が残せないなら削除しない (fail-closed)。
func TestDeleteFailsClosedWhenHistoryUnwritable(t *testing.T) {
	f := newDeleteFixture(t, rmEntry, 64)
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.opt.HistoryDir = filepath.Join(blocker, "history") // ファイルの下にはディレクトリを作れない
	if _, err := Delete(context.Background(), []Result{f.scan(t)}, f.opt); err == nil {
		t.Fatal("記録を残せないのに削除を続けた")
	}
	if !exists(f.target) {
		t.Fatal("記録を残せないのに消した")
	}
}

func TestDeleteDryRunTouchesNothing(t *testing.T) {
	f := newDeleteFixture(t, rmEntry, 64)
	f.opt.DryRun = true
	rep := f.delete(t, f.scan(t))
	if rep.Entries[0].Outcome != OutcomePlanned {
		t.Fatalf("outcome = %s", rep.Entries[0].Outcome)
	}
	if !exists(f.target) {
		t.Fatal("dry-run で消した")
	}
	if exists(f.opt.HistoryDir) {
		t.Error("dry-run で記録を書いた")
	}
}
