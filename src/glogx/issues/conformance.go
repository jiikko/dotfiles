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
	ViolationH1Type                               // H1 の type にファイル名の type が無い
	ViolationNoCreated                            // `起票日: YYYY-MM-DD` の行が無い
	ViolationDeadlineForm                         // 期限が箇条書き・チェックボックス・太字で書かれている (hook が拾わない)
	ViolationHumanNoDeadline                      // human の issue に期限が無い
)

// Violation は規約から外れている点 1 つ。Label は画面の 1 行に並べる短い名前、Detail はクリップボードへ
// 入れる修正指示の 1 項目。
type Violation struct {
	Kind   ViolationKind
	Label  string
	Detail string
}

var (
	// H1 の規約の形。type は語彙で縛らない (spec §2: repo ごとに語彙が違う)。番号と type の食い違いは別に見る
	conformH1Re = regexp.MustCompile(`^(\d+) \(([^()]+)\)[:：] \S`)
	createdRe   = regexp.MustCompile(`^起票日: \d{4}-\d{2}-\d{2}`)
	// 期限は hook (human-tasks-due.sh) が拾う形に合わせる (`^期限[:：]`、コロンの後の空白は任意)。
	// hook より厳しくすると、hook が読める期限に「期限なし」の警告が出る
	deadlineRe = regexp.MustCompile(`^期限[:：]\s*\d{4}-\d{2}-\d{2}`)
	// hook が拾えない書き方の期限 (行頭が箇条書き・チェックボックス・太字)。日付を伴う行だけを見る (規約を説明する
	// 文の中の「`- 期限:` にしない」のような言及は日付を持たないので当たらない)
	badDeadlineRe = regexp.MustCompile(`^\s*(?:[-*+]\s+(?:\[[ xX]\]\s+)?\**|\*\*)期限\**\s*[:：]\**\s*\d{4}-\d{2}-\d{2}`)
	// 先頭に数字を持つが `NNN-` で区切っていないファイル名 (051.md / 051_task_x.md) の数字。番号か年・日付かは
	// 名前から決まらない (2026_roadmap.md) ので番号なしとして扱い、指示文で残すかの判断を促すだけに使う
	leadNumberRe = regexp.MustCompile(`^(\d{2,6})(?:[_. ]|$)`)
)

// bodyFacts は規約の判定に要る本文の事実。本文はファイルを開いたときに 1 回だけ読むので、描画のたびに
// 走査し直さないよう Body に持つ (Body.facts)。
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
		if i == 0 {
			if isFrontMatterOpen(line) {
				inFront = true
				continue
			}
			line = strings.TrimPrefix(line, "\ufeff") // BOM。LoadMeta (PlainLine) と同じく H1 の判定の前に落とす
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

// fileNumber はファイル名の採番の番号 ("" = 無い)。7 桁以上の先頭の数字は日付 (20260101-notes.md) で
// 採番の番号ではない (NextNumber と同じ扱い)。`NNN-` で区切っていない先頭の数字 (051.md) は lead に返す
// (番号とは決めない。leadNumberRe の doc)。
func fileNumber(iss *Issue) (number, lead string) {
	if len(iss.Number) <= maxNumberDigits {
		number = iss.Number
	}
	if number == "" {
		if m := leadNumberRe.FindStringSubmatch(iss.Slug); m != nil {
			lead = m[1]
		}
	}
	return number, lead
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
		number, _ := fileNumber(iss)
		switch {
		case number == "":
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
			if number != "" && m[1] != number {
				add(ViolationH1Number, "H1 の番号",
					"H1 の番号 "+m[1]+" がファイル名の番号 "+number+" と違います")
			}
			if iss.Category != "" && !typeListed(m[2], iss.Category) {
				add(ViolationH1Type, "H1 の type",
					"H1 の type ("+m[2]+") にファイル名の type ("+iss.Category+") がありません")
			}
		}
	}
	if !f.created {
		add(ViolationNoCreated, "起票日なし",
			"`起票日: YYYY-MM-DD` の形の行がありません (行頭に書き、コロンは半角で後ろに空白 1 つ)")
	}
	if f.badDeadline {
		add(ViolationDeadlineForm, "期限の書き方",
			"期限が箇条書き・チェックボックス・太字で書かれています。hook (human-tasks-due.sh) が拾えないので、行頭に `期限: YYYY-MM-DD` と書く")
	}
	if iss.Category == "human" && !f.deadline && !f.badDeadline {
		add(ViolationHumanNoDeadline, "human に期限なし",
			"human の issue には行頭の `期限: YYYY-MM-DD` が要ります")
	}
	return out
}

// typeListed は H1 の括弧の中の type (`perf/design` のような複合も含む) にファイル名の type が入っているか。
func typeListed(h1Type, category string) bool {
	for _, t := range strings.Split(h1Type, "/") {
		if strings.EqualFold(strings.TrimSpace(t), category) {
			return true
		}
	}
	return false
}

var (
	// 規約の形の H1 の、タイトルより前 (conformH1Re と同じ形)
	conformPrefixRe = regexp.MustCompile(`^\d+ \([^()]+\)[:：] `)
	// H1 の先頭の番号。後ろが空白かコロンのときだけ番号の候補にする (`# 3D ビュー` / `# 2026年の計画`)
	h1LeadNumberRe = regexp.MustCompile(`^(\d+)(?:\s+|\s*[:：]\s*)`)
	// 番号の後ろの type の書き方。`(task):` / `(task)` / 括弧の無い旧い形の `feat:`
	h1ParenTypeRe = regexp.MustCompile(`^\([^()]*\)\s*[:：]?\s*`)
	h1WordTypeRe  = regexp.MustCompile(`^([A-Za-z][A-Za-z/-]*)\s*[:：]\s*`)
)

