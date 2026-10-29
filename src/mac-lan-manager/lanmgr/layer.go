package lanmgr

// layer は画面に重なる層 (キーを受け・最下行の案内を決め・板を描く 1 枚)。
type layer int

const (
	layerList          layer = iota // 一覧
	layerFilter                     // 一覧の絞り込みの入力欄
	layerDetail                     // 詳細のパネル
	layerSettings                   // 設定 (帯の入力欄も中に含む)
	layerSetup                      // 初期設定
	layerSetupProgress              // 初期設定の進捗のモーダル (キーは初期設定の画面が受ける)
	layerHelp
	layerSpeedAsk // 測る経路を選ぶ板
	layerAlias    // 別名・ユーザー名の入力欄
	layerLog      // メッセージ一覧
	layerCopyIP   // この Mac の IP のコピー
	layerConfirm  // 確認の板
	layerSpeedProgress
)

// top は一番上の層。キー (HandleInput)・最下行 (hints)・板 (overlayBox)・全キーを取るか (OwnsKeys) が同じ答えを見る
// (それぞれが独立したフラグを別の順で読んでいたので、層を足すたびにどれかが数え漏れた: OwnsKeys が測定の進捗を数えず、
// glogx の横断キーがモーダルの裏で板を閉じた)。足りない画面 (small) は層ではない: 描画とキーを差し替えるだけで、下の層は開いたまま。
// 同時に立たない組 (測定の進捗と初期設定の進捗、絞り込みと詳細のパネル) は、片方の間はもう片方を開くキーが届かないことで保たれる。
func (m *Model) top() layer {
	switch {
	case m.speedShown():
		return layerSpeedProgress
	case m.progressShown():
		return layerSetupProgress
	}
	switch m.ov {
	case ovConfirm:
		return layerConfirm
	case ovCopyIP:
		return layerCopyIP
	case ovLog:
		return layerLog
	case ovAlias:
		return layerAlias
	case ovSpeed:
		return layerSpeedAsk
	case ovHelp:
		return layerHelp
	case ovNone:
	}
	switch {
	case m.sc == scSetup && m.su != nil:
		return layerSetup
	case m.sc == scSettings:
		return layerSettings
	case m.detailOpen:
		return layerDetail
	case m.filtering:
		return layerFilter
	}
	return layerList
}
