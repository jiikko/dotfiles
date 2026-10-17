package term

import "github.com/jiikko/dotfiles/src/tuikit/hintfit"

// HintItem は hint の 1 項目と優先度 (小さいほど残す。1 = 抜ける手段)。
type HintItem struct {
	Text string
	Prio int
}

// FitHintItems は幅 width に収まるところまで項目を採り、元の並び順で繋ぐ (落とし方・優先度 1 の席の予約は hintfit.Fit が正本)。
//
// 🚨 名前を残しているのは、hintLine の hint が必ずここを通ることを hint_registry_gate_test.go が AST の呼び出し名で縛るため。
// 🚨 落とした項目は `--help` / README / docs/status-viewer-spec.md が正本 (issues viewer の hint と同じ作法)。画面の案内は
// 「今の幅で確実に読めるもの」だけにする。
func FitHintItems(width int, items []HintItem) string {
	return hintfit.Fit(width, "  ", HintfitItems(items))
}

func HintfitItems(items []HintItem) []hintfit.Item {
	out := make([]hintfit.Item, 0, len(items))
	for _, it := range items {
		out = append(out, hintfit.Item{Text: it.Text, Prio: it.Prio})
	}
	return out
}
