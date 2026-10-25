package exclude

import (
	"dupscan/internal/group"
	"dupscan/internal/norm"
)

// PresetSingleCall は、定数だけが違う 1 行の委譲の L2 の組を外すプリセット。
// 例: `makeFile(prefix: "a", contents: c)` と `makeFile(prefix: "b", contents: c)` を呼ぶだけのヘルパー。寄せるには定数を引数にするしかなく、それは呼んでいる関数そのもの。
const PresetSingleCall = "l2-single-call"

// Presets は使えるプリセットの名前。
var Presets = []string{PresetSingleCall}

func knownPreset(name string) bool {
	for _, p := range Presets {
		if p == name {
			return true
		}
	}
	return false
}

// presetCovers は、段 lv の組 members をプリセット name が外すか。
//
// l2-single-call は L2 の組で、メンバー全員が 1 つの呼び出しの委譲 (adapter が決める tree.Unit.Delegation) で、L1 のハッシュが全員違うときだけ外す。
// L1 のハッシュが同じメンバーを含む組には効かせない: gate は「違反になった最初の段」で打ち切るので、L1 では違反にならない写し
// (同じ出どころから来た 2 つ) が L2 で初めて違反になることがあり、それを消さないため (issue 919 の反証レビュー)。
func presetCovers(name string, lv norm.Level, members []*group.Entry) bool {
	if name != PresetSingleCall || lv != norm.L2 || len(members) < 2 {
		return false
	}
	seen := map[string]bool{}
	for _, m := range members {
		if !m.Unit.Delegation {
			return false
		}
		h := m.Result.Hash[norm.L1]
		if seen[h] {
			return false
		}
		seen[h] = true
	}
	return true
}
