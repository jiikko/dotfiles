package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// tmux の中の attach (issue 527 の案 A。2026-09-26 にユーザーが決めた): 端末を丸ごと claude attach に渡さず、pro-con の画面の上に
// tmux の popup (display-popup) の窓を開いて、その中で attach する。窓の枠の見出しに pro-con が用意した戻る操作を出す。
//
// 🚨 popup の中では tmux の bind が効かない (root も prefix も、キーは全部 popup の中身へ渡る。隔離 tmux 3.7b で確かめた)。
// そこで popup の中で pro-con 専用の入れ子の tmux サーバ (一時ディレクトリの socket・ユーザーの設定を読まない) を起こし、その中で
// attach する。戻るキーはその入れ子のサーバの bind (kill-server) にする。本番の tmux サーバには bind を足さず、attach が終われば
// 入れ子のサーバごと消える。kill-server で終わるのは attach の接続 (claude attach) だけで、PG の session は動き続ける。
//
// popup の間、pro-con の画面は下で描き続ける (キーは popup が受けるので pro-con には届かない)。display-popup -E は窓が閉じるまで
// 戻らないので、裏の処理 (child) で待ち、戻ったら端末を渡した attach と同じ知らせ (attachDoneMsg) を届ける。

// PopupDirPrefix は入れ子のサーバの一時ディレクトリの名前の頭 (pro-con attach --leave が ps からこれで見つける)。
const PopupDirPrefix = "pro-con-attach-"

// 戻るキーは 2 つ (2026-09-26 にユーザーが選んだ): 外の tmux の prefix に続けて d (普段の detach の手癖) と Ctrl+Z (tmux の外と同じ)。
// prefix は固定で書かず、attach のたびに外の tmux から読む (tmux show -gv prefix)。外の tmux の bind は popup の間効かないので、
// prefix + d で外のセッションが detach されることはない (隔離 tmux で確かめた)。popup の中の Ctrl+Z は入れ子の tmux が受けるので、
// claude attach には届かない (suspend の信号にもならない)。
const popupLeaveKey = "C-z"

// popupBorder は窓の枠の色 (pro-con の現在地の色 202。回答フォーム・送る前の確認の枠と揃える。見本からユーザーが選んだ)。
const popupBorder = "fg=colour202"

// PopupAttach は tmux の popup で attach を開く設定。
type PopupAttach struct {
	Tmux string // tmux の実体のパス
}

// TmuxPopup は tmux の中なら popup の設定を返す (外なら nil。端末を渡す今の attach を使う)。
func TmuxPopup() *PopupAttach {
	if os.Getenv("TMUX") == "" {
		return nil
	}
	p, err := exec.LookPath("tmux")
	if err != nil {
		return nil
	}
	return &PopupAttach{Tmux: p}
}

// outerPrefix は外の tmux の prefix (tmux のキー名。読めない・None なら "")。
func (p *PopupAttach) outerPrefix() string {
	out, err := exec.Command(p.Tmux, "show", "-gv", "prefix").Output()
	if k := strings.TrimSpace(string(out)); err == nil && k != "None" {
		return k
	}
	return ""
}

// popupTitle は窓の枠の見出し (見本からユーザーが選んだ長い形)。prefix が "" なら Ctrl+Z だけを出す。
func popupTitle(cardID, prefix string) string {
	keys := "Ctrl+Z"
	if prefix != "" {
		keys = prefix + " d / Ctrl+Z"
	}
	return " " + cardID + " の PG に attach 中 ─ " + keys + " で pro-con に戻る (PG は動き続ける) "
}

// UsePopupAttach は tmux の中で attach を popup で開くようにする (nil なら端末を渡す)。
func (m *Model) UsePopupAttach(p *PopupAttach) { m.popup = p }

