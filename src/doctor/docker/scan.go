package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"doctor/runner"
)

// DefaultOldAfter は「古い」の境界。これを過ぎて一度も使われていないものだけを候補にする。
//
// 14 日にしたのは、提示するコマンドの filter (until / unused-for) にそのまま渡せる形で、
// かつ「先週まで触っていた作業環境」を巻き込まないため。短くすると、金曜に止めたコンテナを
// 月曜に消せと言い出す。長くすると (30 日) 月次のプロジェクトが毎回対象外になる。
// 🚨 変えるときは提示コマンドの filter も一緒に動く (どちらも OldAfter から組む)。
const DefaultOldAfter = 14 * 24 * time.Hour

// scanTimeout は docker CLI 1 回の上限。daemon が起動途中だと応答しないことがあるので、
// 待たずに「診断できず」へ倒す (0 件に畳まない)。
const scanTimeout = 20 * time.Second

// Kind は候補の種別。表示順もこの順。
type Kind string

const (
	KindContainers Kind = "containers"
	KindImages     Kind = "images"
	KindBuildCache Kind = "build-cache"
	KindVolumes    Kind = "volumes"
)

// Item は候補 1 件。Name は**そのまま提示コマンドに載る識別子**なので、無害化では
// 書き換えず落とす (disk.Item.Path / svc.Finding.Label と同じ判断)。
type Item struct {
	Name     string        `json:"name"`
	Detail   string        `json:"detail,omitempty"` // 状態・元イメージ・キャッシュの説明など
	Size     int64         `json:"size"`
	SizeText string        `json:"size_text"` // docker 自身の表記 (自前で丸め直さない)
	Age      time.Duration `json:"age"`
	AgeKnown bool          `json:"age_known"`
	Command  string        `json:"command,omitempty"` // この 1 件だけを消すコマンド
}

// Group は種別ごとのまとめ。
//
// 🚨 **Total / TotalSize / Reclaimable は docker 自身の申告** (`docker system df` の集計) を
// そのまま持つ。レイヤーは複数のイメージで共有されるので、per-object のサイズを足しても
// docker の合計にはならない (実測 2026-09-04: 未使用イメージ 27 件の UniqueSize 合計 38.3GB に
// 対し、docker の回収可能量は 27.12GB)。**同じ答えを出している経路があるなら近似を作らない。**
//
// Size は「候補として挙げた行のサイズの単純合計」= 見積もりで、上の理由から Reclaimable を
// 上回りうる。2 つを別の名前で持つのはそのため (混ぜると画面の数字が docker と食い違う)。
type Group struct {
	Kind      Kind   `json:"kind"`
	Label     string `json:"label"`
	Total     int    `json:"total"`
	TotalSize int64  `json:"total_size"`
	// Reclaimable は docker が「回収できる」と申告した量 (古さは見ていない = 候補より広い)
	Reclaimable int64    `json:"reclaimable"`
	Items       []Item   `json:"items,omitempty"`
	Size        int64    `json:"size"`
	Command     string   `json:"command,omitempty"` // 群をまとめて回収するコマンド ("" = 出さない)
	Notes       []string `json:"notes,omitempty"`
	Confirm     bool     `json:"confirm"` // 消えると戻らないものを含む (ボリューム)
}

// Report は 1 回の診断結果。
type Report struct {
	Installed   bool          `json:"installed"`             // Docker Desktop / docker CLI があるか
	Unavailable string        `json:"unavailable,omitempty"` // 診断できなかった理由 ("" = 診断できた)
	Groups      []Group       `json:"groups,omitempty"`
	SystemPrune string        `json:"system_prune,omitempty"` // まとめて回収するコマンド
	OldAfter    time.Duration `json:"old_after"`
	ScannedAt   time.Time     `json:"scanned_at"`
	Dropped     int           `json:"dropped"` // 名前が識別子として読めず一覧から外した件数
}

// Estimate は候補として挙げた行のサイズの単純合計 (全群)。**docker の回収可能量ではない** —
// 共有レイヤーを行ごとに数えるので上振れする (実測 2026-09-04: 見積もり 93.4GB に対し
// docker 申告 45.1GB)。画面の見出しに出す数字は DockerReclaimable の方。
func (r Report) Estimate() int64 {
	var n int64
	for _, g := range r.Groups {
		n += g.Size
	}
	return n
}

