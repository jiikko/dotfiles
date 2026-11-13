package visibility

import (
	"fmt"
	"strings"

	"ja-writing/internal/pyunicode"
)

// Version は本家の __version__ (写した本家のバージョン)。
const Version = "3.1.0"

// DocType は DOC_TYPES の 1 行 (値・名前・あわせて読む domains のファイル)。
type DocType struct {
	Value   string
	Name    string
	Domains []string
}

// ReaderLevel は READER_LEVELS の 1 行 (値・名前)。
type ReaderLevel struct{ Value, Name string }

// DocTypes は本家の DOC_TYPES (順も同じ。formal と promo、script は後から足されたもの)。読むだけで、書き換えて使う使い方は約束しない。
var DocTypes = []DocType{
	{"pr", "PR説明", []string{"business.md"}},
	{"article", "技術記事", []string{"tech.md"}},
	{"incident", "障害報告", []string{"business.md"}},
	{"proposal", "提案書", []string{"business.md"}},
	{"minutes", "議事録", []string{"business.md"}},
	{"manual", "マニュアル", []string{"tech.md", "business.md"}},
	{"slides", "スライド", []string{"tech.md", "business.md"}},
	{"essay", "エッセイ・小説", []string{"essay.md"}},
	{"formal", "公用文、規程、契約書、仕様書", []string{"business.md", "tech.md"}},
	{"promo", "紹介記事・宣伝文", []string{"essay.md", "business.md"}},
	{"mail", "メール・チャット", []string{"business.md"}},
	{"paper", "論文・研究レポート", []string{"tech.md"}},
	{"script", "台詞・脚本", []string{"essay.md"}},
}

// ReaderLevels は本家の READER_LEVELS。
var ReaderLevels = []ReaderLevel{
	{"expert", "同じ分野の専門家"},
	{"informed", "分野は違うが技術に詳しい人"},
	{"general", "専門知識のない人"},
}

func docTypeRows() [][2]string {
	rows := make([][2]string, len(DocTypes))
	for i, d := range DocTypes {
		rows[i] = [2]string{d.Value, d.Name}
	}
	return rows
}

func readerRows() [][2]string {
	rows := make([][2]string, len(ReaderLevels))
	for i, r := range ReaderLevels {
		rows[i] = [2]string{r.Value, r.Name}
	}
	return rows
}

// normalizeChoice は _normalize_choice。value が nil (None) なら nil を返す。
// NFKC のあとの小文字化は ASCII の字だけを見ればよい (NFKC が U+212A を K にするので、非 ASCII の字が小文字化で ASCII になる形は残らない)。
func normalizeChoice(option string, value *string, rows [][2]string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized := pyLowerASCII(stripPy(pyunicode.Normalize(pyunicode.NFKC, *value)))
	for _, row := range rows {
		if row[0] == normalized {
			return &normalized, nil
		}
	}
	shown := *value
	if stripPy(*value) == "" {
		shown = "（空）"
	}
	parts := make([]string, len(rows))
	for i, row := range rows {
		parts[i] = row[0] + "（" + row[1] + "）"
	}
	return nil, newError(TypeValueError, fmt.Sprintf("%s に使えない値です: %s。使えるのは %sです。", option, shown, strings.Join(parts, "、")))
}

// NormalizeDocType は normalize_doc_type (--doc-type の値の正規化)。nil はそのまま。使えない値は ValueError。
func NormalizeDocType(value *string) (*string, error) {
	return normalizeChoice("--doc-type", value, docTypeRows())
}

// NormalizeReader は normalize_reader (--reader の値の正規化)。
func NormalizeReader(value *string) (*string, error) {
	return normalizeChoice("--reader", value, readerRows())
}

// ContextLabel は context_label (「文書の種類: スライド（slides）、読者: 指定なし。」)。
func ContextLabel(docType, reader *string) (string, error) {
	dt, err := NormalizeDocType(docType)
	if err != nil {
		return "", err
	}
	rd, err := NormalizeReader(reader)
	if err != nil {
		return "", err
	}
	kinds, readers := map[string]string{}, map[string]string{}
	for _, r := range docTypeRows() {
		kinds[r[0]] = r[1]
	}
	for _, r := range readerRows() {
		readers[r[0]] = r[1]
	}
	label := func(v *string, names map[string]string) string {
		if v == nil || *v == "" {
			return "指定なし"
		}
		return names[*v] + "（" + *v + "）"
	}
	return "文書の種類: " + label(dt, kinds) + "、読者: " + label(rd, readers) + "。", nil
}

// DocTypeValues は DocTypes の値 (--doc-type に使える名前) の並び。
func DocTypeValues() []string {
	values := make([]string, len(DocTypes))
	for i, d := range DocTypes {
		values[i] = d.Value
	}
	return values
}

// ReaderValues は ReaderLevels の値 (--reader に使える名前) の並び。
func ReaderValues() []string {
	values := make([]string, len(ReaderLevels))
	for i, r := range ReaderLevels {
		values[i] = r.Value
	}
	return values
}
