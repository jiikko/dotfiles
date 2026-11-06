package uninstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// planMaxAge を過ぎた計画は apply が断る (見せた時点から状況が変わっている)。
const planMaxAge = time.Hour

// Action は 1 項目の動かし方。
type Action string

const (
	ActionTrash       Action = "trash"        // ゴミ箱へ移す
	ActionBrewCask    Action = "brew-cask"    // brew uninstall --cask <token> (本体は brew が消す)
	ActionBrewFormula Action = "brew-formula" // brew uninstall <name>
	ActionNone        Action = "none"         // 動かさない (手で確かめるもの)
)

// Item は計画の 1 項目。
type Item struct {
	Owner  string `json:"owner"` // bundle ID か formula の名前
	Tier   Tier   `json:"tier"`
	Action Action `json:"action"`
	Path   string `json:"path"`
	Token  string `json:"token,omitempty"` // cask / formula の名前 (brew に渡す)
	SizeKB int64  `json:"size_kb"`
	Dev    uint64 `json:"dev,omitempty"`
	Ino    uint64 `json:"ino,omitempty"`
	ByName bool   `json:"by_name,omitempty"`
	Note   string `json:"note,omitempty"`
}

// Selection は何を消すかの選択 (計画の ID に入れる)。
type Selection struct {
	Apps     []string `json:"apps"`     // bundle ID
	Data     []string `json:"data"`     // 設定とデータも消すアプリの bundle ID (Apps の部分集合)
	Formulae []string `json:"formulae"` // formula の名前
}

// Plan は承認を取るための計画。
type Plan struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Selection Selection `json:"selection"`
	Items     []Item    `json:"items"`
}

// BuildPlan は選択から計画を作る (何も動かさない)。動いているもの・知らない名前は error にする (計画を作らない)。
func BuildPlan(ctx context.Context, o Options, sel Selection) (Plan, error) {
	o.fill(ctx)
	sel = normalize(sel)
	if err := checkSelection(sel); err != nil {
		return Plan{}, err
	}
	inv := List(ctx, o)
	items, err := itemsFor(ctx, o, inv, sel)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{CreatedAt: o.Now().UTC().Truncate(time.Second), Selection: sel, Items: items}
	p.ID, err = planID(p)
	return p, err
}

func normalize(sel Selection) Selection {
	for _, s := range []*[]string{&sel.Apps, &sel.Data, &sel.Formulae} {
		slices.Sort(*s)
		*s = slices.Compact(*s)
	}
	return sel
}

func checkSelection(sel Selection) error {
	if len(sel.Apps)+len(sel.Formulae) == 0 {
		return errors.New("何も選ばれていない")
	}
	for _, d := range sel.Data {
		if !slices.Contains(sel.Apps, d) {
			return fmt.Errorf("設定とデータを消すアプリ %q が、消すアプリに選ばれていない", d)
		}
	}
	for _, f := range sel.Formulae {
		if f == "" || strings.HasPrefix(f, "-") {
			return fmt.Errorf("formula の名前が不正: %q", f)
		}
	}
	return nil
}