// DockerReclaimable は docker 自身が「回収できる」と申告した量の合計 (全群)。
// 🚨 古さは見ていないので、候補 (Items) より広い範囲を指す。
func (r Report) DockerReclaimable() int64 {
	var n int64
	for _, g := range r.Groups {
		n += g.Reclaimable
	}
	return n
}

// Candidates は候補の件数 (全群)。
func (r Report) Candidates() int {
	n := 0
	for _, g := range r.Groups {
		n += len(g.Items)
	}
	return n
}

// Options は走査の差し替え口。zero value は本番 (実 docker / 実時刻)。
type Options struct {
	Run      runner.Runner
	Now      func() time.Time
	OldAfter time.Duration
	// LookPath は docker CLI の探索 (既定 exec.LookPath)。AppExists は Docker Desktop の
	// 実体があるかどうか (既定 /Applications/Docker.app)。
	LookPath   func(string) (string, error)
	AppExists  func() bool
	SkipVolume bool // ボリュームの作成日を引く 2 本目の呼び出しを飛ばす (テスト用)
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o Options) oldAfter() time.Duration {
	if o.OldAfter > 0 {
		return o.OldAfter
	}
	return DefaultOldAfter
}

// Scan は 1 回診断する。削除も停止も一切しない。
func Scan(ctx context.Context, o Options) Report {
	rep := Report{OldAfter: o.oldAfter(), ScannedAt: o.now()}
	look := o.LookPath
	if look == nil {
		look = lookPath
	}
	appExists := o.AppExists
	if appExists == nil {
		appExists = defaultAppExists
	}
	_, lookErr := look("docker")
	app := appExists()
	if lookErr != nil && !app {
		return rep // Docker Desktop を使っていない環境。セクションごと出さない
	}
	rep.Installed = true
	if lookErr != nil {
		// 🚨 「app はあるが CLI が無い」を候補 0 件にしない (見えていないだけ)
		rep.Unavailable = "Docker.app はありますが docker コマンドが PATH にありません"
		return rep
	}
	run := o.Run
	if run == nil {
		run = runner.Exec
	}
	// 集計は docker 自身に出させる (共有レイヤーの勘定を自前で作り直さない)
	sum, reason := systemDFSummary(ctx, run)
	if reason != "" {
		rep.Unavailable = reason
		return rep
	}
	stdout, stderr, rc, err := runner.WithTimeout(ctx, run, scanTimeout, "docker", "system", "df", "-v", "--format", "json")
	if err != nil {
		rep.Unavailable = "docker system df -v: " + err.Error()
		return rep
	}
	if rc != 0 {
		rep.Unavailable = fmt.Sprintf("docker system df -v が exit %d: %s", rc, firstLine(stderr))
		return rep
	}
	var df dfVerbose
	if err := json.Unmarshal([]byte(stdout), &df); err != nil {
		rep.Unavailable = "docker system df -v の出力を読めません: " + err.Error()
		return rep
	}
	old := o.oldAfter()
	now := rep.ScannedAt
	rep.Groups = []Group{
		containerGroup(df.Containers, now, old),
		imageGroup(df.Images, now, old),
		buildCacheGroup(df.BuildCache, now, old),
		volumeGroup(ctx, o, run, df.Volumes, now, old, &rep),
	}
	for i := range rep.Groups {
		if t, ok := sum[rep.Groups[i].Kind]; ok {
			rep.Groups[i].TotalSize, rep.Groups[i].Reclaimable = t.size, t.reclaimable
		}
	}
	rep.SystemPrune = fmt.Sprintf("docker system prune -a --filter until=%dh", hours(old))
	return rep
}

// summaryTypes は `docker system df` の Type 列と Kind の対応 (docker の語彙が正本)。
var summaryTypes = map[string]Kind{
	"Images": KindImages, "Containers": KindContainers,
	"Local Volumes": KindVolumes, "Build Cache": KindBuildCache,
}

type dfTotal struct{ size, reclaimable int64 }

