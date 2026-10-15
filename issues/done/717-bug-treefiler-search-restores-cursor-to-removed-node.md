# 717 (bug): treefiler の検索が、ライブ更新で消えた項目へカーソルを戻す

起票日: 2026-10-12

## 概要

`/` の検索は、始めたときのカーソル (`searchState.origin`) を覚え、一致が無いときや取り消したときにそこへ戻す。検索中にライブ更新で
その項目が消えると、2 つの経路は消えたかを確かめずに戻すので、**カーソルが木に無い項目を指したままになる**
(master の版。2026-10-12 に `src/treefiler/filer/search.go` で確認):

| 経路 | 消えたかを確かめるか |
|---|---|
| `searchKey` の Esc / ctrl+c | 確かめる (`m.inTree(s.origin)`。消えていれば今の位置に留まる) |
| `refreshMatches` の一致なし (`m.setCur(s.origin)`) | **確かめない** |
| `searchKey` の空欄の backspace (`m.setCur(s.origin)`) | **確かめない** |

カーソルが木に無い項目を指すと、配置 (`layout`) にカーソルが居ないので、カメラの目標 (`step` の `pos[m.cur]`) が原点 `{0, 0}` になり、
画面が木の左上へ流れる。ステータスバーも消えた項目を出す。木の外の状態は、次のライブ更新 (`applyChange` の `for !m.inTree(m.cur)`) で直る
(ずっと続くわけではない)。🚨 一致なしの後は `m.cur` 自体が消えた origin になるので、その後に Esc を押しても「今の位置に留まる」=
木の外のままになる。

## 再現 (issue 712 の乱数の操作列のテスト `TestVirtualizedRandomOpsMatchFull` の種 262 で見つけた)

フォルダの大きさは関係ない (反証レビューで 9 件の fixture で再現した。master の版で、一致なし・空欄の backspace の両方とも FAIL)。

1. ファイルにカーソルを置き、`/` で検索を始める
2. 検索中に、そのファイルがライブ更新で消える (別の端末で削除)
3. 一致しない文字を打つ (refreshMatches が一致なしで origin へ戻す)
4. → カーソルが消えたファイルを指し、カメラが木の左上へ動く

## 対応方針 (案)

- origin へ戻す 3 つの経路を 1 つの関数に寄せ、戻す先を「origin。消えていれば生きているいちばん近い祖先」にする (`Refresh` /
  `applyChange` のカーソルの規則と同じ)。「消えていれば今の位置に留まる」を 3 経路に当てても木の外へは出ない (applyChange が先に
  カーソルを祖先へ動かすため) が、一致へ移った後に origin が消えて一致なしになると一致に留まり、「一致なしで始めた位置へ戻る」と食い違う。
  (実装中の変異で分かった訂正。反証レビューの「留まるは効かない」は、今の Esc の形を一致なしの経路と組み合わせた場合に限った話だった)
  (戻す経路が 3 つあり、1 つだけが確かめている = 同じ判定の書き方が分かれた形。寄せて 1 か所にする)
- 回帰テスト: 検索中に origin を消すライブ更新 (`applyChange`) を当て、一致なし・空欄の backspace・その後の Esc でも、カーソルが木の中にあること

## 関連

- issue 712 (この不具合は仮想化とは関係なく、全部を動かす側でも起きる。乱数の操作列のテストが両方の Model で同じ壊れ方を見せた)

## 進捗

- 2026-10-12: 起票
- 2026-10-12: 反証レビュー (dotfiles-5f、sonnet) を当てた。核心 (2 つの経路で消えた origin へ戻す) は反証されず、master で再現した。
  直し方の案を「生きている最寄りの祖先へ戻す」に改め (「留まる」は一致なしの後に効かない)、木の外の状態は次のライブ更新で直ること・
  フォルダの大きさは関係ないことを足した
- 2026-10-12: 修正 (dotfiles-5f)。commit「fix(treefiler): 検索が消えた origin へ戻さず、生きている最寄りの祖先へ戻す (issue 717)」
  - [x] 3 経路 (Esc / ctrl+c・空欄の backspace・`refreshMatches` の一致なし) を `search.go` の `restoreOrigin` に寄せた
  - [x] 回帰テスト (`search_test.go`): `TestSearchRestoresToLiveAncestorWhenOriginRemoved` (一致なし・一致なしの後に Esc・空欄の
    backspace・Esc × ファイルが消える / 親のフォルダごと消える) と `TestSearchNoMatchAfterJumpGoesToOriginAncestor`
  - [x] 変異 (`bin/mutate-verify`): ① 祖先へ辿るループを外す (= 修正前と同じく origin へ戻す) → `TestSearchRestoresToLiveAncestorWhenOriginRemoved`
    の一致なし・backspace 系が red。② 「消えていれば今の位置に留まる」にする → `TestSearchNoMatchAfterJumpGoesToOriginAncestor` が red
    (この形を止めているのはこの 1 本だけ。①の表の 8 本は②では全部 green)
  - [x] `make -C src/treefiler test` rc=0・`make -C src/treefiler lint` 0 issues (最後の編集の後)
  - [x] 敵対レビュー (opus、read-only・観点 3 つ): P1 / P2 なし。root の付け替えは rerootUp だけで検索中には走らず、ループが nil に届く経路は無い
  - 残タスク (スコープ外・記録):
    - `for !m.inTree(x) { x = x.parent }` が Refresh・applyChange・restoreOrigin の 3 か所になった。`liveAncestor` に寄せたいが model.go は
      issue 712 が書き換え中のため触っていない。712 が入った後に寄せる
    - 戻った祖先の `last` は検索中に移った一致を指したまま残り、Esc の後の `l` がその一致へ降りる (Refresh の経路では nil に戻る)。仕様の判断待ち
    - 消えた後に別の周で同じ名前で作り直された項目には戻らない (別の node になる。Refresh / applyChange と同じ振る舞い)。パスで引き直すかは未決
    - 祖先へ戻った後も、検索の列は消えた origin と同じ深さ (`m.column(s.origin)`)。修正前からの振る舞い
