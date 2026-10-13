package filer

import (
	"os"
	"path/filepath"
	"strings"
)

// goto.go は `:` (入力したパスへカーソルを飛ばす。issue 746)。入力は `!` / `a` / `m` と同じ足元の欄で受ける。

func (m *Model) startGoto() { m.openPrompt(promptGoto, nil, "") }

// gotoPath は text のパスへカーソルを移す。絶対パスはそのまま・`~` は家・相対パスは root から (`Y` がコピーする形)。
// root の外なら共通の祖先まで root を上げる (`-` を繰り返すのと同じで、開いていたフォルダとアニメを保つ)。
// 無いパスでは root もカーソルも動かさない。
func (m *Model) gotoPath(text string) {
	text = strings.TrimSpace(text) // 貼り付けた行末の改行
	if text == "" {
		return
	}
	abs, ok := m.inputAbs(text)
	if !ok {
		m.fail("移動できません: 家のフォルダが分かりません")
		return
	}
	if _, err := os.Lstat(abs); err != nil {
		m.fail("移動できません: " + termsafeLine(err.Error())) // エラーに入力したパスが入る
		return
	}
	abs = m.rootSpelling(abs)
	// 在っても木に載るとは限らない (大文字小文字の違う綴り・途中の symlink のフォルダ)。上げてから見つからなければ、上げる前へ戻す
	// (戻さないと「無いパスでは動かさない」が破れる。大文字小文字の違いは / まで上がっていた。issue 746 の敵対レビュー)
	undo := m.rootUndo()
	for !under(abs, m.root.abs) {
		before := m.root
		m.rerootUp()
		if m.root == before {
			undo() // 上がれなかった理由は rerootUp が知らせた
			return
		}
	}
	if !m.reveal(abs) {
		undo()
		// 木は symlink を辿らず、名前を綴りどおりに照らす
		m.fail("移動できません: 木の中に見つかりません (大文字小文字の違い・途中の symlink): " + termsafeLine(abs))
	}
}

// rootUndo は今の root へ戻す関数を返す。rerootUp が書き換えるのは元の root の名前・親・属性だけ
// (途中で作った root は捨てる。model.go の rerootUp)。
func (m *Model) rootUndo() func() {
	r := m.root
	raw, parent, mtime, size := r.raw, r.parent, r.mtime, r.size
	return func() {
		r.raw, r.parent, r.mtime, r.size = raw, parent, mtime, size
		m.root = r
	}
}

// inputAbs は入力のパスを絶対パスにする (ok=false は `~` を展開できなかった)。
func (m *Model) inputAbs(text string) (string, bool) {
	if text == "~" || strings.HasPrefix(text, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		text = filepath.Join(home, text[1:])
	}
	if !filepath.IsAbs(text) {
		text = filepath.Join(m.root.abs, text)
	}
	return filepath.Clean(text), true
}

// rootSpelling は、root を別の綴り (macOS の /var と /private/var のような symlink 越し) で書いたパスを root の綴りへ直す。
// 木の項目は root の綴り (newRoot の filepath.Abs) で照合するので、直さないと中にあるパスで root を上げてしまう。
// 直すのは root にあたる頭だけで、その下は書いたまま (root の中の symlink を解くと、別の項目へ黙って飛ぶ)。
// そのため頭は上から探して最初の一致 (最短) を取り、各段の末尾は辿らない (root を指す symlink そのものは root と見なさない)。
// 直せなければそのまま返す。
func (m *Model) rootSpelling(abs string) string {
	if under(abs, m.root.abs) {
		return abs
	}
	realRoot, err := filepath.EvalSymlinks(m.root.abs)
	if err != nil {
		return abs
	}
	for i := 1; i <= len(abs); i++ {
		if i < len(abs) && abs[i] != filepath.Separator {
			continue
		}
		p := abs[:i]
		dir, err := filepath.EvalSymlinks(filepath.Dir(p))
		if err == nil && filepath.Join(dir, filepath.Base(p)) == realRoot {
			return m.root.abs + abs[i:]
		}
	}
	return abs
}

// under は p が root そのものか配下か。
func under(p, root string) bool { return p == root || strings.HasPrefix(p, withSep(root)) }

// reveal は root の中の絶対パスの項目を、途中のフォルダを読み込んで開いてカーソルに置く (見つからなければ false で何もしない)。
func (m *Model) reveal(abs string) bool {
	n := m.loadPath(abs)
	if n == nil || !m.inTree(n) {
		return false
	}
	for a := n.parent; a != nil; a = a.parent {
		a.expanded = true
	}
	m.setCur(n)
	return true
}
