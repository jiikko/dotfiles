# 240 bug: `Inspect` の 5 エントリはディレクトリ単位で選べないのに、README と画面は「選べる」と言う

起票日: 2026-09-04
出典: audit (ux) 2026-09-04 / doctor スコープ
重要度: **P2**（対象は全部 `RiskConfirm` = ユーザーデータの可能性がある側。一括かゼロかしか選べない）
対象: `src/glogx/doctor_view.go` の `diskDetail` / `jumpIntoDetail`

## 症状

`Entry.Inspect` が立つカタログ 5 件（`finder-nsird` / `swiftui-drag-cache` / `orphan-container` /
`brew-orphan-state` / `versionmanager-orphan-root`。**全部 `RiskConfirm`**）を `Enter` で開くと、
`diskDetail` は `if r.Entry.Inspect || len(r.Contents) > 0` の枝で中身一覧を出して返る。
選べる行（`diskitem:`）を出す `diskItemRows` へ到達するのは `len(r.Contents) == 0` のときだけで、
`disk.listContents` は Inspect のとき各 Item 直下の名前を積むので、**Item が 1 つでもあれば
`Contents` は非空**になる。結果:

- `Space` はエントリ全体の on/off しかできない（`orphan-container` なら「孤児コンテナ全部」か「ゼロ」）
- `jumpIntoDetail` の `jumpTo("diskitem:"+id+"\x00")` は必ず外れ、カーソルが動かない
- 画面は `削除経路: trash (Space で選び d で削除)` と出し、hint も「Space: 選択」と言う
- `Contents` は全 Item を平坦に連結した `basename/child` の羅列で、パスもサイズも持たない

## 発火条件

上記 5 エントリのいずれかで候補が 1 件以上あり、`Enter` で開いたとき（`Contents` が非空なら常に）。

## ドキュメントとの食い違い

`src/glogx/README.md` の `D` の行は「`Enter` で中を開く/畳む（開くとカーソルが対象パスへ移り、
**ディレクトリ単位で選べる**）」と無条件に書き、`issues/done/148` の冒頭も同様。
コード側の意図的な記述は「Inspect の**中身一覧**は選べない」までで、
「対象パス行を出さない」とはどこにも書かれていない。

## 直し方

Inspect でも `diskItemRows`（選べる対象パス行）を出し、中身一覧はその下か対象パス行の detail へ置く。
🚨 **`(Inspect || RiskConfirm) && !inspected` のゲート（`doctor_delete.go`）は残す前提**。
README:199 の「`risk: 要確認` の行は `Enter` で中身を見るまで選べず」はそのゲートの正しい記述で、
ここを壊す提案ではない（選べる**粒度**をディレクトリ単位へ戻す話）。
**実装を変えず README / 148 を実態へ合わせる**なら、`RiskConfirm` の 5 件が一括削除しか
できない事実を README に明記する必要がある（どちらを採るかは判断待ち）。

## 既存 issue との関係

issue 163 の `Contents` への言及は**メモリ上限**のみ。236 / 237 / 233 に該当なし。
