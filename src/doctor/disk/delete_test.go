package disk

import (
	"context"
	"encoding/json"
	"errors"
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
	sandboxAllow(t, env.Home) // ハーネス: この HOME の外は破壊的操作を拒否する
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
	sandboxAllow(t, env.Home)
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
	sandboxAllow(t, dir)
	dev, ino := identityOf(t, src)
	dst, err := trashMove(src, trash, dev, ino)
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
	// 一時ファイルは os.CreateTemp が付ける乱数入りの名前なので、置き場ごと見る
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(rep.HistoryPath), "*.tmp"))
	if len(leftovers) > 0 {
		t.Errorf("一時ファイルが残っている: %v", leftovers)
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

// ---- 敵対的レビュー (2026-09-03) が開けた穴を塞ぐテスト ----

// 🚨 細工した Result: ID だけ本物で Items のパスがそのエントリのものでない。
// Reused / FromSnapshot の 3 フラグを立てなければ素通りしていた (任意パスの削除が成立した)。
func TestDeleteRefusesPathsOutsideTheEntry(t *testing.T) {
	f := newDeleteFixture(t, rmEntry, 64)
	victim := filepath.Join(f.env.Home, "Documents", "photos")
	mkfile(t, filepath.Join(victim, "wedding.jpg"), 8)
	vfi, err := os.Lstat(victim)
	if err != nil {
		t.Fatal(err)
	}
	dev, ino, _ := statIdentity(vfi)

	r := f.scan(t)
	r.Items = []Item{{Path: victim, Size: 8192, Dev: dev, Ino: ino}}
	rep := f.delete(t, r)
	if !exists(victim) {
		t.Fatal("このエントリの対象でないパスを消した")
	}
	got := rep.Entries[0]
	if got.Items[0].Outcome != OutcomeFailed || !strings.Contains(got.Items[0].Reason, "このエントリの対象ではありません") {
		t.Errorf("item = %+v", got.Items[0])
	}
	if got.Freed != 0 {
		t.Errorf("freed = %d (何も消えていない)", got.Freed)
	}
}

// 走査時の実体が識別できない Item (Dev も Ino も 0) は触らない。
// 「識別できないときは素通り」だと、守りが自分を無効化する条件を持つことになる。
func TestDeleteSkipsItemWithoutIdentity(t *testing.T) {
	f := newDeleteFixture(t, rmEntry, 64)
	r := f.scan(t)
	r.Items[0].Dev, r.Items[0].Ino = 0, 0
	rep := f.delete(t, r)
	if !exists(f.target) {
		t.Fatal("識別できない Item を消した")
	}
	if got := rep.Entries[0].Items[0]; got.Outcome != OutcomeSkipped || !strings.Contains(got.Reason, "識別できません") {
		t.Errorf("item = %+v", got)
	}
}

// 複数 Item: 片方だけ差し替わっていたら、そちらだけ飛ばして残りは消す。
// (Item 1 個の fixture だけだと「合計」と「先頭」を取り違える変異が素通りする)
func TestDeleteHandlesMixedItemOutcomes(t *testing.T) {
	env := testEnv(t)
	sandboxAllow(t, env.Home)
	a := filepath.Join(env.Home, "Library", "Caches", "multi", "a")
	b := filepath.Join(env.Home, "Library", "Caches", "multi", "b")
	mkfile(t, filepath.Join(a, "blob"), 128)
	mkfile(t, filepath.Join(b, "blob"), 64)
	e := Entry{ID: "multi", Label: "2 つある", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "x", Paths: []string{"~/Library/Caches/multi/*"}}
	run := &deleteRunner{}
	opt := DeleteOptions{Env: env, Run: run.run, BootTime: okBoot, Catalog: []Entry{e},
		HistoryDir: filepath.Join(t.TempDir(), "h"), TrashDir: filepath.Join(env.Home, ".Trash"), Now: time.Now}
	scan := Scan(context.Background(), Options{Env: env, Run: run.run, Catalog: []Entry{e}, BootTime: okBoot})
	r := scan.Results[0]
	if len(r.Items) != 2 {
		t.Fatalf("前提が崩れている: Item が %d 件", len(r.Items))
	}
	// b だけ差し替える (同じパス・別 inode)
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	mkfile(t, filepath.Join(b, "other"), 32)

	rep, err := Delete(context.Background(), []Result{r}, opt)
	if err != nil {
		t.Fatal(err)
	}
	if exists(a) {
		t.Error("差し替わっていない方が消えていない")
	}
	if !exists(b) {
		t.Error("差し替わった方を消した")
	}
	got := rep.Entries[0]
	if got.Outcome != OutcomeIncomplete {
		t.Errorf("outcome = %s (残りがあるので incomplete)", got.Outcome)
	}
	if len(got.Remaining) != 1 {
		t.Errorf("残り = %v (1 件のはず)", got.Remaining)
	}
	// freed は「消えた a のぶん」ちょうど。飛ばした b は入らない
	var deletedSize int64
	for _, it := range got.Items {
		if it.Outcome == OutcomeDeleted {
			deletedSize += it.Size
		}
	}
	if deletedSize == 0 {
		t.Fatal("前提が崩れている: 消えた Item が無い")
	}
	if got.Freed != deletedSize {
		t.Errorf("freed = %d, 消えた Item の合計 = %d (触った分だけを数えること)", got.Freed, deletedSize)
	}
}

// 削除の直前に実体が差し替わっていたら、fd で掴んだ親から見て違うので触らない。
func TestRemoveItemRefusesLateIdentitySwap(t *testing.T) {
	root := t.TempDir()
	sandboxAllow(t, root)
	target := filepath.Join(root, "tree")
	mkfile(t, filepath.Join(target, "x"), 1)
	dev, ino := identityOf(t, target)
	// plan の後・exec の前に差し替わった状況
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	mkfile(t, filepath.Join(target, "IMPORTANT"), 1)

	o := &ItemOutcome{Path: target, dev: dev, ino: ino}
	if err := removeItem(o); err == nil {
		t.Fatal("差し替わった実体を消した")
	}
	if !exists(filepath.Join(target, "IMPORTANT")) {
		t.Fatal("差し替え後の実体が消えた")
	}
	if o.Outcome != OutcomeSkipped {
		t.Errorf("outcome = %s", o.Outcome)
	}
}

// 中断: ctx が切れていたら、以降のエントリを 1 つも触らない。
func TestDeleteStopsOnCancel(t *testing.T) {
	f := newDeleteFixture(t, rmEntry, 64)
	r := f.scan(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rep, err := Delete(ctx, []Result{r}, f.opt)
	if err != nil {
		t.Fatal(err)
	}
	if !exists(f.target) {
		t.Fatal("中断済みなのに消した")
	}
	if got := rep.Entries[0]; got.Outcome != OutcomeSkipped || !strings.Contains(got.Reason, "中断") {
		t.Errorf("entry = %+v", got)
	}
}

// 再走査が blocked (guard で対象外) を返したら「消えた」にしない。
// blocked は Items が nil / Size 0 で返るので、素通りさせると全額 freed に化ける。
func TestDeleteBlockedRescanIsIncomplete(t *testing.T) {
	env := testEnv(t)
	sandboxAllow(t, env.Home)
	target := filepath.Join(env.Home, "Library", "Caches", "guarded")
	mkfile(t, filepath.Join(target, "blob"), 128)
	e := Entry{ID: "guarded", Label: "プロセス次第", Tier: 1, Risk: RiskSafe,
		DeleteVia: "cli:faketool purge", Recover: "x", Guard: GuardProcessAbsent,
		Processes: []string{"FakeApp"}, Paths: []string{"~/Library/Caches/guarded"}}
	run := &deleteRunner{resp: map[string]cmdResp{
		"pgrep -x FakeApp": {rc: 1}, // 走査時は起動していない
		"faketool purge":   {rc: 0}, // 成功を申告するが実際には何もしない
	}}
	scan := Scan(context.Background(), Options{Env: env, Run: run.run, Catalog: []Entry{e}, BootTime: okBoot})
	if scan.Results[0].Status != StatusOK {
		t.Fatalf("前提が崩れている: %+v", scan.Results[0])
	}
	// 削除の後、再走査の時点では起動している = blocked
	run.mu.Lock()
	run.resp["pgrep -x FakeApp"] = cmdResp{rc: 0}
	run.mu.Unlock()

	rep, err := Delete(context.Background(), scan.Results, DeleteOptions{Env: env, Run: run.run,
		BootTime: okBoot, Catalog: []Entry{e}, HistoryDir: filepath.Join(t.TempDir(), "h"), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	got := rep.Entries[0]
	if got.Outcome != OutcomeIncomplete {
		t.Fatalf("outcome = %s (blocked は「消えた」の根拠にならない): %+v", got.Outcome, got)
	}
	if got.Freed != 0 || rep.Freed != 0 {
		t.Errorf("freed = %d / %d (確認できていないのに数えた)", got.Freed, rep.Freed)
	}
	if !exists(target) {
		t.Fatal("cli: の対象を消した")
	}
}

// コマンドが起動できなかった / 中断されたを、rc≠0 と同じ扱いにしない。
func TestDeleteCLICancelledIsIncompleteNotFailed(t *testing.T) {
	f := newDeleteFixture(t, cliEntry, 64)
	f.run.resp = map[string]cmdResp{"faketool purge": {rc: -1, err: context.DeadlineExceeded}}
	rep := f.delete(t, f.scan(t))
	got := rep.Entries[0]
	if got.Items[0].Outcome != OutcomeIncomplete {
		t.Errorf("item = %+v (途中まで消している可能性があるので incomplete)", got.Items[0])
	}
	if got.Commands[0].Err == "" {
		t.Error("起動失敗 / 中断の error を記録していない")
	}
	if !strings.Contains(got.Items[0].Reason, "コマンドを実行できません") {
		t.Errorf("理由 = %q", got.Items[0].Reason)
	}
}

// 起動そのものに失敗したときは failed (何も起きていない)。
func TestDeleteCLIStartupFailureIsFailed(t *testing.T) {
	f := newDeleteFixture(t, cliEntry, 64)
	f.run.resp = map[string]cmdResp{"faketool purge": {rc: -1, err: errors.New(`exec: "faketool": not found`)}}
	rep := f.delete(t, f.scan(t))
	if got := rep.Entries[0].Items[0]; got.Outcome != OutcomeFailed {
		t.Errorf("item = %+v", got)
	}
}

// simctl 経路の成功: 識別子で消えたことを再走査で確認する。
func TestDeleteSimRuntimeSuccess(t *testing.T) {
	env := testEnv(t)
	sandboxAllow(t, env.Home)
	e := Entry{ID: "simulator-runtimes", Label: "ランタイム", Tier: 1, Risk: RiskCaution,
		DeleteVia: "cli:xcrun simctl runtime delete <id>", Recover: "x", Guard: GuardSimRuntime}
	list := `{"a":{"identifier":"ABC-123","runtimeIdentifier":"com.apple.iOS-18-0","version":"18.0","build":"22A","sizeBytes":2048,"lastUsedAt":"2026-01-01T00:00:00Z","path":"/nonexistent"}}`
	run := &deleteRunner{resp: map[string]cmdResp{
		"xcrun simctl runtime list":   {stdout: list},
		"xcrun simctl runtime delete": {rc: 0},
	}}
	run.onCall = func(args []string) {
		if strings.HasPrefix(strings.Join(args, " "), "xcrun simctl runtime delete") {
			run.mu.Lock()
			run.resp["xcrun simctl runtime list"] = cmdResp{stdout: `{}`} // 消えた
			run.mu.Unlock()
		}
	}
	scan := Scan(context.Background(), Options{Env: env, Run: run.run, Catalog: []Entry{e}, BootTime: okBoot})
	rep, err := Delete(context.Background(), scan.Results, DeleteOptions{Env: env, Run: run.run,
		BootTime: okBoot, Catalog: []Entry{e}, HistoryDir: filepath.Join(t.TempDir(), "h"), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	got := rep.Entries[0]
	if got.Outcome != OutcomeDeleted || got.Freed != 2048 {
		t.Fatalf("outcome=%s freed=%d before=%d", got.Outcome, got.Freed, got.BeforeSize)
	}
}

// 既に消えていた対象は「解放した」と数えない。
func TestDeleteAlreadyGoneIsSkipped(t *testing.T) {
	f := newDeleteFixture(t, rmEntry, 64)
	r := f.scan(t)
	if err := os.RemoveAll(f.target); err != nil {
		t.Fatal(err)
	}
	rep := f.delete(t, r)
	got := rep.Entries[0]
	if got.Items[0].Outcome != OutcomeSkipped || !strings.Contains(got.Items[0].Reason, "既に存在しません") {
		t.Errorf("item = %+v", got.Items[0])
	}
	if got.Outcome != OutcomeSkipped || got.Freed != 0 {
		t.Errorf("outcome=%s freed=%d (無かったものを解放量に数えない)", got.Outcome, got.Freed)
	}
}

// 対象 0 件のエントリを「消えた」にしない。
func TestDeleteNoItemsIsSkipped(t *testing.T) {
	f := newDeleteFixture(t, rmEntry, 64)
	r := f.scan(t)
	r.Items, r.Size = nil, 0
	rep := f.delete(t, r)
	if got := rep.Entries[0]; got.Outcome != OutcomeSkipped || got.Freed != 0 {
		t.Errorf("entry = %+v", got)
	}
}

// 同じエントリを 2 回渡しても 1 回しか処理しない (解放量の二重計上を防ぐ)。
func TestDeleteDedupesTargets(t *testing.T) {
	f := newDeleteFixture(t, rmEntry, 128)
	r := f.scan(t)
	rep, err := Delete(context.Background(), []Result{r, r}, f.opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Entries) != 1 {
		t.Fatalf("エントリが %d 件", len(rep.Entries))
	}
	if rep.Freed != rep.Entries[0].BeforeSize {
		t.Errorf("freed = %d (二重計上している)", rep.Freed)
	}
}

// 🚨 記録の一時ファイルが symlink を辿ると、任意のファイルが上書きされ、しかも記録は
// 実体を持たない (fail-closed が無音で破れる)。名前は秒粒度で予測できるので現実的な穴だった。
func TestHistoryWriteDoesNotFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("PRECIOUS"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &history{path: filepath.Join(dir, "20260903-000000.json")}
	// 攻撃者が撒く形: 固定名の .tmp を victim への symlink にしておく
	if err := os.Symlink(victim, h.path+".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := h.write(DeleteReport{}, phasePlanned); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "PRECIOUS" {
		t.Fatalf("symlink を辿って上書きした: %q", string(b))
	}
	if fi, err := os.Lstat(h.path); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("記録が実体を持っていない (err=%v)", err)
	}
}

// 記録の名前は同じ秒に 2 回消しても衝突しない (ヘッダの主張を実測で固定する)。
func TestNewHistoryAvoidsCollision(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "h")
	fixed := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	opt := DeleteOptions{HistoryDir: dir, Now: func() time.Time { return fixed }}
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		h, err := newHistory(opt)
		if err != nil {
			t.Fatal(err)
		}
		if seen[h.path] {
			t.Fatalf("同じ名前を 2 回確保した: %s", h.path)
		}
		seen[h.path] = true
	}
}

// 確保だけして書けなかった記録ファイルは消す (0 バイトの .json を残すと、次に記録を読む処理が
// JSON のパースエラーになる)。
func TestHistoryDiscardRemovesClaimedFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "h")
	h, err := newHistory(DeleteOptions{HistoryDir: dir, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	if !exists(h.path) {
		t.Fatal("名前を確保できていない")
	}
	h.discard()
	if exists(h.path) {
		t.Fatal("確保だけした記録ファイルが残っている")
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(left) != 0 {
		t.Errorf("残骸: %v", left)
	}
}

func TestWithDeleteDefaults(t *testing.T) {
	got := withDeleteDefaults(DeleteOptions{Env: Env{Home: "/h"}})
	if got.TrashDir != "/h/.Trash" {
		t.Errorf("TrashDir = %q", got.TrashDir)
	}
	if got.PerEntry <= 0 || got.VerifyTimeout <= 0 || got.CmdTimeout <= 0 || got.Now == nil ||
		got.Run == nil || got.BootTime == nil || len(got.Catalog) == 0 {
		t.Errorf("既定値が埋まっていない: %+v", got)
	}
}

func TestDeleteReportHasFailures(t *testing.T) {
	for _, tc := range []struct {
		o    Outcome
		want bool
	}{{OutcomeDeleted, false}, {OutcomeTrashed, false}, {OutcomeSkipped, false},
		{OutcomeFailed, true}, {OutcomeIncomplete, true}} {
		r := DeleteReport{Entries: []EntryOutcome{{Outcome: tc.o}}}
		if got := r.HasFailures(); got != tc.want {
			t.Errorf("%s: HasFailures = %v", tc.o, got)
		}
	}
}

func TestParseDeleteViaRejectsEmbeddedPlaceholder(t *testing.T) {
	if _, _, err := parseDeleteVia("cli:faketool --runtime=<id>"); err == nil {
		t.Error("埋め込まれた <id> を受け入れた (リテラルのまま渡る)")
	}
	if _, _, err := parseDeleteVia("cli:/usr/bin/sudo rm"); err == nil {
		t.Error("絶対パスの sudo を受け入れた")
	}
}

func TestProposeHasNoCommand(t *testing.T) {
	f := newDeleteFixture(t, Entry{ID: "testcache", Label: "提示だけ", Tier: 4, Risk: RiskCaution,
		DeleteVia: "propose", Recover: "x"}, 64)
	rep := f.delete(t, f.scan(t))
	if got := rep.Entries[0].Command; got != "" {
		t.Errorf("Command = %q (propose には提示するコマンドが無い。カタログに形式を足すまで空)", got)
	}
}

// カタログの ID は一意 (lookupEntry は先勝ちなので、重複すると別のエントリの作法で消える)。
func TestCatalogIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range catalog {
		if seen[e.ID] {
			t.Errorf("ID が重複している: %s", e.ID)
		}
		seen[e.ID] = true
	}
	if len(seen) < 20 {
		t.Fatalf("カタログが %d 件しかない (走査が壊れている)", len(seen))
	}
}

// rm / trash のエントリは Paths を持つ (空だと永久に「対象がありません」になり、誰も気づかない)。
func TestCatalogPathEntriesHavePaths(t *testing.T) {
	n := 0
	for _, e := range catalog {
		m, _, err := parseDeleteVia(e.DeleteVia)
		if err != nil || (m != methodRM && m != methodTrash) {
			continue
		}
		n++
		if len(e.Paths) == 0 {
			t.Errorf("%s: %s なのに Paths が空 (永久に候補 0 件になる)", e.ID, m)
		}
	}
	if n == 0 {
		t.Fatal("rm / trash のエントリが 1 件も無い (検査が空振りしている)")
	}
}
