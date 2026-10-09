package filer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jiikko/dotfiles/src/tuikit/editor"
	"github.com/jiikko/dotfiles/src/tuikit/lineedit"
)

// shell.go は `!` (選んだフォルダで 1 行のコマンドを実行) と `s` (そこでシェルを開く)。spec §5.7。
// 端末を明け渡すのは呼び出し側 (bubbletea の ExecProcess)。filer は何をどこで起こすかを ExecRequest で返すだけ
// (filer は os/exec を import しない。exec_boundary_test)。

// ExecRequest は呼び出し側に起こしてほしいプロセス。呼び出し側は Dir・Argv・Env をそのまま exec.Cmd に入れて前景で起こし、
// 戻ったら ExecDone を呼ぶ。
type ExecRequest struct {
	Dir  string   // 作業ディレクトリ (カーソルがフォルダならそれ、ファイルなら親)
	Argv []string // 起こすコマンド
	Env  []string // 環境の全部 (今の環境に f=選んだパスを足した完成形)
}

// waitIfQuick は `!` のコマンドを包む sh のスクリプト。3 秒未満で終わったら、出力を読めるようキーを待ってから戻る (spec §5.7)。
// $1 = ユーザーのシェル、$2 = コマンド行。
const waitIfQuick = `start=$(date +%s); "$1" -ic "$2"; rc=$?; end=$(date +%s)
if [ $((end - start)) -lt 3 ]; then
  if [ "$rc" -eq 0 ]; then printf '\n[done] 何かキーを押すと treefiler に戻ります'; else printf '\n[exit %d] 何かキーを押すと treefiler に戻ります' "$rc"; fi
  stty -icanon -echo 2>/dev/null; dd bs=1 count=1 >/dev/null 2>&1; stty icanon echo 2>/dev/null
fi
:` // 終了コードは最後の stty に左右させない (端末が無いと stty が失敗する)

type promptState struct {
	active  bool
	line    lineedit.Line
	history []string
	hist    int // 履歴を辿っている位置 (len(history) = 今の入力)
}

func userShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/zsh"
}

// workDir はシェルを起こすフォルダ (カーソルがフォルダならそれ、ファイルなら親)。
func (m *Model) workDir() string {
	if m.cur.dir {
		return m.cur.path()
	}
	return filepath.Dir(m.cur.path())
}

func (m *Model) execEnv() []string { return append(os.Environ(), "f="+m.cur.path()) }

// ExecDone は Exec で頼んだプロセスから戻った合図。シェルで作った・消したファイルを出し、起動できなかったとき
// (作業フォルダが消えた・$SHELL やエディタが実行できない) だけ知らせの文言を返す。終了コードは失敗と言わない
// (シェルは最後のコマンドの終了コードで抜けるのが普通。glogx の editorClosedMsg と同じ判断)。
func (m *Model) ExecDone(err error) (warning string) {
	m.Refresh()
	var exitErr interface{ ExitCode() int } // *exec.ExitError (os/exec は import しない。exec_boundary_test)
	if err == nil || errors.As(err, &exitErr) {
		return ""
	}
	return "起動できませんでした: " + termsafeLine(err.Error())
}

// requestEdit は path をエディタで開く (木の `e` がファイルの上のとき・タイルの `e`)。エディタの選び方は tuikit/editor
// ($VISUAL → $EDITOR → nvim)。起こすフォルダはファイルの親 (エディタの中の相対パスと :e がそこを起点にする)。
// $f は開くファイル (タイル・ジャンプの e ではカーソルと違う)。
func (m *Model) requestEdit(path string) {
	m.exec = &ExecRequest{Dir: filepath.Dir(path), Argv: editor.Argv(path, nil), Env: append(os.Environ(), "f="+path)}
}

// requestShell は `s` (そのフォルダで対話のシェル)。
func (m *Model) requestShell() {
	m.exec = &ExecRequest{Dir: m.workDir(), Argv: []string{userShell(), "-i"}, Env: m.execEnv()}
}

// promptKey は `!` の入力中のキー。lineedit の編集キーを先に渡し、Enter / Esc / 履歴だけを自分で捌く。
func (m *Model) promptKey(key, text string) {
	p := &m.prompt
	switch key {
	case "enter":
		cmd := p.line.String()
		p.active = false
		if strings.TrimSpace(cmd) == "" { // 空白だけでは走らせない
			return
		}
		if n := len(p.history); n == 0 || p.history[n-1] != cmd {
			p.history = append(p.history, cmd)
		}
		m.exec = &ExecRequest{Dir: m.workDir(), Argv: []string{"/bin/sh", "-c", waitIfQuick, "treefiler", userShell(), cmd}, Env: m.execEnv()}
	case "esc", "ctrl+c":
		p.active = false
	case "up":
		if p.hist > 0 {
			p.hist--
			p.line.Reset()
			p.line.Insert(p.history[p.hist])
		}
	case "down":
		if p.hist < len(p.history) {
			p.hist++
			p.line.Reset()
			if p.hist < len(p.history) {
				p.line.Insert(p.history[p.hist])
			}
		}
	case "backspace":
		if p.line.Empty() {
			p.active = false // 空欄の backspace で閉じる
			return
		}
		p.line.Key(key, text)
	default:
		p.line.Key(key, text)
	}
}

func (m *Model) startPrompt() {
	m.prompt.active = true
	m.prompt.line.Reset()
	m.prompt.hist = len(m.prompt.history)
}

// TakeExec は起こしてほしいプロセスを取り出す (無ければ ok=false)。
func (m *Model) TakeExec() (ExecRequest, bool) {
	if m.exec == nil {
		return ExecRequest{}, false
	}
	r := *m.exec
	m.exec = nil
	return r, true
}

// Paste は貼り付け (bracketed paste) を入力欄に入れる。入力欄が無いときは捨てる: キーとして解釈すると、
// 貼った文字列が 1 字ずつ木のキーとして走る (glogx-ui-guide §7)。改行・タブは空白に、制御文字は落とす (lineedit.Insert)。
func (m *Model) Paste(text string) {
	switch {
	case m.search.active:
		m.search.line.Insert(text)
		m.refreshMatches()
	case m.prompt.active:
		m.prompt.line.Insert(text)
	}
}
