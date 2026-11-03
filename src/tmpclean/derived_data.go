package tmpclean

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// kindDerivedData は Xcode の DerivedData (~/Library/Developer/Xcode/DerivedData の直下) のうち、持ち主が消えたもの。
//
// Xcode は `<名前>-<hash>/` を作り、持ち主 (project・workspace・package の dir) の絶対パスを info.plist の WorkspacePath に書く。
// 持ち主の dir が無いと言い切れるものだけを候補にし、古さ (mtime) では選ばない: 1 つで数十万ファイルあり全部を歩くと重いうえ、
// 持ち主が在る DerivedData は古くても次のビルドで使われる。
//
// SwiftPM の package を xcodebuild でビルドすると、info.plist を持つ <package 名>-<hash> と別に、Build・SourcePackages を持ち
// info.plist の無い <ディレクトリ名>-<hash> を作る (2026-11-03 に実測。容量の大半はこちら)。hash は WorkspacePath から決まるので、
// info.plist の無い dir は、同じ hash で info.plist を持つ entry が 1 つ以上あり、その全部の持ち主が消えているときだけ候補にする。
const kindDerivedData = "derived-data"

// derivedDataShape は Xcode が作る dir の名前 (<名前>-<英小文字 28 字>)。ModuleCache.noindex など他の dir は当たらない
var derivedDataShape = regexp.MustCompile(`^.+-([a-z]{28})$`)

// ddEntry は DerivedData の直下の 1 件と、読んだ持ち主。
type ddEntry struct {
	name      string
	path      string
	hash      string // 形に当たらなければ ""
	realDir   bool
	hasPlist  bool
	workspace string // info.plist の WorkspacePath (読めない・無いなら "")
}

// resolveDerivedDataRoot は DerivedData の根を決める。無ければ "" (Xcode を使わない機械では 0 件)。在るのに使えなければ止める。
func resolveDerivedDataRoot(d deps) (string, error) {
	if d.derivedDataRoot == "" {
		return "", stopf("DerivedData の場所が分からない")
	}
	if d.strictRoots {
		home, err := os.UserHomeDir()
		if err != nil || d.derivedDataRoot != filepath.Join(home, "Library/Developer/Xcode/DerivedData") {
			return "", stopf("DerivedData の根が想定外: %s", d.derivedDataRoot)
		}
	}
	if _, err := os.Lstat(d.derivedDataRoot); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err := realDir(d.derivedDataRoot); err != nil {
		return "", stopf("DerivedData の根を使えない: %v", err)
	}
	return d.derivedDataRoot, nil
}

// collectDerivedData は根の直下 (only が空でなければ、名前が "-"+only で終わるものだけ) を読み、info.plist の WorkspacePath を
// plutil で読む (plist の形式を自分で解釈しない)。plutil が起動できない・時間切れなら、その entry は持ち主が分からない (残す) に倒す:
// 1 件の失敗で、他の種類を含む apply 全体を止めない
func collectDerivedData(ctx context.Context, d deps, root, only string) []ddEntry {
	var out []ddEntry
	for _, name := range readDirSorted(root) {
		if only != "" && !strings.HasSuffix(name, "-"+only) {
			continue
		}
		e := ddEntry{name: name, path: filepath.Join(root, name), realDir: isRealDir(filepath.Join(root, name))}
		if m := derivedDataShape.FindStringSubmatch(name); m != nil {
			e.hash = m[1]
		}
		if e.realDir && e.hash != "" {
			plist := filepath.Join(e.path, "info.plist")
			if fi, err := os.Lstat(plist); err == nil && fi.Mode().IsRegular() {
				e.hasPlist = true
				pctx, cancel := context.WithTimeout(ctx, cmdTimeout)
				stdout, _, rc, err := d.run(pctx, "plutil", "-extract", "WorkspacePath", "raw", "-o", "-", plist)
				cancel()
				if err == nil && rc == 0 {
					e.workspace = strings.TrimSuffix(stdout, "\n")
				}
			} else if err == nil || !errors.Is(err, fs.ErrNotExist) {
				e.hasPlist = true // ファイルでない・読めない info.plist は「持ち主が分からない」に倒す (workspace は空)
			}
		}
		out = append(out, e)
	}
	return out
}

