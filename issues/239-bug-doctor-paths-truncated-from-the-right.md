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
