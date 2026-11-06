package uninstall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"doctor/runner"
	"safefs"
)

// ApplyOptions は apply の依存。TrashRoot は Finder のゴミ箱 (~/.Trash)、Guard は safefs の検査点
// (production は safefs.ProductionGuard、テストはサンドボックスの外を実行前に拒否するものを差す)。
type ApplyOptions struct {
	Options
	TrashRoot string
	Guard     safefs.Guard
}

// Done は動かした 1 件。
type Done struct {
	Owner  string `json:"owner"`
	Action Action `json:"action"`
	Path   string `json:"path"`
	To     string `json:"to,omitempty"` // ゴミ箱の中の移し先 (brew が消したものは空)
}

// Skip は動かさなかった 1 件と理由。
type Skip struct {
	Owner  string `json:"owner"`
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason"`
}

// Result は apply の結果。
type Result struct {
	PlanID  string `json:"plan_id"`
	Done    []Done `json:"done"`
	Skipped []Skip `json:"skipped,omitempty"` // 計画に無い・実体が変わった・動いている (安全のために止めたもの)
	Failed  []Skip `json:"failed,omitempty"`  // 動かそうとして失敗したもの
	Manual  []Item `json:"manual,omitempty"`  // 手で確かめるもの (動かしていない)
	Trash   string `json:"trash,omitempty"`   // 移し先のゴミ箱の dir
}

type key struct {
	action Action
	path   string
}

// Apply は承認された計画を実行する。持ち主 (アプリ / formula) ごとに候補を作り直し、計画との交わりのうち実体が
// 変わっていないものだけを動かす。持ち主の本体を消せなかったら、その持ち主の残骸には触らない (アプリが残るのに設定だけ消えるのを避ける)。
func Apply(ctx context.Context, o ApplyOptions, p Plan) Result {
	o.fill(ctx)
	res := Result{PlanID: p.ID}
	if o.TrashRoot == "" || o.Guard == nil {
		res.Failed = append(res.Failed, Skip{Reason: "ゴミ箱の場所か検査点が無い"})
		return res
	}
	res.Trash = filepath.Join(o.TrashRoot, "mac-uninstall-"+o.Now().Format("20060102-150405")+"-"+p.ID)
	planned := map[key]Item{}
	for _, it := range p.Items {
		planned[key{it.Action, it.Path}] = it
	}
	type owner struct {
		sel  Selection
		name string
	}
	var owners []owner
	for _, bid := range p.Selection.Apps {
		s := Selection{Apps: []string{bid}}
		if slices.Contains(p.Selection.Data, bid) {
			s.Data = []string{bid}
		}
		owners = append(owners, owner{s, bid})
	}
	for _, f := range p.Selection.Formulae {
		owners = append(owners, owner{Selection{Formulae: []string{f}}, f})
	}
	for _, ow := range owners {
		if ctx.Err() != nil {
			res.Skipped = append(res.Skipped, Skip{Owner: ow.name, Reason: "中断した"})
			continue
		}
		// 持ち主ごとに一覧を取り直す: 前の持ち主の brew uninstall (最大 10 分) の間に起動したアプリを見落とさない
		fresh, err := itemsFor(ctx, o.Options, List(ctx, o.Options), ow.sel)
		if err != nil {
			res.Skipped = append(res.Skipped, Skip{Owner: ow.name, Reason: err.Error()})
			continue
		}
		o.applyOwner(ctx, ow.name, fresh, planned, &res)
	}
	return res
}

