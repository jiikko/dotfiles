package issues

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ファイル名とパスから issue のメタデータを組み立てる層。
//
// 仕様の一次情報は docs/issues-viewer-spec.md (repo 側を寄せるときの契約)。ここでは
// 「なぜこの読み方なのか」を実測に紐づけて残す。
//
// 状態 (Status) の正本はパス (状態ディレクトリ) で、ファイル名には状態を載せない。
// 実測 (6 repo / 405 ファイル) で判明した、ファイル名に状態を載せられない理由:
//
//   - ファイル名 position 2 は repo ごとに 3 系統の語彙で埋まっている: 変更種別 19 語 /
//     サブシステム名 (swift-smbee の auth・dfs、SnapTrim の waveform 等) / トークンなし。
//     状態語を置くと「カテゴリか状態か」を判定できない。状態語と同綴りのスラッグが実在する
//     (119-open-with-app-context-menu.md は done/ にあるのに名前が open)
//   - 規約は retroactive に適用されない (dropbox の変更種別トークン遵守率は番号帯 000-024 で
//     6/23、175-199 で 6/6)。既存 405 ファイルは状態トークンを永久に持たないので、
//     「トークン不在」に既定値を当てるしかなく、未着手と付け忘れが区別できない
//   - basename を含む既存参照が 35 箇所ある (production のコメント 10 箇所を含む)。
//     状態遷移ごとに rename すると、そのたびに参照が腐る
//
// 逆に「ディレクトリを増やす」のも見送った (ongoing/ を作らない)。理由は
// docs/issues-viewer-spec.md の「ongoing をどう扱うか」に記録。

// Status は issue の状態。パス (状態ディレクトリ) から決まる。
type Status uint8

const (
	StatusOpen    Status = iota // issue ディレクトリ直下 = 未完了
	StatusPending               // 保留 (着手条件・trigger 待ち)
	StatusDone                  // 完了
	StatusUnknown               // 未知のサブディレクトリ配下 (状態へ写像しない)
)

// String は状態の表示名。
func (s Status) String() string {
	switch s {
	case StatusOpen:
		return "open"
	case StatusPending:
		return "pending"
	case StatusDone:
		return "done"
	case StatusUnknown:
		return "other"
	default:
		return "other"
	}
}

// Badge は一覧に出す 1 文字のバッジ。VS16 付き絵文字は使わない (幅解釈が層ごとに割れる)。
func (s Status) Badge() string {
	switch s {
	case StatusOpen:
		return "○"
	case StatusPending:
		return "⏸"
	case StatusDone:
		return "✓"
	case StatusUnknown:
		return "?"
	default:
		return "?"
	}
}

// 状態ディレクトリ名は閉じた allow-list にする。未知のサブディレクトリを状態へ写像しないのは、
// 状態でないサブディレクトリを持つ repo が実在するため (src/working/issues は 18 個の
// プロダクト名ディレクトリ、DualNoteApp/macOS/issues/mid-long-term は時間軸)。
// 黙って状態にすると「存在しない状態」がタブに並ぶ。
var statusDirs = map[string]Status{
	"done": StatusDone, "closed": StatusDone, "completed": StatusDone, "resolved": StatusDone,
	"pending": StatusPending, "hold": StatusPending, "on-hold": StatusPending,
}

// metaFiles は issue ではない付随ファイル。この repo の issues/README.md は自ら
// 「この README.md も issue ではない」と明記しており、実測でも README.md が 4 repo、
// INDEX.md が 2 repo、TEMPLATE.md が 1 repo にある。一覧に混ぜると件数もタブも汚れる。
var metaFiles = map[string]bool{"readme.md": true, "index.md": true, "template.md": true}