// itemsFor は選択と今の一覧から項目を作る。BuildPlan と apply の両方がこれを通る (apply はこれで候補を作り直す)。
func itemsFor(ctx context.Context, o Options, inv Inventory, sel Selection) ([]Item, error) {
	if inv.RunningUnknown {
		return nil, errors.New("動いているかを確かめられない (lsof / ps が失敗した)。動いているものを消しうるので計画を作らない")
	}
	var items []Item
	for _, bid := range sel.Apps {
		var same []App
		for _, a := range inv.Apps {
			// APFS は大小文字を区別しないので、大小だけが違う bundle ID は同じ残骸の dir を指す
			if strings.EqualFold(a.BundleID, bid) {
				same = append(same, a)
			}
		}
		switch len(same) {
		case 0:
			return nil, fmt.Errorf("アプリ %q が見つからない (Apple のアプリは対象外)", bid)
		case 1:
		default:
			// 残骸は bundle ID で決まるので、片方を消すと他方の設定・キャッシュも動かしてしまう
			return nil, fmt.Errorf("bundle ID %q のアプリが %d 個ある。どれを消すかで残骸が決まらないので計画を作らない", bid, len(same))
		}
		app := same[0]
		// 探す置き場の外 (開発中のビルド・サブフォルダ・App Translocation) にも同じ bundle ID のコピーがありうる。残骸は bundle ID で
		// 決まるので、コピーが 1 つでもあれば計画を作らない。Spotlight で数え、数えられなければ (索引が無い) 作らない
		copies, err := spotlightCopies(ctx, o.Run, bid)
		switch {
		case err != nil:
			return nil, fmt.Errorf("%s の他のコピーを Spotlight で確かめられないので計画を作らない: %w", app.Name, err)
		case !slices.Contains(copies, app.Path):
			return nil, fmt.Errorf("%s が Spotlight の索引に無い (索引が古いか無効)。他のコピーを確かめられないので計画を作らない", app.Name)
		case len(copies) > 1:
			return nil, fmt.Errorf("bundle ID %q のアプリが %d 個ある (%s)。片方の設定・キャッシュを消すと他方も壊れるので計画を作らない",
				bid, len(copies), strings.Join(copies, ", "))
		}
		// 動いているかは Spotlight に頼らない (ドットで始まる dir・.noindex・App Translocation は索引の外)。動いている実行ファイルの外側の
		// .app を読み、bundle ID で突き合わせる。🚨 動いていないコピーが索引の外にある形は検出しない
		if app.Running || slices.ContainsFunc(copies, func(c string) bool { return runningUnder(inv.exes, c) }) || runningBundle(inv.exes, bid) {
			return nil, fmt.Errorf("%s が動いている。終了してから計画を作り直す", app.Name)
		}
		top := Item{Owner: bid, Tier: TierApp, Action: ActionTrash, Path: app.Path, SizeKB: app.SizeKB}
		if app.Source == SourceCask {
			top.Action, top.Token = ActionBrewCask, app.CaskToken
		}
		// 本体の外に常駐・ドライバを置くもの (pkg・root の常駐) と、sudo の要る cask の手順を持つものは、本体も残骸も動かさず、
		// 手で消すものとして載せる (本体だけをゴミ箱へ移すと、常駐が壊れた登録になって残る)
		manual := app.Manual != ""
		if manual {
			top.Tier, top.Action, top.Note = TierManual, ActionNone, app.Manual
		}
		items = append(items, withIdentity(top))
		wantData := slices.Contains(sel.Data, bid)
		for _, l := range leftovers(o.Home, app) {
			it := Item{Owner: bid, Tier: l.Tier, Action: ActionTrash, Path: l.Path, ByName: l.ByName, Note: l.Note, SizeKB: duKB(ctx, o.Run, l.Path)}
			switch {
			case l.Tier == TierManual || manual || l.ByName:
				// 名前で一致したものは同名の別物 (Apple の MobileSync・Google など) でありうるので、動かさずに見せるだけ
				it.Tier, it.Action = TierManual, ActionNone
			case l.Tier == TierData && !wantData:
				continue // 選ばれていない設定とデータは計画に載せない (残す)
			}
			items = append(items, withIdentity(it))
		}
	}
	for _, name := range sel.Formulae {
		i := slices.IndexFunc(inv.Formulae, func(f Formula) bool { return f.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("formula %q が brew leaves に無い (他の formula が依存しているか、入っていない)", name)
		}
		f := inv.Formulae[i]
		if f.Running {
			return nil, fmt.Errorf("formula %s のプログラムかライブラリが使われている", name)
		}
		if f.Protected != "" {
			return nil, fmt.Errorf("formula %s は消さない: %s", name, f.Protected)
		}
		items = append(items, Item{Owner: name, Tier: TierApp, Action: ActionBrewFormula, Path: f.Path, Token: name, SizeKB: f.SizeKB})
	}
	return items, nil
}

// runningBundle は、動いている実行ファイルのどれかが bundle ID bid の .app の中にあるか (置き場を問わない)。
func runningBundle(exes []string, bid string) bool {
	seen := map[string]bool{}
	for _, e := range exes {
		i := strings.Index(e, ".app/Contents/")
		if i < 0 {
			continue
		}
		top := e[:i+len(".app")]
		if seen[top] {
			continue
		}
		seen[top] = true
		if a, ok := readApp(top); ok && strings.EqualFold(a.BundleID, bid) {
			return true
		}
	}
	return false
}

// spotlightCopies は同じ bundle ID のアプリのパスを Spotlight で全部引く (大小文字を区別しない)。
// bundle ID は validBundleID を通ったもの (英数字と . - _ だけ) なので、問い合わせの文字列に入れても壊れない。
func spotlightCopies(ctx context.Context, run runner.Runner, bid string) ([]string, error) {
	out, stderr, rc, err := runner.WithTimeout(ctx, run, 30*time.Second, "/usr/bin/mdfind", "kMDItemCFBundleIdentifier == '"+bid+"'c")
	if err != nil || rc != 0 {
		return nil, cmdErr("mdfind", rc, err, stderr)
	}
	var paths []string
	for line := range strings.SplitSeq(out, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			paths = append(paths, filepath.Clean(s))
		}
	}
	return paths, nil
}

// withIdentity は今の実体 (dev, ino) を控える。apply はこれが変わったものを動かさない。
func withIdentity(it Item) Item {
	if id, err := safefs.Identity(it.Path); err == nil {
		it.Dev, it.Ino = id.Dev, id.Ino
	}
	return it
}

func planID(p Plan) (string, error) {
	body, err := json.Marshal(struct {
		CreatedAt time.Time `json:"created_at"`
		Selection Selection `json:"selection"`
		Items     []Item    `json:"items"`
	}{p.CreatedAt, p.Selection, p.Items})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:12], nil
}

// WritePlan は計画を <dir>/plan-<ID>.json に書く。
func WritePlan(dir string, p Plan) (string, error) {
	body, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "plan-"+p.ID+".json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// ReadPlan は計画を読み、ID と古さを確かめる。
func ReadPlan(path, approve string, now time.Time) (Plan, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, err
	}
	var p Plan
	if err := json.Unmarshal(body, &p); err != nil {
		return Plan{}, fmt.Errorf("計画が読めない: %w", err)
	}
	id, err := planID(p)
	if err != nil {
		return Plan{}, err
	}
	if err := checkSelection(normalize(p.Selection)); err != nil {
		return Plan{}, fmt.Errorf("計画の選択が不正: %w", err)
	}
	switch {
	case id != p.ID:
		return Plan{}, errors.New("計画の中身と ID が合わない (書き換えられた)")
	case approve != p.ID:
		return Plan{}, fmt.Errorf("承認の ID %q が計画の ID %q と違う", approve, p.ID)
	case now.Sub(p.CreatedAt) > planMaxAge:
		return Plan{}, fmt.Errorf("計画が古い (%s 前に作った。1 時間以内に作り直す)", now.Sub(p.CreatedAt).Round(time.Minute))
	case now.Before(p.CreatedAt.Add(-time.Minute)):
		return Plan{}, errors.New("計画の作成時刻が未来になっている")
	}
	return p, nil
}
