package ui

// PG がカードに付けた添付 (issue 453) の見せ方。画像とファイルは o で外のアプリ (open → Preview) に渡し、
// 文字の添付 (tmux の capture-pane -e の色つきの画面など) は詳細の中にそのまま出す。

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"termsafe"

	"pro-con/card"
)

const (
	attachTextBytes = 256 << 10 // 詳細に出す文字の添付の上限 (これより後ろは読まない)
	attachTextLines = 400
)

// openedMsg は添付を外のアプリに渡した結果。
type openedMsg struct {
	n   int
	err error
}

// openWithSystem は paths を macOS の open に渡す (Preview は複数の画像を 1 つの窓にまとめる)。
func openWithSystem(paths []string) error {
	out, err := exec.Command("open", paths...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// openable は外のアプリで開く添付 (文字の添付は詳細に出しているので含めない)。
func openable(c card.Card) []string {
	var out []string
	for _, a := range c.Attachments {
		if a.Kind != card.AttachText {
			out = append(out, a.Path)
		}
	}
	return out
}

// openAttachments は選んでいるカードの画像とファイルの添付を開く (o。docs/glogx-ui-guide.md の o = 外で開く)。
func (m *Model) openAttachments() tea.Cmd {
	c, ok := m.selectedCard()
	if !ok {
		m.refuse("開くカードが選ばれていない")
		return nil
	}
	paths := openable(c)
	if len(paths) == 0 {
		if len(c.Attachments) > 0 {
			m.refuse("このカードの添付は文字だけ (enter の詳細に出ている)")
		} else {
			m.refuse("このカードに添付は無い")
		}
		return nil
	}
	open := m.openFiles
	return m.child(func() tea.Msg { return openedMsg{n: len(paths), err: open(paths)} })
}

func (m *Model) onOpened(msg openedMsg) {
	if msg.err != nil {
		m.fail("添付を開けない: " + msg.err.Error())
		return
	}
	m.done(fmt.Sprintf("添付を %d 件開いた", msg.n))
}

// attachmentLines は詳細の「添付」の行 (幅 w)。文字の添付は中身を字下げして続ける (画面の写しなので折り返さずに切る)。
func (m *Model) attachmentLines(c card.Card, w int) []string {
	var out []string
	for _, a := range c.Attachments {
		label := a.Note
		if strings.TrimSpace(label) == "" {
			label = a.Name
		}
		out = append(out, ansi.Truncate("  "+a.At.Local().Format("15:04")+" "+string(a.Kind)+"  "+termsafe.PlainLine(label), w, "…"))
		if a.Kind != card.AttachText {
			continue
		}
		for _, l := range m.attachText(a.Path) {
			out = append(out, ansi.Truncate("    "+l, w, "…")+sgrReset)
		}
	}
	return out
}

// attachText は文字の添付の中身 (色は残し、それ以外の制御は落とす)。添付は付けた後に書き換わらない (名前が依頼ごとに違う) ので、
// パスごとに 1 度だけ読む (詳細は描くたびに組み直す)。
func (m *Model) attachText(path string) []string {
	if ls, ok := m.attachTexts[path]; ok {
		return ls
	}
	var ls []string
	data, err := readHead(path, attachTextBytes)
	if err != nil {
		ls = []string{sgrDim + "(読めない: " + termsafe.PlainLine(err.Error()) + ")" + sgrReset}
	} else {
		raw := strings.Split(strings.TrimRight(string(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))), "\n "), "\n")
		for i, l := range raw {
			if i == attachTextLines {
				ls = append(ls, sgrDim+fmt.Sprintf("(残り %d 行は省いた: %s)", len(raw)-i, path)+sgrReset)
				break
			}
			ls = append(ls, termsafe.DetailLine(l))
		}
	}
	if m.attachTexts == nil {
		m.attachTexts = map[string][]string{}
	}
	m.attachTexts[path] = ls
	return ls
}

func readHead(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, n))
}
