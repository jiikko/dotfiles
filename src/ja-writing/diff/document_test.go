package diff

import (
	"testing"

	"ja-writing/internal/pyjson"
)

func documentJSON(t *testing.T, old, new string) string {
	t.Helper()
	d, err := diffDocument(request{old: old, new: new})
	if err != nil {
		t.Fatalf("diffDocument: %v", err)
	}
	text, err := pyjson.Dumps(d, pyjson.Options{Indented: true, Indent: 1})
	if err != nil {
		t.Fatal(err)
	}
	return text
}

// 2 つの文書を続けて処理しても、前の文書の範囲 (文の種類の印・許可・能力・選ぶ範囲・因果が使い回す typeCache) が残らない。
// 本家の diff() と meaning_risks() は finally でグローバルの _LAST_TYPE_RANGES を空に戻す所で、Go は 1 回の diffDocument ごとに cache を作る。
func TestDiffDocumentKeepsNothingFromThePreviousDocument(t *testing.T) {
	docs := [][2]string{
		{diffDocForTest("保存してもよい。必ず遅れる。", "どれか一つを選ぶ。"), diffDocForTest("保存する。遅れます。", "すべてを選ぶ。")},
		{diffDocForTest("遅れるかもしれない。", "担当者が確認する。"), diffDocForTest("遅れます。", "管理者が確認する。")},
		{diffDocForTest("原因は未確認です。"), diffDocForTest("原因は対策済みです。")},
		{diffDocForTest("確認します。"), diffDocForTest("確認します。")},
	}
	fresh := make([]string, len(docs))
	for i, d := range docs {
		fresh[i] = documentJSON(t, d[0], d[1])
	}
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {1, 1, 0, 0, 2, 3, 1}} {
		for _, i := range order {
			if got := documentJSON(t, docs[i][0], docs[i][1]); got != fresh[i] {
				t.Fatalf("文書 %d の出力が、前に処理した文書で変わった (順 %v)", i, order)
			}
		}
	}
}

// 出力の鍵の並びは本家の辞書の挿入の順 (--json の出力の順になる)。
func TestDiffDocumentKeyOrderIsTheUpstreamInsertionOrder(t *testing.T) {
	d, err := diffDocument(request{old: "確認します。\n", new: "確認しました。\n"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"format", "format_version", "tool_version", "status", "changed", "candidate_count", "meaning_risk_count", "fail_on_meaning_risk_count",
		"not_judged", "context", "meaning_risks", "markers", "new_words", "lost_words", "structure", "spans", "logic", "bold", "endings", "type_changes",
		"full_diff", "stiffness", "tone", "feeling", "hedges", "immutable", "politeness", "register", "added_actors", "heading_shapes", "comma_increase"}
	got := d.Keys()
	if len(got) != len(want) {
		t.Fatalf("鍵が %d 個 (本家 %d 個): %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%d 番目の鍵が %q (本家 %q)", i, got[i], want[i])
		}
	}
}

// 公開の入口 (HedgesSection ほか) は、本家の diff() の組み立て (diffDocument) の節と同じ答えを返す。入口が本番と違う入力で作られていないことを固める。
func TestEntryPointsReturnTheSectionsOfTheDocument(t *testing.T) {
	old := diffDocForTest("遅れると考えられる。[1]を見る。「はい」と言った。", "設定を開く。", "保存を押した。", "結果は3件だ。", "田中様、", "担当者が確認する。")
	new := diffDocForTest("遅れます。[2]を見る。「いいえ」と言った。", "設定を開きます。", "保存を押しました。", "結果は3件です。", "山田様、", "管理者が確認します。")
	for _, docType := range []string{"", "mail", "article"} {
		req := request{old: old, new: new}
		if docType != "" {
			req.docType = &docType
		}
		doc, err := diffDocument(req)
		if err != nil {
			t.Fatal(err)
		}
		same := func(key string, entry pyjson.Value, err error) {
			t.Helper()
			if err != nil {
				t.Fatalf("%s: %v", key, err)
			}
			want, _ := doc.Get(key)
			if !equalValues(t, entry, want) {
				t.Errorf("doc_type %q: 入口が diffDocument の %s と違う", docType, key)
			}
		}
		hedges, err := HedgesSection(old, new, docType)
		same("hedges", hedges, err)
		politeness, err := PolitenessSection(old, new, docType)
		same("politeness", politeness, err)
		register, err := RegisterChanges(old, new, docType)
		same("register", register, err)
		actors, err := AddedActors(old, new, nil)
		same("added_actors", actors, err)
		risks, err := MeaningRisks(old, new, true, docType)
		same("meaning_risks", risks, err)
		immutable, err := ImmutableValues(old, new)
		if err != nil {
			t.Fatal(err)
		}
		same("immutable", immutable.([]pyjson.Value)[0], nil)
	}
}

// QuoteRisks と CertaintyRisks の入口は、意味が変わる恐れ (本番の MeaningRisks) の同じ kind の候補と同じ答えを返す。
func TestKindEntryPointsMatchTheMeaningRisks(t *testing.T) {
	old := diffDocForTest("遅れるかもしれない。「はい」と言った。", "遅れることがある。", "遅れる可能性がある。")
	new := diffDocForTest("遅れます。「いいえ」と言った。", "必ず遅れる。", "確実に遅れる。")
	risks, err := MeaningRisks(old, new, true, "")
	if err != nil {
		t.Fatal(err)
	}
	pick := func(kinds ...string) pyjson.Value {
		var out []pyjson.Value
		for _, r := range risks.([]pyjson.Value) {
			kind, _ := r.(*pyjson.Object).Get("kind")
			for _, k := range kinds {
				if kind == k {
					out = append(out, r)
				}
			}
		}
		return out
	}
	for _, kinds := range [][]string{{"quote"}, {"certainty", "certainty_review"}} {
		if len(pick(kinds...).([]pyjson.Value)) == 0 {
			t.Fatalf("見本の文書に %v の候補が出ない (比べても何も確かめない)", kinds)
		}
	}
	quotes, err := QuoteRisks(old, new)
	if err != nil {
		t.Fatal(err)
	}
	if !equalValues(t, quotes, pick("quote")) {
		t.Error("QuoteRisks が MeaningRisks の quote と違う")
	}
	certainty, err := CertaintyRisks(old, new, "")
	if err != nil {
		t.Fatal(err)
	}
	if !equalValues(t, certainty, pick("certainty", "certainty_review")) {
		t.Errorf("CertaintyRisks が MeaningRisks の certainty と違う")
	}
}
