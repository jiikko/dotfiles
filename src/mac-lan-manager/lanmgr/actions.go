package lanmgr

import (
	"cmp"
	"slices"
)

// rowAction は行の Mac への動作のキー 1 つ。refuse は断る理由 ("" なら効く) で、キー (keyList)・最下行の案内 (rowHints)・
// 詳細のカードの keycap が同じ refuse を見る (案内とキーを別の条件で書くと、案内しないのに効く・案内するのに断るがずれて出た)。
// refuse は描画のたびに呼ぶので、知らせ・状態の書き換えをしない。
type rowAction struct {
	key   string
	label func(m *Model) string // 最下行の案内の語 (nil なら案内しない)
	prio  int
	// needsNet は、この Mac のネットワークを読めるまで断る (案内もこの間は出さない。読む前は有線・繋ぐ先・登録する内部 IP を誤って分類する)
	needsNet bool
	inDetail bool // 詳細のパネルの中でも一覧と同じに効く
	refuse   func(m *Model, r row) string
	run      func(m *Model, r row) Action
}

func fixed(s string) func(*Model) string { return func(*Model) string { return s } }

// rowActions は表の順が最下行の案内の順 (hintfit は元の並び順で繋ぐ)。
var rowActions = []rowAction{
	{key: "s", label: fixed("繋ぐ"), prio: 3, needsNet: true, inDetail: true, // シェル (treefiler の s と同じ動詞)
		refuse: func(m *Model, r row) string { return m.sshRefusal(r) },
		run: func(m *Model, r row) Action {
			// known_hosts を IP でなく台帳の鍵に結び付ける (IP を付け替えても「鍵が変わった」にしない)
			m.handOff(ExecRequest{Argv: []string{"ssh", "-o", "HostKeyAlias=" + r.host, "--", m.login(r, m.target(r))}}, nil)
			return Exec
		}},
	{key: "Y", label: fixed("コピー"), prio: 4, needsNet: true, inDetail: true, // コピー (整形した参照 = コマンド)
		refuse: func(m *Model, r row) string {
			if why := selfRefusal(r); why != "" {
				return why
			}
			if user, _ := m.userFor(r); user == "" {
				return noUserNote
			}
			return connectRefusal(r)
		},
		run: func(m *Model, r row) Action {
			m.copy("ssh "+m.login(r, m.target(r)), guessNote(m, r))
			return None
		}},
	{key: "o", label: fixed("画面共有"), prio: 5, needsNet: true, inDetail: true, // 外のアプリで開く (§2)
		refuse: vncRefusal,
		run: func(m *Model, r row) Action {
			m.handOff(ExecRequest{Argv: []string{"open", "vnc://" + m.target(r)}, ExitIsFailure: true}, nil)
			return Exec
		}},
	{key: "a", label: fixed("SyakeScreen"), prio: 11, needsNet: true, inDetail: true, // SyakeScreen (screen-share-app) で開く (issue 848)。宛先は o と同じ
		refuse: func(m *Model, r row) string {
			if why := vncRefusal(m, r); why != "" {
				return why
			}
			if user, _ := m.userFor(r); user == "" { // SyakeScreen は user の無い URL を断る (一覧の鍵が user@host なので、省くと同じ Mac の 2 本目になる)
				return noUserNote
			}
			return ""
		},
		run: func(m *Model, r row) Action {
			user, guessed := m.userFor(r)
			if guessed {
				m.note("SyakeScreen で開く"+guessNote(m, r), false)
			}
			// 入っていなければ open が失敗し、知らせに出る
			m.handOff(ExecRequest{Argv: []string{"open", syakeScreenURL(m.target(r), user)}, ExitIsFailure: true}, nil)
			return Exec
		}},
	{key: "y", label: fixed("smb"), prio: 6, needsNet: true, inDetail: true, // コピー (素の値 = URL。Finder の「サーバへ接続」に貼る)
		refuse: smbRefusal,
		run: func(m *Model, r row) Action {
			m.copy(m.smbURL(r), "")
			return None
		}},
	{key: "O", label: fixed("Finder で繋ぐ"), prio: 12, needsNet: true, inDetail: true, // Finder で smb に繋ぐコマンドのコピー。o の強い版ではない例外 (docs/glogx-ui-guide.md §2)
		refuse: smbRefusal,
		run: func(m *Model, r row) Action {
			m.copy("open "+m.smbURL(r), "")
			return None
		}},
	{key: "t", label: func(m *Model) string {
		if m.speedRun != nil {
			return "測定の進捗"
		}
		return "速度"
	}, prio: 9, needsNet: true, inDetail: true, // 通信速度を測る (speed.go。確認の板を通す。測定中はどの行でも進捗を開き直す)
		refuse: func(m *Model, r row) string { return m.speedRefusal(r) },
		run: func(m *Model, r row) Action {
			m.askSpeed(r)
			return None
		}},
	{key: "p", label: fixed("疎通"), prio: 10, needsNet: true, inDetail: true, // 疎通を確かめる (reach.go。板は通さない。確かめている途中なら終わったら知らせる)
		refuse: func(m *Model, r row) string { return m.reachRefusal(r) },
		run: func(m *Model, r row) Action {
			m.startReach(r)
			return None
		}},
	{key: "space", needsNet: true, inDetail: true, // 台帳への登録 / 削除 (Space = 印を付ける。ガイド §2)。一覧では半ページの送りより先に捌く
		run: func(m *Model, r row) Action {
			m.askLedger(r)
			return None
		}},
	{key: "m", inDetail: true, // 名前の変更 (treefiler と同じ動詞)
		run: func(m *Model, r row) Action {
			m.openEdit(r, aliasEdit, r.alias)
			return None
		}},
	{key: "u", inDetail: true, // ssh のユーザー名 (台帳の user)。欄は台帳の値で開く (推測の名前で開くと、そのまま enter で台帳に固定された。敵対レビュー)
		run: func(m *Model, r row) Action {
			m.openEdit(r, userEdit, r.user)
			return None
		}},
}

