package ui

// 画面の中継 (issue 443) の受け口。描いたままの文字と画面の状態を、描くたびに渡す (書き出しは package relay。ui はファイルに触らない)。

// FrameSink は描いた 1 枚を受け取る。🚨 待たないこと (View の中で呼ぶ。relay.Writer.Put は最新の 1 枚を置いて戻るだけ)。
type FrameSink func(ansi string, width, height int, state map[string]string)

// SetFrameSink は中継の受け口を付ける (nil なら中継しない)。
func (m *Model) SetFrameSink(f FrameSink) { m.frameSink = f }

// relayState は中継に載せる画面の状態 (外から読む人が「どの画面のどこを見ているか」を知るため)。
func (m *Model) relayState() map[string]string {
	mode := "board"
	switch m.mode {
	case modeInput:
		mode = "input"
	case modeConfirm:
		mode = "confirm"
	case modeBoard:
	}
	st := map[string]string{"mode": mode, "tab": m.tab, "selected": m.selected}
	if m.drawerShown() {
		st["drawer"] = m.drawerCard
	}
	return st
}
