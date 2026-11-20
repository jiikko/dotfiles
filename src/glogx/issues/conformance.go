package issues

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jiikko/dotfiles/src/termsafe"
)

// ViolationKind は issue の規約 (~/dotfiles/_claude/issue-rules.md) から外れている点の種類。
type ViolationKind uint8

const (
	ViolationNoNumber        ViolationKind = iota // ファイル名に番号が無い (plan.md など)
	ViolationNoType                               // 番号の後に type のトークンが無い (050-foo.md)
	ViolationNoH1                                 // H1 が無い
	ViolationH1Form                               // H1 が `# NNN (<type>): タイトル` の形でない
	ViolationH1Number                             // H1 の番号がファイル名の番号と違う
	ViolationH1Type                               // H1 の type がファイル名の type と違う
	ViolationNoCreated                            // `起票日: YYYY-MM-DD` の行が無い
	ViolationDeadlineForm                         // 期限が箇条書き・太字で書かれている (hook が拾わない)
	ViolationHumanNoDeadline                      // human の issue に `期限:` が無い
)

// Violation は規約から外れている点 1 つ。Label は画面の 1 行に並べる短い名前、Detail はクリップボードへ
// 入れる修正指示の 1 項目。
type Violation struct {
	Kind   ViolationKind
	Label  string
	Detail string
}

var (
	// H1 の規約の形。type は語彙で縛らない (spec §2: repo ごとに語彙が違う)。`perf/design` のような
	// 複合も括弧の中なら受ける。番号と type の食い違いは別に見る
	conformH1Re = regexp.MustCompile(`^(\d+) \(([^()]+)\)[:：] \S`)
	createdRe   = regexp.MustCompile(`^起票日: \d{4}-\d{2}-\d{2}`)
	deadlineRe  = regexp.MustCompile(`^期限: \d{4}-\d{2}-\d{2}`)
	// hook (human-tasks-due.sh) が拾えない書き方の期限。日付を伴う行だけを見る (規約を説明する文の中の
	// 「`- 期限:` にしない」のような言及は日付を持たないので当たらない)
	badDeadlineRe = regexp.MustCompile(`^\s*(?:[-*+]\s+\**|\*\*)期限\**\s*[:：]\**\s*\d{4}-\d{2}-\d{2}`)
)

// bodyFacts は規約の判定に要る本文の事実。本文はファイルを開いたときに 1 回だけ読むので、描画のたびに
// 走査し直さないよう Body に持つ (Body.conformanceFacts)。
type bodyFacts struct {
	h1          string // 最初の H1 (front matter・コードフェンスの外)。"" = 無い
	created     bool
	deadline    bool
	badDeadline bool
}

func scanBodyFacts(src string) bodyFacts {
	var f bodyFacts
	inFence, inFront := false, false
	for i, line := range strings.Split(src, "\n") {
		line = strings.TrimRight(line, "\r")
		if i == 0 && isFrontMatterOpen(line) {
			inFront = true
			continue
		}
		if inFront {
			inFront = !isFrontMatterClose(line)
			continue
		}
		if fenceLine.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if f.h1 == "" {
			if m := h1Re.FindStringSubmatch(line); m != nil {
				f.h1 = termsafe.PlainLine(m[1])
			}
		}
		f.created = f.created || createdRe.MatchString(line)
		f.deadline = f.deadline || deadlineRe.MatchString(line)
		f.badDeadline = f.badDeadline || badDeadlineRe.MatchString(line)
	}
	return f
}

// Conformance は iss とその本文 b が issue の規約から外れている点を返す (外れていなければ nil)。
//
// 対象にしないもの:
//   - done の issue。規約より前に書かれたファイルが多い repo では、昔の issue を開くたびに警告が出る
//   - `CATEGORY-NNN-*.md` の番号の形。spec §2 が別の ID の形として認めている (ファイル名と H1 は見ない)
//
// type の語彙は見ない (spec §2「語彙は viewer 側で固定しない」)。番号の後にトークンがあるかだけを見る。
func (b *Body) Conformance(iss *Issue) []Violation {
	if b == nil || iss == nil || iss.Status == StatusDone {
		return nil
	}
	if !b.factsDone {
		b.facts, b.factsDone = scanBodyFacts(b.src), true
	}
	f := b.facts
	var out []Violation
	add := func(k ViolationKind, label, detail string) {
		out = append(out, Violation{Kind: k, Label: label, Detail: detail})
	}
	if iss.Prefix == "" {
		switch {
		case iss.Number == "":
			add(ViolationNoNumber, "ファイル名に番号なし",
				"ファイル名に番号がありません (規約は NNN-<type>-<slug>.md。番号の無い md は issue として扱われない)")
		case iss.Category == "":
			add(ViolationNoType, "ファイル名に type なし",
				"ファイル名の番号の後に type がありません (規約は NNN-<type>-<slug>.md)")
		}
		switch m := conformH1Re.FindStringSubmatch(f.h1); {
		case f.h1 == "":
			add(ViolationNoH1, "H1 なし", "H1 (`# NNN (<type>): タイトル`) がありません")
		case m == nil:
			add(ViolationH1Form, "H1 の形", "H1 が `# NNN (<type>): タイトル` の形ではありません: 「"+f.h1+"」")
		default:
			if iss.Number != "" && m[1] != iss.Number {
				add(ViolationH1Number, "H1 の番号",
					"H1 の番号 "+m[1]+" がファイル名の番号 "+iss.Number+" と違います")
			}
			if iss.Category != "" && !strings.EqualFold(m[2], iss.Category) {
				add(ViolationH1Type, "H1 の type",
					"H1 の type ("+m[2]+") がファイル名の type ("+iss.Category+") と違います")
			}
		}
	}
	if !f.created {
		add(ViolationNoCreated, "起票日なし", "`起票日: YYYY-MM-DD` の行がありません (行頭に半角コロンで書く)")
	}
	if f.badDeadline {
		add(ViolationDeadlineForm, "期限の書き方",
			"期限が箇条書きか太字で書かれています。hook (human-tasks-due.sh) が拾えないので、行頭に `期限: YYYY-MM-DD` と書く")
	}
	if iss.Category == "human" && !f.deadline && !f.badDeadline {
		add(ViolationHumanNoDeadline, "human に期限なし",
			"human の issue には `期限: YYYY-MM-DD` の行が要ります (行頭・半角コロン)")
	}
	return out
}

