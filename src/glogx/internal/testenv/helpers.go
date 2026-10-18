package testenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
	"github.com/jiikko/dotfiles/src/termsafe/ctlprobe"

	"glogx/internal/app"
	"glogx/internal/extcmd"
)

// StubClock は app.TimeNow を固定時刻 Unix(1000, 0) に差し替え、テスト終了時に戻す。返す advance で
// 時計を進める (演出の進捗・経過時間の判定を決定的にする)。実時間に戻したいテストは従来どおり
// 自前で退避する。🚨 基準時刻を変えないこと: tui_panel_test.go の ETA fixture (StartedAt に
// Unix(880/910/940) 等) が「Unix(1000) から見た相対時間」で組まれている。
func StubClock(t *testing.T) (advance func(time.Duration)) {
	t.Helper()
	now := time.Unix(1000, 0)
	StubClockFrom(t, &now)
	return func(d time.Duration) { now = now.Add(d) }
}

// StubClockFrom は app.TimeNow を *now を返す時計に差し替え、テスト終了時に戻す。呼び出し側は *now を
// 書き換えて時刻を進める (絶対時刻や実時刻を起点にしたいとき用。起点を固定でよいなら StubClock)。
func StubClockFrom(t *testing.T, now *time.Time) {
	t.Helper()
	orig := app.TimeNow
	t.Cleanup(func() { app.TimeNow = orig })
	app.TimeNow = func() time.Time { return *now }
}

// PopupWidth は tmux popup で glogx が実際に使う幅の代表値。🚨 production はこの値を持たない
// (幅は m.contentWidth() = 端末幅から決まる)。tmux 側が端末幅の 90% を popup に割り当てる運用
// (_tmux.conf) での実測値で、「1 行に収まるか」を見るテストの基準として共有する。
// ここを変えると hint の長さの許容量が変わるので、変えたら実機で末尾が切れないか確認すること。
const PopupWidth = 84

// PinFallbackEditor は $VISUAL / $EDITOR を空にして extcmd.EditorCommand の fallback (nvim) を固定する。
//
// 🚨 起動コマンドを**完全一致**で検証するテストはこれを呼ぶ。extcmd.EditorCommand は環境変数を読むので、
// 開発者の環境 (EDITOR="code -w" 等) がそのまま期待値に混ざる。末尾引数だけ見る形にすれば環境に
// 強いが、それだと前に引数を挿し込む変異 (例: -R を足して実ファイルを readonly で開く =
// editCmd の doc が禁じている) を通してしまうので、環境を固定して完全一致で見る方を選ぶ。
func PinFallbackEditor(t *testing.T) {
	t.Helper()
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
}

// StubEditorCapture は extcmd.RunEditorCmd を「起動せず *exec.Cmd を記録する」実装へ差し替える。
//
// 🚨 エディタ連携のテストは全部これを使う。回数だけ数える stub も昔あったが、それだと
// 「何を開いたか」が見えず、渡す対象を取り違えた実装 (iss.Path → iss.Dir 等) を通してしまう。
// 差し替え点を 1 つに保つのは、extcmd.RunEditorCmd の契約 (非 nil な Cmd を返す) が変わったときに
// 直す箇所を 1 箇所にするため。
//
// エディタを起動するキーは 3 系統ある: issues viewer の e (別名 v) = その issue の実ファイル /
// git log 一覧の e = repo root を `nvim .` / job パネルの v = 標準入力の scratch。
func StubEditorCapture(t *testing.T) *[]*exec.Cmd {
	t.Helper()
	var cmds []*exec.Cmd
	orig := extcmd.RunEditorCmd
	extcmd.RunEditorCmd = func(c *exec.Cmd) tea.Cmd {
		cmds = append(cmds, c)
		return func() tea.Msg { return extcmd.EditorClosedMsg{} }
	}
	t.Cleanup(func() { extcmd.RunEditorCmd = orig })
	return &cmds
}

// NoticeOKOf は takeNotice の戻りを (文言, 成功か) に読み替える (成功 / 失敗だけを見るテスト用)。
// 失敗の種類 (app.NoticeRefused / app.NoticeError) を見るテストは takeNotice を直接読む。
func NoticeOKOf(text string, kind app.NoticeKind) (string, bool) {
	return text, kind == app.NoticeOK
}

// HasTerminalControl は github.com/jiikko/dotfiles/src/termsafe/ctlprobe の薄い別名。
//
// 🚨 判定そのものをここへ書き戻さないこと。同じオラクルが 5 箇所に複製されており、
// 無害化の定義を広げるとき 4 箇所を直し忘れても全パッケージ green のままになる。
func HasTerminalControl(s string) bool { return ctlprobe.HasControl(s) }

const (
	CSI8 = "\u009b" // 8bit CSI (C1)
	OSC8 = "\u009d" // 8bit OSC (C1)
	ST8  = "\u009c" // 8bit ST (C1)
)

// FakeWatcher は app.DirWatcher のフェイク。Add の履歴と watch 集合を観測できる。
//
// 実 fsnotify に合わせている点:
//   - 存在しないパスの Add は失敗する (watch も張られない)
//   - Close でイベントチャネルが閉じる (待っている goroutine が解ける)
//
// 合わせていない点 (明示的にテストが動かす): ディレクトリが消えたときの watch の消失は
// OS が黙ってやるので、フェイクでは loseWatch で再現する (Add の失敗契機に混ぜると
// 「Add を呼ばなくても消えている」形になり、再 Add の検査が空回りしうる)。
type FakeWatcher struct {
	mu     sync.Mutex
	added  []string // Add の呼び出し履歴 (成否に関わらず、順に全件)
	list   []string // 現に張られている watch
	closed bool
	events chan fsnotify.Event
	errors chan error
}

