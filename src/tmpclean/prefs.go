package tmpclean

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// kindPrefs は ~/Library/Preferences の直下の、名前に UUID を含む plist のうち、ユーザーが選んだ形 (UUID を <UUID> に置き換えた名前) のもの。
//
// 一時領域と違い、本物の設定が同じ dir に並び、名前に UUID を含む本物の設定もある (アプリが作る profile の suite など)。なので
// 「UUID を含めば候補」にせず、形をユーザーが選んだものだけを候補にする。形がテストの残骸かを決めるのは、その形を作るソースを読んだ
// 利用者 (skill cleanup-tmp-dir の手順) で、ここでは決めない。
//
// 移す直前の 1 件ずつの lsof は取らない: plist は cfprefsd が読み書きし、開いたままにしない (lsof に出ない)。1 件 0.3 秒の lsof は
// 1 万件で 1 時間かかるうえ、何も検出しない。守りは古さ (mtime) と、plan の時点の mtime との照合 (同じ inode のまま書かれたものを見分ける)。
// 中身が入っているもの・空かを判定できないものは、selection.PrefsNonEmpty を立てた plan (別の承認) でだけ候補にする。
const kindPrefs = "prefs"

// prefsMaxDays は prefs の日数の上限 (time.Duration に直すと 106,752 日で溢れ、閾値が未来になって全部が古く見える)
const prefsMaxDays = 3650

// prefsUUID は名前の中の UUID (大小を問わない)。
var prefsUUID = regexp.MustCompile(`[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}`)

const prefsUUIDToken = "<UUID>"

// prefsShape は名前の形を返す。候補にしうる名前 (.plist で終わり、UUID を含み、com.apple. で始まらない) でなければ ""。
func prefsShape(name string) string {
	base, ok := strings.CutSuffix(name, ".plist")
	if !ok || strings.HasPrefix(name, "com.apple.") || !prefsUUID.MatchString(base) {
		return ""
	}
	return prefsUUID.ReplaceAllString(base, prefsUUIDToken)
}

// validPrefsShape は plan に書く形を検査する。prefsShape が返しうる形だけを通す (<UUID> を含む・com.apple. で始まらない・
// UUID そのものや区切りの / を含まない)。
func validPrefsShape(s string) error {
	switch {
	case !strings.Contains(s, prefsUUIDToken):
		return stopf("形に %s が無い: %q", prefsUUIDToken, s)
	case strings.HasPrefix(s, "com.apple."):
		return stopf("com.apple. の形は扱わない: %q", s)
	case strings.Contains(s, "/") || prefsUUID.MatchString(s):
		return stopf("形に / か UUID そのものが入っている: %q", s)
	}
	return nil
}

// resolvePrefsRoot は Preferences の根を決める。無ければ "" (0 件)。在るのに使えなければ止める。
func resolvePrefsRoot(d deps) (string, error) {
	if d.prefsRoot == "" {
		return "", stopf("Preferences の場所が分からない")
	}
	if d.strictRoots {
		home, err := os.UserHomeDir()
		if err != nil || d.prefsRoot != filepath.Join(home, "Library/Preferences") {
			return "", stopf("Preferences の根が想定外: %s", d.prefsRoot)
		}
	}
	if _, err := os.Lstat(d.prefsRoot); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err := realDir(d.prefsRoot); err != nil {
		return "", stopf("Preferences の根を使えない: %v", err)
	}
	return d.prefsRoot, nil
}

