# 239 bug: doctor がパスを末尾から切るので「どのディレクトリを消すか」がまさに消える

起票日: 2026-09-04
出典: audit (ux) 2026-09-04 / doctor スコープ。実パスを `termwidth` に通して実測
重要度: **P2**（削除の確認画面で、同一プロジェクトの旧 DerivedData を区別できなくなる）
対象: `src/glogx/doctor_view.go` の `diskItemRows` / `src/glogx/doctor_delete.go` の `deletePathLines`
      （切っているのは `lines` と `assembleDeletePanel` の `truncateDisp`）

## 症状と発火条件

パスの表示幅が **item 行 55 桁 / 確認画面 66 桁**（どちらも contentWidth 77 のとき）を超えると
末尾から切られる。item 行の予算は `contentWidth - 22` なので、**幅 91 のパスなら contentWidth 113
（≒端末 120 桁）未満のすべてで切れる**（77 は実測に使った 1 点にすぎない）。カタログの主要エントリはほぼ全部超える
（`~/Library/Developer/Xcode/DerivedData/*` / `/private/var/tmp/com.apple.CoreSimulator.SimDevice.*` /
`$TMPDIR/TemporaryItems/NSIRD_Finder_*`）。

実測（`…/DerivedData/ThumbnailThumb-cxxbmelbwqqahjagpvzoszkxfvfz`、幅 91）:

| 場所 | 予算 | 出る文字列 | 失うもの |
|---|---|---|---|
| item 行 | 55 | `/Users/koji/Library/Developer/Xcode/DerivedData/Thumbn…` | プロジェクト名が 6 文字しか残らない。**この行が `Space` で選ぶ対象そのもの** |
| 確認画面 | 66 | `/Users/koji/…（中略）…/DerivedData/ThumbnailThumb-cx…` | **ハッシュ部**。同一プロジェクトの旧 DerivedData を区別できない（Xcode がプロジェクト移動時に作る = まさに消したい状況） |
| 左切りなら | 55 | `…erivedData/ThumbnailThumb-cxxbmelbwqqahjagpvzoszkxfvfz` | 識別子が残る |

## repo 内に明文の規律がある側の違反

`termwidth.TruncateLeft` の doc は「末尾を残したいもの（ファイルパスの basename）に使う:
末尾から切ると『どのファイルか』が分からなくなるため」と書き、`status_view.go:statusPathText` が
🚨 付きで実装している（`truncateDispLeft`）。**doctor は 1 箇所も使っていない**。
`deletePathLines` 自身の doc は「確認の本体はここ。ラベルとサイズだけでは『どのディレクトリが
消えるのか』が分からず、中身を確かめずに y を押すことになる」と主張しているが、幅で崩れている。

## 直し方

パスは**行内で予算を計算して `truncateDispLeft` で詰める**（`statusRow` の `pathW` が手本）。
行全体を後から `truncateDisp` する今の形だと、どの列が犠牲になるかを制御できない。

## 既存 issue との関係

issue 182 は「マーク列 / ラベル / pad / 再利用注記」の 4 系統でパス行を含まない。
`diskItemRows` / `deletePathLines` はどちらも 182 対応後（2026-09-03）に追加された。236 / 237 / 233 に該当なし。

## 決着 (2026-09-04)

パスを**先頭から削って末尾を残す**形へ変えた。出典は `doctorFitPath`
(`doctor_view.go`) 1 つで、一覧の対象パス行と確認画面の両方が通る。

- 一覧 (`diskItemRows`): 予算は `o.width - doctorItemPathFixedW`
  (8 空白 + 選択の印 1 + サイズ 9 + 空白 2 + カーソル欄 2 = 22)。**この層で予算に収める**のが要点で、
  収めずに返すと後段の `lines()` が行末を切って末尾が落ちる
- 確認画面 (`deletePathLines`): 予算は `o.width - deleteNoteIndent` (11)。
  `deleteNote` の字下げをリテラルから定数へ出して、予算計算と出典を 1 つにした
- `copyPath` は**切っていない実パス**のまま (表示の都合でコピーを壊さない)

実測 (幅 77): `…per/Xcode/DerivedData/ThumbnailThumb-cxxbmelbwqqahjagpvzoszkxfvfz` —
世代を見分けるハッシュ部が残る。

### 検証

- 変異 2 本 (`go build` 通過を確認してから判定):
  | 変異 | 結果 |
  |---|---|
  | 一覧のパスを右切りへ戻す | `TestDiskItemRowKeepsPathTail` が red |
  | 確認画面のパスを右切りへ戻す | `TestDeleteConfirmKeepsPathTail` が red |
- 🚨 1 本目は**最初 green だった**: テストが `diskItemRows` の戻り値に末尾が含まれるかしか見ておらず、
  切らずに返す変異でも通っていた (切るのは後段の `lines()` なので、この層では末尾が残る)。
  **行が予算に収まっているか**を足して red になった
- `make -C src/glogx lint` 0 issues / `make -C src/glogx test` (-race) 全緑
