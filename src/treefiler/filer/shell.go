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
// 端末を明け渡すのは呼び出し側 (tea.Exec + tuikit/foreground の Exec。戻った後に前面を取り戻す)。filer は何をどこで起こすかを ExecRequest で返すだけ
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
//
// 🚨 `trap 'exit $?' EXIT` を先頭に置き、シェルに最後のコマンドを exec させない。`-i` (alias・関数を使えるように) のシェルは job control の
// ために自分のグループを作って端末の前面を取り、`-c` の最後のコマンドを exec する。exec されたコマンドが終わると前面を戻す者が
// 居なくなり、後の stty・dd と戻った treefiler / glogx が前面でないまま端末を触って固まる (外のシェルの下では SIGTTOU で止まり、
// tmux のペインで直に動く glogx では EIO で何も読めない。2026-10-11)。exec しなければ、シェルはジョブの後と自分が抜けるときに
// 前面を戻す。あわせて、実行中の Ctrl-Z で止まったジョブを待つ者が居なくなって永久に固まる形と、bash 3.2 がコマンドに
// SIGTSTP 等を無視させたまま exec する形も消える。
// 前提: シェルが EXIT の trap を持つときは最後のコマンドを exec しないこと (zsh 5.9・bash 3.2 で実測。bash 5.3 は trap が無くても
// 前面を戻す)。trap の中身を `exit $?` にするのは、zsh が errexit (`set -e` / ERR_EXIT) で抜ける経路では前面を戻す終了処理を
// 通らず、trap の中で exit したときだけ通るため (実測。rc はそのまま)。`$` は外の sh に展開させない (`\$?`。展開されると
// trap が `exit 0` になり、失敗が [done] に化ける)。fish・dash は未測定。
// 代償: ユーザーの rc が張った EXIT の trap (zsh の TRAPEXIT も) は上書きされる。コマンドの中で `trap - EXIT` すると zsh では
// 前面を持ち去る形に戻る (下の受け皿で戻る)。
// 採らなかった案: シェルの job control を切る (zsh は引数の `+m`、bash 3.2 は `-c` の先頭の `set +m` だけが効き、コマンドの中の
// `sh -ic` には効かない)、包みの中で perl に前面を取り返させる (依存が増え、trap の後は bash・zsh で差を作れない)。
//
// trap で戻らない形 (コマンドの中の `trap - EXIT`・`kill -KILL $$`・未測定のシェル) の受け皿は 2 段: キー待ちの前に前面が自分の
// グループ (= treefiler のグループ) かを ps で見て、違えば stty・dd を飛ばして戻る (前景ジョブの形では stty が SIGTTOU で止まり、
// Go まで戻れない)。前面は戻った Go が取り戻す (tuikit/foreground の Exec。issue 707)。キー待ちはその場合だけ失う。
// 制御端末が無い (tpgid が 0 以下) ときと、ps が失敗して何も出さない (両方が空で一致する) ときは従来どおり待つ (止まる相手が居ない)。
//
// キー待ちは `-isig` にする: 前面が treefiler のグループに戻った後の Ctrl-\ (SIGQUIT) は treefiler 自身を落とすので、ただのキーとして読む。
// 端末の設定は案内を出す前に済ませる (案内を見てすぐ押したキーが、設定の前に届いてシグナルにならないように)。
const waitIfQuick = `start=$(date +%s); "$1" -ic "trap 'exit \$?' EXIT
$2"; rc=$?; end=$(date +%s)
if [ $((end - start)) -lt 3 ]; then
  set -- $(ps -o tpgid=,pgid= -p $$ 2>/dev/null)
  if [ "$1" = "$2" ] || [ "${1:-0}" -le 0 ]; then
    saved=$(stty -g 2>/dev/null); stty -icanon -echo -isig 2>/dev/null
    if [ "$rc" -eq 0 ]; then printf '\n[done] 何かキーを押すと treefiler に戻ります'; else printf '\n[exit %d] 何かキーを押すと treefiler に戻ります' "$rc"; fi
    dd bs=1 count=1 >/dev/null 2>&1; stty "$saved" 2>/dev/null
  fi
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
		appendHistory(historyPath(), cmd) // 続けて同じでも足す (読むときに畳む。history.go)
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