// Issue は issue ファイル 1 件。
//
// 同一性キーは Path。番号や basename は一意ではない (実測: 同一ディレクトリ内の番号重複が
// dropbox で 37 件・SnapTrim 4 件・DualNote 4 件。done へ移動後の番号再利用もある)。
type Issue struct {
	Path     string // 絶対パス (同一性キー)
	Dir      string // 属する issue ディレクトリ
	Rel      string // Dir からの相対パス (表示・参照用)
	Number   string // "028" (ゼロ埋めのまま保持。"" = 番号なし)
	Category string // ファイル名から取れたカテゴリトークン ("" = 無し)
	Slug     string // カテゴリを除いた説明部分 (タイトル未読時の代替表示)
	Status   Status
	Group    string // StatusUnknown のときの出自サブディレクトリ名

	// 以下は本文を読むまで空 (LoadMeta で埋まる)。起動パスで全件読まないのは、
	// ファイル名のみの走査に対して 26〜58 倍のコストになる実測があるため
	// (dropbox 229 ファイルで 0.23ms → 13.6ms。glogx の起動は Bench の監視対象)。
	Title    string // 本文の H1
	Declared string // front matter の status: の生値 ("" = 宣言なし)
	Checked  int    // チェック済みチェックボックス数
	Boxes    int    // チェックボックス総数 (0 = チェックボックスを使っていない)
	loaded   bool
}

var (
	// NNN-... 形式 (実測 405/405 が 3 桁ゼロ埋めだが、桁数は縛らない)
	numberedRe = regexp.MustCompile(`^(\d{2,})-(.*)$`)
	// CATEGORY-NNN-... 形式 (SnapTrim / DualNote に計 25 件実在する第 2 の ID スキーム)
	upperIDRe = regexp.MustCompile(`^([A-Z][A-Z0-9]*)-(\d+)-(.*)$`)
	// front matter の status: 行
	frontStatusRe = regexp.MustCompile(`(?i)^status\s*:\s*(.+?)\s*$`)
	// チェックボックス
	checkboxRe = regexp.MustCompile(`^\s*[-*+]\s+\[([ xX])\]`)
	// 本文の H1
	h1Re = regexp.MustCompile(`^#\s+(.+?)\s*$`)
)

// Scan は issue ディレクトリ群を走査して Issue を返す。本文は読まない (ファイル名とパスだけ)。
// warnings には viewer が表示すべき異常 (同じ basename の重複など) を入れる。
func Scan(dirs []string) (issues []*Issue, warnings []string) {
	issues = make([]*Issue, 0, 64)
	for _, dir := range dirs {
		issues = append(issues, scanDir(dir)...)
	}
	sortIssues(issues)
	return issues, conflicts(issues)
}

// scanDir は 1 つの issue ディレクトリを走査する (直下 + サブディレクトリ 1 段)。
// さらに深い階層を掘らないのは、実測でどの repo も「直下 + 状態ディレクトリ 1 段」しか
// 使っていないため。
func scanDir(dir string) []*Issue {
	out := make([]*Issue, 0, 32)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() {
			if skipDirs[e.Name()] {
				continue
			}
			subEntries, err := os.ReadDir(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			status, known := statusDirs[strings.ToLower(e.Name())]
			for _, se := range subEntries {
				if se.IsDir() || !isMarkdown(se.Name()) || metaFiles[strings.ToLower(se.Name())] {
					continue
				}
				iss := newIssue(dir, filepath.Join(e.Name(), se.Name()))
				if known {
					iss.Status = status
				} else {
					iss.Status, iss.Group = StatusUnknown, e.Name()
				}
				out = append(out, iss)
			}
			continue
		}
		if !isMarkdown(e.Name()) || metaFiles[strings.ToLower(e.Name())] {
			continue
		}
		out = append(out, newIssue(dir, e.Name()))
	}
	return out
}

