package main

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// overlay が「自分の語彙を持っている (a)」「中断できない処理を走らせている (b)」を主張する場所は
// 5 箇所に散っている (issue 213)。148 ④ S2 では doctor に (b) を足したとき **3 箇所を直し忘れ**、
// 敵対的レビューが独立に同じ P1 を実測した (1 回目の Ctrl-C でアプリが落ちる / 削除中に再起動
// ダイアログが出る / 終了で削除の ctx を切らない)。
//
// ここでやるのは 2 つ。
//  1. **参加者 × 2 軸の表**を 1 箇所に置き、表の主張と実装 (browseModel の各経路) の一致を pin する
//  2. **表に載っていない参加者が現れたら red にする** (reflect で ownsKeys の実装を数え上げる)
//
// ⚠️ production の述語を新しく発明しない (issue 213 の判断)。`actModal.active()` /
// `running()` と `doctorDelete.active()` / `blocking()` が既に 2 軸そのもので、doc も
// 一致テストの前例 (`TestActionModalActiveMatchesHandleKey`) も在る。ここは**参照側の
// 網羅**だけを守る。
//
// ⚠️ compile error にはできない (issue 213 に明記)。リストへ書き忘れても型は満たされるので、
// 「書き忘れ」を捕まえるのは reflect の数え上げだけ。

// overlayParticipant は 2 軸の主張。owns = (a) 自分の語彙を持ちうるか /
// uninterruptible = (b) 中断できない処理を走らせうるか。
type overlayParticipant struct {
	name string
	// owns は (a) を持つか。持つなら updateKeyReachable が「見えていて語彙を持つとき譲る」こと
	owns bool
	// uninterruptible は (b) を持つか。持つなら ctrl+c と restartPromptVisible が譲ること
	uninterruptible bool
	// whyNotUninterruptible は (b) を持たない理由。⚠️ **「壊れないから」ではない**
	whyNotUninterruptible string
	// stoppedByCancelAll は cancelAll が止めるか (走行中の非同期を終了・再起動で残さない)
	stoppedByCancelAll bool
}

// overlayOwnershipTable は 2026-09-03 時点の主張。**新しい参加者を足したらここへ 1 行足す**
// (足し忘れは TestOverlayOwnershipTableCoversAllParticipants が red にする)。
var overlayOwnershipTable = []overlayParticipant{
	{
		name: "actModal", owns: true, uninterruptible: true, stoppedByCancelAll: true,
	},
	{
		name: "doctorOv", owns: true, uninterruptible: true, stoppedByCancelAll: true,
	},
	{
		name: "issuesOv", owns: true, uninterruptible: false,
		// Update の中で同期に終わる操作しか持たない = 「実行中」という相を跨がない
		whyNotUninterruptible: "非同期の処理を持たない (番号入力・確認はすべて Update 内で決着する)",
		stoppedByCancelAll:    true,
	},
	{
		name: "statusOv", owns: true, uninterruptible: false,
		// ⚠️ 破壊的操作 (runGitRestoreWorktree / runGitCleanUntracked) は **Update の中で同期に**
		// 走るので相を跨がない。非同期は fetchDiff (読み取り専用) だけ。
		// **status に非同期の破壊的操作を足した瞬間に (b) 側へ移る**
		whyNotUninterruptible: "破壊的操作は Update 内で同期に完了し、非同期は読み取り専用の fetchDiff だけ",
		// ⚠️ 既知の穴 (issue 213 が記録): cancelAll は statusOv を止めていないので、
		// fetchDiff の git diff が終了時に残りうる。ここを true にするなら stop() の実装が要る
		stoppedByCancelAll: false,
	},
}

// 表の (a) の主張と `updateKeyReachable` の実装が一致する。
// ⚠️ **overlay の handleKey を直叩きしない**。browseModel 経由で見ないと、
// 「経路の 1 つを直し忘れた」形は検出できない (issue 213 の発火条件)。
func TestOverlayOwnsKeysMatchesUpdateKeyReachable(t *testing.T) {
	for _, p := range overlayOwnershipTable {
		if !p.owns || p.name == "actModal" {
			continue // actModal は overlay の visible() を持たないので別経路 (下のテスト)
		}
		t.Run(p.name, func(t *testing.T) {
			m := newTestBrowse(t, 1, map[string]CIState{}, nil)
			showOverlayOwning(t, m, p.name)
			if m.updateKeyReachable("X") {
				t.Fatalf("%s が語彙を持っているのに updateKeyReachable が true (X が横取りされる)", p.name)
			}
		})
	}
}

