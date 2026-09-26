package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"glogx/issues"

	"pro-con/card"
)

// カードに紐づく issue のファイル (e でエディタ、y でパスのコピー)。issue は repo + 番号で持っているので、
// repo の issue ディレクトリから番号のファイルを探す (状態はファイルの位置なので、どのディレクトリに居ても見つける)。

var errNoIssue = errors.New("このカードは issue に紐づいていない")

// findIssue は repo の issue ディレクトリから番号 number の issue のファイルを 1 つ探す。読み方 (状態のディレクトリ・
// epic/<name>/ の 2 段・next/ の目印は実体へ) は glogx の issues viewer と同じ glogx/issues に任せる。
// 同じ番号が 2 つ見つかったら、黙ってどちらかを選ばずエラーにする (番号の衝突は issues/README.md の検査の対象)。
func findIssue(repoPath string, number int) (string, error) {
	dirs := issues.FindDirs(repoPath)
	if len(dirs) == 0 {
		return "", fmt.Errorf("%s に issue のディレクトリが無い", repoPath)
	}
	list, _ := issues.Scan(dirs)
	var found []string
	for _, iss := range list {
		if n, err := strconv.Atoi(iss.Number); err == nil && n == number {
			found = append(found, iss.Path)
		}
	}
	switch {
	case len(found) == 0:
		return "", fmt.Errorf("issue %03d が %s に見つからない", number, strings.Join(dirs, ", "))
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
		m.fail("開けない: " + err.Error())
		return nil
	}
	return m.execProcess(m.openEditor(p), func(err error) tea.Msg { return editorDoneMsg{path: p, err: err} })
}

func (m *Model) onEditorDone(msg editorDoneMsg) {
	if msg.err != nil {
		m.fail("エディタが失敗した: " + msg.err.Error())
		return
	}
	m.done("エディタから戻った: " + msg.path)
}

// yankPath は選択中のカードの issue のファイルのパス (素の値) をコピーする (y。docs/glogx-ui-guide.md の y = 素の値)。
func (m *Model) yankPath() {
	c, ok := m.selectedCard()
	if !ok {
		m.refuse("コピーするカードが選ばれていない")
		return
	}
	p, err := m.issuePath(c)
	if err != nil {
		m.fail("コピーできない: " + err.Error() + " (Y でタイトルと内容をコピー)")
		return
	}
	if err := m.copy(p); err != nil {
		m.fail("コピーに失敗した: " + err.Error())
		return
	}
	m.done("パスをコピーした: " + p)
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

// cardHeading はカードの 1 行目 (ID + issue 番号 + タイトル)。タイトルの頭が最初の issue の番号 + ": " (「452: …」。
// 積む側が付けた形) なら、番号は issueTag に出ているので落とす (issue 491)。記録のタイトルは書き換えない。
// 「437 の続き: …」のような形は番号だけ落とすと意味が崩れるので、そのまま出す。確認のカードは題名の頭に card.QuestionMark を付ける。
func cardHeading(c card.Card) string {
	title := c.Title
	if len(c.Issues) > 0 {
		if num, rest, ok := strings.Cut(title, ": "); ok && num != "" && strings.Trim(num, "0123456789") == "" {
			if n, _ := strconv.Atoi(num); n == c.Issues[0].Number {
				title = rest
			}
		}
	}
	if c.Purpose == card.ForQuestion { // 確認のカードは番号の後に印 (issue 531)
		title = card.QuestionMark + " " + title
	}
	return c.ID + issueTag(c) + " " + title
}
