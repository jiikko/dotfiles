package live

import (
	"context"
	"os/exec"
	"strings"
)

// paneFormat は tmux list-panes の 1 行 (端末と、その pane の session:window.pane)。
const paneFormat = "#{pane_tty} #{session_name}:#{window_index}.#{pane_index}"

// PaneNames は端末を tmux の pane の名前に直す表 (backend.PaneNamer)。聞くのは既定の tmux サーバだけ
// (別のサーバ (-L) の pane と tmux の外の端末は入らず、画面には tty のまま出る)。tmux が無い・サーバが居なければ空。
func (b *Backend) PaneNames(ctx context.Context) map[string]string {
	out, err := exec.CommandContext(ctx, "tmux", "list-panes", "-a", "-F", paneFormat).Output()
	if err != nil {
		return nil
	}
	return parsePanes(string(out))
}

// PaneNames は見ているだけの画面でも同じ (tmux に聞くだけで、画面・dispatcher・PG の状態を変えない)。
func (v viewOnly) PaneNames(ctx context.Context) map[string]string { return v.b.PaneNames(ctx) }

// parsePanes は paneFormat の行を読む。session の名前に空白があってもよい (端末は空白を含まない)。
func parsePanes(out string) map[string]string {
	panes := map[string]string{}
	for line := range strings.Lines(out) {
		tty, name, ok := strings.Cut(strings.TrimRight(line, "\n"), " ")
		if ok && tty != "" && name != "" {
			panes[tty] = name
		}
	}
	return panes
}