// systemDFSummary は docker 自身の集計を引く。出力は 1 行 1 JSON (JSON Lines)。
func systemDFSummary(ctx context.Context, run runner.Runner) (map[Kind]dfTotal, string) {
	stdout, stderr, rc, err := runner.WithTimeout(ctx, run, scanTimeout, "docker", "system", "df", "--format", "json")
	if err != nil {
		return nil, "docker system df: " + err.Error()
	}
	if rc != 0 {
		return nil, fmt.Sprintf("docker system df が exit %d: %s", rc, firstLine(stderr))
	}
	out := map[Kind]dfTotal{}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row struct {
			Type, Size, Reclaimable string
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, "docker system df の出力を読めません: " + err.Error()
		}
		if k, ok := summaryTypes[row.Type]; ok {
			out[k] = dfTotal{size: parseSize(row.Size), reclaimable: parseSize(row.Reclaimable)}
		}
	}
	if len(out) == 0 {
		// 空を「合計 0」に畳まない (書式が変わったのに気づけなくなる)
		return nil, "docker system df の出力に既知の種別が 1 つもありません"
	}
	return out, ""
}

// --- docker system df -v の JSON (すべて文字列で来る) ---

type dfVerbose struct {
	Images     []dfImage      `json:"Images"`
	Containers []dfContainer  `json:"Containers"`
	Volumes    []dfVolume     `json:"Volumes"`
	BuildCache []dfBuildCache `json:"BuildCache"`
}

type dfImage struct {
	Containers string `json:"Containers"`
	CreatedAt  string `json:"CreatedAt"`
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Size       string `json:"Size"`
	Tag        string `json:"Tag"`
	UniqueSize string `json:"UniqueSize"`
}

type dfContainer struct {
	CreatedAt string `json:"CreatedAt"`
	ID        string `json:"ID"`
	Image     string `json:"Image"`
	Names     string `json:"Names"`
	Size      string `json:"Size"`
	State     string `json:"State"`
	Status    string `json:"Status"`
}

type dfVolume struct {
	Links string `json:"Links"`
	Name  string `json:"Name"`
	Size  string `json:"Size"`
}

type dfBuildCache struct {
	CacheType   string `json:"CacheType"`
	CreatedAt   string `json:"CreatedAt"`
	Description string `json:"Description"`
	ID          string `json:"ID"`
	InUse       string `json:"InUse"`
	LastUsedAt  string `json:"LastUsedAt"`
	Size        string `json:"Size"`
}

// --- 群ごとの判定 ---

// stoppedStates は「止まっている」と断定してよいコンテナの状態。
//
// 🚨 **allowlist にする** (「running でなければ停止」にしない)。docker が状態を増やしたとき、
// 未知の状態を停止側に倒すと「動いているものを消せ」と案内しうる。未知は候補にせず、
// 件数を注記に出す (黙って落とすと、提示コマンドが消す範囲との差が見えない)。
var stoppedStates = map[string]bool{"exited": true, "dead": true, "created": true}

// activeStates は「動いている」と断定してよい状態。どちらの allowlist にも無いものが未知。
var activeStates = map[string]bool{"running": true, "paused": true, "restarting": true, "removing": true}

func containerGroup(cs []dfContainer, now time.Time, old time.Duration) Group {
	g := Group{Kind: KindContainers, Label: "停止したコンテナ", Total: len(cs),
		Command: fmt.Sprintf("docker container prune --filter until=%dh", hours(old)),
		Notes: []string{
			humanDur(old) + "以上前に作られた停止コンテナだけを数えています (docker が持つのは作成日で、停止した日ではありません)",
			"サイズは書き込みレイヤーの分だけです (元イメージは「未使用イメージ」側)",
		}}
	unknown := 0
	for _, c := range cs {
		size := parseSize(c.Size)
		state := strings.ToLower(c.State)
		if !stoppedStates[state] {
			if !activeStates[state] {
				unknown++
			}
			continue
		}
		age, ok := age(now, c.CreatedAt)
		if !ok || age < old {
			continue
		}
		g.Items = append(g.Items, Item{
			Name: firstName(c.Names, c.ID), Detail: c.Status + " / " + c.Image,
			Size: size, SizeText: c.Size, Age: age, AgeKnown: true,
			Command: "docker rm " + firstName(c.Names, c.ID),
		})
		g.Size += size
	}
	if unknown > 0 {
		g.Notes = append(g.Notes, fmt.Sprintf(
			"🚨 このツールが知らない状態のコンテナが %d 件あります。候補には数えていませんが、docker が停止扱いなら上のコマンドは消します", unknown))
	}
	sortItems(g.Items)
	return g
}