// 表の (b) の主張と `ctrl+c` / `restartPromptVisible` の実装が一致する。
func TestOverlayUninterruptibleMatchesCtrlCAndRestartPrompt(t *testing.T) {
	for _, p := range overlayOwnershipTable {
		if !p.uninterruptible || p.name == "actModal" {
			continue
		}
		t.Run(p.name, func(t *testing.T) {
			m := newTestBrowse(t, 1, map[string]CIState{}, nil)
			showOverlayUninterruptible(t, m, p.name)

			// 1 回目の Ctrl-C でプロセスが落ちない (quit へ行かない)
			if _, cmd := m.handleKey("ctrl+c"); isQuitCmd(cmd) {
				t.Fatalf("%s が中断できない処理中なのに 1 回目の Ctrl-C で終了した", p.name)
			}
			// 再起動ダイアログを出さない (出るとどのキーもそちらに吸われる)
			m.restartPending = true
			if m.restartPromptVisible() {
				t.Fatalf("%s が中断できない処理中なのに再起動ダイアログが出る", p.name)
			}
		})
	}
}

// 表に載っていない参加者が現れたら red にする (issue 213 の受け入れ条件)。
//
// ⚠️ **compile error にはできない**ので、`browseModel` のフィールドを reflect で走査し、
// `ownsKeys()` を実装している型が表に在ることを要求する。非公開メソッドの interface は
// 同一 package なら判定できる。走査の実体は `ownsKeysFieldNames` (ポインタ / スライスの
// フィールドを取りこぼさないこと自体を canary で検査している)。
func TestOverlayOwnershipTableCoversAllParticipants(t *testing.T) {
	mt := reflect.TypeOf(browseModel{})
	inTable := map[string]bool{}
	for _, p := range overlayOwnershipTable {
		inTable[p.name] = true
	}

	names := ownsKeysFieldNames(mt)
	if len(names) == 0 {
		t.Fatal("判定不能: ownsKeys() を実装したフィールドが 1 つも見つからない (走査が壊れている)")
	}
	for _, name := range names {
		if !inTable[name] {
			t.Errorf("browseModel.%s は ownsKeys() を実装しているのに overlayOwnershipTable に無い"+
				" (updateKeyReachable / ctrl+c / restartPromptVisible / cancelAll の直し忘れが黙る)", name)
		}
	}

	// 表に in-table だけあって実体が無い行も検出する (消えた overlay を表に残さない)
	for name := range inTable {
		if name == "actModal" {
			continue // actModal は ownsKeys() を持たない (active()/running() が対応する語彙)
		}
		if _, ok := mt.FieldByName(name); !ok {
			t.Errorf("overlayOwnershipTable の %s が browseModel に無い (表が古い)", name)
		}
	}
}

// (b) を持たない参加者は**理由が書かれている**こと。「壊れないから」は理由にしない
// (issue 213: status は「Update を跨ぐ相にならない」が理由で、非同期の破壊的操作を足した
// 瞬間に (b) 側へ移る)。
func TestOverlayNonUninterruptibleHasReason(t *testing.T) {
	for _, p := range overlayOwnershipTable {
		if p.uninterruptible {
			continue
		}
		if p.whyNotUninterruptible == "" {
			t.Errorf("%s は (b) を持たないのに理由が空 (次の人が同じ穴を掘る)", p.name)
		}
	}
}

