package tmpclean

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	prefsU1 = "0017A4E5-D57C-4350-BE33-C86ED388A36B"
	prefsU2 = "11111111-2222-3333-4444-555555555555"
	prefsU3 = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
)

// 本物の plutil が書いた binary plist (2026-11-20 に plutil -convert binary1 で取った)。
// 空の dict は cfprefsd が書き戻す 42 バイトの plist と同じバイト列 (ホストの test.favorites.<UUID>.plist で確かめた)
var (
	bplistEmptyDict = mustHex("62706c6973743030d0080000000000000101000000000000000100000000000000000000000000000009")
	bplistOneKey    = mustHex("62706c6973743030d10102516b5176080b0d000000000000010100000000000000030000000000000000000000000000000f")
	bplistEmptyArr  = mustHex("62706c6973743030a0080000000000000101000000000000000100000000000000000000000000000009")
)

// twoObjectsEmptyTop は空の dict の plist の trailer の object の数だけを 2 にしたもの (offset table は 1 件ぶんしか無い)
func twoObjectsEmptyTop() []byte {
	b := append([]byte{}, bplistEmptyDict...)
	b[len(b)-32+15] = 2
	return b
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// pref は Preferences の根に name を body で書き、days 日前の mtime にする。
func (f *fixture) pref(name string, body []byte, days int) string {
	f.t.Helper()
	p := filepath.Join(f.prefs, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, body, 0o600); err != nil {
		f.t.Fatal(err)
	}
	f.age(p, days)
	return p
}

func TestPrefsShapeName(t *testing.T) {
	for name, want := range map[string]string{
		"test.favorites." + prefsU1 + ".plist":                    "test.favorites.<UUID>",
		"test.x." + strings.ToLower(prefsU1) + ".plist":           "test.x.<UUID>",
		"com.jiikko.obaket.profile.work-" + prefsU1 + ".plist":    "com.jiikko.obaket.profile.work-<UUID>",
		"Sync-" + prefsU1 + "-" + prefsU2 + ".plist":              "Sync-<UUID>-<UUID>",
		"com.apple.something." + prefsU1 + ".plist":               "",
		"com.example.app.plist":                                   "",
		"test.favorites." + prefsU1 + ".plist.lockfile":           "",
		"test.favorites." + prefsU1:                               "",
		"test." + strings.Replace(prefsU1, "-", "", 1) + ".plist": "",
	} {
		if got := prefsShape(name); got != want {
			t.Errorf("prefsShape(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestPrefsMovesOnlySelectedOldShapes(t *testing.T) {
	f := newFixture(t)
	old1 := f.pref("test.favorites."+prefsU1+".plist", bplistEmptyDict, 30)
	old2 := f.pref("test.favorites."+prefsU2+".plist", bplistEmptyDict, 30)
	nonEmpty := f.pref("test.favorites.eeeeeeee-0000-0000-0000-000000000000.plist", bplistOneKey, 30)
	unknown := f.pref("test.favorites.ffffffff-0000-0000-0000-000000000000.plist", []byte("junk"), 30)
	fresh := f.pref("test.favorites."+prefsU3+".plist", bplistEmptyDict, 1)
	otherShape := f.pref("com.example.profile."+prefsU1+".plist", bplistOneKey, 30)
	noUUID := f.pref("com.example.app.plist", bplistOneKey, 30)
	// 形の外で古い空のもの (形の照合だけが残す)
	otherShapeEmpty := f.pref("com.example.profile."+prefsU2+".plist", bplistEmptyDict, 30)
	noUUIDEmpty := f.pref("com.example.empty.plist", bplistEmptyDict, 30)
	appleEmpty := f.pref("com.apple.test.favorites."+prefsU1+".plist", bplistEmptyDict, 30)
	dirShaped := filepath.Join(f.prefs, "test.favorites.cccccccc-0000-0000-0000-000000000000.plist")
	f.mkdir(dirShaped)
	f.age(dirShaped, 30)
	link := filepath.Join(f.prefs, "test.favorites.dddddddd-0000-0000-0000-000000000000.plist")
	if err := os.Symlink(otherShape, link); err != nil {
		t.Fatal(err)
	}
	sub := f.pref("ByHost/test.favorites."+prefsU3+".plist", bplistEmptyDict, 30)

	id, path := f.plan(kindPrefs, "-prefs-shape", "test.favorites.<UUID>")
	rc, out, errOut := f.apply(id, path)
	if rc != exitOK {
		t.Fatalf("apply rc=%d\n%s\n%s", rc, out, errOut)
	}
	for _, p := range []string{old1, old2} {
		if exists(p) || !f.trashed(kindPrefs, filepath.Base(p)) {
			t.Errorf("選んだ形の古い plist がゴミ箱の prefs/ に移っていない: %s", p)
		}
	}
	for _, p := range []string{fresh, otherShape, noUUID, otherShapeEmpty, noUUIDEmpty, appleEmpty, dirShaped, link, sub, nonEmpty, unknown} {
		if !exists(p) {
			t.Errorf("残すべきものを移した: %s", p)
		}
	}
}

func TestPrefsNonEmptyOnlyWithFlag(t *testing.T) {
	f := newFixture(t)
	empty := f.pref("test.favorites."+prefsU1+".plist", bplistEmptyDict, 30)
	nonEmpty := f.pref("test.favorites."+prefsU2+".plist", bplistOneKey, 30)
	unknown := f.pref("test.favorites."+prefsU3+".plist", []byte("junk"), 30)
	id, path := f.plan(kindPrefs, "-prefs-shape", "test.favorites.<UUID>", "-prefs-nonempty")
	if rc, out, errOut := f.apply(id, path); rc != exitOK {
		t.Fatalf("apply rc=%d\n%s\n%s", rc, out, errOut)
	}
	for _, p := range []string{empty, nonEmpty, unknown} {
		if exists(p) {
			t.Errorf("-prefs-nonempty の plan で移していない: %s", p)
		}
	}
}

func TestPrefsPlanStopsOnBadShapes(t *testing.T) {
	f := newFixture(t)
	for _, args := range [][]string{
		{"-kinds", kindPrefs},
		{"-kinds", kindPrefs, "-prefs-shape", "test.favorites.plist"},
		{"-kinds", kindPrefs, "-prefs-shape", "com.apple.x.<UUID>"},
		{"-kinds", kindPrefs, "-prefs-shape", "test." + prefsU1 + ".<UUID>"},
		{"-kinds", kindPrefs, "-prefs-shape", "../x.<UUID>"},
		{"-kinds", kindPrefs, "-prefs-shape", "a.<UUID>", "-prefs-shape", "a.<UUID>"},
		{"-kinds", kindPrefs, "-prefs-shape", "a.<UUID>", "-prefs-days", "0"},
		{"-kinds", kindPrefs, "-prefs-shape", "a.<UUID>", "-prefs-days", "3651"},
		{"-kinds", kindTmpdir, "-prefs-shape", "a.<UUID>"},
		{"-kinds", kindTmpdir, "-prefs-nonempty"},
		{"-kinds", kindTmpdir, "-prefs-days", "30"},
		{"-kinds", kindTmpdir, "-prefs-days", "0"},
	} {
		rc, out, errOut := f.runCLI(append(append([]string{"plan"}, args...), "-out", f.out)...)
		if rc != exitStopped {
			t.Errorf("%v: rc=%d (2 で止まるはず)\n%s\n%s", args, rc, out, errOut)
		}
	}
}

func TestPrefsKeptWhenUpdatedAfterPlan(t *testing.T) {
	f := newFixture(t)
	p := f.pref("test.favorites."+prefsU1+".plist", bplistEmptyDict, 30)
	id, path := f.plan(kindPrefs, "-prefs-shape", "test.favorites.<UUID>")
	// plan の後に同じ inode のまま書かれた。古さの閾値の内側には入らない (20 日前) ので、古さの取り直しでは見分けられない
	f.age(p, 20)
	rc, out, _ := f.apply(id, path)
	if rc != exitOK || !exists(p) || !strings.Contains(out, "plan の後に更新された") {
		t.Fatalf("plan の後に更新された plist を移した (rc=%d)\n%s", rc, out)
	}
}

func TestPrefsKeptWhenUpdatedAfterApplyRebuiltCandidates(t *testing.T) {
	f := newFixture(t)
	p := f.pref("test.favorites."+prefsU1+".plist", bplistEmptyDict, 30)
	id, path := f.plan(kindPrefs, "-prefs-shape", "test.favorites.<UUID>")
	// apply が候補を作り直した後、移す前に書かれた
	f.d.beforeRecheck = func(string) { f.age(p, 20) }
	rc, out, _ := f.apply(id, path)
	if rc != exitOK || !exists(p) || !strings.Contains(out, "移す直前に更新された") {
		t.Fatalf("候補を作り直した後に更新された plist を移した (rc=%d)\n%s", rc, out)
	}
}

func TestPrefsGuardOnlyRootDirectChildren(t *testing.T) {
	f := newFixture(t)
	base := filepath.Join(f.trash, "tmpclean-x")
	g := trashGuard(roots{session: f.session}, "", newPrefsScope(f.prefs, []string{"test.x.<UUID>"}), base, func(string, string) error { return nil })
	ok := f.pref("test.x."+prefsU1+".plist", bplistEmptyDict, 30)
	if err := g("trash", ok); err != nil {
		t.Errorf("根の直下の選んだ形を拒否した: %v", err)
	}
	other := f.pref("com.example.real."+prefsU1+".plist", bplistOneKey, 30)
	sub := f.pref("ByHost/test.x."+prefsU1+".plist", bplistEmptyDict, 30)
	dir := filepath.Join(f.prefs, "test.x."+prefsU2+".plist")
	f.mkdir(dir)
	link := filepath.Join(f.prefs, "test.x."+prefsU3+".plist")
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{f.prefs, other, sub, dir, link, filepath.Join(filepath.Dir(f.prefs), "test.x."+prefsU1+".plist")} {
		if err := g("trash", p); err == nil {
			t.Errorf("根の直下でないものを通した: %s", p)
		}
	}
}

func TestPrefsScopeRejectsShapelessNames(t *testing.T) {
	f := newFixture(t)
	s := newPrefsScope(f.prefs, []string{""})
	for _, name := range []string{"com.apple.finder.plist", ".GlobalPreferences.plist", "com.example.app.plist"} {
		p := f.pref(name, bplistEmptyDict, 30)
		if s.allowsRegularFile(p) {
			t.Errorf("形に \"\" が紛れたとき、形を持たない名前を通した: %s", name)
		}
	}
}

func TestPrefsKeptWhenOpen(t *testing.T) {
	f := newFixture(t)
	p := f.pref("test.favorites."+prefsU1+".plist", bplistEmptyDict, 30)
	f.sys.open = append(f.sys.open, p)
	rc, out, errOut := f.runCLI("plan", "-kinds", kindPrefs, "-prefs-shape", "test.favorites.<UUID>", "-out", f.out)
	if rc != exitOK || !strings.Contains(out, "candidates.prefs: 0") || !strings.Contains(out, "kept (開かれている): 1") {
		t.Fatalf("lsof の写しに出た plist を候補にした (rc=%d)\n%s\n%s", rc, out, errOut)
	}
}

func TestPlistContent(t *testing.T) {
	xmlEmpty := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\">\n<dict/>\n</plist>\n")
	xmlOpenClose := []byte("<?xml version=\"1.0\"?><plist version=\"1.0\"><dict>\n</dict></plist>")
	xmlArray := []byte("<?xml version=\"1.0\"?><plist version=\"1.0\"><array/></plist>")
	xmlOne := []byte("<?xml version=\"1.0\"?><plist version=\"1.0\"><dict><key>k</key><string>v</string></dict></plist>")
	truncated := append([]byte{}, bplistEmptyDict[:30]...)
	brokenOffset := append([]byte{}, bplistEmptyDict...)
	brokenOffset[len(brokenOffset)-1] = 0xFF // offset table の位置を範囲外にする
	for name, c := range map[string]struct {
		body []byte
		want plistContent
	}{
		"binary の空の dict":            {bplistEmptyDict, plistEmpty},
		"binary の 1 key":             {bplistOneKey, plistNonEmpty},
		"binary の空の array":           {bplistEmptyArr, plistNonEmpty},
		"XML の <dict/>":              {xmlEmpty, plistEmpty},
		"XML の空の dict":               {xmlOpenClose, plistEmpty},
		"XML の 1 key":                {xmlOne, plistNonEmpty},
		"XML の top が array":          {xmlArray, plistNonEmpty},
		"途中で切れた binary":              {truncated, plistUnknown},
		"offset が範囲外":                {brokenOffset, plistUnknown},
		"plist でない":                  {[]byte("hello"), plistUnknown},
		"閉じていない XML":                 {[]byte("<?xml version=\"1.0\"?><plist version=\"1.0\"><dict>"), plistUnknown},
		"object が 2 つで top が空の dict": {twoObjectsEmptyTop(), plistUnknown},
		"空のファイル":                     {nil, plistUnknown},
	} {
		p := filepath.Join(t.TempDir(), "x.plist")
		if err := os.WriteFile(p, c.body, 0o600); err != nil {
			t.Fatal(err)
		}
		if got := plistContentOf(p, int64(len(c.body))); got != c.want {
			t.Errorf("%s: got %d, want %d", name, got, c.want)
		}
	}
}

func TestPrefsShapesReport(t *testing.T) {
	f := newFixture(t)
	f.pref("test.favorites."+prefsU1+".plist", bplistEmptyDict, 30)
	f.pref("test.favorites."+prefsU2+".plist", bplistOneKey, 30)
	f.pref("test.favorites."+prefsU3+".plist", []byte("junk"), 1)
	f.pref("com.example.s."+prefsU1+".plist", bplistOneKey, 30)
	f.pref("com.apple.x."+prefsU1+".plist", bplistOneKey, 30)
	f.pref("com.example.app.plist", bplistOneKey, 30)
	rc, out, errOut := f.runCLI("prefs-shapes", "-json")
	if rc != exitOK {
		t.Fatalf("rc=%d\n%s\n%s", rc, out, errOut)
	}
	var rep PrefsReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Total != 4 || len(rep.Shapes) != 2 {
		t.Fatalf("total=%d shapes=%d (4 と 2 のはず)\n%s", rep.Total, len(rep.Shapes), out)
	}
	s := rep.Shapes[0]
	if s.Shape != "test.favorites.<UUID>" || s.Count != 3 || s.Old != 2 || s.Empty != 1 || s.NonEmpty != 1 || s.Unknown != 1 {
		t.Errorf("形の集計が違う: %+v", s)
	}
}

func TestPrefsMissingRootIsZero(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(f.prefs); err != nil {
		t.Fatal(err)
	}
	rc, out, errOut := f.runCLI("plan", "-kinds", kindPrefs, "-prefs-shape", "a.<UUID>", "-out", f.out)
	if rc != exitOK || !strings.Contains(out, "candidates.prefs: 0") {
		t.Fatalf("Preferences の無い機械で plan が 0 件にならない (rc=%d)\n%s\n%s", rc, out, errOut)
	}
}