func imageGroup(is []dfImage, now time.Time, old time.Duration) Group {
	g := Group{Kind: KindImages, Label: "使われていないイメージ", Total: len(is),
		Command: fmt.Sprintf("docker image prune -a --filter until=%dh", hours(old)),
		Notes: []string{
			"どのコンテナからも参照されていないイメージだけを数えています",
			"サイズは他イメージと共有していないレイヤー (UniqueSize) の分です",
			"候補の合計は見積もりです (共有レイヤーがあるため、docker の回収可能量と一致しません)",
		}}
	for _, im := range is {
		size := parseSize(im.UniqueSize)
		sizeText := im.UniqueSize
		if size == 0 && im.UniqueSize == "" {
			size, sizeText = parseSize(im.Size), im.Size
		}
		if im.Containers != "0" {
			continue
		}
		age, ok := age(now, im.CreatedAt)
		if !ok || age < old {
			continue
		}
		name := im.Repository + ":" + im.Tag
		if im.Repository == "" || im.Repository == "<none>" {
			name = shortID(im.ID)
		}
		g.Items = append(g.Items, Item{
			Name: name, Detail: shortID(im.ID), Size: size, SizeText: sizeText,
			Age: age, AgeKnown: true, Command: "docker rmi " + shortID(im.ID),
		})
		g.Size += size
	}
	sortItems(g.Items)
	return g
}

func buildCacheGroup(bs []dfBuildCache, now time.Time, old time.Duration) Group {
	g := Group{Kind: KindBuildCache, Label: "ビルドキャッシュ", Total: len(bs),
		Command: fmt.Sprintf("docker builder prune --filter unused-for=%dh", hours(old)),
		Notes: []string{
			humanDur(old) + "以上使われていないレイヤーだけを数えています",
			"消すと次のビルドがキャッシュ無しになります (壊れはしません)",
			"候補の合計は見積もりです (レイヤーを共有するため、docker の回収可能量と一致しません)",
		}}
	for _, b := range bs {
		size := parseSize(b.Size)
		if strings.EqualFold(b.InUse, "true") {
			continue
		}
		stamp := b.LastUsedAt
		if strings.TrimSpace(stamp) == "" {
			stamp = b.CreatedAt // 一度も使われていないものは作成日で見る
		}
		age, ok := age(now, stamp)
		if !ok || age < old {
			continue
		}
		g.Items = append(g.Items, Item{
			Name: b.ID, Detail: b.Description, Size: size, SizeText: b.Size,
			Age: age, AgeKnown: true,
		})
		g.Size += size
	}
	sortItems(g.Items)
	return g
}

// volumeGroup は「参照するコンテナが無いボリューム」を挙げる。
//
// 🚨 **「最後にマウントされた日」は docker が記録していない** (API にも CLI にも無い。
// Mountpoint は Linux VM の中なので mtime も見られない)。ここで見ているのは
// 「今どのコンテナからも参照されていない (Links=0)」と「作成日」の 2 つだけで、
// 「長い間マウントされていない」の近似でしかない。文言でもそう書く。
func volumeGroup(ctx context.Context, o Options, run runner.Runner, vs []dfVolume, now time.Time, old time.Duration, rep *Report) Group {
	g := Group{Kind: KindVolumes, Label: "参照されていないボリューム", Total: len(vs), Confirm: true,
		Notes: []string{
			"最後にマウントされた日時は Docker が記録していないため、「今どのコンテナからも参照されていない」+ 作成日で判定しています",
			"🚨 中身はデータです。docker volume prune -a は未使用ボリュームを全部消し、戻せません",
		}}
	var unused []dfVolume
	for _, v := range vs {
		if v.Links == "0" {
			unused = append(unused, v)
		}
	}
	created := map[string]time.Time{}
	if len(unused) > 0 && !o.SkipVolume {
		names := make([]string, 0, len(unused))
		for _, v := range unused {
			if !safeVolumeName(v.Name) {
				rep.Dropped++
				continue
			}
			names = append(names, v.Name)
		}
		if len(names) > 0 {
			created = volumeCreatedAt(ctx, run, names, &g)
		}
	}
	for _, v := range unused {
		if !safeVolumeName(v.Name) {
			continue // 提示コマンドに載せられない名前 (Dropped で数えた)
		}
		size := parseSize(v.Size)
		it := Item{Name: v.Name, Size: size, SizeText: v.Size, Command: "docker volume rm " + v.Name}
		if c, ok := created[v.Name]; ok {
			it.Age, it.AgeKnown = now.Sub(c), true
			if it.Age < old {
				continue
			}
			it.Detail = "作成 " + c.Format("2006-01-02")
		} else {
			// 作成日が取れなかったものは落とさず「不明」で出す (0 件に畳まない)
			it.Detail = "作成日不明"
		}
		g.Items = append(g.Items, it)
		g.Size += size
	}
	sortItems(g.Items)
	return g
}

