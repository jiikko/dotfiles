package tmpclean

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tmpclean/internal/runner"
)

const (
	ddHashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ddHashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	ddHashC = "cccccccccccccccccccccccccccc"
	ddHashD = "dddddddddddddddddddddddddddd"
	ddHashE = "eeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

// dd は DerivedData の直下に name を作る。plist が nil なら info.plist を置かない。fake の plutil は info.plist の中身を
// WorkspacePath として返す (空なら key が無い扱い)。
func (f *fixture) dd(name string, plist *string) string {
	f.t.Helper()
	p := f.mkdir(filepath.Join(f.derivedData, name, "Build"))
	_ = p
	dir := filepath.Join(f.derivedData, name)
	if plist != nil {
		if err := os.WriteFile(filepath.Join(dir, "info.plist"), []byte(*plist), 0o600); err != nil {
			f.t.Fatal(err)
		}
	}
	return dir
}

func ptr(s string) *string { return &s }

func TestDerivedDataMovesOnlyOrphans(t *testing.T) {
	f := newFixture(t)
	root := filepath.Dir(f.derivedData)
	live := f.mkdir(filepath.Join(root, "src", "live", "App.xcodeproj"))
	gone := filepath.Join(root, "src", "gone-worktree", "App.xcodeproj")

	orphan := f.dd("App-"+ddHashA, ptr(gone))
	// package: info.plist の無い <ディレクトリ名>-<hash> は、同じ hash の持ち主が消えていれば候補
	orphanPkg := f.dd("PkgName-"+ddHashB, ptr(filepath.Join(root, "src", "gone-worktree", "Pkg")))
	orphanPkgBuild := f.dd("pkg-dir-"+ddHashB, nil)
	liveEntry := f.dd("App-"+ddHashC, ptr(live))
	// 同じ hash で持ち主が在る info.plist があれば、info.plist の無い兄弟は残す
	mixedOrphan := f.dd("Gone-"+ddHashD, ptr(gone))
	mixedLive := f.dd("Live-"+ddHashD, ptr(live))
	mixedBuild := f.dd("build-dir-"+ddHashD, nil)
	noPlistAlone := f.dd("alone-"+ddHashE, nil)
	moduleCache := f.mkdir(filepath.Join(f.derivedData, "ModuleCache.noindex"))
	// Xcode の名前の形でない dir は、info.plist があり持ち主が消えていても触らない
	notShaped := f.dd("NotXcode-short", ptr(gone))
	emptyKey := f.dd("NoKey-ffffffffffffffffffffffffffff", ptr(""))
	relative := f.dd("Rel-gggggggggggggggggggggggggggg", ptr("src/gone/App.xcodeproj"))
	volume := f.dd("Vol-hhhhhhhhhhhhhhhhhhhhhhhhhhhh", ptr("/Volumes/NoSuchVolume-tmpclean-test/x/App.xcodeproj"))
	opened := f.dd("Open-iiiiiiiiiiiiiiiiiiiiiiiiiiii", ptr(filepath.Join(root, "src", "gone-2", "App.xcodeproj")))
	f.sys.open = append(f.sys.open, filepath.Join(opened, "Build", "f.txt"))
	if err := os.Symlink(orphan, filepath.Join(f.derivedData, "Link-jjjjjjjjjjjjjjjjjjjjjjjjjjjj")); err != nil {
		t.Fatal(err)
	}

	id, path := f.plan(kindDerivedData)
	rc, out, errOut := f.apply(id, path)
	if rc != exitOK {
		t.Fatalf("apply rc=%d\n%s\n%s", rc, out, errOut)
	}
	for _, p := range []string{orphan, orphanPkg, orphanPkgBuild} {
		if exists(p) {
			t.Errorf("持ち主の消えた DerivedData が残った: %s", p)
		}
		if !f.trashed(kindDerivedData, filepath.Base(p)) {
			t.Errorf("ゴミ箱の derived-data/%s に無い", filepath.Base(p))
		}
	}
	for _, p := range []string{liveEntry, mixedLive, mixedBuild, noPlistAlone, moduleCache, notShaped, emptyKey, relative, volume, opened} {
		if !exists(p) {
			t.Errorf("残すべき DerivedData を移した: %s", p)
		}
	}
	if exists(mixedOrphan) {
		t.Errorf("同じ hash に持ち主の在る entry があっても、自分の持ち主が消えた entry は移す: %s", mixedOrphan)
	}
	if !exists(filepath.Join(f.derivedData, "Link-jjjjjjjjjjjjjjjjjjjjjjjjjjjj")) {
		t.Errorf("symlink の entry を移した")
	}
}

func TestDerivedDataKeptWhenOpenedBeforeMove(t *testing.T) {
	f := newFixture(t)
	gone := filepath.Join(filepath.Dir(f.derivedData), "src", "gone", "App.xcodeproj")
	orphan := f.dd("App-"+ddHashA, ptr(gone))
	id, path := f.plan(kindDerivedData)
	// plan の後に Xcode が開いた (移す直前の lsof +D に出る)
	f.sys.openNow[orphan] = [3]any{"p1\nn" + filepath.Join(orphan, "Build", "f.txt") + "\n", "", 1}
	rc, out, _ := f.apply(id, path)
	if rc != exitOK || !exists(orphan) {
		t.Fatalf("移す直前に開かれていた DerivedData を移した (rc=%d)\n%s", rc, out)
	}
	if !strings.Contains(out, "移す直前に開かれていた") {
		t.Errorf("残した理由が出ていない:\n%s", out)
	}
}

func TestDerivedDataRecheckedAtApply(t *testing.T) {
	f := newFixture(t)
	owner := filepath.Join(filepath.Dir(f.derivedData), "src", "wt", "App.xcodeproj")
	orphan := f.dd("App-"+ddHashA, ptr(owner))
	id, path := f.plan(kindDerivedData)
	// plan の後に持ち主が戻った (同じ path に worktree を作り直した)
	f.mkdir(owner)
	rc, out, _ := f.apply(id, path)
	if rc != exitOK || !exists(orphan) {
		t.Fatalf("plan の後に持ち主が戻った DerivedData を移した (rc=%d)\n%s", rc, out)
	}
}

func TestDerivedDataMissingRootIsZero(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(f.derivedData); err != nil {
		t.Fatal(err)
	}
	rc, out, errOut := f.runCLI("plan", "-kinds", kindDerivedData, "-out", f.out)
	if rc != exitOK || !strings.Contains(out, "candidates.derived-data: 0") {
		t.Fatalf("DerivedData の無い機械で plan が 0 件にならない (rc=%d)\n%s\n%s", rc, out, errOut)
	}
}

func TestDerivedDataRootSymlinkStops(t *testing.T) {
	f := newFixture(t)
	real := filepath.Join(filepath.Dir(f.derivedData), "elsewhere")
	if err := os.Remove(f.derivedData); err != nil {
		t.Fatal(err)
	}
	f.mkdir(real)
	if err := os.Symlink(real, f.derivedData); err != nil {
		t.Fatal(err)
	}
	if rc, _, _ := f.runCLI("plan", "-kinds", kindDerivedData, "-out", f.out); rc != exitStopped {
		t.Fatalf("DerivedData の根が symlink でも止まらない (rc=%d)", rc)
	}
}

func TestTrashGuardDerivedDataDepth(t *testing.T) {
	r := roots{session: "/s"}
	g := trashGuard(r, "/h/DerivedData", "/h/.Trash/tmpclean-x", func(string, string) error { return nil })
	if err := g("trash", "/h/DerivedData/App-"+ddHashA); err != nil {
		t.Errorf("DerivedData の直下を拒否した: %v", err)
	}
	for _, p := range []string{"/h/DerivedData", "/h/DerivedData/App-" + ddHashA + "/Build", "/h/Other/App-" + ddHashA} {
		if err := g("trash", p); err == nil {
			t.Errorf("DerivedData の直下でないものを通した: %s", p)
		}
	}
	if err := trashGuard(r, "", "/h/.Trash/tmpclean-x", func(string, string) error { return nil })("trash", "/h/DerivedData/App-"+ddHashA); err == nil {
		t.Errorf("derived-data を扱わないときに DerivedData を通した")
	}
}

func TestOwnerGone(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		filepath.Join(dir, "gone", "macOS", "App.xcodeproj"): true,
		dir:                            false, // 在る
		filepath.Join(dir, "dangling"): false, // 切れた symlink も在る扱い
		"relative/App.xcodeproj":       false,
		"":                             false,
		dir + "/a/../b":                false, // 正規化されていない
		"/Volumes/NoSuchVolume-tmpclean/x/App.xcodeproj": false, // 最も近い祖先が浅い
		"/NoSuchTop-tmpclean/App.xcodeproj":              false,
		// 存在する最も近い祖先が symlink (その先の外付けの volume が外れている)
		filepath.Join(dir, "dangling", "proj", "App.xcodeproj"): false,
		filepath.Join(dir, "linked", "proj", "App.xcodeproj"):   false,
	}
	for in, want := range cases {
		if got := ownerGone(in); got != want {
			t.Errorf("ownerGone(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDerivedDataKeptReasons(t *testing.T) {
	f := newFixture(t)
	f.mkdir(filepath.Join(f.derivedData, "ModuleCache.noindex"))
	f.dd("alone-"+ddHashE, nil)
	rc, out, errOut := f.runCLI("plan", "-kinds", kindDerivedData, "-out", f.out)
	if rc != exitOK {
		t.Fatalf("plan rc=%d\n%s\n%s", rc, out, errOut)
	}
	for _, want := range []string{
		"kept (DerivedData の entry の形でない): 1",
		"kept (info.plist が無く、同じ hash の持ち主が在る・分からない): 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("残した理由 %q が出ていない:\n%s", want, out)
		}
	}
}

func TestDerivedDataOwnerBackJustBeforeMove(t *testing.T) {
	f := newFixture(t)
	root := filepath.Dir(f.derivedData)
	ownerA := filepath.Join(root, "src", "wt-a", "App.xcodeproj")
	a := f.dd("App-"+ddHashA, ptr(ownerA))
	b := f.dd("App-"+ddHashB, ptr(filepath.Join(root, "src", "wt-b", "App.xcodeproj")))
	id, path := f.plan(kindDerivedData)
	// apply の始めの候補の後、a を移す直前の lsof の間に、同じ path に worktree が作り直された
	f.sys.onLsof1 = func(p string) {
		if p == a {
			f.mkdir(ownerA)
		}
	}
	rc, out, _ := f.apply(id, path)
	if rc != exitOK || !exists(a) {
		t.Fatalf("移す直前に持ち主が戻った DerivedData を移した (rc=%d)\n%s", rc, out)
	}
	if exists(b) {
		t.Errorf("持ち主の消えたままの DerivedData が残った: %s", b)
	}
	if !strings.Contains(out, "移す直前に持ち主が戻った") {
		t.Errorf("残した理由が出ていない:\n%s", out)
	}
}

func TestDerivedDataSymlinkPlistIsUnknown(t *testing.T) {
	f := newFixture(t)
	gone := filepath.Join(filepath.Dir(f.derivedData), "src", "gone", "App.xcodeproj")
	const h = "kkkkkkkkkkkkkkkkkkkkkkkkkkkk"
	f.dd("Real-"+h, ptr(gone))
	sym := f.dd("Sym-"+h, nil)
	target := filepath.Join(filepath.Dir(f.derivedData), "plist-target")
	if err := os.WriteFile(target, []byte(gone), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(sym, "info.plist")); err != nil {
		t.Fatal(err)
	}
	sibling := f.dd("sib-"+h, nil)
	id, path := f.plan(kindDerivedData)
	if rc, out, _ := f.apply(id, path); rc != exitOK {
		t.Fatalf("apply rc=%d\n%s", rc, out)
	}
	// symlink の info.plist は「持ち主が分からない」。info.plist の無い dir として数えると、兄弟と一緒に移してしまう
	for _, p := range []string{sym, sibling} {
		if !exists(p) {
			t.Errorf("info.plist が symlink の hash の dir を移した: %s", p)
		}
	}
}

// fake の plutil は本物の出力の形を模しているだけなので、本物の plutil で XML と binary の plist の WorkspacePath を読めることを固定する
func TestCollectDerivedDataRealPlutil(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ws := "/Users/someone/src/app wt/App.xcodeproj"
	for i, format := range []string{"xml1", "binary1"} {
		dir := filepath.Join(root, "App-"+strings.Repeat(string(rune('a'+i)), 28))
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		plist := filepath.Join(dir, "info.plist")
		body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>LastAccessedDate</key><date>2026-11-03T00:00:00Z</date><key>WorkspacePath</key><string>` + ws + `</string></dict></plist>
`
		if err := os.WriteFile(plist, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, rc, err := runner.Exec(context.Background(), "plutil", "-convert", format, plist); err != nil || rc != 0 {
			t.Fatalf("plutil -convert %s: rc=%d err=%v", format, rc, err)
		}
	}
	ents := collectDerivedData(context.Background(), deps{run: runner.Exec}, root, "")
	if len(ents) != 2 {
		t.Fatalf("entry の数 = %d, want 2", len(ents))
	}
	for _, e := range ents {
		if !e.hasPlist || e.workspace != ws {
			t.Errorf("%s: hasPlist=%v workspace=%q, want %q", e.name, e.hasPlist, e.workspace, ws)
		}
	}
}

// 外付けの volume の下は、外れて空の mount point の dir が残っていても (存在する祖先が深くても) 持ち主が消えたと言わない
func TestOwnerGoneOutsideExternalRoots(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mount := filepath.Join(dir, "Volumes", "Ext")
	if err := os.MkdirAll(mount, 0o700); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(mount, "src", "app", "App.xcodeproj")
	if ownerGoneOutside(ws, []string{filepath.Join(dir, "Volumes") + "/"}) {
		t.Errorf("外付けの根の下を持ち主が消えたと判定した: %s", ws)
	}
	if !ownerGoneOutside(ws, nil) {
		t.Errorf("外付けの根でなければ消えたと判定する (前提): %s", ws)
	}
}

// ownerGone は外付けの volume の場所として /Volumes/ を渡す (テストでは /Volumes の下に dir を作れないので、配線を固定する)
func TestOwnerGoneUsesVolumesRoot(t *testing.T) {
	if len(externalVolumeRoots) != 1 || externalVolumeRoots[0] != "/Volumes/" {
		t.Fatalf("externalVolumeRoots = %q, want [/Volumes/]", externalVolumeRoots)
	}
}

// plutil が時間切れ・起動できないときは、その entry を持ち主が分からないとして残し、apply 全体は止めない
func TestDerivedDataPlutilFailureKeepsEntry(t *testing.T) {
	f := newFixture(t)
	gone := filepath.Join(filepath.Dir(f.derivedData), "src", "gone", "App.xcodeproj")
	a := f.dd("App-"+ddHashA, ptr(gone))
	orig := f.d.run
	f.d.run = func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		if name == "plutil" {
			return "", "", -1, context.DeadlineExceeded
		}
		return orig(ctx, name, args...)
	}
	rc, out, errOut := f.runCLI("plan", "-kinds", kindDerivedData, "-out", f.out)
	if rc != exitOK || !strings.Contains(out, "candidates.derived-data: 0") || !exists(a) {
		t.Fatalf("plutil の失敗で plan が止まった・候補にした (rc=%d)\n%s\n%s", rc, out, errOut)
	}
}
