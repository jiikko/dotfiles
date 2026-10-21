package tmpclean

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"safefs"
)

const (
	kindSession = "session"
	kindTmpdir  = "tmpdir"
)

// entry は移す候補。dev/ino は選んだ時点の同一性 (safefs.Identity。TrashMove が移す直前に照合する)。
type entry struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
	Dev  uint64 `json:"dev"`
	Ino  uint64 `json:"ino"`
}

type kept struct {
	path   string
	reason string
}

// selection は候補を選ぶ条件。plan に書き、apply は plan から読む (apply 側で days >= 1 と既知の kinds を検査し直す)。
type selection struct {
	Kinds       []string `json:"kinds"`
	SessionDays int      `json:"session_days"`
	TmpdirDays  int      `json:"tmpdir_days"`
}

func (s selection) has(kind string) bool {
	for _, k := range s.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

func (s selection) validate() error {
	if len(s.Kinds) == 0 {
		return stopf("種類が空")
	}
	seen := map[string]bool{}
	for _, k := range s.Kinds {
		if k != kindSession && k != kindTmpdir {
			return stopf("知らない種類: %q", k)
		}
		if seen[k] {
			return stopf("種類が重複している: %q", k)
		}
		seen[k] = true
	}
	if s.has(kindSession) && s.SessionDays < 1 || s.has(kindTmpdir) && s.TmpdirDays < 1 {
		return stopf("日数は 1 以上 (session=%d, tmpdir=%d)", s.SessionDays, s.TmpdirDays)
	}
	return nil
}

var sessionIDShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// world は 1 回の判定で使う材料 (根・生きているもの・開かれているパス・今の時刻)。
type world struct {
	roots roots
	live  liveInfo
	open  openSet
	procs map[string]bool // 動いているプロセスの名前 (tmpdir で使う)
	now   time.Time
}

func gatherWorld(ctx context.Context, d deps, sel selection) (world, error) {
	r, err := resolveRoots(d, sel.has(kindTmpdir), false)
	if err != nil {
		return world{}, err
	}
	w := world{roots: r, now: d.now()}
	if sel.has(kindSession) {
		if w.live, err = collectLive(ctx, d, r); err != nil {
			return world{}, err
		}
	}
	if w.open, err = collectOpen(ctx, d); err != nil {
		return world{}, err
	}
	if sel.has(kindTmpdir) {
		if w.procs, err = collectProcNames(ctx, d); err != nil {
			return world{}, err
		}
	}
	return w, nil
}

// candidates は今の候補と、残したものの理由を返す。plan と apply が同じこの関数を呼ぶ (apply は plan を信用せず、
// plan に載っていて今も候補であるものだけを移す)。
func candidates(w world, sel selection) ([]entry, []kept) {
	var out []entry
	var ks []kept
	keep := func(p, why string) { ks = append(ks, kept{path: p, reason: why}) }
	if sel.has(kindSession) {
		cut := w.now.Add(-time.Duration(sel.SessionDays) * 24 * time.Hour)
		for _, proj := range readDirSorted(w.roots.session) {
			pp := filepath.Join(w.roots.session, proj)
			if !strings.HasPrefix(proj, "-") || !isRealDir(pp) {
				continue // project dir の形でない (根の直下のファイル・他のツールの dir)。数えるだけで触らない
			}
			for _, sid := range readDirSorted(pp) {
				p := filepath.Join(pp, sid)
				switch {
				case !sessionIDShape.MatchString(sid) || !isRealDir(p):
					keep(p, "セッション dir の形でない")
				case w.live.projectLive(proj):
					keep(p, "生きている claude が居るプロジェクト")
				case w.live.ids[sid]:
					keep(p, "生きているセッション")
				case w.open.has(p):
					keep(p, "開かれている")
				default:
					if why := staleReason(p, cut, false); why != "" {
						keep(p, why)
						continue
					}
					if e, ok := newEntry(kindSession, p); ok {
						out = append(out, e)
					} else {
						keep(p, "同一性を取れない")
					}
				}
			}
		}
	}
	if sel.has(kindTmpdir) && w.roots.tmpdir != "" {
		cut := w.now.Add(-time.Duration(sel.TmpdirDays) * 24 * time.Hour)
		for _, name := range readDirSorted(w.roots.tmpdir) {
			p := filepath.Join(w.roots.tmpdir, name)
			fi, err := os.Lstat(p)
			switch {
			case err != nil:
				keep(p, "読めない")
			case strings.HasPrefix(name, "com.apple."):
				keep(p, "システム")
			case w.procs[name]:
				// 長く動く daemon の一時 dir (contentlinkingd など)。中が空で mtime は起動時刻のままなので、日数の閾値では守れない
				keep(p, "動いているプロセスと同じ名前")
			case fi.Mode()&fs.ModeSymlink != 0:
				keep(p, "symlink")
			case w.open.has(p):
				keep(p, "開かれている")
			default:
				if why := staleReason(p, cut, true); why != "" {
					keep(p, why)
					continue
				}
				if e, ok := newEntry(kindTmpdir, p); ok {
					out = append(out, e)
				} else {
					keep(p, "同一性を取れない")
				}
			}
		}
	}
	return out, ks
}

// staleReason は p (自身と中の全部。ファイルも dir も) が cut より前に最後に更新されたなら "" を返し、そうでなければ残す理由を返す。
// dir 自身の mtime は中を書き換えても変わらないので中まで見る。ファイルだけで見ない (テストが mktemp -d で作ったばかりの空の dir を
// 古いと読む)。走査に失敗したら残す。noSockets なら socket / FIFO を含むものも残す (パスを覚えて fd を持たない常駐の置き場)。
func staleReason(p string, cut time.Time, noSockets bool) string {
	why := ""
	err := filepath.WalkDir(p, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !fi.ModTime().Before(cut) {
			why = "新しい"
			return fs.SkipAll
		}
		if noSockets && fi.Mode()&(fs.ModeSocket|fs.ModeNamedPipe) != 0 {
			why = "socket / FIFO を含む"
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "読めない"
	}
	return why
}

func newEntry(kind, p string) (entry, bool) {
	id, err := safefs.Identity(p)
	if err != nil || id == (safefs.ID{}) {
		return entry{}, false
	}
	return entry{Kind: kind, Path: p, Dev: id.Dev, Ino: id.Ino}, true
}

func isRealDir(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.IsDir()
}

func readDirSorted(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}