// rowActionOf は key の動作 (行に効くキーでなければ nil)。
func rowActionOf(key string) *rowAction {
	if i := slices.IndexFunc(rowActions, func(a rowAction) bool { return a.key == key }); i >= 0 {
		return &rowActions[i]
	}
	return nil
}

// actionOK は r で key の動作が効くか (案内・keycap を出すか)。行の無いタブ (current が空の行) では効かない: keyList が
// 表より先に「このタブには Mac が無い」で断る (測定中の t のように行を見ずに通す述語があり、行が無くても案内した)。
func (m *Model) actionOK(key string, r row) bool {
	a := rowActionOf(key)
	return a != nil && (r.self || r.host != "") && (a.refuse == nil || a.refuse(m, r) == "")
}

// runAction は r に a を効かせる (断るなら理由を知らせる)。
func (m *Model) runAction(a *rowAction, r row) Action {
	if a.refuse != nil {
		if why := a.refuse(m, r); why != "" {
			m.note(why, false)
			return None
		}
	}
	return a.run(m, r)
}

// rowActionKeys は表から f が真の動作のキーを集め、extra を足す。
func rowActionKeys(f func(rowAction) bool, extra ...string) []string {
	var ks []string
	for _, a := range rowActions {
		if f(a) {
			ks = append(ks, a.key)
		}
	}
	return append(ks, extra...)
}

var (
	// needsNet は、この Mac のネットワークを読めるまで断るキー (最下行の案内もこの間は出さない)。c はこの Mac の IP のコピー。
	// 初期設定 (詳細のパネルの enter) も keyDetail が同じ判定 (netRefused) で断る: 読む前に開くと相手のメイン IP が分からず、
	// 宛先が台帳の有線の IP のまま閉じるまで固まった。一覧の enter (詳細のパネルを開く) は読めていなくても開ける
	needsNet = rowActionKeys(func(a rowAction) bool { return a.needsNet }, "c")
	// rowKeys はカーソルの行の Mac に効くキー (見ていた Mac が一覧から消えた・タブに行が無いときは断る)。c は行に効かない
	rowKeys = rowActionKeys(func(rowAction) bool { return true }, "enter")
	// detailKeys は詳細のパネルの中で一覧と同じに効くキー (カーソルの行の Mac への動作。パネルは開いたまま)
	detailKeys = rowActionKeys(func(a rowAction) bool { return a.inDetail }, "?", "e")
	// listKeys は一覧が使うキー (Binds。移動の語彙は別に見る)
	listKeys = rowActionKeys(func(rowAction) bool { return true }, "e", "c", ",", "/", "?", "r", "enter", "tab", "shift+tab", "h", "l", "left", "right")
)

// rowHints は行の Mac への動作のキーの案内 (一覧と詳細のパネルが共有する)。キーと同じ refuse で決める。
func (m *Model) rowHints(r row) []keyHint {
	var hs []keyHint
	for _, a := range rowActions {
		if a.label != nil && m.actionOK(a.key, r) {
			hs = append(hs, keyHint{a.key, a.label(m), a.prio})
		}
	}
	return hs
}

// selfRefusal はこの Mac 自身には繋がない理由 (s は shellRefusal が断る。最下行・カードも出さない)。
func selfRefusal(r row) string {
	if r.self {
		return "この Mac には繋がない (この Mac の IP・名前は c でコピー)"
	}
	return ""
}

// connectRefusal は繋ぐ IP が無い理由 (有線で網が分かれている等、分かるなら具体的に)。
func connectRefusal(r row) string {
	if r.connectIP() == "" {
		return cmp.Or(r.awayReason(), "繋ぐ IP が分からない")
	}
	return ""
}

// vncRefusal は画面共有 (o / a) を断る理由。
func vncRefusal(_ *Model, r row) string {
	if why := selfRefusal(r); why != "" {
		return why
	}
	if !r.vnc {
		return "画面共有を広告していない"
	}
	return connectRefusal(r)
}

// smbRefusal はファイル共有 (y / O) を断る理由 (y と O が同じ断り方をする)。
func smbRefusal(_ *Model, r row) string {
	if why := selfRefusal(r); why != "" {
		return why
	}
	if !r.smb {
		return "ファイル共有 (smb / 445) を開いていない"
	}
	return connectRefusal(r)
}

// guessNote はユーザー名を推測したときに知らせへ足す注記。
func guessNote(m *Model, r row) string {
	if _, guessed := m.userFor(r); guessed {
		return "  (ユーザー名は推測)"
	}
	return ""
}