func NewFakeWatcher() *FakeWatcher {
	return &FakeWatcher{events: make(chan fsnotify.Event, 16), errors: make(chan error, 4)}
}

func (f *FakeWatcher) Add(dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return fsnotify.ErrClosed
	}
	f.added = append(f.added, dir)
	if _, err := os.Stat(dir); err != nil {
		return err // 消えているディレクトリは張れない
	}
	if !slices.Contains(f.list, dir) {
		f.list = append(f.list, dir)
	}
	return nil
}

func (f *FakeWatcher) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil // 冪等 (二重 Close で panic しない)
	}
	f.closed = true
	close(f.events)
	close(f.errors)
	return nil
}

func (f *FakeWatcher) WatchList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.list...)
}

func (f *FakeWatcher) Events() <-chan fsnotify.Event { return f.events }

func (f *FakeWatcher) Errors() <-chan error { return f.errors }

// LoseWatch は「ディレクトリが消えて OS が watch を黙って落とした」を再現する。
func (f *FakeWatcher) LoseWatch(dir string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.list = slices.DeleteFunc(f.list, func(s string) bool { return s == dir })
}

// AddCount は dir に対する Add の呼び出し回数 (成否を問わない)。
func (f *FakeWatcher) AddCount(dir string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, d := range f.added {
		if d == dir {
			n++
		}
	}
	return n
}

// Emit はイベントを 1 本流す (実 fsnotify の代わり)。
//
// 🚨 送信までロックを持ったままにする。closed を読んでから解錠して送ると、その隙間に Close が
// 入って「閉じたチャネルへの送信」で panic する (2000 回ループで再現)。events は
// バッファ 16 で、ドレインするのは検査対象のコードだけ (Add/Close/WatchList はチャネルを読まない)
// なので、ロックを持ったままの送信でデッドロックしない。
func (f *FakeWatcher) Emit(ev fsnotify.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.events <- ev
}

// FakeWatcherSeam は app.NewDirWatcher をフェイクへ差し替え、そのフェイクを返す。
//
// 🚨 seam は 2 つの見張り (issues / git log) の共用なので、1 つのテストで両方を起動すると
// **同じフェイクを 2 つが掴んでイベントチャネルを取り合う**。両方を動かすテストを書くなら
// フェイクを見張りごとに分けること。
func FakeWatcherSeam(t *testing.T) *FakeWatcher {
	t.Helper()
	fw := NewFakeWatcher()
	prev := app.NewDirWatcher
	app.NewDirWatcher = func() (app.DirWatcher, error) { return fw, nil }
	t.Cleanup(func() {
		app.NewDirWatcher = prev
		_ = fw.Close()
	})
	return fw
}

// 細工した値の部品 (端末への制御シーケンス。無害化のテストが外部由来の文字列に混ぜる)
const (
	EscSeq   = "\x1b"
	BelSeq   = "\a"
	OSC52Seq = EscSeq + "]52;c;cHduZWQ=" + BelSeq // クリップボード書き込み (貼った先で発火する)
	ClearSeq = EscSeq + "[2J"                     // 画面消去
)

// HasControlExceptNewline は「1 件が複数行の塊」用 (コピー文は改行が正常)。
func HasControlExceptNewline(s string) bool { return ctlprobe.HasControlExceptNewline(s) }

// PopupHintBudget は popup の幅 (PopupWidth の content 幅を出す端末) で browseModel.hintWidth が返す最下行の案内の予算。
// 画面のパッケージのテストが「代表幅の端末で案内が欠けない」を browseModel 抜きで見るのに使う。
// 🚨 正本は main の hintWidth (描画側が切る幅と同じ 1 か所)。main の TestPopupHintBudgetMatchesBrowse が
// この値と実物の一致を固定する (frame の寸法や hint の余白を変えたら、そちらが落ちてここを直す)。
const PopupHintBudget = 89

// AssertSanitizedAndVisible は無害化テストの 2 つの主張を対で確かめる:
//
//  1. 制御シーケンス / OSC の中身 (PWNED) が残っていない (本来の主張)
//  2. 🚨 **細工した値が実際に描かれている** (陽性対照。wantVisible)
//
// 2 が無いと、箱が空を返した瞬間に 1 の assert が 1 つも実行されず vacuous に緑を返す
// (実測: markNextBox の先頭に `return nil` を入れても PASS した)。
func AssertSanitizedAndVisible(t *testing.T, where string, lines []string, wantVisible ...string) {
	t.Helper()
	for _, line := range lines {
		if HasTerminalControl(line) {
			t.Errorf("%s に制御シーケンスが残った: %q", where, line)
		}
		if strings.Contains(line, "PWNED") {
			t.Errorf("%s に OSC の中身が残った: %q", where, line)
		}
	}
	joined := strings.Join(lines, "\n")
	for _, w := range wantVisible {
		if !strings.Contains(joined, w) {
			t.Fatalf("%s に %q が描かれていない (検査対象が空 = 上の assert は 1 つも sink を守っていない)",
				where, w)
		}
	}
}

// IsolateCache はキャッシュの置き場 (XDG_CACHE_HOME。画面の記憶もここ) をこのテストの一時領域へ向ける。
func IsolateCache(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
}

// IsolateScreenMemory はキャッシュの置き場を一時領域へ向け、時計を固定した時刻 (2026-10-11 12:00 の現地時刻) に止めて返す
// (画面の記憶の保存と 30 分の失効を決定的に見る)。
func IsolateScreenMemory(t *testing.T) time.Time {
	t.Helper()
	IsolateCache(t)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.Local)
	StubClockFrom(t, &now)
	return now
}