// popupConf は入れ子のサーバの設定。キーは戻るキー (prefix + d・Ctrl+Z) だけにし、他は全部 claude attach へ渡す。
// prefix を 2 回押すと prefix のキーそのものを claude へ送る (C-t は Claude Code も使う)。prefix が "" なら prefix を外す。
func popupConf(prefix string) string {
	lines := []string{
		"set -g status off",
		"set -g destroy-unattached on", // popup が外から閉じられても (client が消えても) attach を残さない
		"set -g exit-empty on",
		"set -g prefix None",
		"set -g prefix2 None",
		"set -g escape-time 0",
		"set -g extended-keys on", // Shift+Enter などを claude へ渡す (外の tmux が送ってくる形を落とさない)
		"set -g focus-events on",
		"unbind -a",
		"bind -n " + popupLeaveKey + " kill-server",
	}
	if prefix != "" { // キー名は引用する (# はそのままだと注釈の始まりに読まれ、conf 全体が効かなくなる。敵対的レビューで再現)
		q := tmuxQuote(prefix)
		lines = append(lines, "set -g prefix "+q, "bind d kill-server", "bind "+q+" send-prefix")
	}
	return strings.Join(append(lines, ""), "\n")
}

// tmuxQuote は tmux の設定の 1 語として k を引用する (' を含まなければ '...'、含めば "..." で \ と " と $ を逃がす)。
func tmuxQuote(k string) string {
	if !strings.Contains(k, "'") {
		return "'" + k + "'"
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`).Replace(k) + `"`
}

// command は c を popup の中の入れ子のサーバで走らせる tmux のコマンドを組む (dir は一時ディレクトリ)。
// popup の中身は外の tmux サーバの環境で起きるので、c の作業場所と環境 (Env を決めていれば) を -d / -e で渡す。
func (p *PopupAttach) command(c *exec.Cmd, cardID, prefix, dir string) *exec.Cmd {
	args := []string{"display-popup", "-E", "-w", "90%", "-h", "90%", "-S", popupBorder, "-T", popupTitle(cardID, prefix)}
	if c.Dir != "" {
		args = append(args, "-d", c.Dir)
	}
	for _, e := range c.Env {
		args = append(args, "-e", e)
	}
	// 入れ子の tmux は TMUX があると入れ子を断るので落とす (popup の中身には外の tmux の TMUX が付く)
	args = append(args, "--", "/usr/bin/env", "-u", "TMUX", "-u", "TMUX_PANE",
		p.Tmux, "-S", filepath.Join(dir, "sock"), "-f", filepath.Join(dir, "conf"), "new-session", "--", c.Path)
	return exec.Command(p.Tmux, append(args, c.Args[1:]...)...)
}

// run は popup で c を走らせ、窓が閉じるまで待つ (裏の処理から呼ぶ)。窓の閉じ方 (戻るキー・claude attach の終了) は問わず nil を返す
// (戻るキーの kill-server で入れ子の tmux は 0 以外で終わるので、終わり方を失敗として出さない)。popup を開けなかったときだけ失敗を返す。
func (p *PopupAttach) run(c *exec.Cmd, cardID string) error {
	dir, err := os.MkdirTemp("", PopupDirPrefix)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }() // tmux はサーバが終わっても socket を消さない
	prefix := p.outerPrefix()
	if err := os.WriteFile(filepath.Join(dir, "conf"), []byte(popupConf(prefix)), 0o600); err != nil {
		return err
	}
	out, err := p.command(c, cardID, prefix, dir).CombinedOutput()
	var exit *exec.ExitError
	switch msg := strings.TrimSpace(string(out)); {
	case err == nil:
		return nil
	case !errors.As(err, &exit): // tmux を起動できない
		return fmt.Errorf("tmux の popup を開けない: %w", err)
	case msg != "": // display-popup 自身が断った (client が無い等)
		return fmt.Errorf("tmux の popup を開けない: %s", msg)
	}
	return nil // 窓の中身が 0 以外で終わった (戻るキーの kill-server)
}
