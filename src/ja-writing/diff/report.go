package diff

import (
	"fmt"
	"slices"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/visibility"
)

// 群 AD: 人が読む出力 (本家の report と _fail_on_line)。report は diff() の dict (diffDocument の値か、本家の --json を pyjson.Loads で読んだ値) だけを読む。
// 節ごとの文は report_*.go。本家の detector_summary は check が使うので (diff の CLI からは届かない)、1077 で写す。

const (
	reportFirstLine  = "差分の検査を実行しました。"
	reportDisclaimer = "全文差分は変わった箇所を示すだけで、意味が同じだという証明ではありません。" +
		"この検査では、意味の保持と読者にとっての分かりやすさは判定していません。"
	// 種類か読者を指定したときに、断りの行のすぐあとに足す 1 行の後半
	reportContextTail = "候補の見つけ方は種類と読者によらず同じです。"
	// 事実や仕様を述べる種類 (countedTypes) を指定したときに、その後ろに足す文
	reportCountedTail = "この種類では、「場合がある」「ことがある」を消した言い切りと、断定に「必ず」などを足した書き換えも" +
		" --fail-on に数えます。"
)

// statusLine は _status_line (状態の行。変更の数は hunk の数)。最後の行 (above) では、硬化と勢いと新しい節の添え書きが「上の節」になる。
func statusLine(d *pyjson.Object, above bool) string {
	status := fieldStr(d, "status")
	if status == "no_change" {
		return "状態: 変更なし"
	}
	places := len(fieldList(fieldObj(d, "full_diff"), "hunks"))
	var line string
	switch {
	case status == "changed_without_candidates" && places == 0:
		return "状態: 変更はあるが、専用の規則に当たる見直し候補はない（変更 0か所。改行コード・BOM・末尾の改行だけが違う）"
	case status == "changed_without_candidates":
		line = fmt.Sprintf("状態: 変更はあるが、専用の規則に当たる見直し候補はない（変更 %dか所）", places)
	default:
		line = fmt.Sprintf("状態: 変更あり（%dか所）。見直し候補 %d件、うち意味が変わる恐れ %d件", places, fieldInt(d, "candidate_count"), fieldInt(d, "meaning_risk_count"))
	}
	position := "下"
	if above {
		position = "上"
	}
	if reviews := detectorReviewCount(d); reviews > 0 {
		line += fmt.Sprintf("（文体の硬化と勢いの見直し候補 %d件は%sの節）", reviews, position)
	}
	extra := 0
	for _, key := range newReviewSections {
		extra += fieldInt(fieldObj(d, key), "candidate_count")
	}
	if extra > 0 {
		label := "慎重表現・値・敬語"
		if fieldInt(fieldObj(d, "register"), "candidate_count") > 0 {
			label += "・文体の切り替え"
		}
		line += fmt.Sprintf("（%sの見直し候補 %d件は%sの節）", label, extra, position)
	}
	return line
}

func stringPtr(v pyjson.Value) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	return &s
}

// report は人が読むレポート。1 行目、状態の行、断り、意味が変わる恐れ、規則の見直し候補、足した人や担当、見出しの形、読点の増加、硬化、勢い、
// 慎重表現・値・敬語・文体の切り替え、全文差分、状態の行の順。末尾に改行は付けない。
func report(d *pyjson.Object) (string, error) {
	parts := []string{riskSection(d), ruleSection(d), addedActorSection(d), headingShapeSection(d), commaSection(d),
		detectorSection(d, "stiffness"), detectorSection(d, "tone")}
	for _, key := range newReviewSections {
		parts = append(parts, newReviewSection(d, key))
	}
	parts = append(parts, fullDiffSection(d))
	var body []string
	for _, part := range parts {
		if part != "" {
			body = append(body, part)
		}
	}
	head := []string{reportFirstLine, statusLine(d, false), reportDisclaimer}
	context := fieldObj(d, "context")
	docType, reader := field(context, "doc_type"), field(context, "reader")
	if docType != nil || reader != nil {
		label, err := visibility.ContextLabel(stringPtr(docType), stringPtr(reader))
		if err != nil {
			return "", err
		}
		line := label + reportContextTail
		if s, ok := docType.(string); ok && slices.Contains(countedTypes, s) {
			line += reportCountedTail
		}
		head = append(head, line)
	}
	if words := fieldObj(context, "words"); words != nil {
		// 語の一覧を使ったときだけ (種類と読者の行のあと)
		head = append(head, visibility.WordsLabel(visibility.WordsContextFromValue(words)))
	}
	return strings.Join(head, "\n") + "\n\n" + strings.Join(body, "\n\n") + "\n\n" + statusLine(d, true), nil
}

// failOnLine は _fail_on_line (--fail-on=meaning-risk に当たったときの最後の行。数えない種類 (変えない箇所) が混ざるときは、全件と数えた件数を書く)。
func failOnLine(total, counted int, risks []pyjson.Value) string {
	if total == counted {
		return fmt.Sprintf("終了コード1: --fail-on=meaning-risk の条件に当たりました（意味が変わる恐れ %d件）。", counted)
	}
	var labels strings.Builder
	for _, kl := range [][2]string{{"certainty", "・確度"}, {"state", "・状態の語"}, {"promise", "・約束"}, {"modality", "・許可の言い方"}, {"choice", "・選ぶ範囲"}, {"causality", "・因果"}} {
		if slices.ContainsFunc(risks, func(r pyjson.Value) bool { return fieldStr(asObj(r), "kind") == kl[0] }) {
			labels.WriteString(kl[1])
		}
	}
	return fmt.Sprintf("終了コード1: --fail-on=meaning-risk の条件に当たりました（意味が変わる恐れ %d件のうち、数値・否定・入れ替え・文の種類%s %d件）。", total, labels.String(), counted)
}
