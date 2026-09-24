package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"pro-con/card"
)

// カードに紐づく issue のファイル (e でエディタ、y でパスのコピー)。issue は repo + 番号で持っているので、
// repo の issues/ の下を歩いて番号のファイルを探す (状態はファイルの位置なので、どのディレクトリに居ても見つける)。

var errNoIssue = errors.New("このカードは issue に紐づいていない")

// findIssue は repo の issues/ の下から NNN-*.md を 1 つ探す。next/ の claim の目印 (symlink) は飛ばして実体を返す。
// 同じ番号が 2 つ見つかったら、黙ってどちらかを選ばずエラーにする (番号の衝突は issues/README.md の検査の対象)。
func findIssue(repoPath string, number int) (string, error) {
	prefix := fmt.Sprintf("%03d-", number)
	var found []string
	root := filepath.Join(repoPath, "issues")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 || d.IsDir() {
			return nil
		}
		if n := d.Name(); strings.HasPrefix(n, prefix) && strings.HasSuffix(n, ".md") {
			found = append(found, p)
		}
		return nil
	})
	switch {
	case err != nil:
		return "", fmt.Errorf("%s を読めない: %w", root, err)
	case len(found) == 0:
		return "", fmt.Errorf("issue %03d が %s に見つからない", number, root)
	case len(found) > 1:
		return "", fmt.Errorf("issue %03d が %d 個ある (番号が衝突している): %s", number, len(found), strings.Join(found, ", "))
	}
	return found[0], nil
}

// issuePath は選択中のカードの最初の issue のファイル。
func (m *Model) issuePath(c card.Card) (string, error) {
	if len(c.Issues) == 0 {
		return "", errNoIssue
	}
	ref := c.Issues[0]
	for _, r := range m.repos {
		if r.Name == ref.Repo {
			return findIssue(r.Path, ref.Number)
		}
	}
	return "", fmt.Errorf("repo %s が設定 (~/.config/pro-con/config.toml) に無いので issue のファイルを探せない", ref.Repo)
}

type editorDoneMsg struct {
	path string
	err  error
}

// openIssue は選択中のカードの issue をエディタで開く ($VISUAL → $EDITOR → nvim。tuikit/editor)。
func (m *Model) openIssue() tea.Cmd {
	c, ok := m.selectedCard()
	if !ok {
		return nil
	}
	p, err := m.issuePath(c)
	if err != nil {
		m.flash = "開けない: " + err.Error()
		return nil
	}
	return tea.ExecProcess(m.openEditor(p), func(err error) tea.Msg { return editorDoneMsg{path: p, err: err} })
}

func (m *Model) onEditorDone(msg editorDoneMsg) {
	if msg.err != nil {
		m.flash = "エディタが失敗した: " + msg.err.Error()
		return
	}
	m.flash = "エディタから戻った: " + msg.path
}

// yankPath は選択中のカードの issue のファイルのパス (素の値) をコピーする (y。docs/glogx-ui-guide.md の y = 素の値)。
func (m *Model) yankPath() {
	c, ok := m.selectedCard()
	if !ok {
		m.flash = "コピーするカードが選ばれていない"
		return
	}
	p, err := m.issuePath(c)
	if err != nil {
		m.flash = "コピーできない: " + err.Error() + " (Y でタイトルと内容をコピー)"
		return
	}
	if err := m.copy(p); err != nil {
		m.flash = "コピーに失敗した: " + err.Error()
		return
	}
	m.flash = "パスをコピーした: " + p
}

// issueTag はカードの 1 行目に出す issue 番号 (最初の 1 つ + 残りの数)。
func issueTag(c card.Card) string {
	if len(c.Issues) == 0 {
		return ""
	}
	t := fmt.Sprintf(" #%03d", c.Issues[0].Number)
	if n := len(c.Issues) - 1; n > 0 {
		t += fmt.Sprintf("+%d", n)
	}
	return t
}
