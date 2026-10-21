package disk

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const lsofTxtKey = "lsof -nP -d txt -Fn"

func derivedDataEntry() Entry {
	return Entry{ID: "xcode-deriveddata", Risk: RiskSafe, DeleteVia: "rm", Guard: GuardDerivedDataLive,
		Paths: []string{"~/Library/Developer/Xcode/DerivedData/*"}}
}

// derivedDataProject は DerivedData/<name> にビルド済みのアプリを置き、その実行ファイルの実パスを返す。
func derivedDataProject(t *testing.T, env Env, name string) string {
	t.Helper()
	exe := filepath.Join(env.Home, "Library", "Developer", "Xcode", "DerivedData", name,
		"Build", "Products", "Debug", "App.app", "Contents", "MacOS", "App")
	mkfile(t, exe, 4)
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// lsofTxt は lsof -Fn の出力を組む。lsof 自身の行を必ず含める (本物も必ず出す)。
func lsofTxt(paths ...string) string {
	var b strings.Builder
	b.WriteString("p1\nftxt\nn/usr/sbin/lsof\n")
	for _, p := range paths {
		b.WriteString("p2\nftxt\nn" + p + "\n")
	}
	return b.String()
}

func itemNames(r Result) []string {
	out := make([]string, 0, len(r.Items))
	for _, it := range r.Items {
		out = append(out, filepath.Base(it.Path))
	}
	sort.Strings(out)
	return out
}

// 起動中のアプリのビルドが入ったプロジェクトだけを落とし、他のプロジェクトは候補に残す。
func TestDerivedDataDropsProjectWithRunningBuild(t *testing.T) {
	env := testEnv(t)
	liveExe := derivedDataProject(t, env, "Live-abc")
	derivedDataProject(t, env, "Quiet-def")
	run := &fakeRunner{resp: map[string]fakeResp{lsofTxtKey: {out: lsofTxt(liveExe)}}}
	r := scanOne(t, env, run, derivedDataEntry(), okBoot)
	if got := itemNames(r); strings.Join(got, ",") != "Quiet-def" {
		t.Fatalf("候補=%v, want [Quiet-def] (status=%s reason=%s)", got, r.Status, r.Reason)
	}
}

// 本体が別の場所にあっても、DerivedData の dylib を読み込んでいれば落とす (txt を見る理由)。
func TestDerivedDataDropsProjectWhoseLibraryIsLoaded(t *testing.T) {
	env := testEnv(t)
	exe := derivedDataProject(t, env, "Host-abc")
	dylib := filepath.Join(filepath.Dir(exe), "App.debug.dylib")
	derivedDataProject(t, env, "Quiet-def")
	run := &fakeRunner{resp: map[string]fakeResp{lsofTxtKey: {out: lsofTxt("/Applications/Xcode.app/Contents/MacOS/Xcode", dylib)}}}
	r := scanOne(t, env, run, derivedDataEntry(), okBoot)
	if got := itemNames(r); strings.Join(got, ",") != "Quiet-def" {
		t.Fatalf("候補=%v, want [Quiet-def]", got)
	}
}

// 名前の接頭辞が同じだけの別プロジェクトを巻き込まない (パスの区切りまで一致を求める)。
func TestDerivedDataMatchesWholeDirectoryName(t *testing.T) {
	env := testEnv(t)
	liveExe := derivedDataProject(t, env, "Foo-abc2")
	derivedDataProject(t, env, "Foo-abc")
	run := &fakeRunner{resp: map[string]fakeResp{lsofTxtKey: {out: lsofTxt(liveExe)}}}
	r := scanOne(t, env, run, derivedDataEntry(), okBoot)
	if got := itemNames(r); strings.Join(got, ",") != "Foo-abc" {
		t.Fatalf("候補=%v, want [Foo-abc]", got)
	}
}

// lsof が大文字小文字の違うパスを出しても落とす (APFS の既定は case-insensitive)。
func TestDerivedDataIgnoresCase(t *testing.T) {
	env := testEnv(t)
	liveExe := derivedDataProject(t, env, "Live-abc")
	derivedDataProject(t, env, "Quiet-def")
	run := &fakeRunner{resp: map[string]fakeResp{lsofTxtKey: {out: lsofTxt(strings.ToUpper(liveExe))}}}
	r := scanOne(t, env, run, derivedDataEntry(), okBoot)
	if got := itemNames(r); strings.Join(got, ",") != "Quiet-def" {
		t.Fatalf("候補=%v, want [Quiet-def]", got)
	}
}

// 全部が起動中で落ちたら blocked にする (0 件で畳まれて「候補なし」に見えないように)。
func TestDerivedDataAllHeldIsBlocked(t *testing.T) {
	env := testEnv(t)
	liveExe := derivedDataProject(t, env, "Live-abc")
	run := &fakeRunner{resp: map[string]fakeResp{lsofTxtKey: {out: lsofTxt(liveExe)}}}
	r := scanOne(t, env, run, derivedDataEntry(), okBoot)
	if r.Status != StatusBlocked || len(r.Items) != 0 || !strings.Contains(r.Reason, "Live-abc") {
		t.Fatalf("status=%s items=%v reason=%q, want blocked で Live-abc を名指し", r.Status, itemNames(r), r.Reason)
	}
}

// lsof が使えないときは候補にしない (fail-closed)。
func TestDerivedDataFailsClosedWhenLsofFails(t *testing.T) {
	cases := map[string]fakeResp{
		"実行できない":     {err: errors.New("exec: lsof: not found")},
		"rc が 0 でない": {rc: 1, out: lsofTxt()},
		"パスが 0 件":    {out: "p1\nftxt\n"},
	}
	for name, resp := range cases {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t)
			derivedDataProject(t, env, "Quiet-def")
			run := &fakeRunner{resp: map[string]fakeResp{lsofTxtKey: resp}}
			r := scanOne(t, env, run, derivedDataEntry(), okBoot)
			if r.Status != StatusFailed || len(r.Items) != 0 {
				t.Fatalf("status=%s items=%v, want failed で候補 0 件", r.Status, itemNames(r))
			}
		})
	}
}