// volumeCreatedAt は名前ごとの作成日を引く。失敗しても走査全体は落とさず、注記に残す。
func volumeCreatedAt(ctx context.Context, run runner.Runner, names []string, g *Group) map[string]time.Time {
	out := map[string]time.Time{}
	// 🚨 `--` を置いて、名前がフラグとして読まれる余地を消す (名前は allowlist 済みだが、
	// 先頭が `-` の名前を docker が作れる可能性に賭けない)
	args := append([]string{"volume", "inspect", "--format", "json", "--"}, names...)
	stdout, stderr, rc, err := runner.WithTimeout(ctx, run, scanTimeout, "docker", args...)
	if err != nil || rc != 0 {
		reason := firstLine(stderr)
		if err != nil {
			reason = err.Error()
		}
		g.Notes = append(g.Notes, "🚨 作成日を取れませんでした (docker volume inspect: "+reason+")")
		return out
	}
	var vs []struct {
		Name      string `json:"Name"`
		CreatedAt string `json:"CreatedAt"`
	}
	if err := json.Unmarshal([]byte(stdout), &vs); err != nil {
		g.Notes = append(g.Notes, "🚨 作成日の出力を読めませんでした: "+err.Error())
		return out
	}
	for _, v := range vs {
		if t, err := time.Parse(time.RFC3339, v.CreatedAt); err == nil {
			out[v.Name] = t
		}
	}
	return out
}

// --- 小物 ---

func defaultAppExists() bool { return dirExists("/Applications/Docker.app") }

func sortItems(items []Item) {
	sort.SliceStable(items, func(a, b int) bool { return items[a].Size > items[b].Size })
}

func firstName(names, id string) string {
	if n := strings.TrimSpace(strings.Split(names, ",")[0]); n != "" {
		return n
	}
	return shortID(id)
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func hours(d time.Duration) int { return int(d / time.Hour) }

func humanDur(d time.Duration) string {
	if days := int(d / (24 * time.Hour)); days > 0 {
		return fmt.Sprintf("%d 日", days)
	}
	return fmt.Sprintf("%d 時間", hours(d))
}

// age は docker の時刻表記から経過時間を返す。読めなければ ok=false (推測しない)。
func age(now time.Time, stamp string) (time.Duration, bool) {
	t, ok := parseDockerTime(stamp)
	if !ok {
		return 0, false
	}
	// 🚨 負の経過 (未来の作成日 / 時計の巻き戻し) を 0 に丸めない。丸めても丸めなくても
	// `age < OldAfter` で候補から外れるだけなので、丸める分岐は観測できない死んだ防御になる
	// (実測 2026-09-04: 丸めを外す変異を当ててもテストは緑のままだった)
	return now.Sub(t), true
}

// parseDockerTime は `docker system df -v` が出す 2 つの形を読む。
//
//	"2026-08-14 04:34:32 +0900 JST"              (Images / Containers)
//	"2025-11-07 02:05:40.521062297 +0000 UTC"    (BuildCache)
func parseDockerTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02 15:04:05.999999999 -0700 MST", "2006-01-02 15:04:05 -0700 MST", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// safeVolumeName は docker のボリューム名として受け入れてよい形か。
// docker 自身の制限 ([a-zA-Z0-9][a-zA-Z0-9_.-]*) をそのまま使う。
// 落とした名前は提示コマンドに載せない (svc / brew と同じ規律)。
func safeVolumeName(s string) bool {
	if s == "" || len(s) > 255 {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case i > 0 && (r == '_' || r == '.' || r == '-'):
		default:
			return false
		}
	}
	return true
}