// showOverlayOwning は「その参加者が (a) 語彙を持っている」状態を作る。
// ⚠️ **状態の作り方をここに集約する**。各テストで別々に作ると、片方だけ実装に追従して
//
//	もう片方が「別の状態を検査している」ことに気づけない
func showOverlayOwning(t *testing.T, m *browseModel, name string) {
	t.Helper()
	switch name {
	case "doctorOv":
		m.doctorOv.shown = true
		m.doctorOv.del = doctorDelete{confirm: true}
	case "issuesOv":
		m.issuesOv.shown = true
		m.issuesOv.numFilter.typing = true // 番号入力中はキーを解釈し切る
	case "statusOv":
		m.statusOv.shown = true
		m.statusOv.discarding = true // 破棄の y/N 確認中
	default:
		t.Fatalf("showOverlayOwning: 未知の参加者 %s (表に足したらここも足す)", name)
	}
	if !overlayOwnsKeys(m, name) {
		t.Fatalf("前提が作れていない: %s が語彙を持っていない", name)
	}
}

// showOverlayUninterruptible は「その参加者が (b) 中断できない処理を走らせている」状態を作る。
func showOverlayUninterruptible(t *testing.T, m *browseModel, name string) {
	t.Helper()
	switch name {
	case "doctorOv":
		m.doctorOv.shown = true
		m.doctorOv.del = doctorDelete{running: true}
		if !m.doctorOv.del.blocking() {
			t.Fatal("前提が作れていない: doctor の削除が blocking でない")
		}
	default:
		t.Fatalf("showOverlayUninterruptible: 未知の参加者 %s (表に足したらここも足す)", name)
	}
}

func overlayOwnsKeys(m *browseModel, name string) bool {
	switch name {
	case "doctorOv":
		return m.doctorOv.ownsKeys()
	case "issuesOv":
		return m.issuesOv.ownsKeys()
	case "statusOv":
		return m.statusOv.ownsKeys()
	}
	return false
}

// isQuitCmd は cmd が終了 (tea.Quit) かどうか。Ctrl-C の 1 回目で落ちないことの判定に使う。
func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// ownsKeysFieldNames は t のフィールドのうち `ownsKeys()` を持つ型の名前を返す。
//
// ⚠️ **値フィールドだけを見ては駄目**。`reflect.PointerTo(f.Type).Implements(...)` は
// **ポインタ / スライス / interface のフィールドを素通りする** (実測 2026-09-03:
// `*fooView` のフィールドは false、`[]fooView` も false)。既存の値フィールドで
// 件数 > 0 になるので「判定不能」ガードも鳴らず、**次の overlay をポインタで持たせた瞬間に
// 静かに無検査になる** (敵対的レビューの P2)。ポインタ / スライスは要素型まで開いて見る。
func ownsKeysFieldNames(t reflect.Type) []string {
	type ownsKeyser interface{ ownsKeys() bool }
	want := reflect.TypeOf((*ownsKeyser)(nil)).Elem()

	implements := func(ft reflect.Type) bool {
		for range 2 { // 値 → 要素型 の 2 段まで開く (*T / []T)
			if ft.Implements(want) || reflect.PointerTo(ft).Implements(want) {
				return true
			}
			switch ft.Kind() {
			case reflect.Pointer, reflect.Slice, reflect.Array:
				ft = ft.Elem()
			default:
				return false
			}
		}
		return false
	}

	var names []string
	for i := range t.NumField() {
		f := t.Field(i)
		if implements(f.Type) {
			names = append(names, f.Name)
		}
	}
	return names
}

// 走査そのものを canary で検査する (取りこぼす形を固定する)。
// ⚠️ **本走査と同じ関数を通す**。式をコピーして別に書くと canary はコピーを検査するだけになる。
func TestOwnsKeysFieldScanFindsPointerAndSliceFields(t *testing.T) {
	type probe struct {
		Value    doctorView
		Pointer  *doctorView
		Slice    []doctorView
		Unrelate int
	}
	got := ownsKeysFieldNames(reflect.TypeOf(probe{}))
	want := map[string]bool{"Value": true, "Pointer": true, "Slice": true}
	for _, n := range got {
		if !want[n] {
			t.Errorf("無関係なフィールドを拾った: %s", n)
		}
		delete(want, n)
	}
	for n := range want {
		t.Errorf("%s フィールドを取りこぼした (この形で overlay を持たせると無検査になる)", n)
	}
}
