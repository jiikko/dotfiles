package filer

import (
	"path/filepath"
	"strings"

	"github.com/jiikko/dotfiles/src/termsafe"
	"subproc/clipboard"
)

// copy.go は `y` / `Y` (カーソルの項目のパスをクリップボードへ。y = 絶対パス・Y = root からの相対パス)。

// copyToClipboard はテストで実クリップボードを触らないための差し替え点 (本体と失敗モードは subproc/clipboard の doc)。
var copyToClipboard = clipboard.Copy

// copyPath はカーソルの項目のパスをコピーする。渡すのはディスク上のパス (表示用に termsafe を通した名前ではない。貼る先で使うため)。
func (m *Model) copyPath(relative bool) {
	p := m.cur.path()
	if relative {
		r, err := filepath.Rel(m.root.path(), p)
		if err != nil { // root の配下にしかカーソルは居ないので起きないはず。起きたら絶対パスを渡さず断る
			m.fail("相対パスを作れません: " + m.cur.name)
			return
		}
		p = r
	}
	if err := copyToClipboard(p); err != nil {
		msg, _, _ := strings.Cut(err.Error(), "\n")
		m.fail("コピーに失敗しました: " + termsafe.PlainLine(msg))
		return
	}
	m.notices = append(m.notices, Notice{Text: "パスをコピーしました: " + m.cur.name, OK: true})
}
