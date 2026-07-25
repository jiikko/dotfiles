package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// macismCurrentWarn は macism (引数なし) の出力/エラーを (prev, warn) に分類する純関数。
func TestMacismCurrentWarn(t *testing.T) {
	// 正常: 現在ソース ID が取れれば prev にセット、warn なし
	prev, warn := macismCurrentWarn([]byte("com.apple.inputmethod.Kotoeri.Japanese\n"), nil)
	if prev != "com.apple.inputmethod.Kotoeri.Japanese" || warn != "" {
		t.Fatalf("正常系: prev=%q warn=%q", prev, warn)
	}

	// #2: exit 0 だが出力が空 → 「返さなかった」警告 (silent no-op にしない)
	prev, warn = macismCurrentWarn([]byte("  \n"), nil)
	if prev != "" || warn == "" || !strings.Contains(warn, "macism") {
		t.Fatalf("空出力で警告が出ない: prev=%q warn=%q", prev, warn)
	}

	// #3: 非ゼロ終了 + stderr → stderr の中身を詳細に含める ("exit status N" より有用)
	ee := &exec.ExitError{ProcessState: nil, Stderr: []byte("macism: no such input source\n")}
	prev, warn = macismCurrentWarn(nil, ee)
	if prev != "" || !strings.Contains(warn, "no such input source") {
		t.Fatalf("stderr が詳細に含まれない: warn=%q", warn)
	}

	// stderr が無いエラーは err.Error() (exit status 等) にフォールバック
	prev, warn = macismCurrentWarn(nil, errors.New("exec: \"macism\": executable file not found"))
	if prev != "" || warn == "" || !strings.Contains(warn, "取得に失敗") {
		t.Fatalf("stderr 無しエラーで警告が出ない: warn=%q", warn)
	}
}

// finish は問い合わせ結果 (imeCurrent) の 4 分類を切替 fork の有無へ写す。
// 問い合わせを先出しにした分、この分岐が唯一の合流点になるので網羅しておく。
func TestIMESwitchFinishClassifies(t *testing.T) {
	handle := func(cur imeCurrent) *imeSwitch {
		ch := make(chan imeCurrent, 1)
		ch <- cur
		return &imeSwitch{ch: ch}
	}
	// 存在しない cli を渡す: 切替 fork を試みたら warn が出るので「試みなかった」ことが観測できる
	const bogusCLI = "/nonexistent/macism"

	t.Run("nil ハンドル (非 TTY / --no-pager 経路) は no-op", func(t *testing.T) {
		var s *imeSwitch
		restore, warn := s.finish()
		if warn != "" {
			t.Errorf("warn=%q; want 空", warn)
		}
		restore() // panic しないこと
	})
	t.Run("macism 未導入は無警告 no-op (案内は起動時チェック側)", func(t *testing.T) {
		_, warn := handle(imeCurrent{}).finish()
		if warn != "" {
			t.Errorf("warn=%q; want 空 (二重通知しない)", warn)
		}
	})
	t.Run("問い合わせ失敗は warn をそのまま伝える", func(t *testing.T) {
		_, warn := handle(imeCurrent{cli: bogusCLI, warn: "macism の現在の入力ソース取得に失敗しました: boom"}).finish()
		if !strings.Contains(warn, "boom") {
			t.Errorf("warn=%q; want 取得失敗の詳細", warn)
		}
	})
	t.Run("既に英数なら切替 fork を試みない", func(t *testing.T) {
		_, warn := handle(imeCurrent{cli: bogusCLI, prev: asciiInputSource}).finish()
		if warn != "" {
			t.Errorf("warn=%q; want 空 (切替を試みたら bogus cli で warn が出る)", warn)
		}
	})
	t.Run("切替失敗は理由付きで warn", func(t *testing.T) {
		_, warn := handle(imeCurrent{cli: bogusCLI, prev: "com.apple.inputmethod.Kotoeri.Japanese"}).finish()
		if !strings.Contains(warn, "切替に失敗") {
			t.Errorf("warn=%q; want 切替失敗", warn)
		}
	})
}

// beginIMESwitch は macism が PATH に無くても goroutine を詰まらせず、無警告 no-op で join できる。
func TestBeginIMESwitchWithoutMacism(t *testing.T) {
	t.Setenv("PATH", "")
	restore, warn := beginIMESwitch().finish()
	if warn != "" {
		t.Errorf("未導入で warn=%q; want 空", warn)
	}
	restore()
}
