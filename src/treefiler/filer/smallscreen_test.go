package filer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lastRow は描いた画面の最下行。
func lastRow(m *Model) string {
	v := m.View()
	return v[len(v)-1]
}

// 板を描けない大きさ (幅 22 未満・高さ 7 未満) でも、確認の板はキーを取る。最下行に答えのキーを出さないと、見えないまま Enter でゴミ箱へ移る (issue 991)。
func TestAskBoardTooSmallShowsAnswerKeysOnBottomRow(t *testing.T) {
	m, _ := newOps(t)
	cdTo(t, m, "c.txt")
	m.Resize(21, 6)
	settle(m)
	m.HandleKey("d")
	if m.ask == nil {
		t.Fatal("d で確認の板が開かない")
	}
	if row := lastRow(m); !strings.Contains(row, "y/enter") || !strings.Contains(row, "ゴミ箱") {
		t.Errorf("最下行に確認の答え方が無い: %q", row)
	}
	m.HandleKey("n")
	if _, err := os.Lstat(filepath.Join(m.root.abs, "c.txt")); err != nil {
		t.Fatal("取り消したのに移した")
	}
}

// 高さ 7 でも、lead が 2 行に折れる幅では板の答え方の行が枠の外へ押し出される (名前は最低 1 行残す)。そのときも最下行に答えのキーを出す。
func TestAskBoardWithoutRoomForAnswerShowsAnswerKeysOnBottomRow(t *testing.T) {
	m, _ := newOps(t)
	cdTo(t, m, "c.txt")
	m.Resize(30, 7)
	settle(m)
	m.HandleKey("d")
	if row := lastRow(m); !strings.Contains(row, "y/enter") {
		t.Errorf("最下行に答えのキーが無い: %q", row)
	}
}

// 設定の板も描けない大きさでキーを取る (l / Enter で値が変わる)。最下行に板が開いていることと閉じるキーを出す。
func TestSettingsPanelTooSmallShowsItselfOnBottomRow(t *testing.T) {
	isolateConfig(t)
	m := newTest(t)
	m.Resize(21, 6)
	settle(m)
	m.HandleKey(",")
	if !m.panel.open {
		t.Fatal(", で設定の板が開かない")
	}
	if row := lastRow(m); !strings.Contains(row, "設定") || !strings.Contains(row, "esc") {
		t.Errorf("最下行に設定の板の案内が無い: %q", row)
	}
}

// 板を描ける大きさでは、最下行は今までどおり (板は中央・右端に描く)。
func TestBoardsFitKeepBottomRow(t *testing.T) {
	m, _ := newOps(t)
	cdTo(t, m, "c.txt")
	settle(m)
	before := lastRow(m)
	m.HandleKey("d")
	if row := lastRow(m); row != before {
		t.Errorf("板を描ける大きさで最下行が変わった:\n前 %q\n後 %q", before, row)
	}
}

// 読めない設定の行が多いと、足元が増えて項目の行が 1 行も入らない。そのときも落ちずに最下行へ案内を出す (rows[skip:] が範囲外になっていた)。
func TestSettingsPanelWithManyLoadErrorsDoesNotPanic(t *testing.T) {
	isolateConfig(t)
	m := newTest(t)
	m.Resize(80, 24)
	for range 30 {
		m.setErrs = append(m.setErrs, "読めない行")
	}
	settle(m)
	m.HandleKey(",")
	for range items {
		m.HandleKey("j") // 最後の項目まで動かす (選んだ行で skip が変わる)
	}
	if row := lastRow(m); !strings.Contains(row, "設定") {
		t.Errorf("最下行に設定の板の案内が無い: %q", row)
	}
}