func (o ApplyOptions) applyOwner(ctx context.Context, name string, fresh []Item, planned map[key]Item, res *Result) {
	// 本体 (TierApp) を先に動かし、失敗したら残骸に進まない
	slices.SortStableFunc(fresh, func(a, b Item) int {
		if (a.Tier == TierApp) != (b.Tier == TierApp) {
			if a.Tier == TierApp {
				return -1
			}
			return 1
		}
		return 0
	})
	for _, it := range fresh {
		pl, ok := planned[key{it.Action, it.Path}]
		reason := ""
		switch {
		case !ok:
			reason = "計画に無い (計画の後に増えたもの・動かし方が変わったもの)"
		case pl.Owner != it.Owner:
			reason = "計画では別の持ち主の項目"
		case it.Action != ActionBrewFormula && (pl.Dev != it.Dev || pl.Ino != it.Ino || it.Ino == 0):
			reason = "計画の後に実体が変わった (作り直された・差し替えられた)"
		}
		if reason != "" {
			res.Skipped = append(res.Skipped, Skip{Owner: name, Path: it.Path, Reason: reason})
			// 本体が計画と合わないなら残骸に進まない (アプリが残るのに設定だけ消える。cask の台帳が一時的に読めず、
			// 本体の動かし方が brew からゴミ箱へ変わったときもここに来る)
			if it.Tier == TierApp {
				return
			}
			continue
		}
		if it.Action == ActionNone {
			res.Manual = append(res.Manual, it)
			continue
		}
		if err := o.do(ctx, it, res); err != nil {
			res.Failed = append(res.Failed, Skip{Owner: name, Path: it.Path, Reason: err.Error()})
			if it.Tier == TierApp {
				return
			}
		}
	}
}

func (o ApplyOptions) do(ctx context.Context, it Item, res *Result) error {
	switch it.Action {
	case ActionBrewCask:
		if err := o.brew(ctx, "uninstall", "--cask", it.Token); err != nil {
			return err
		}
		return o.gone(it, res)
	case ActionBrewFormula:
		if err := o.brew(ctx, "uninstall", "--formula", it.Token); err != nil {
			return err
		}
		return o.gone(it, res)
	case ActionTrash:
		return o.trash(it, res)
	case ActionNone:
	}
	return fmt.Errorf("知らない動かし方: %q", it.Action)
}

// brew は brew を 1 回呼ぶ。--force・--zap・--ignore-dependencies は付けない (計画に無いものを消しうる / 依存を壊しうる)。
func (o ApplyOptions) brew(ctx context.Context, args ...string) error {
	out, stderr, rc, err := runner.WithTimeout(ctx, o.Run, 10*time.Minute, "brew", args...)
	if err != nil || rc != 0 {
		return cmdErr("brew "+strings.Join(args, " "), rc, err, stderr+"\n"+out)
	}
	return nil
}

// gone は brew が消したはずのものが本当に無くなったかを確かめる (rc=0 を成功の証拠にしない)。
func (o ApplyOptions) gone(it Item, res *Result) error {
	if _, err := os.Lstat(it.Path); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("brew は成功を返したが %s が残っている", it.Path)
	}
	res.Done = append(res.Done, Done{Owner: it.Owner, Action: it.Action, Path: it.Path})
	return nil
}

func (o ApplyOptions) trash(it Item, res *Result) error {
	if !o.allowedTrash(it) {
		return errors.New("動かしてよい置き場の外なので動かさない")
	}
	// 親までの経路に symlink があると、実体は別の場所にある (TrashMove は最後の要素だけを O_NOFOLLOW で見る)
	if err := safefs.NoSymlinkInPath(filepath.Dir(it.Path)); err != nil {
		return err
	}
	dest := filepath.Join(res.Trash, safeName(it.Owner))
	to, err := safefs.TrashMove(it.Path, dest, safefs.ID{Dev: it.Dev, Ino: it.Ino}, o.Guard)
	if err != nil {
		return err
	}
	res.Done = append(res.Done, Done{Owner: it.Owner, Action: it.Action, Path: it.Path, To: to})
	return nil
}

// allowedTrash は計画とは別に、動かしてよい置き場かを見る (計画は作り直した候補との交わりなので通常ここで落ちないが、
// 候補の作り方に誤りが入っても置き場の外へは届かないようにする)。本体はアプリの置き場の直下の .app、残骸は ~/Library の決まった置き場の下。
func (o ApplyOptions) allowedTrash(it Item) bool {
	if it.Tier == TierApp {
		return strings.HasSuffix(it.Path, ".app") && slices.Contains(o.AppDirs, filepath.Dir(filepath.Clean(it.Path)))
	}
	return (it.Tier == TierCache || it.Tier == TierData) && allowedLeftover(o.Home, it.Path)
}

// safeName は持ち主の名前をゴミ箱の中の dir の名前にする (tap の formula は user/tap/name の形)。
func safeName(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	if s == "" || s == "." || s == ".." {
		return "_"
	}
	return s
}
