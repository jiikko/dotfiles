// Package langtest は adapter のテストが共有する helper (テストからだけ import する)。
package langtest

import (
	"testing"

	"dupscan/internal/lang"
	"dupscan/internal/norm"
	"dupscan/internal/tree"
)

// Find は r の中で名前が name の最初の単位。無ければテストを止める。
func Find(t *testing.T, r lang.FileResult, name string) *tree.Unit {
	t.Helper()
	for _, u := range r.Units {
		if u.Name == name {
			return u
		}
	}
	t.Fatalf("単位 %s が無い (%d 個)", name, len(r.Units))
	return nil
}

// NormBoth は形つきで正規化し、本番の経路 (形を作らない) とハッシュが同じかも確かめる。
func NormBoth(t *testing.T, u *tree.Unit) norm.Result {
	t.Helper()
	with, plain := norm.NormalizeWithForms(u), norm.Normalize(u)
	for _, l := range norm.Levels {
		if with.Hash[l] != plain.Hash[l] || with.L3Trivial != plain.L3Trivial || with.Nodes != plain.Nodes {
			t.Errorf("%s: 本番の経路と形を作る経路で結果が違う: %s / %s", l, plain.Hash[l], with.Hash[l])
		}
	}
	return with
}