// h1TypeLeadRe は H1 の番号の後ろの type の書き方 (`(task):` / 括弧の無い旧い形の `feat:`)。
var h1TypeLeadRe = regexp.MustCompile(`^(?:\([^()]*\)|[A-Za-z][A-Za-z/-]*)\s*[:：]\s*`)

// bareTitle は H1 から番号と type を剥がしたタイトルの文言 (H1 を規約の形へ組み直すときに使う)。
func bareTitle(h1 string) string {
	t := strings.TrimLeft(h1, "0123456789")
	if t != h1 {
		t = h1TypeLeadRe.ReplaceAllString(strings.TrimLeft(t, " "), "")
	}
	return strings.TrimSpace(t)
}

// FixPrompt は iss の規約外の点 (Conformance) を直すための、Claude Code へそのまま貼れる指示文を組む。
// 外れていなければ ""。root は repo の root (パスを repo 相対で書くため)、next は次に採番すべき番号
// (NextNumber。番号を振り直すときに使う)。
func (b *Body) FixPrompt(iss *Issue, root, next string) string {
	vs := b.Conformance(iss)
	if len(vs) == 0 {
		return ""
	}
	path := iss.PathFrom(root)
	base := filepath.Base(iss.Rel)
	issueDir := filepath.Dir(path)
	has := map[ViolationKind]bool{}
	for _, v := range vs {
		has[v.Kind] = true
	}
	title := bareTitle(b.facts.h1)
	if title == "" {
		title = "<タイトル>"
	}

	var w strings.Builder
	w.WriteString(path + " が issue の規約 (~/dotfiles/_claude/issue-rules.md) に合っていません (glogx の issues viewer の警告)。\n")
	for _, v := range vs {
		w.WriteString("- " + v.Detail + "\n")
	}
	w.WriteString("\n直し方:\n")
	renamed := false
	switch {
	case has[ViolationNoNumber]:
		renamed = true
		w.WriteString("内容を読んで、どちらかを選ぶ。\n")
		w.WriteString("1. issue にする: 次の番号 " + next + " (viewer が数えた値。採番の前に git fetch して origin 側も数え直す) で " +
			filepath.Join(issueDir, next+"-<type>-<slug>.md") + " へ git mv し、H1 を `# " + next + " (<type>): " + title +
			"` にして、H1 の次に `起票日: YYYY-MM-DD` を書く。<type> は issue-rules の語彙から内容で選ぶ\n")
		w.WriteString("2. issue でない資料 (計画・メモ・調査) なら issues/ の外 (docs/ など) へ移し、関係する issue から参照する\n")
	case has[ViolationNoType]:
		renamed = true
		w.WriteString("- 番号はそのままで " + filepath.Join(issueDir, iss.Number+"-<type>-"+iss.Slug+".md") +
			" へ git mv する。<type> は issue-rules の語彙から内容で選ぶ\n")
	}
	if !has[ViolationNoNumber] && (has[ViolationNoH1] || has[ViolationH1Form] || has[ViolationH1Number] || has[ViolationH1Type]) {
		typ := iss.Category
		if typ == "" {
			typ = "<type>"
		}
		w.WriteString("- H1 を `# " + iss.Number + " (" + typ + "): " + title + "` の形にする (タイトルの文言は変えない)\n")
	}
	if has[ViolationNoCreated] && !has[ViolationNoNumber] {
		w.WriteString("- H1 の次に `起票日: YYYY-MM-DD` を書く (日付は `git log --diff-filter=A --format=%as -- " + path +
			"` の最も古い日付。取れなければ今日)\n")
	}
	if has[ViolationDeadlineForm] || has[ViolationHumanNoDeadline] {
		w.WriteString("- 期限は行頭に `期限: YYYY-MM-DD` と書く (箇条書き・太字にしない)\n")
	}
	if renamed {
		w.WriteString("- ファイルを動かしたら、このファイルを指すリンクを張り直す (`grep -rn '" + base + "' " + iss.Dir +
			"` で探す。issues/ の外からは番号で参照する)\n")
	}
	return strings.TrimSuffix(w.String(), "\n")
}