// ownerGone は WorkspacePath の持ち主が無いと言い切れるか。次のどれかなら false (分からないものは残す):
// 絶対パスでない・在る (切れた symlink も在る扱い)・/Volumes の下 (外付けの volume は外れているだけかもしれない)・
// 存在する最も近い祖先が symlink (その先の volume が外れていると、中の持ち主は Lstat で無いように見える)・その祖先が浅い。
// 検出しない形: /Volumes 以外に置いた mount point の dir (~/mnt/nas など) が、外れて空の dir として残っているとき
// 祖先が symlink のときは、その先が在って持ち主を消しただけでも残す (取りこぼしになるが、外れた volume と見分けられない)
func ownerGone(workspace string) bool { return ownerGoneOutside(workspace, externalVolumeRoots) }

// externalVolumeRoots は外付けの volume が mount される場所 (末尾 /)
var externalVolumeRoots = []string{"/Volumes/"}

// ownerGoneOutside は ownerGone の本体。externalRoots (末尾 /) の下は外付けの volume として常に false
func ownerGoneOutside(workspace string, externalRoots []string) bool {
	if !filepath.IsAbs(workspace) || filepath.Clean(workspace) != workspace {
		return false
	}
	for _, r := range externalRoots {
		if strings.HasPrefix(workspace, r) {
			return false
		}
	}
	if _, err := os.Lstat(workspace); !errors.Is(err, fs.ErrNotExist) {
		return false
	}
	ancestor := filepath.Dir(workspace)
	for ancestor != "/" {
		fi, err := os.Lstat(ancestor)
		if err == nil {
			if fi.Mode()&fs.ModeSymlink != 0 {
				return false
			}
			break
		}
		ancestor = filepath.Dir(ancestor)
	}
	return strings.Count(ancestor, "/") >= 2
}

// derivedDataStillCandidate は移す直前に e と同じ hash の entry だけを読み直し、e が今も候補か (持ち主が戻っていないか) を確かめる。
// apply の始めに作り直した候補は、1 件ずつの lsof +D (最長 cmdTimeout) を挟むので、後ろの entry ほど古い。候補かどうかは e 自身と
// 同じ hash の info.plist だけで決まるので、全部を読み直さない (entry の数の 2 乗の plutil を避ける)。
// movedGone は同じ apply の中で移した info.plist の hash。plistHash は e が info.plist を持つときのその hash (移せた後に呼び出し側が足す)
func derivedDataStillCandidate(ctx context.Context, d deps, root string, e entry, movedGone map[string]bool) (still bool, plistHash string) {
	m := derivedDataShape.FindStringSubmatch(filepath.Base(e.Path))
	if m == nil {
		return false, ""
	}
	ents := collectDerivedData(ctx, d, root, m[1])
	for _, c := range derivedDataCandidates(world{derivedData: ents, derivedDataMovedGone: movedGone}, func(string, string) {}) {
		if c.Path == e.Path && c.Dev == e.Dev && c.Ino == e.Ino {
			for _, x := range ents {
				if x.path == e.Path && x.hasPlist {
					plistHash = x.hash
				}
			}
			return true, plistHash
		}
	}
	return false, ""
}

// derivedDataCandidates は持ち主の消えた DerivedData を選ぶ。
func derivedDataCandidates(w world, keep func(p, why string)) []entry {
	gone := map[string]bool{} // hash → info.plist を持つ entry が全部持ち主を失っているか
	for h := range w.derivedDataMovedGone {
		gone[h] = true
	}
	for _, e := range w.derivedData {
		if e.hash == "" || !e.realDir || !e.hasPlist {
			continue
		}
		g := ownerGone(e.workspace)
		if prev, ok := gone[e.hash]; ok {
			g = prev && g
		}
		gone[e.hash] = g
	}
	var out []entry
	for _, e := range w.derivedData {
		switch {
		case !e.realDir:
			keep(e.path, "dir でない")
		case e.hash == "":
			keep(e.path, "DerivedData の entry の形でない")
		case e.hasPlist && !ownerGone(e.workspace):
			keep(e.path, "持ち主が在る・分からない")
		case !e.hasPlist && !gone[e.hash]:
			// 同じ hash で info.plist を持つ entry が無い (gone に key が無い) ときも、持ち主が分からないので残す
			keep(e.path, "info.plist が無く、同じ hash の持ち主が在る・分からない")
		case w.open.has(e.path):
			keep(e.path, "開かれている")
		default:
			if c, ok := newEntry(kindDerivedData, e.path); ok {
				out = append(out, c)
			} else {
				keep(e.path, "同一性を取れない")
			}
		}
	}
	return out
}
