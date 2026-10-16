package live

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"

	"mac-lan-manager/ledger"

	"atomicfile"
)

// LogMax はログのファイルの上限 (超えたら新しい方の半分だけを残す)。
const LogMax = 1 << 20

// DefaultLogPath は操作のログの置き場所 (~/Library/Logs/mac-lan-manager.log。台帳の置き場所を環境変数で差し替えているとき (テスト・
// dump) はその中: 本物のログを汚さない)。
func DefaultLogPath() (string, error) {
	if d := os.Getenv(ledger.EnvDir); d != "" {
		return filepath.Join(d, "mac-lan-manager.log"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Logs", "mac-lan-manager.log"), nil
}

// FileLog は path へ 1 行ずつ追記する口 (lanmgr.Options.Log)。書けなくても画面の操作は止めない (ログは調べるための写しで、
// 失敗を知らせると操作のたびに知らせが出る)。自分だけが読める権限で作る (相手の名前・IP・ssh のエラーを含む)。
// 受容: 2 つのプロセスが同時に書き、片方が切り詰めた瞬間に、もう片方の行が消えることがある (調べるための写しなので)。
func FileLog(path string) func(string) {
	var mu sync.Mutex
	return func(line string) {
		mu.Lock()
		defer mu.Unlock()
		if fi, err := os.Stat(path); err == nil && fi.Size() > LogMax {
			trimLog(path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		_ = f.Chmod(0o600) // 既に在るファイルが 0644 でも自分だけが読める形にする (O_CREATE の権限は作るときにしか効かない)
		_, _ = f.WriteString(line + "\n")
		_ = f.Close()
	}
}

// trimLog はログの新しい方の半分 (行の頭から) だけを残す (別のファイルへ回すと、名前の変更を safefs に通す手間に見合わない)。
func trimLog(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if len(b) <= LogMax/2 { // 別のプロセス (単体と glogx の L) が先に切り詰めた: 負の添字で panic し、TUI ごと落ちた (敵対レビュー)
		return
	}
	keep := b[len(b)-LogMax/2:]
	if i := bytes.IndexByte(keep, '\n'); i >= 0 {
		keep = keep[i+1:]
	}
	_ = atomicfile.Write(path, keep, 0o600)
}
