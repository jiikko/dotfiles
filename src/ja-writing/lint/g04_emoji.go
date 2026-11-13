package lint

import "strings"

// G4 絵文字 (emoji_prohibited。本家 yomiyasu_lint.py の 5678-5686)。
//
// 依存: T1 (EmojiMatches)

// g04State は G4 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g04State struct{}

// g04Emoji は行のループの最初 (引用の行と表の行でも出る)。コード・URI・HTML の中の字は scanText に入っていない (見える文字だけを数える)。
func (s *lintState) g04Emoji(r *rowCtx) {
	matches := EmojiMatches(r.scanText)
	if len(matches) == 0 {
		return
	}
	s.add(newFinding("emoji_prohibited", r.lineNo, "warn",
		"絵文字（"+strings.Join(matches[:min(3, len(matches))], " ")+"）が検出されました。AI特有の不要な装飾であれば整理してください。ただし、意味や必要な用途を担っている表現は保ってください。", r.stripped))
}