// newIssue はファイル名から番号・カテゴリ・スラッグを取り出す。
func newIssue(dir, rel string) *Issue {
	iss := &Issue{Path: filepath.Join(dir, rel), Dir: dir, Rel: rel, Status: StatusOpen}
	name := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	switch {
	case numberedRe.MatchString(name):
		m := numberedRe.FindStringSubmatch(name)
		iss.Number = m[1]
		iss.Category, iss.Slug = splitCategory(m[2])
	case upperIDRe.MatchString(name):
		m := upperIDRe.FindStringSubmatch(name)
		iss.Category = strings.ToLower(m[1])
		iss.Number = m[2]
		iss.Slug = m[3]
	default:
		// 番号を持たないファイル (README.md / 素のスラッグ) はカテゴリを取らない。
		// 先頭語をカテゴリにすると "architecture" "resource" のような 1 件だけのタブが
		// 量産される (実測: SnapTrim の done/ に素スラッグのファイルが 4 件ある)
		iss.Slug = name
	}
	return iss
}

// splitCategory は "refactor-glogx-box" を ("refactor", "glogx-box") に割る。
//
// この先頭トークンが「変更種別」なのか「サブシステム名」なのかは repo の運用しだいで、
// viewer は判定しない (実測で 3 系統が併存する)。見つかったトークンをそのままタブにする。
func splitCategory(rest string) (category, slug string) {
	if rest == "" {
		return "", ""
	}
	i := strings.IndexByte(rest, '-')
	if i <= 0 {
		return "", rest // 単一トークン = カテゴリではなくスラッグとして扱う
	}
	token := rest[:i]
	// 数字だけ・1 文字のトークンはカテゴリにしない (日付や連番の続きを拾わないため)
	if len(token) < 2 || isAllDigits(token) {
		return "", rest
	}
	return strings.ToLower(token), rest[i+1:]
}

func isAllDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// sortIssues は番号の降順 (新しい issue が上)、番号なしは末尾、同番号は Rel 昇順。
func sortIssues(issues []*Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]
		an, aok := numOf(a)
		bn, bok := numOf(b)
		switch {
		case aok && bok && an != bn:
			return an > bn
		case aok != bok:
			return aok // 番号ありを先に
		default:
			return a.Rel < b.Rel
		}
	})
}

func numOf(iss *Issue) (int, bool) {
	if iss.Number == "" {
		return 0, false
	}
	n, err := strconv.Atoi(iss.Number)
	if err != nil {
		return 0, false
	}
	return n, true
}

// conflicts は viewer が表示すべき異常を返す。
//
// 同じ basename が複数の状態ディレクトリに現れるのは、並行セッションの git mv と
// pathspec commit の組み合わせで実際に起きる二重化 (再現確認済み)。片方が古い内容のまま
// HEAD に残るので、conflict も error も出ないまま静かに内容が失われる。viewer が
// 「同じ issue が 2 箇所にある」と言えば、この静かな喪失に気づける。
func conflicts(issues []*Issue) []string {
	byName := make(map[string][]*Issue, len(issues))
	for _, iss := range issues {
		key := iss.Dir + "\x00" + filepath.Base(iss.Rel)
		byName[key] = append(byName[key], iss)
	}
	warns := make([]string, 0, 2)
	for _, group := range byName {
		if len(group) < 2 {
			continue
		}
		places := make([]string, 0, len(group))
		for _, iss := range group {
			places = append(places, iss.Rel)
		}
		sort.Strings(places)
		warns = append(warns, "同じファイル名が複数の状態ディレクトリにあります: "+strings.Join(places, " / "))
	}
	sort.Strings(warns)
	return warns
}

// Display は一覧に出すタイトル (本文の H1 があればそれ、無ければスラッグ)。
//
// H1 が issue 番号で始まる場合は番号を落とす: 一覧では番号を別の列に出しているので
// 「028  028 refactor: ...」と二重になる (実測でこの repo の H1 は番号始まりが多数)。
func (iss *Issue) Display() string {
	if iss.Title == "" {
		return strings.ReplaceAll(iss.Slug, "-", " ")
	}
	title := iss.Title
	if iss.Number != "" && strings.HasPrefix(title, iss.Number) {
		title = strings.TrimLeft(strings.TrimPrefix(title, iss.Number), " :-\t")
	}
	return title
}

