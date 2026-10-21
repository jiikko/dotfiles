package tmpclean

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"safefs"
)

type moved struct {
	Kind string    `json:"kind"`
	From string    `json:"from"`
	To   string    `json:"to"`
	At   time.Time `json:"at"`
}

type applyResult struct {
	moved   []moved
	kept    []kept
	failed  []kept
	stopped error // 前提の検査や判定不能で止めた理由 (nil = 走り切った)
}

// apply は plan に載っていて今も候補であるものだけを、1 件ずつ判定し直してからゴミ箱へ移す。
// この module で破壊的な操作をするのはここだけ (safefs.TrashMove)。
func apply(ctx context.Context, d deps, p planFile, outDir string) applyResult {
	var res applyResult
	w, err := gatherWorld(ctx, d, p.Selection)
	if err != nil {
		res.stopped = err
		return res
	}
	base, err := prepareTrash(d, w.now, p.ID)
	if err != nil {
		res.stopped = err
		return res
	}
	defer func() { _ = os.Remove(base) }() // 1 件も移さなかったときの空の移し先を残さない (中身があれば消えない)
	if !isRealDir(outDir) {
		res.stopped = stopf("-out の dir が無い: %s", outDir)
		return res
	}
	manifest, err := os.OpenFile(filepath.Join(outDir, "moved-"+p.ID+".jsonl"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		res.stopped = stopf("移した記録を書けない: %v", err)
		return res
	}
	defer func() { _ = manifest.Close() }()

	cur, _ := candidates(w, p.Selection)
	now := make(map[string]entry, len(cur))
	for _, e := range cur {
		now[e.Path] = e
	}
	guard := trashGuard(w.roots, base, d.inner)
	keep := func(path, why string) { res.kept = append(res.kept, kept{path: path, reason: why}) }

	for _, e := range p.Entries {
		c, ok := now[e.Path]
		switch {
		case !ok:
			keep(e.Path, "今は候補でない")
			continue
		case c.Dev != e.Dev || c.Ino != e.Ino:
			keep(e.Path, "plan の後に差し替わった")
			continue
		}
		dest := filepath.Join(base, e.Kind)
		if e.Kind == kindSession {
			proj := filepath.Base(filepath.Dir(e.Path))
			// 走査の間に再開されたセッション (claude -r) を守るため、移す直前に生きているものを取り直す
			live, err := collectLive(ctx, d, w.roots)
			if err != nil {
				res.stopped = err
				return res
			}
			if live.projectLive(proj) || live.ids[filepath.Base(e.Path)] {
				keep(e.Path, "移す直前に claude が居た")
				continue
			}
			dest = filepath.Join(dest, proj)
		}
		days := p.Selection.TmpdirDays
		if e.Kind == kindSession {
			days = p.Selection.SessionDays
		}
		if why := staleReason(e.Path, d.now().Add(-time.Duration(days)*24*time.Hour), e.Kind == kindTmpdir); why != "" {
			keep(e.Path, "移す直前に"+why)
			continue
		}
		switch openNow(ctx, d, e.Path, isRealDir(e.Path)) {
		case isOpen:
			keep(e.Path, "移す直前に開かれていた")
			continue
		case openUnknown:
			// 判定できなかったものは残すうえで「移せなかった」に数える (全件が判定不能でも成功に見せない)
			res.failed = append(res.failed, kept{path: e.Path, reason: "lsof で使用中かを判定できない"})
			continue
		case notOpen:
		}
		to, err := safefs.TrashMove(e.Path, dest, safefs.ID{Dev: e.Dev, Ino: e.Ino}, guard)
		if err != nil {
			res.failed = append(res.failed, kept{path: e.Path, reason: err.Error()})
			continue
		}
		m := moved{Kind: e.Kind, From: e.Path, To: to, At: d.now().UTC()}
		res.moved = append(res.moved, m)
		// 1 件ごとに記録して fsync する (途中で止まっても、何をどこへ移したかが残る。TrashMove は名前が埋まっていれば
		// `<名前> 2` に改名するので、名前からは辿れない)。書けなければそこで止める
		line, _ := json.Marshal(m)
		if _, err := manifest.Write(append(line, '\n')); err != nil {
			res.stopped = stopf("移した記録を書けない: %v", err)
			return res
		}
		if err := manifest.Sync(); err != nil {
			res.stopped = stopf("移した記録を書けない: %v", err)
			return res
		}
	}
	return res
}

// prepareTrash は移す先 (<ゴミ箱>/tmpclean-<日時>-<ID>) を決める。どの dir も作る前に、ゴミ箱の経路に symlink が無いことを
// 確かめる (safefs.TrashMove は MkdirAll を symlink の検査より先に呼ぶので、そのままだと symlink の先に dir ができる)。
func prepareTrash(d deps, now time.Time, id string) (string, error) {
	if d.trashRoot == "" {
		return "", stopf("ゴミ箱の場所が分からない")
	}
	if err := realDir(d.trashRoot); err != nil {
		return "", stopf("ゴミ箱を使えない: %v", err)
	}
	// 移し先は自分で排他的に作り、実体の dir であることを確かめる (先に symlink を置かれていると、TrashMove の MkdirAll が
	// リンクの先に dir を作ってから断る)
	base := filepath.Join(d.trashRoot, fmt.Sprintf("tmpclean-%s-%s", now.Format("20060102-150405"), id))
	if err := os.Mkdir(base, 0o700); err != nil {
		return "", stopf("移し先を作れない: %v", err)
	}
	if err := realDir(base); err != nil {
		return "", stopf("移し先が実体の dir でない: %v", err)
	}
	return base, nil
}

// trashGuard は tmpclean 自身の検査点。移す元は根の配下の決まった深さ (session = 根/project/セッション、tmpdir = 根の直下) だけ、
// 移す先は今回の移し先の配下だけを通し、その上で inner (production は safefs.ProductionGuard、テストは sandbox) を呼ぶ。
// 候補の選定が壊れても、根の外・根そのもの・浅い dir を移せないようにする 2 段目。
func trashGuard(r roots, base string, inner safefs.Guard) safefs.Guard {
	return func(op, path string) error {
		path = filepath.Clean(path)
		switch op {
		case "trash":
			if !underAtDepth(r.session, path, 2) && (r.tmpdir == "" || !underAtDepth(r.tmpdir, path, 1)) {
				return fmt.Errorf("tmpclean: 根の配下の決まった深さでないので移さない: %s", path)
			}
		case "trash-dest":
			if path != base && !strings.HasPrefix(path, base+"/") {
				return fmt.Errorf("tmpclean: 今回の移し先の外へは移さない: %s", path)
			}
		default:
			return fmt.Errorf("tmpclean: 想定外の操作 %s: %s", op, path)
		}
		if inner == nil {
			return fmt.Errorf("tmpclean: 検査点が無い: %s %s", op, path)
		}
		return inner(op, path)
	}
}

// underAtDepth は path が root のちょうど depth 段下か (.. や空の要素を含まない)。
func underAtDepth(root, path string, depth int) bool {
	if root == "" || !strings.HasPrefix(path, root+"/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, root+"/"), "/")
	if len(parts) != depth {
		return false
	}
	for _, s := range parts {
		if s == "" || s == "." || s == ".." {
			return false
		}
	}
	return true
}