// bareTitle は H1 から番号と type を剥がしたタイトルの文言 (H1 を規約の形へ組み直すときに使う)。
//
// 🚨 先頭の数字を剥がすのは、規約の形 (conformH1Re) のときか、ファイル名の番号と同じ数のときだけ。
// `# 3 つの案の比較` / `# 2026 年の振り返り` / `# 10 Tips: x` の数字はタイトルの一部で、剥がすと指示文に
// 「タイトルの文言は変えない」と並んで壊れたタイトルが載る。括弧の type と `word:` も、番号を剥がした後か、
// word がファイル名の type と同じときだけ type と読む (`# Note: …` の語を剥がさない)。
func bareTitle(h1, number, category string) string {
	if loc := conformPrefixRe.FindStringIndex(h1); loc != nil {
		return strings.TrimSpace(h1[loc[1]:])
	}
	t := h1
	numbered := false
	if m := h1LeadNumberRe.FindStringSubmatch(t); m != nil && number != "" && sameNumber(m[1], number) {
		t, numbered = t[len(m[0]):], true
	}
	if loc := h1ParenTypeRe.FindStringIndex(t); loc != nil && numbered {
		t = t[loc[1]:]
	} else if m := h1WordTypeRe.FindStringSubmatch(t); m != nil && (numbered || strings.EqualFold(m[1], category)) {
		t = t[len(m[0]):]
	}
	return strings.TrimSpace(t)
}

// sameNumber は 2 つの番号が同じ数か (ゼロ埋めの違いを無視する。`# 51 …` と 051-*.md)。
func sameNumber(a, b string) bool {
	return strings.TrimLeft(a, "0") == strings.TrimLeft(b, "0")
}

// FixPrompt は iss の規約外の点 (Conformance) を直すための、Claude Code へそのまま貼れる指示文を組む。
// 外れていなければ ""。root は repo の root (パスを repo 相対で書くため)、next は次に採番すべき番号
// (NextNumber。番号を振り直すときに使う)。
//
// 🚨 指示文にシェルのコマンドを埋め込まない。パスやファイル名を埋め込むとクォートの規則を背負い、空白や `'` を
// 含む名前で壊れたコマンドを Claude に打たせる。何を探すか・どこを書き換えるかを文で書き、打ち方は Claude に任せる。
// 制御文字・改行を含む名前を断るのは呼び出し側 (viewer のコピーの口)。
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
	number, lead := fileNumber(iss)
	title := bareTitle(b.facts.h1, number, iss.Category)
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
			filepath.Join(issueDir, next+"-<type>-<slug>.md") + " へ git mv し、H1 を「# " + next + " (<type>): " + title +
			"」にして、H1 の次に `起票日: YYYY-MM-DD` を書く。<type> は issue-rules の語彙から内容で選ぶ\n")
		w.WriteString("2. issue でない資料 (計画・メモ・調査) なら issues/ の外 (docs/ など) へ移し、関係する issue から参照する\n")
		if lead != "" {
			w.WriteString("ファイル名の先頭の " + lead + " が採番の番号なら、1 で " + next + " の代わりに " + lead +
				" を使ってよい。ただし年・日付・版の数字なら使わない。同じ番号の issue が既にあるなら使わない (issues/ 全体で " +
				lead + "- で始まるファイルを探す)\n")
		}
	case has[ViolationNoType]:
		renamed = true
		rest := strings.TrimPrefix(strings.TrimSuffix(base, filepath.Ext(base)), iss.Number+"-")
		w.WriteString("- 番号はそのままで " + filepath.Join(issueDir, number+"-<type>-"+rest+".md") +
			" へ git mv する。<type> は issue-rules の語彙から内容で選ぶ\n")
	}
	if !has[ViolationNoNumber] && (has[ViolationNoH1] || has[ViolationH1Form] || has[ViolationH1Number] || has[ViolationH1Type]) {
		typ := iss.Category
		if typ == "" {
			typ = "<type>"
		}
		w.WriteString("- H1 を「# " + number + " (" + typ + "): " + title + "」の形にする (タイトルの文言は変えない)\n")
	}
	if has[ViolationNoCreated] && !has[ViolationNoNumber] {
		w.WriteString("- H1 の次に `起票日: YYYY-MM-DD` を書く (日付は git log でこのファイルが足された日。取れなければ今日)\n")
	}
	if has[ViolationDeadlineForm] || has[ViolationHumanNoDeadline] {
		w.WriteString("- 期限は行頭に `期限: YYYY-MM-DD` と書く (箇条書き・チェックボックス・太字にしない)\n")
	}
	if renamed {
		w.WriteString("- ファイルを動かしたら、repo 全体 (issues/ の外の docs・コード・CLAUDE.md も) から「" + base +
			"」を含む行を探し、このファイル (" + path + ") を指すリンクだけを新しいパスへ張り直す。" +
			"別のディレクトリにある同じ名前のファイルへのリンクは触らない\n")
	}
	return strings.TrimSuffix(w.String(), "\n")
}
