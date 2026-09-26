package upgrade

import (
	"bytes"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
)

// 切り替え (exec) の間に元のシェルの画面を見せない (issue 509)。bubbletea は終了のときに alt screen を抜け (\e[?1049l)、
// カーソルを出す (\e[?25h)。この 2 つが書かれると、新版が alt screen に入り直すまでの間 (exec と起動と最初の読み込み)
// シェルの画面が見える。切り替えのときだけ 2 つを落とし、旧版が最後に描いた画面を出したまま新版へ渡す。
// 新版が alt screen に入る列 (\e[?1049h) は、既に alt screen にいれば tmux は何もしない (screen_alternate_on が早く戻る)。
// ほかの端末は未確認 (xterm は入り直すときにカーソルの保存を上書きしうる。そのときは終了後のプロンプトが最下行に出るだけ)。

var (
	leaveAltScreen = []byte("\x1b[?1049l")
	showCursor     = []byte("\x1b[?25h")
)

// Screen は画面の出力 (端末) を包む。Keep の後に書かれた「alt screen を抜ける」「カーソルを出す」を落とす。
// Fd を持つ (*os.File を埋める) ので、bubbletea は端末として扱う (大きさ・色の数を端末から読む)。
//
// 🚨 bubbletea の tea.ExecProcess は、子の Stdout が空なら出力 (これ) を渡す。*os.File でないと os/exec は
// パイプを挟み、子 (attach の claude・エディタ) の stdout が端末でなくなる。子を起こす側で Stdout を端末に固定する (ui の execOnTerminal)。
type Screen struct {
	*os.File
	keep atomic.Bool
	mu   sync.Mutex
	stop func() // Keep の間のシグナルの見張りを外す (GuardAltScreen)
}

// NewScreen は f (端末) を包む。
func NewScreen(f *os.File) *Screen { return &Screen{File: f} }

// Keep は、これ以降の終了の処理で alt screen を抜けないようにする (切り替えの直前に呼ぶ)。
// exec するまでの間 (裏の処理の終わりを最大 5 秒待つ・状態を書く) に割り込み (ctrl+c) 等で終わっても閉じ込めないよう、
// シグナルを受けたら alt screen を抜けて終わる見張りも置く (GuardAltScreen)。
func (s *Screen) Keep() {
	s.keep.Store(true)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop == nil {
		s.stop = GuardAltScreen(s.File)
	}
}

// Release は Keep を取り消す (切り替えに失敗して旧版のまま続ける・普通に終了するとき)。
func (s *Screen) Release() {
	s.keep.Store(false)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
}

// GuardAltScreen は、alt screen を bubbletea の外で持っている間 (旧版の exec の前・新版の画面を出す前) に割り込み・終了・
// 端末の切断のシグナルを受けたら、alt screen を抜けてから終わる見張りを置く。戻り値で見張りを外す。
// 🚨 bubbletea の外の間は端末が cooked のままなので、ctrl+c は SIGINT として届き、既定の動作では alt screen に閉じ込めたまま終わる。
// exec すると見張りは消える (捕まえたシグナルは既定の動作に戻る)。
func GuardAltScreen(f *os.File) (stop func()) {
	ch := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		select {
		case sig := <-ch:
			LeaveAltScreen(f)
			code := 128 + int(sig.(syscall.Signal))
			os.Exit(code)
		case <-done:
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			signal.Stop(ch)
			close(done)
		})
	}
}

// Write は Keep のあいだだけ 2 つの列を落として書く。bubbletea は終了の処理の出力を 1 度の Write で書くので、
// 列が 2 回の Write にまたがることは無い (cursedRenderer.close は溜めた出力を io.Copy で 1 度に書く)。
func (s *Screen) Write(p []byte) (int, error) {
	if !s.keep.Load() {
		return s.File.Write(p)
	}
	q := bytes.ReplaceAll(bytes.ReplaceAll(p, leaveAltScreen, nil), showCursor, nil)
	if _, err := s.File.Write(q); err != nil {
		return 0, err
	}
	return len(p), nil
}

// LeaveAltScreen は、旧版から alt screen のまま受け取った新版が、画面を出せずに終わるときに書く (issue 509)。
// 書かないと、シェルへ戻っても alt screen に閉じ込められ、エラーの文も暗い画面の上に紛れる。
func LeaveAltScreen(f *os.File) {
	_, _ = f.Write([]byte("\x1b[0m" + string(showCursor) + string(leaveAltScreen)))
}
