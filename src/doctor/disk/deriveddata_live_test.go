package disk

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
)

const lsofTxtKey = "lsof -nP -d txt -Fni"

func derivedDataEntry() Entry {
	return Entry{ID: "xcode-deriveddata", Risk: RiskSafe, DeleteVia: "rm", Guard: GuardDerivedDataLive,
		Paths: []string{"~/Library/Developer/Xcode/DerivedData/*"}}
}

// derivedDataProject は DerivedData/<name> にビルド済みのアプリを置き、その実行ファイルの実パスを返す。
func derivedDataProject(t *testing.T, env Env, name string) string {
	t.Helper()
	exe := filepath.Join(env.Home, "Library", "Developer", "Xcode", "DerivedData", name,
		"Build", "Products", "Debug", "App.app", "Contents", "MacOS", "App")
	mkmacho(t, exe, 0xcffaedfe)
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// mkmacho は先頭 4 バイトが magic (big endian) のファイルを作る。
func mkmacho(t *testing.T, p string, magic uint32) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 4096)
	binary.BigEndian.PutUint32(b, magic)
	if err := os.WriteFile(p, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

// lsofTxt は lsof -Fni の出力を組む。inode はディスク上のそのファイルのもの (無ければ 1)。
// lsof 自身の行を必ず含める (本物も必ず出す)。
func lsofTxt(paths ...string) string {
	out := txtEntry("/usr/sbin/lsof", inoOf("/usr/sbin/lsof"))
	for _, p := range paths {
		out += txtEntry(p, inoOf(p))
	}
	return out
}

// txtEntry は 1 ファイル分 (本物と同じ f → i → n の順)。
func txtEntry(shown string, ino uint64) string {
	return fmt.Sprintf("p2\nftxt\ni%d\nn%s\n", ino, shown)
}

func inoOf(p string) uint64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 1
	}
	return fi.Sys().(*syscall.Stat_t).Ino
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
	mkmacho(t, dylib, 0xcffaedfe)
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
	run := &fakeRunner{resp: map[string]fakeResp{lsofTxtKey: {out: lsofTxt() + txtEntry(strings.ToUpper(liveExe), inoOf(liveExe))}}}
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
			run := &fakeRunner{resp: map[string]fakeResp{lsofTxtKey: {out: lsofTxt() + txtEntry(lsofEscape(liveExe), inoOf(liveExe))}}}
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
			fmt.Fprintf(&b, `\x%02x`, c)
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

// 他のプロセスがリソース (Assets.car・InfoPlist.strings) を掴んでいるだけなら、使用中と読まない。
// NotificationCenter は通知を出したアプリのバンドルの文言を、そのアプリの終了後も掴み続ける。
func TestDerivedDataIgnoresMappedResources(t *testing.T) {
	env := testEnv(t)
	exe := derivedDataProject(t, env, "Quit-abc")
	res := filepath.Join(filepath.Dir(filepath.Dir(exe)), "Resources", "Assets.car")
	mkmacho(t, res, 0x424f4d53) // "BOMS"
	run := &fakeRunner{resp: map[string]fakeResp{lsofTxtKey: {out: lsofTxt(res)}}}
	r := scanOne(t, env, run, derivedDataEntry(), okBoot)
	if got := itemNames(r); strings.Join(got, ",") != "Quit-abc" {
		t.Fatalf("候補=%v (status=%s reason=%s), want [Quit-abc]", got, r.Status, r.Reason)
	}
}

// 掴まれているファイルの中身で判定できないものは使用中と読む (fail-closed)。Mach-O の magic は 8 種とも使用中。
// 4 バイトに満たないファイルは Mach-O ではない。
func TestDerivedDataHeldFileKinds(t *testing.T) {
	write := func(b []byte) func(*testing.T, string) {
		return func(t *testing.T, p string) {
			if err := os.WriteFile(p, b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	cases := []struct {
		name string
		make func(t *testing.T, p string)
		held bool
	}{
		{"消されたファイル", func(*testing.T, string) {}, true},
		{"ディレクトリ", func(t *testing.T, p string) {
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
		}, true},
		// 開くと書き手が来るまで止まるので、開かずに使用中と読む (通常ファイルの検査を外すとこのテストが止まる)
		{"FIFO", func(t *testing.T, p string) {
			if err := syscall.Mkfifo(p, 0o644); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"読めないファイル", func(t *testing.T, p string) {
			if os.Geteuid() == 0 {
				t.Skip("root は権限 000 でも読めるので、この経路を作れない")
			}
			mkmacho(t, p, 0x424f4d53)
			if err := os.Chmod(p, 0); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"4 バイト未満", write([]byte{0xcf, 0xfa}), false},
		{"空ファイル", write(nil), false},
	}
	for _, m := range []uint32{0xfeedface, 0xcefaedfe, 0xfeedfacf, 0xcffaedfe, 0xcafebabe, 0xbebafeca, 0xcafebabf, 0xbfbafeca} {
		cases = append(cases, struct {
			name string
			make func(t *testing.T, p string)
			held bool
		}{fmt.Sprintf("magic %08x", m), func(t *testing.T, p string) { mkmacho(t, p, m) }, true})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := testEnv(t)
			exe := derivedDataProject(t, env, "P-abc")
			heldPath := filepath.Join(filepath.Dir(exe), "held")
			tc.make(t, heldPath)
			run := &fakeRunner{resp: map[string]fakeResp{lsofTxtKey: {out: lsofTxt(heldPath)}}}
			r := scanOne(t, env, run, derivedDataEntry(), okBoot)
			if got := r.Status == StatusBlocked; got != tc.held {
				t.Fatalf("blocked=%v, want %v (items=%v reason=%q)", got, tc.held, itemNames(r), r.Reason)
			}
		})
	}
}

// 起動中の実行ファイルが消され、同じ名前で Mach-O でないファイルが作られても、使用中と読む。
// lsof は消えた vnode を最後の名前のまま出すので、名前で開いたファイルは掴まれているファイルではない (inode で見分ける)。
func TestDerivedDataReplacedFileIsHeldByInode(t *testing.T) {
	env := testEnv(t)
	exe := derivedDataProject(t, env, "Live-abc")
	old := inoOf(exe)
	sandboxAllow(t, env.Home)
	rmFixture(t, exe)
	mkmacho(t, exe, 0x68656c6c) // "hell"
	if inoOf(exe) == old {
		t.Skip("同じ inode が再利用された (この入力では差し替えを作れない)")
	}
	run := &fakeRunner{resp: map[string]fakeResp{lsofTxtKey: {out: lsofTxt() + txtEntry(exe, old)}}}
	r := scanOne(t, env, run, derivedDataEntry(), okBoot)
	if r.Status != StatusBlocked {
		t.Fatalf("status=%s items=%v, want blocked (掴まれているのは消えた実行ファイル)", r.Status, itemNames(r))
	}
}

// lsof の出力に inode が無いファイルは、中身で判定できないので使用中と読む。
func TestDerivedDataHeldWithoutInode(t *testing.T) {
	env := testEnv(t)
	exe := derivedDataProject(t, env, "P-abc")
	res := filepath.Join(filepath.Dir(exe), "res")
	mkmacho(t, res, 0x424f4d53)
	run := &fakeRunner{resp: map[string]fakeResp{lsofTxtKey: {out: lsofTxt() + "p2\nftxt\nn" + res + "\n"}}}
	r := scanOne(t, env, run, derivedDataEntry(), okBoot)
	if r.Status != StatusBlocked {
		t.Fatalf("status=%s items=%v, want blocked", r.Status, itemNames(r))
	}
}
