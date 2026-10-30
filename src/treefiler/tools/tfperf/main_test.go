package main

import (
	"strings"
	"testing"
)

const ms = int64(1e6)

// 送った打鍵とログを組にして、届くまで・最初の書き込み・次のフレーム・止まるまでを、key の Update が始まった時刻から測る。
func TestAnalyzeMeasuresFromKeyUpdateStart(t *testing.T) {
	sends := []send{{t: 0, done: 40 * ms, key: "j"}} // send-keys が戻るのは Update より後 (tmux のクライアントの終わり)
	evs := []event{
		{t: 9 * ms, kind: "update", ns: 1 * ms, msg: "key", key: "j", anim: true}, // 8ms に始まり 9ms に終わる
		{t: 10 * ms, kind: "view", ns: 1 * ms, bytes: 100},
		{t: 12 * ms, kind: "write", ns: 1 * ms, bytes: 50},
		{t: 26 * ms, kind: "update", ns: 1 * ms, msg: "tick", key: "-", anim: true}, // 25ms に始まる = キーの 16ms 後
		{t: 300 * ms, kind: "update", ns: 1 * ms, msg: "tick", key: "-", anim: false},
	}
	st, err := analyze(evs, sends)
	if err != nil {
		t.Fatal(err)
	}
	got := st[0]
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"deliver", got.deliver, 8},
		{"handle", got.handle, 1},
		{"draw", got.draw, 4},
		{"nextFrame", got.nextFrame, 16},
		{"settle", got.settle, 292},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if got.frames != 1 || got.bytes != 50 {
		t.Errorf("frames=%d bytes=%d, want 1 と 50", got.frames, got.bytes)
	}
}

// 動きの始まらない打鍵は次のフレームを測らず、その場で止まったとする。
func TestAnalyzeKeyWithoutMotion(t *testing.T) {
	sends := []send{{t: 0, done: 1, key: "j"}}
	evs := []event{
		{t: 5 * ms, kind: "update", ns: 1 * ms, msg: "key", key: "j", anim: false},
		{t: 30 * ms, kind: "update", ns: 1 * ms, msg: "tick", key: "-", anim: false},
	}
	st, err := analyze(evs, sends)
	if err != nil {
		t.Fatal(err)
	}
	if st[0].nextFrame != -1 || st[0].settle != 1 {
		t.Fatalf("nextFrame=%v settle=%v, want -1 と 1 (キーの Update が終わった時点で止まっている)", st[0].nextFrame, st[0].settle)
	}
}

// 次の打鍵を送った後のログは前の打鍵に数えない。
func TestAnalyzeWindowEndsAtNextSend(t *testing.T) {
	sends := []send{{t: 0, done: 1, key: "j"}, {t: 100 * ms, done: 101 * ms, key: "k"}}
	evs := []event{
		{t: 5 * ms, kind: "update", ns: 1 * ms, msg: "key", key: "j", anim: true},
		{t: 104 * ms, kind: "write", ns: 1 * ms, bytes: 70},
		{t: 106 * ms, kind: "update", ns: 1 * ms, msg: "key", key: "k", anim: true},
	}
	st, err := analyze(evs, sends)
	if err != nil {
		t.Fatal(err)
	}
	if st[0].frames != 0 || st[0].draw != -1 {
		t.Fatalf("次の打鍵の後の書き込みを前の打鍵に数えた: frames=%d draw=%v", st[0].frames, st[0].draw)
	}
}

// 送った数とログの key の数が合わなければ判定不能にする (最後の q だけは送った記録に無いので許す)。
func TestAnalyzeRejectsMismatchedKeys(t *testing.T) {
	sends := []send{{t: 0, done: 1, key: "j"}, {t: 10, done: 11, key: "j"}}
	one := []event{{t: 5, kind: "update", ns: 1, msg: "key", key: "j", anim: false}}
	if _, err := analyze(one, sends); err == nil || !strings.Contains(err.Error(), "判定不能") {
		t.Fatalf("取りこぼしを判定不能にしない: %v", err)
	}
	withQ := []event{
		{t: 5, kind: "update", ns: 1, msg: "key", key: "j"},
		{t: 15, kind: "update", ns: 1, msg: "key", key: "j"},
		{t: 25, kind: "update", ns: 1, msg: "key", key: "q"},
	}
	if _, err := analyze(withQ, sends); err != nil {
		t.Fatalf("閉じる q を数え違えた: %v", err)
	}
}

