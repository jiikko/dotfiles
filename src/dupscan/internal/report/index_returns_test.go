package report

import (
	"slices"
	"strings"
	"testing"

	"dupscan/internal/tree"
)

// -returns の完全一致 (末尾の ? ! を除く) が、部分一致より前に並ぶ。完全一致の組の中は従来の並び (prod → test) を保つ。
func TestIndexReturnsExactMatchComesBeforePartial(t *testing.T) {
	fn := func(file, name, ret string, test bool) *tree.Unit {
		return &tree.Unit{Kind: tree.KindFunc, File: file, Name: name, ReturnType: ret, Test: test}
	}
	units := []*tree.Unit{
		fn("a.swift", "viewModel", "StorageConnectionsViewModel", false),
		fn("b.swift", "list", "[StorageConnection]", false),
		fn("c.swift", "optionalTest", "StorageConnection?", true),
		fn("d.swift", "plainProd", "StorageConnection", false),
		fn("e.swift", "forced", "StorageConnection!", true),
		fn("f.swift", "noReturn", "", false),
	}
	names := func(q Query) []string {
		var out []string
		for _, e := range BuildIndex(units, nil, q).Entries {
			out = append(out, e.Name)
		}
		return out
	}
	// 完全一致 (prod → test。test 同士は場所の順) → 部分一致 (場所の順)
	if got, want := names(Query{Returns: "StorageConnection"}), []string{"plainProd", "optionalTest", "forced", "viewModel", "list"}; !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	// -returns が無ければ従来の並び (prod → test → 場所) のまま。
	if got, want := names(Query{}), []string{"viewModel", "list", "plainProd", "noReturn", "optionalTest", "forced"}; !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

// -returns の語にも同じ整形 (末尾の ? !) を当てる。`StorageConnection?` と渡しても完全一致の優先が効く。
func TestIndexReturnsQueryIsTrimmedLikeReturnType(t *testing.T) {
	for _, suffix := range []string{"?", "!"} {
		units := []*tree.Unit{
			{Kind: tree.KindFunc, File: "a.swift", Name: "list", ReturnType: "[StorageConnection" + suffix + "]"},
			{Kind: tree.KindFunc, File: "b.swift", Name: "plain", ReturnType: "StorageConnection" + suffix},
		}
		got := BuildIndex(units, nil, Query{Returns: "StorageConnection" + suffix}).Entries
		if len(got) != 2 || got[0].Name != "plain" {
			t.Errorf("-returns StorageConnection%s: 完全一致の plain が先頭: %v", suffix, got)
		}
	}
}

// 並びの優先順位: 名前の一致の強さ > 戻り型の完全一致 > 共通の置き場所。
func TestIndexReturnsExactRanksBelowNameScoreAndAboveShared(t *testing.T) {
	fn := func(file, name, ret string) *tree.Unit {
		return &tree.Unit{Kind: tree.KindFunc, File: file, Name: name, ReturnType: ret}
	}
	names := func(units []*tree.Unit, q Query) []string {
		var out []string
		for _, e := range BuildIndex(units, nil, q).Entries {
			out = append(out, e.Name)
		}
		return out
	}
	// 名前の強さが先: 前方一致で戻り型が完全一致の makeX より、同じ名前で部分一致の make が前
	units := []*tree.Unit{fn("a.swift", "makeX", "Foo"), fn("b.swift", "make", "[Foo]")}
	if got, want := names(units, Query{Name: "make", Returns: "Foo"}), []string{"make", "makeX"}; !slices.Equal(got, want) {
		t.Errorf("名前の一致の強さが完全一致より先: got %v want %v", got, want)
	}
	// 完全一致が shared より先: shared の部分一致より、shared でない完全一致が前
	units = []*tree.Unit{fn("shared/a.swift", "partial", "[Foo]"), fn("z.swift", "exact", "Foo")}
	shared := func(f string) bool { return strings.HasPrefix(f, "shared/") }
	if got, want := names(units, Query{Returns: "Foo", Shared: shared}), []string{"exact", "partial"}; !slices.Equal(got, want) {
		t.Errorf("完全一致が shared より先: got %v want %v", got, want)
	}
}
