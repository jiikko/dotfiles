# 1033 (test): glogx の H (git log 一覧へ戻る) のテストが、戻った後の tick を見ていない

起票日: 2026-11-05

## 概要

`H` でどの全画面からも git log 一覧へ戻る機能 (commit「feat(glogx): H でどの全画面からも git log 一覧へ戻る」) のテスト
`TestHomeKeyReturnsToGitLogFromEveryFullScreen` (`src/glogx/fullscreen_test.go`) は、`m.handleKey("H")` の戻り値の cmd を
`_, _ =` で捨てている。見ているのは「全画面が 0 枚」「`activeFullScreen` が None」「終了に入っていない」の 3 つだけ。

## 詳細

- doctor / ratelimit / filer / lan の 4 経路は `browseModel.crossTo` (`src/glogx/fullscreen.go`) を通る。行き先が None (H) のとき
  `crossTo` は `openFullScreen` を呼ばず `maybeTick` を返す。`openFullScreen(FullScreenNone)` は nil を返すので、
  呼び出しを `m.openFullScreen(crossScreen(...))` に戻す退行では、H の後に tick のチェーンが張られない
- issues / status の 2 経路は `crossTo` を通らず、行き先が None なら各 route の末尾の `tea.Batch(cmd, m.maybeTick())` に落ちる
- 上の退行が起きても、今のテストの 3 つの assert はどれも変わらない。H を押す既存テストはこの 1 本だけで、
  `TestCrossKeysReachEveryFullScreen` は行き先が None 以外のキーしか押さないので `crossTo` の None 分岐を通らない
- 🚨 **未確認**: tick が張られないと利用者に何が見えるか。`tickInterval` (`src/glogx/browse_anim.go`) が演出として見る全画面は
  filer (`filerV.Animating()`) だけで、doctor は route のコメントが「閉じ済み (演出なし)」と書いている。ratelimit / lan に閉じる演出が
  あるかは読んでいない。演出の無い画面では tick が無くても見た目が変わらない可能性がある。テストを足す前に、どの画面の何が止まるかを
  実際に退行を当てて確かめ、ここへ書く (何も止まらない画面まで tick を固定すると過剰な固定になる)
- 変異を当てて今のテストが緑のままかは試していない

## 対応方針

- 既存の helper (`src/glogx/tick_helpers_test.go`) を使う: `clearTick(m)` で予約を空にしてから H を押し、`tickPending(m)` を assert する。
  `cmd != nil` では見ない (single-flight なので、別の経路が先に張っていると cmd は nil になる。`tui_nav_test.go` の
  `TestBrowseBatchedRunesArmsTick` が同じ理由で `tickPending` を使っている)。`fireTick` は届いた tick が張り直しうるので「予約が無い」状態を作れない
- assert を掛けるのは、上の未確認を確かめて tick が要ると分かった画面だけにする
- 変異 (`crossTo` を `openFullScreen(crossScreen(key))` に戻す) で red になることを確かめる

## 検討して不要とした項目

- 「入力中 (検索欄・絞り込み) に H が画面へ渡り、一覧へ戻らないこと」を H 固有に固定するテスト: 入力中の判定はキーの種類を見ない
  (filer / lan の `Binds` は `OwnsKeys()` が真なら全キーを取る。`src/treefiler/filer/model.go` の `Binds`・`src/mac-lan-manager/lanmgr/model.go`。
  issues / status の `OwnsKeys` はキーを引数に取らない)。そのため H を足しても、既に固定された他のキーと同じ経路を通るだけなので書かない。
  固定済みのテスト: lan の絞り込み中の `i` (`lan_view_test.go`)、issues の番号入力中の `s` (`tui_overlay_test.go`)、
  filer の入力中の横断キー (`filer_view_test.go`)
  - 範囲の断り: 入力中**でない**ときの filer / lan の `Binds` はキーを見る (`treeKeys` / `listKeys` / `listnav.MotionOf`)。H が今どれにも
    入っていないので横断できている。filer / lan に H を語彙として足すと横断が黙って消える (`s` が shell / ssh に取られているのと同じ形)。
    これは `TestHomeKeyReturnsToGitLogFromEveryFullScreen` が赤くなるので検出できる
  - doctor の削除確認中・status の pager / 破棄確認中・lan の設定板で横断キーが飲まれることを固定するテストは確認していない (H に限らない話)

## 関連ファイル

- `src/glogx/fullscreen_test.go` (`TestHomeKeyReturnsToGitLogFromEveryFullScreen`)
- `src/glogx/fullscreen.go` (`crossTo`)
- `src/glogx/browse_screens.go` (各全画面の route)
- `src/glogx/browse_anim.go` (`maybeTick` / `tickInterval`)
- `src/glogx/tick_helpers_test.go` (`clearTick` / `tickPending`)
- `src/tuikit/tickchain/tickchain.go` (`Chain.Want` / `Pending`)

## 進捗

- [ ] 退行を当てて、どの画面の何が止まるかを確かめる
- [ ] 止まる画面に tick の予約を見る assert を足す
- [ ] 変異で red を確認する
