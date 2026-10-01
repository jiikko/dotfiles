// Package framebench は、描いた画面の文字列を bubbletea v2 のレンダラと同じ手順でセルへ書き直し、端末への差分まで通す
// 計測の道具 (issue 607)。View の文字列を作るだけのベンチは、この分 (pro-con の 200 × 50 で View の約 2.3 倍) を測らない。
//
// 再現しているのは bubbletea v2.0.8 の cursedRenderer.flush の alt screen の経路: 前のコマと同じなら何もしない (viewEquals) →
// Clear → uv.NewStyledString(content).Draw → TerminalRenderer.Render → Flush。
// 再現していないもの: カーソルの移動・表示の切り替え・同期出力の囲み・タイトル等の端末モード (どれも数十バイトで、コマの重さに効かない)。
// bubbletea を上げたら、この手順が flush と食い違っていないかを読み直す。
//
// 🚨 色のプロファイルは TrueColor に固定する。uv.NewTerminalRenderer は書き込み先 (tty でない) と env から NoTTY を選び、
// 色を全部捨てる (差分と出るバイトが本物より軽く出る)。
// 🚨 ultraviolet の版は、この package を取り込んだ側 (消費者) の go.mod で決まる。tuikit と消費者の版は
// tests/scripts/test_tuikit_consumers_aligned.sh が揃える (ずれると tuikit 単体のテストが消費者と別の版を測る)。
package framebench

import (
	"fmt"
	"io"

	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
)

// Renderer は固定の大きさの alt screen を描くレンダラ。
type Renderer struct {
	out       *countWriter
	scr       *uv.TerminalRenderer
	buf       uv.ScreenBuffer
	started   bool
	last      string
	lastAttrs string
}

// NewRenderer は width × height の画面を描くレンダラを作る。
func NewRenderer(width, height int) *Renderer { return newRenderer(width, height, io.Discard) }

func newRenderer(width, height int, sink io.Writer) *Renderer {
	r := &Renderer{out: &countWriter{w: sink}, buf: uv.NewScreenBuffer(width, height)}
	r.scr = uv.NewTerminalRenderer(r.out, []string{"TERM=xterm-256color"})
	r.scr.SetColorProfile(colorprofile.TrueColor)
	r.scr.SetTabStops(-1)      // bubbletea の reset の既定 (hard tab の最適化は使わない)
	r.scr.SetScrollOptim(true) // bubbletea の reset (Windows 以外)
	// alt screen に入ったときの設定 (bubbletea の enterAltScreen)
	r.scr.SetFullscreen(true)
	r.scr.SetRelativeCursor(false)
	r.scr.Erase()
	return r
}

// Frame は 1 コマを描く。attrs は content 以外に bubbletea が前のコマと比べる属性 (tea.View.Cursor など) で、%+v の文字列で比べる。
// content と attrs が前のコマと同じなら何もせず false を返す (bubbletea も描かない)。
func (r *Renderer) Frame(content string, attrs any) bool {
	a := fmt.Sprintf("%+v", attrs)
	if r.started && content == r.last && a == r.lastAttrs {
		return false
	}
	r.started, r.last, r.lastAttrs = true, content, a
	r.buf.Clear()
	uv.NewStyledString(content).Draw(r.buf, r.buf.Bounds())
	r.scr.Render(r.buf.RenderBuffer)
	_ = r.scr.Flush() // 書き込み先は countWriter で失敗しない
	return true
}

// Written は端末へ出たバイトの累計。
func (r *Renderer) Written() int64 { return r.out.n }

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return c.w.Write(p)
}