func TestParseKeys(t *testing.T) {
	got, err := parseKeys("j*2 Down k")
	if err != nil || strings.Join(got, ",") != "j,j,Down,k" {
		t.Fatalf("got %v %v", got, err)
	}
	for _, bad := range []string{"", "j*0", "j*x"} {
		if _, err := parseKeys(bad); err == nil {
			t.Errorf("%q を通した", bad)
		}
	}
}

// 打鍵の名前がログの key と合わなければ判定不能にする (数が合っていても 1 つずれた組み合わせで正しそうな数字を出さない)。
func TestAnalyzeRejectsShiftedKeys(t *testing.T) {
	sends := []send{{t: 0, done: 1, key: "j"}, {t: 10, done: 11, key: "k"}}
	evs := []event{
		{t: 5, kind: "update", ns: 1, msg: "key", key: "k"},
		{t: 15, kind: "update", ns: 1, msg: "key", key: "j"},
	}
	if _, err := analyze(evs, sends); err == nil || !strings.Contains(err.Error(), "判定不能") {
		t.Fatalf("名前の食い違いを通した: %v", err)
	}
	down := []send{{t: 0, done: 1, key: "Down"}, {t: 10, done: 11, key: "C-n"}}
	ok := []event{
		{t: 5, kind: "update", ns: 1, msg: "key", key: "down"},
		{t: 15, kind: "update", ns: 1, msg: "key", key: "ctrl+n"},
	}
	if _, err := analyze(ok, down); err != nil {
		t.Fatalf("tmux と bubbletea の名前の違いを食い違いと読んだ: %v", err)
	}
}

// キーの後の最初の View が終わる前に書き始めた書き込みは前のフレームなので、描画にも frames・bytes にも数えない。
func TestAnalyzeDrawIgnoresWriteStartedBeforeKeyView(t *testing.T) {
	sends := []send{{t: 0, done: 1, key: "j"}}
	evs := []event{
		{t: 10 * ms, kind: "update", ns: 2 * ms, msg: "key", key: "j", anim: true}, // 8〜10ms
		{t: 11 * ms, kind: "write", ns: 4 * ms, bytes: 10},                         // 7ms に書き始めた前のフレーム
		{t: 12 * ms, kind: "write", ns: 1 * ms, bytes: 30},                         // 11ms (Update の後・View の前) に書き始めた前のフレーム
		{t: 13 * ms, kind: "view", ns: 2 * ms, bytes: 100},                         // キーの後の View が 13ms に終わる
		{t: 20 * ms, kind: "write", ns: 1 * ms, bytes: 20},                         // 19ms に書き始めたキーの描画
	}
	st, err := analyze(evs, sends)
	if err != nil {
		t.Fatal(err)
	}
	if st[0].draw != 12 || st[0].frames != 1 || st[0].bytes != 20 {
		t.Fatalf("draw=%v frames=%d bytes=%d, want 12・1・20 (前のフレームの書き込みを数えた)", st[0].draw, st[0].frames, st[0].bytes)
	}
}

// tmux の名前を bubbletea の名前へ写す (写し間違いは判定不能で止まるので、使いうる名前を固定する)。
func TestTeaKeyName(t *testing.T) {
	for tmux, want := range map[string]string{
		"j": "j", "J": "J", "Down": "down", "Enter": "enter", "Space": "space", "Escape": "esc", "BSpace": "backspace",
		"PPage": "pgup", "NPage": "pgdown", "DC": "delete", "IC": "insert", "BTab": "shift+tab", "F1": "f1",
		"C-n": "ctrl+n", "c-n": "ctrl+n", "M-j": "alt+j", "m-j": "alt+j", "S-Up": "shift+up", "C-[": "esc", "C-m": "enter", "C-i": "tab",
	} {
		if got := teaKeyName(tmux); got != want {
			t.Errorf("teaKeyName(%q) = %q, want %q", tmux, got, want)
		}
	}
}

// 画面のファイルの名前: 打鍵の番号を 3 桁で揃え、キーの記号はファイル名に使える文字へ寄せる。
func TestSnapName(t *testing.T) {
	for _, c := range []struct {
		i         int
		key, want string
	}{
		{0, "start", "000-start.txt"},
		{12, "C-n", "012-C_n.txt"},
		{3, "/", "003-_.txt"},
	} {
		if got := snapName(c.i, c.key); got != c.want {
			t.Errorf("snapName(%d, %q) = %q, want %q", c.i, c.key, got, c.want)
		}
	}
}
