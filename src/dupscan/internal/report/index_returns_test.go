package report

import (
	"slices"
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