// 既定カタログの DerivedData がこの guard を通っていること (ここが外れると上のテストは全部緑のまま守らなくなる)。
func TestCatalogDerivedDataUsesLiveGuard(t *testing.T) {
	e, ok := CatalogEntry("xcode-deriveddata")
	if !ok {
		t.Fatal("xcode-deriveddata がカタログに無い")
	}
	if e.Guard != GuardDerivedDataLive {
		t.Fatalf("xcode-deriveddata の Guard=%q, want %q", e.Guard, GuardDerivedDataLive)
	}
}

// lsof が locale の無い環境で出す `\xNN` (非 ASCII) と `\\` (`\` そのもの) を戻してから比べる。
// 戻さないと、日本語や `\` を名前に含むプロジェクトは起動中でも候補に残る。
func TestDerivedDataUnescapesLsofNames(t *testing.T) {
	for _, name := range []string{"プロジェクト-abc", `back\x41slash-abc`, "tab\tname-abc"} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t)
			liveExe := derivedDataProject(t, env, name)
			derivedDataProject(t, env, "Quiet-def")
			run := &fakeRunner{resp: map[string]fakeResp{lsofTxtKey: {out: lsofTxt(lsofEscape(liveExe))}}}
			r := scanOne(t, env, run, derivedDataEntry(), okBoot)
			if got := itemNames(r); strings.Join(got, ",") != "Quiet-def" {
				t.Fatalf("候補=%v, want [Quiet-def]", got)
			}
		})
	}
}

// lsofEscape は locale の無い lsof 4.91 の出し方を写す (実測: 非 ASCII のバイトと 0x7f は `\xNN`、`\` は `\\`、
// タブ・改行は `\t` `\n`)。`^X` の形で出る他の制御文字は戻せないので写さない (guard.go の unescapeLsofName)。
func lsofEscape(p string) string {
	var b strings.Builder
	for _, c := range []byte(p) {
		switch {
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\n':
			b.WriteString(`\n`)
		case c >= 0x7f:
			b.WriteString(fmt.Sprintf(`\x%02x`, c))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// 画面を見ているあいだにアプリを起動したら、削除の直前の再走査がそのプロジェクトだけを外し、
// 他のプロジェクトは消す (この guard は entry ごと blocked にするより、Item 単位で外す方が普通の効き方)。
func TestDeleteSkipsProjectThatStartedRunningBeforeDelete(t *testing.T) {
	env := testEnv(t)
	sandboxAllow(t, env.Home)
	liveExe := derivedDataProject(t, env, "Live-abc")
	quietExe := derivedDataProject(t, env, "Quiet-def")
	e := derivedDataEntry()
	e.Label, e.Recover = "Xcode DerivedData", "x"
	run := &deleteRunner{resp: map[string]cmdResp{lsofTxtKey: {stdout: lsofTxt()}}}
	scan := Scan(context.Background(), Options{Env: env, Run: run.run, Catalog: []Entry{e}, BootTime: okBoot})
	if r := scan.Results[0]; r.Status != StatusOK || len(r.Items) != 2 {
		t.Fatalf("前提が崩れている: status=%s items=%v", r.Status, itemNames(r))
	}
	// 走査の後に Live-abc のビルドを起動した
	run.mu.Lock()
	run.resp[lsofTxtKey] = cmdResp{stdout: lsofTxt(liveExe)}
	run.mu.Unlock()
	if _, err := Delete(context.Background(), scan.Results, deleteOptFor(t, env, e, run)); err != nil {
		t.Fatal(err)
	}
	if !exists(liveExe) {
		t.Fatal("起動中のビルドを消した")
	}
	if exists(quietExe) {
		t.Fatal("起動していないプロジェクトを消していない (guard が entry ごと止めている?)")
	}
}
