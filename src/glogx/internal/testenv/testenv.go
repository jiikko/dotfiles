// Package testenv は glogx のテストを持つパッケージ (main と internal の各パッケージ) が共有する TestMain の前処理と、
// 外部作用の差し替えのヘルパー。
//
// なぜ 1 か所に置くか: TestMain の前処理はパッケージ単位でしか掛からないので、テストを別のパッケージへ移すと
// 黙って外れる (doctor のテストを移したとき、幅 env のガードとキャッシュの隔離が外れて緑のまま通った)。
// パッケージごとに写すと写しがずれるので、各パッケージの TestMain はここの Main を呼ぶだけにする。
// テストを持つ internal のパッケージが Main を呼んでいることは structure_ratchet_test.go の
// TestEveryTestPackageUsesTestenv が検査する。
//
// 🚨 本番のコードから import しない (testing を import する。depguard が止める)。
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"doctor/testtmp"
	"glogx/internal/extcmd"
	"subproc"

	"github.com/jiikko/dotfiles/src/tuikit/widthenv"
)

// Main は隔離した環境で m.Run を走らせて終了する。name は一時領域の名前 (testtmp の接頭辞)。
// setup はパッケージ固有の前処理 (一時領域の dir を受け取る。nil 可)。
//
// 隔離するもの:
//   - 継承した GIT_DIR などを外す: 一時 dir に作る repo の git が別の repo を書かない
//   - 支持しない幅 env (RUNEWIDTH_EASTASIAN) では理由を 1 度だけ言って止める。真だと幅系のテストが
//     まとめて落ち、期待値の話に見えるが、実体は glogx がこの env を支持していないこと
//   - キャッシュ置き場 (XDG_CACHE_HOME) を一時領域へ: 画面の記憶・doctor の走査の記憶・CI のキャッシュは
//     app.CacheBaseDir の下に書くので、隔離しないと make test が開発者の ~/.cache/glog を書き換える
//   - treefiler (F) と mac-lan-manager (L) の設定の置き場も一時領域へ (読むと利用者の設定で結果が変わり、書くと利用者の
//     設定を書き換える。2026-10-09 に実際に書いた)
//   - GOTMPDIR も一時領域へ: Go 1.26 の t.TempDir は GOTMPDIR を優先する。repo の中のパスを指定されると「repo の外」を
//     検査するテストが glogx の git root を拾う
//   - 外部作用 (クリップボード・ブラウザ) の既定は「呼ばれたら落ちる」: 差し替えを書き忘れたテストは実クリップボードを
//     書き換え・ブラウザを開き、しかも「何も起きなかった」を検査できない。StubClipboard / StubBrowser は
//     終了時にこの既定へ戻す
//
// 🚨 後始末は m.Run の後の cleanup だけに預けない。m.Run から戻らない終了 (SIGKILL / -timeout の SIGQUIT /
// panic) では末尾に届かず残骸が残る。testtmp.Setup が起動時に前回の残骸を回収する。
func Main(m *testing.M, name string, setup func(dir string)) {
	if err := subproc.UnsetGitLocationEnv(); err != nil {
		panic(err)
	}
	widthenv.ExitIfUnsupported()
	dir, swept, cleanup, err := testtmp.Setup(name)
	if err != nil {
		panic(err) // 隔離できないまま走らせると実ユーザーのキャッシュを触る
	}
	if swept > 0 {
		fmt.Fprintf(os.Stderr, "[testtmp] 前回の残骸を %d 件回収した\n", swept)
	}
	for k, v := range map[string]string{
		"XDG_CACHE_HOME":             dir,
		"TREEFILER_CONFIG_DIR":       filepath.Join(dir, "treefiler-config"),
		"MAC_LAN_MANAGER_CONFIG_DIR": filepath.Join(dir, "mac-lan-manager-config"),
		"GOTMPDIR":                   dir,
	} {
		if err := os.Setenv(k, v); err != nil {
			panic(err)
		}
	}
	extcmd.CopyToClipboard = func(text string) error {
		panic("テストが extcmd.CopyToClipboard を差し替えずに呼んだ (testenv.StubClipboard を使う): " + text)
	}
	extcmd.OpenInBrowser = func(url string) error {
		panic("テストが extcmd.OpenInBrowser を差し替えずに呼んだ (testenv.StubBrowser を使う): " + url)
	}
	if setup != nil {
		setup(dir)
	}
	code := m.Run() // 🚨 os.Exit は defer を走らせないので、片付けは Run の後に手で書く
	cleanup()
	os.Exit(code)
}

// StubClipboardFunc は extcmd.CopyToClipboard を fn に差し替え、テスト終了時に戻す (失敗させる・複数記録する用)。
func StubClipboardFunc(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := extcmd.CopyToClipboard
	extcmd.CopyToClipboard = fn
	t.Cleanup(func() { extcmd.CopyToClipboard = orig })
}

// StubClipboard は extcmd.CopyToClipboard を「記録して成功する」実装に差し替え、記録先を返す (多数派の形)。
func StubClipboard(t *testing.T) *string {
	t.Helper()
	var copied string
	StubClipboardFunc(t, func(text string) error { copied = text; return nil })
	return &copied
}

// StubBrowserFunc は extcmd.OpenInBrowser を fn に差し替え、テスト終了時に戻す (ガード・複数記録用)。
func StubBrowserFunc(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := extcmd.OpenInBrowser
	extcmd.OpenInBrowser = fn
	t.Cleanup(func() { extcmd.OpenInBrowser = orig })
}

// StubBrowser は extcmd.OpenInBrowser を「最後に開いた URL を記録して成功する」実装に差し替え、記録先を返す。
func StubBrowser(t *testing.T) *string {
	t.Helper()
	var opened string
	StubBrowserFunc(t, func(url string) error { opened = url; return nil })
	return &opened
}