// Progress はチェックボックスの生の事実 ("3/7")。チェックボックスが無ければ ""。
//
// ⚠️ ここから「着手中」を導出しない。実測でパスと真逆になる: done/ にあるのに 0/N の
// ファイルが 36 件 (dropbox 16 / DualNote 13 / SnapTrim 7)、逆に全チェック済みでも
// 本文が未完を明記している open ファイルがある。チェックボックスは「作業項目の進捗」
// ではなく「将来の実装計画」や「Phase 追跡」に使われていて、意味が repo・ファイルごとに違う。
func (iss *Issue) Progress() string {
	if iss.Boxes == 0 {
		return ""
	}
	return strconv.Itoa(iss.Checked) + "/" + strconv.Itoa(iss.Boxes)
}

// StatusLabel は状態の表示。front matter の status: 宣言がパスと食い違う場合は融合せず
// 両方出す (どちらかを黙って採用すると viewer が確信を持って嘘をつく)。
func (iss *Issue) StatusLabel() string {
	if iss.Declared == "" {
		return iss.Status.String()
	}
	if strings.EqualFold(iss.Declared, iss.Status.String()) {
		return iss.Status.String()
	}
	return iss.Status.String() + " ⚠ status:" + iss.Declared
}

// LoadMeta は本文から H1・front matter の status:・チェックボックス数を読む。
// 一覧の表示に必要な行だけ遅延で呼ぶ (Scan は本文を読まない)。二度目は何もしない。
func (iss *Issue) LoadMeta() error {
	if iss.loaded {
		return nil
	}
	f, err := os.Open(iss.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	iss.loaded = true
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 実測の最大 issue は 43KB。長い 1 行にも耐える
	inFront, firstLine := false, true
	for sc.Scan() {
		lineText := strings.TrimRight(sc.Text(), "\r")
		switch {
		case firstLine && strings.TrimSpace(lineText) == "---":
			inFront = true
		case inFront && (strings.TrimSpace(lineText) == "---" || strings.TrimSpace(lineText) == "..."):
			inFront = false
		case inFront:
			if m := frontStatusRe.FindStringSubmatch(strings.TrimSpace(lineText)); m != nil {
				iss.Declared = strings.Trim(m[1], `"'`)
			}
		default:
			if iss.Title == "" {
				if m := h1Re.FindStringSubmatch(lineText); m != nil {
					iss.Title = dropVS16(m[1])
				}
			}
			if m := checkboxRe.FindStringSubmatch(lineText); m != nil {
				iss.Boxes++
				if m[1] != " " {
					iss.Checked++
				}
			}
		}
		firstLine = false
	}
	return sc.Err()
}

// ReadBody は本文を読んで整形用の Body を作る (issue を開いたときだけ呼ぶ)。
func (iss *Issue) ReadBody() (*Body, error) {
	b, err := os.ReadFile(iss.Path)
	if err != nil {
		return nil, err
	}
	return NewBody(string(b)), nil
}

// NextNumber は次に採番すべき番号 (最大番号 + 1) をゼロ埋めで返す。
//
// なぜ viewer がこれを持つか: issues/README.md の採番コマンドは対象ディレクトリを固定列挙
// しているため、状態ディレクトリが増えると最大番号を見落として番号を再利用する。viewer は
// 全ディレクトリを走査済みなので、ここが「全部を見て」計算できる唯一の場所。番号重複は
// 他 repo で実際に 37 件発生している (dotfiles だけが重複ゼロ)。
//
// 桁数は観測した最大桁に合わせる (実測 405/405 が 3 桁ゼロ埋め)。番号付きが 1 つも無ければ
// 3 桁の "001"。
func NextNumber(list []*Issue) string {
	maxNum, width := 0, 3
	for _, iss := range list {
		n, ok := numOf(iss)
		if !ok {
			continue
		}
		maxNum = max(maxNum, n)
		width = max(width, len(iss.Number))
	}
	return fmt.Sprintf("%0*d", width, maxNum+1)
}

// Reference は他所へ貼るための 1 行参照。番号は rename も move も生き残る唯一安定した参照
// 形式 (実測: repo 内 59 箇所・commit message 25 件がこの形) なので先頭に置き、パスは補助
// として括弧に入れる。root が空ならパスはそのまま出す。
func (iss *Issue) Reference(root string) string {
	var b strings.Builder
	if iss.Number != "" {
		b.WriteString("issue ")
		b.WriteString(iss.Number)
		b.WriteString(" ")
	}
	b.WriteString(iss.Display())
	b.WriteString(" (")
	b.WriteString(iss.PathFrom(root))
	b.WriteString(")")
	return b.String()
}

// PathFrom は root からの相対パス (取れなければ絶対パス)。issues ディレクトリが複数ある
// repo では Rel だけでは出自が分からないため、貼り付け用には repo 相対を使う。
func (iss *Issue) PathFrom(root string) string {
	if root == "" {
		return iss.Path
	}
	rel, err := filepath.Rel(root, iss.Path)
	if err != nil {
		return iss.Path
	}
	return rel
}

// Tab はカテゴリタブ 1 個。
type Tab struct {
	Name  string // カテゴリトークン ("other" = カテゴリ無し + 少数派の寄せ先)
	Count int
}

// OtherTab はカテゴリを持たないファイルと、少数派カテゴリの寄せ先の名前。
const OtherTab = "other"

// Tabs はカテゴリタブを件数の多い順に返す (同数は名前順)。issues に含まれるトークンから
// 組み立てる: カテゴリ語彙は repo ごとに違い (実測 19 語 + サブシステム名体系)、
// ハードコードすると別 repo で空タブと欠落が出る。
//
// minCount 未満のカテゴリは OtherTab に寄せる。1 件だけのトークンをすべてタブにすると、
// position 2 にコンポーネント名を入れる repo でタブが数十個に膨らむ。
func Tabs(issues []*Issue, minCount int) []Tab {
	count := make(map[string]int, 8)
	for _, iss := range issues {
		count[iss.Category]++
	}
	other := count[""]
	delete(count, "")
	tabs := make([]Tab, 0, len(count))
	for name, n := range count {
		if n < minCount {
			other += n
			continue
		}
		tabs = append(tabs, Tab{Name: name, Count: n})
	}
	sort.Slice(tabs, func(i, j int) bool {
		if tabs[i].Count != tabs[j].Count {
			return tabs[i].Count > tabs[j].Count
		}
		return tabs[i].Name < tabs[j].Name
	})
	if other > 0 {
		tabs = append(tabs, Tab{Name: OtherTab, Count: other})
	}
	return tabs
}

// Filter はタブと状態フィルタで絞り込む。tab が "" なら全カテゴリ。
// showDone=false のとき StatusDone を落とす (done は既定で伏せる: 実測で done が
// 全体の 8 割を占める repo があり、既定で混ぜると open が埋もれる)。
func Filter(issues []*Issue, tab string, showDone bool) []*Issue {
	// OtherTab は「自前のタブを持たないカテゴリ」の寄せ先。タブ集合を 1 回だけ作って
	// 判定に使う (要素ごとに Tabs を呼ぶと件数の 2 乗になる)
	own := make(map[string]bool, 8)
	if tab == OtherTab {
		for _, t := range Tabs(issues, TabMinCount) {
			own[t.Name] = true
		}
	}
	out := make([]*Issue, 0, len(issues))
	for _, iss := range issues {
		if !showDone && iss.Status == StatusDone {
			continue
		}
		switch {
		case tab == "":
		case tab == OtherTab:
			if iss.Category != "" && own[iss.Category] {
				continue
			}
		case iss.Category != tab:
			continue
		}
		out = append(out, iss)
	}
	return out
}

// TabMinCount はタブとして独立させるのに必要な最小件数 (これ未満は OtherTab へ寄せる)。
const TabMinCount = 2