// readPrefsNames は根の直下の名前を読む。読めなければ止める (0 件に丸めると、権限の誤りが「候補なし」の成功に見える)。
func readPrefsNames(root string) ([]string, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, stopf("Preferences を読めない: %v", err)
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// prefsScope は prefs で移してよいもの (根の直下の、選んだ形の名前の通常のファイル)。候補の選定と検査点が同じこれを使う。
type prefsScope struct {
	root   string
	shapes map[string]bool
}

func newPrefsScope(root string, shapes []string) prefsScope {
	s := prefsScope{root: root, shapes: map[string]bool{}}
	for _, x := range shapes {
		s.shapes[x] = true
	}
	return s
}

// allows は path が根の直下で、名前の形が選んだ形のどれかか (ファイルの種類は見ない)。形を持たない名前 (prefsShape が "") は、
// 選んだ形に "" が紛れても通さない (検査点が validatePrefs の検証に頼らないため)。
func (s prefsScope) allows(path string) bool {
	shape := prefsShape(filepath.Base(path))
	return s.root != "" && shape != "" && underAtDepth(s.root, path, 1) && s.shapes[shape]
}

// allowsRegularFile は allows に加えて、path が通常のファイル (symlink・dir でない) か (検査点が使う)。
func (s prefsScope) allowsRegularFile(path string) bool {
	if !s.allows(path) {
		return false
	}
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode().IsRegular()
}

// prefsCandidates は選んだ形に当たり、PrefsDays 日より古い通常のファイルを選ぶ。PrefsNonEmpty が立っていなければ空のものだけ。
// 形に当たらないものは数えない (本物の設定が数千件あり、残した理由に並べても読めない)。
func prefsCandidates(w world, sel selection, keep func(p, why string)) []entry {
	if w.prefsRoot == "" {
		return nil
	}
	scope := newPrefsScope(w.prefsRoot, sel.PrefsShapes)
	cut := w.now.Add(-time.Duration(sel.PrefsDays) * 24 * time.Hour)
	var out []entry
	for _, name := range w.prefsNames {
		p := filepath.Join(w.prefsRoot, name)
		if !scope.allows(p) {
			continue
		}
		fi, err := os.Lstat(p)
		switch {
		case err != nil:
			keep(p, "読めない")
		case !fi.Mode().IsRegular():
			keep(p, "通常のファイルでない")
		case !fi.ModTime().Before(cut):
			keep(p, "新しい")
		case unmovable(fi):
			keep(p, "移せない属性 (immutable / SIP)")
		case w.open.has(p):
			keep(p, "開かれている")
		case !sel.PrefsNonEmpty && plistContentOf(p, fi.Size()) != plistEmpty:
			keep(p, "中身がある・空かを判定できない")
		default:
			e, ok := newEntry(kindPrefs, p)
			if !ok {
				keep(p, "同一性を取れない")
				continue
			}
			e.MtimeNs = fi.ModTime().UnixNano()
			out = append(out, e)
		}
	}
	return out
}

// prefsUnchanged は移す直前に e を Lstat し直し、通常のファイルで、mtime が plan の時点と同じかを確かめる
// (同じ inode のまま書かれたものは dev/ino の照合では見分けられない)。この確かめから rename までの窓は残る。
func prefsUnchanged(e entry) bool {
	fi, err := os.Lstat(e.Path)
	return err == nil && fi.Mode().IsRegular() && fi.ModTime().UnixNano() == e.MtimeNs
}

// PrefsShape は 1 つの形の集計。
type PrefsShape struct {
	Shape    string    `json:"shape"`
	Count    int       `json:"count"`
	Old      int       `json:"old"`       // Days 日より古い (plan の候補になりうる) 件数
	Empty    int       `json:"empty"`     // 中身が空の dict
	NonEmpty int       `json:"non_empty"` // 値を持つ
	Unknown  int       `json:"unknown"`   // 空かを判定できない (形式が違う・壊れている・大きすぎる・読めない)
	Bytes    int64     `json:"bytes"`
	Oldest   time.Time `json:"oldest"`
	Newest   time.Time `json:"newest"`
	Example  string    `json:"example"`
}

// PrefsReport は prefs-shapes の出力。Root が空なら Preferences の dir が無い (0 件)。
type PrefsReport struct {
	Root   string       `json:"root"`
	Days   int          `json:"days"`
	Total  int          `json:"total"`
	Shapes []PrefsShape `json:"shapes"`
}

// prefsShapes は Preferences の直下の、候補にしうる名前の通常のファイルを形ごとに数える。何も動かさない。
func prefsShapes(d deps, days int) (PrefsReport, error) {
	if days < 1 || days > prefsMaxDays {
		return PrefsReport{}, stopf("日数は 1〜%d (days=%d)", prefsMaxDays, days)
	}
	root, err := resolvePrefsRoot(d)
	if err != nil {
		return PrefsReport{}, err
	}
	rep := PrefsReport{Root: root, Days: days, Shapes: []PrefsShape{}}
	if root == "" {
		return rep, nil
	}
	names, err := readPrefsNames(root)
	if err != nil {
		return PrefsReport{}, err
	}
	cut := d.now().Add(-time.Duration(days) * 24 * time.Hour)
	by := map[string]*PrefsShape{}
	for _, name := range names {
		shape := prefsShape(name)
		if shape == "" {
			continue
		}
		p := filepath.Join(root, name)
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		s := by[shape]
		if s == nil {
			s = &PrefsShape{Shape: shape, Example: name, Oldest: fi.ModTime(), Newest: fi.ModTime()}
			by[shape] = s
		}
		rep.Total++
		s.Count++
		s.Bytes += fi.Size()
		if fi.ModTime().Before(cut) {
			s.Old++
		}
		if fi.ModTime().Before(s.Oldest) {
			s.Oldest = fi.ModTime()
		}
		if fi.ModTime().After(s.Newest) {
			s.Newest = fi.ModTime()
		}
		switch plistContentOf(p, fi.Size()) {
		case plistEmpty:
			s.Empty++
		case plistNonEmpty:
			s.NonEmpty++
		default:
			s.Unknown++
		}
	}
	for _, s := range by {
		rep.Shapes = append(rep.Shapes, *s)
	}
	sort.Slice(rep.Shapes, func(i, j int) bool {
		a, b := rep.Shapes[i], rep.Shapes[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Shape < b.Shape
	})
	return rep, nil
}

type plistContent int

const (
	plistUnknown plistContent = iota
	plistEmpty
	plistNonEmpty
)

// plistMaxRead より大きい plist は読まずに判定できないとする (空の dict の plist は数十〜数百バイト)。
const plistMaxRead = 1 << 20

// plistContentOf は p の top の値が空の dict か、値を持つかを返す。binary (bplist00) と XML だけを読み、それ以外・壊れたもの・
// 大きいもの・読めないものは plistUnknown (空にも中身ありにも数えない)。plutil を 1 万回起こさないために自分で読む。
func plistContentOf(p string, size int64) plistContent {
	if size > plistMaxRead {
		return plistUnknown
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return plistUnknown
	}
	if bytes.HasPrefix(b, []byte("bplist00")) {
		return bplistContent(b)
	}
	return xmlPlistContent(b)
}

// bplistContent は binary plist の trailer (末尾 32 バイト) から top の object を引き、その marker で判定する。
// dict の marker は上位 4 bit が 0xD で、下位 4 bit が要素の数 (0xF なら数は後ろの int object にあり、15 以上)。
func bplistContent(b []byte) plistContent {
	const trailer = 32
	if len(b) < 8+trailer {
		return plistUnknown
	}
	t := b[len(b)-trailer:]
	offSize := uint64(t[6])
	num := binary.BigEndian.Uint64(t[8:16])
	top := binary.BigEndian.Uint64(t[16:24])
	tableOff := binary.BigEndian.Uint64(t[24:32])
	body := uint64(len(b) - trailer)
	if offSize < 1 || offSize > 8 || num > body || top >= num || tableOff >= body || (body-tableOff)/offSize <= top {
		return plistUnknown
	}
	at := tableOff + top*offSize
	var objOff uint64
	for _, c := range b[at : at+offSize] {
		objOff = objOff<<8 | uint64(c)
	}
	if objOff < 8 || objOff >= tableOff {
		return plistUnknown
	}
	m := b[objOff]
	if m>>4 != 0xD {
		return plistNonEmpty // top が dict でない値 (UserDefaults は dict しか書かないが、値を持つものとして数える)
	}
	if m&0x0F != 0 {
		return plistNonEmpty
	}
	if num != 1 {
		// 空の dict だけの plist は object が 1 つ。他の object を持つのに top が空の dict なら、壊れているか知らない形
		return plistUnknown
	}
	return plistEmpty
}

// xmlPlistContent は <plist> の最初の子の要素を見る。dict で子の要素が無く、</plist> まで閉じていれば空。
// 途中で切れたもの・閉じていないものは判定できない (Strict で読む。緩く読むと、切れたファイルを空の dict と読む)。
func xmlPlistContent(b []byte) plistContent {
	dec := xml.NewDecoder(bytes.NewReader(b))
	depth := 0
	topIsDict := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return plistUnknown // io.EOF を含む: </plist> の前に終わった
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch {
			case depth == 1 && t.Name.Local != "plist":
				return plistUnknown
			case depth == 2 && t.Name.Local != "dict":
				return plistNonEmpty
			case depth == 2:
				topIsDict = true
			case depth >= 3:
				return plistNonEmpty
			}
		case xml.EndElement:
			depth--
			if depth == 0 && topIsDict {
				return plistEmpty
			}
		}
	}
}
