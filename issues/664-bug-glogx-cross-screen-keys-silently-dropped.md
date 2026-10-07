# 664 (bug): glogx の横断キー (`i` / `s` / `R` / `D`) が画面・モードごとの手書きの列挙で、穴の場所では無言で捨てられる

> 🚨 **担当中: claude (glogx 監査の修正セッション)**（2026-10-08〜）

起票日: 2026-10-07

## 概要

`docs/glogx-ui-guide.md` §2 は「`i` `s` `R` `D` `U` … どの画面からも同じ板へ飛べる」と定めているが、横断は各 viewer の
`handleKey` が `want*` フラグを立て、tui.go の `routeKeyToIssues` / `routeKeyToStatus` が `takeWant*` → `finishClose` →
相手の `toggle` を書き分ける形で、画面・モードごとに手で列挙している。列挙から漏れた場所ではキーが無言で捨てられる。

出典: glogx issues viewer 監査 (668) の ui-components U1。

## 詳細

- `want*` を立てる箇所 (非テスト、監査時の grep): issues_view.go に `wantStatus` ×2 / `wantRatelimit` ×2、
  status_view.go に `wantIssues` / `wantRatelimit`。**`D` (doctor) へ移る `want` はどの viewer にも無い**
- 穴 (`D` は main の grep と反証レビューの両方で確認。`D` は tui.go の handleKey で全画面の dispatch より後ろでしか拾われない):
  - issues / status / ratelimit ダッシュボードのどこからも `D` が効かない
  - doctor (`doctor_view.go` の handleKey) から `i` / `s` / `R` が効かない (default が `listnav.MotionOf` に落ちる。反証レビューの指摘)
- **穴ではないもの (既存の設計判断)**: viewer が `ownsKeys` のモード (status の pager / 破棄の確認、issues の URL ピッカー / 番号入力 / y/N 確認) の間は
  横断キーを通さない。status_view.go の listKey の `case "i"` のコメント (「確認中や pager 中はこの switch まで届かないので誤爆しない」) と
  tui.go の routeKeyToStatus 冒頭の 🚨 (b が push に化けた実測) が根拠。監査体はこれを穴に数えていたが取り下げた
- 過去の後追い: 本文モードの `s` は一覧より後から足された (954eb937 → 064f7de0)、本文の `i` は issue 122 で後から足された。
  新しいモード・全画面を足すたびに書き忘れる形
- issues/done/148 に `D` を外すと決めた記録は見当たらない (監査体の確認)

### 発火条件

issues viewer / status viewer / ratelimit ダッシュボードを開いた状態で `D` を押す → 何も起きず、通知も無い。
silent。

## 対応方針

- 横断先を `fullScreenID` (enum) で表し、`crossTarget(key) (fullScreenID, bool)` の表を 1 つ置いて、各 viewer のモード共通の
  入口 (issues なら `actionKey` の手前) で引く
- `want` は 3 つの bool をやめ `fullScreenID` 1 つにする。tui 側は `crossTo(id)` に寄せ、exhaustive な switch で ID の取りこぼしを lint で止める
- fullscreen.go は「interface + スライスのレジストリ」を確保増を理由に却下しているが、enum の switch は確保 0 なので矛盾しない
- 表を引くのは `ownsKeys` が偽のときだけにする (上の「穴ではないもの」を壊さない)。viewer の内部状態で `U` が飲まれるのも意図的 (issue 113)
- `docs/glogx-ui-guide.md` §2 の「どの画面からも」に、`ownsKeys` のモードでは効かないことを書き足す
- 先に決めること: `D` を本当に全画面から効かせるか (ui-guide の記述を直す側に倒す選択肢もある)

## 関連ファイル

- `src/glogx/issues_view.go` / `src/glogx/status_view.go` / `src/glogx/ratelimit_dashboard.go` / `src/glogx/doctor_view.go` (`handleKey`)
- `src/glogx/tui.go` (`routeKeyToIssues` / `routeKeyToStatus`)
- `src/glogx/fullscreen.go`
- `docs/glogx-ui-guide.md` §2

## 進捗

- [ ] `D` を横断の対象に入れるかを決める
- [ ] 横断キーの表と `crossTo` に寄せる
- [ ] 全モード × 横断キーの表駆動テストを置き、1 か所の列挙を外す変異で red を確認
