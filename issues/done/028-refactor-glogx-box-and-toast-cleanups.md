# 028 refactor: glogx の box 引数肥大 / toast 調停 / spinnerActive 手動不変条件の整理

## 背景

2026-07-25 のトースト改修 (中央ダイアログ → 右下トースト移行 / 枠線の種別色化 / easeOutCubic 化)
で box.go・toast.go・tui.go を触った際に見つけた改善候補の記録。いずれも**単独では動作に問題
なし**で、trigger 待ちで凍結してよいもの ([`verify-design-intent-before-refactor.md`](../_claude/rules/verify-design-intent-before-refactor.md))。

**評価原則**: 行数分割はリファクタではない。「認知負荷 (読む時の jump 数 / 変更時の touch 箇所)・
結合・重複・状態の局所化」が**実際に下がるか**で判定する。「やらない」も正当な結論。

⚠️ 以下は着手前にコードを再確認すること (下記は 2026-07-25 時点の評価)。

## P1: `buildPanelBoxImpl` の引数 7 個 (自分で持ち込んだ負債)

`src/glogx/box.go` の `buildPanelBoxImpl`:

```go
func buildPanelBoxImpl(title string, rows []string, width int, colored bool, shadow bool, b boxBorder, border string) []string
```

`colored bool` / `shadow bool` が連続し、`b boxBorder` (罫線の字形) と `border string`
(枠線の SGR 色) という**名前が似て意味が違う**引数が並ぶ。呼び出し側が意味を読めない:

```go
buildPanelBoxImpl(title, rows, width, colored, false, borderLight, ansiDim)
buildPanelBoxImpl("", content, termW-2, colored, true, borderDouble, ansiDim)
```

7 番目の `border string` は 2026-07-25 にトースト枠の色付けで**私 (Claude) が追加したもの**。
追加時点で引数が閾値を越えた。

### 方針

`shadow` / `b` / `border` を 1 つの構造体に畳む (例 `panelBoxStyle{shadow bool; glyphs boxBorder; color string}`)。

**scope が小さいのが利点**: `buildPanelBoxImpl` の呼び出しは**内部 3 箇所のみ** (box.go:82 /
box.go:96 / box.go:105)。公開ラッパー `buildPanelBox` (6 呼び出し) と
`buildShadowPanelBox` (3 呼び出し) のシグネチャは変えずに済むため、外への波及はゼロ。

- 命名は `boxBorder` (字形) と色を混同させないこと。`glyphs` / `color` のように役割で分ける
- テストは `box_test.go` がラッパー経由なので影響が小さい (要確認)

## P2: トーストの単一スロット調停をタイマーで実現している

`src/glogx/tui.go` の `claudeUpdateToastDefer` / `claudeUpdateRetryMsg` /
`showOrDeferClaudeUpdate`:

```go
const claudeUpdateToastDefer = 4 * time.Second   // toastHold(3s) + スライドより長め
```

「先行トーストが表示中なら上書きせず、4 秒後に 1 度だけ再送。まだ塞がっていたら諦める」という
作り。実質「キューが無いことを sleep で埋めている」形で、`toastHold` を変えるとこの定数が
暗黙に壊れる時間的結合がある。

### ⚠️ ただし現状の設計は意図的と明記されている

コード側コメントで「単一スロット・後勝ちの toast 設計を歪めずに『重要度 error > info』を守る
ための調停」と説明されており、**素朴なキュー化は設計意図の逆転になりうる** (キューにすると
「後勝ち」で最新結果を見せる性質が失われ、古い info が新しい結果より先に出る)。

### 方針 (trigger 待ち)

- **今は触らない**。trigger = 「3 つ目以降の遅延通知源が増えたとき」または「`toastHold` を
  変更したくなったとき」。それまでは現状の 1 対 1 調停で足りている
- 着手するなら「キュー」ではなく **優先度付き単一スロット** (error > info、同 priority は
  後勝ち) が設計意図に沿う。この場合 `claudeUpdateToastDefer` と再送メッセージが両方消える
- 着手前に `issues/done/024-feat-glogx-claude-update-toast.md` / `026` を読み、この調停が
  どの要件から来たかを確認すること

## P3: `spinnerActive()` の不変条件が手動管理

`src/glogx/tui.go:1933-1935` — 1 行に **14 項**の OR:

```go
return m.fetching || m.actModal.running() || m.pullAnimating || m.pushAnimating ||
  len(m.pushSlides) > 0 || m.scrollAnim || m.toast.animating() || len(m.pushPoll) > 0 ||
  len(m.detailsLoading) > 0 || m.detailOv.fetching() || m.diffOv.fetching() ||
  m.prStatusOv.fetching() || m.panelHasRunningJob() || m.usageOv.loading()
```

問題は行の長さより**不変条件が人手**な点: 新しい非同期処理・アニメを足したときここへの追記を
忘れると tick が回らずアニメが止まる (静的に検出されない)。2026-07-25 のトースト改修でも
`m.toast.animating()` がここに入っているかを目視確認する必要があった。

### 方針 (要検討 — 実際に複雑性が下がるか不明)

- 案 A: 「アニメ源」を `[]func() bool` か interface の slice で登録し、`spinnerActive` は
  それを畳むだけにする。追記漏れは防げるが、**登録漏れ**に問題が移るだけの可能性がある
  (複雑性の移動 = 却下すべきパターン)
- 案 B: 現状維持 + テストで担保する。「各非同期状態を立てたら `spinnerActive()` が true」を
  table test で網羅すれば、追記漏れが test 失敗として現れる。**こちらの方が筋が良い可能性が高い**
  (構造を変えずに不変条件を機械化できる)

→ 着手するなら案 B から検討する。案 A は「登録漏れ」に問題をずらすだけなら不可。

## P4 (軽微): `toast` が usage overlay の定数を借用

`src/glogx/toast.go:144` が `usageBoxChrome` (`usage_overlay.go:84`、値 5) を使っている:

```go
boxW := dispWidth(row) + usageBoxChrome
```

「影付き枠が内容幅に加える固定分 (`"│ "` + `" │"` + 影 1 桁)」という**box の性質**であって
usage overlay 固有ではない。usage 側の都合で値を変えるとトーストが無言で崩れる。

### 方針

`box.go` へ移して `shadowBoxChrome` 等の中立名にリネームする (定義位置と名前だけの変更で、
値は不変)。P1 で box.go を触るなら同時にやるのが安い。

## 着手順の推奨

1. **P1** (自分で増やした引数の返却。scope が内部 3 箇所で明確に閉じている)
2. **P4** (P1 と同じファイルなので同時)
3. P3 は案 B (テストで担保) の検討から
4. P2 は trigger 待ち (触らない)

なお本 issue は codex レビュー未通過 (codex 不使用の運用指示による)。事実関係は
file:line と実コードで自己検証済みだが、対応方針の妥当性は着手時に再評価すること。

## 関連

- [`verify-design-intent-before-refactor.md`](../_claude/rules/verify-design-intent-before-refactor.md) — 「複雑性が実際に下がるか」で判定する原則 (P3 案 A の却下根拠)
- [`issues/018-refactor-god-struct-audit-2026-07-22.md`](018-refactor-god-struct-audit-2026-07-22.md) — glogx の構造監査 (browseModel は抽出完了と判定済み)
- `issues/done/025-feat-glogx-window-drop-shadow.md` — box.go の影実装の経緯 (P1 で触る範囲)
- `issues/done/026-feat-glogx-copy-last-warning.md` — toast と lastWarning の関係 (P2 の前提)

## 対応状況

### 2026-08-13: P2 が別ルートで解消し、P3 の追記漏れを補充してクローズ

**P2 は「対応不要」ではなく「済み」**。issue が指す `claudeUpdateToastDefer` /
`claudeUpdateRetryMsg` / `showOrDeferClaudeUpdate` は**現在のコードに存在しない**。
`aea166a` (refactor(glogx): トーストを通知スタックにし、塞がり時の調停 2 実装を捨てる) が
単一スロットを最大 3 枚のスタック (`toastStackMax`) に置き換え、調停そのものを不要にした。

`toast.go` の doc がその判断を記録している: 「1 枠の後勝ちだと『今それを消したくない』場面ごとに
呼び出し側が調停する必要があり、同じ問題に 3 つの実装ができていた (claude version 通知の専用
タイマー付き遅延再送 / autobuild の pending 保持 / 調停なしの即上書き)。積めるようにすれば
どの経路も素直に show() を呼ぶだけで済み、調停そのものが要らなくなる」。

懸念の本体だった **`toastHold` への時間的結合は消えた** — `toastHold` に結合した定数は
現在 1 つも無い (grep で確認済み)。

⚠️ **ただし「スタックの方が優先度付き単一スロットより要件を満たしている」と最初に書いたのは
誤りだった** (敵対的レビューが実測で反証)。スタック化で消えたのはタイマー結合だけで、
**優先度はどこにも実装されていなかった**: 溢れ時の追い出しは `s.older[:toastStackMax-1]` =
年齢順 FIFO のみで、`ok` / `info` は判定に参加していない。実測で「警告 (ok=false) の後に成功通知
3 回」で警告が消えた。`b3d0123` が要求した「重要度 error > info の逆転を防ぐ」は、error 1 枚 +
info 1 枚という個別ケースでだけ成立していた。

そこで **追い出しを severity-aware にした** (`evictOne`): 成功/進行中 (`ok` または `info`) の最古を
先に捨て、全部が警告なら最古を捨てる。同じ重要度どうしは従来どおり年齢順 (後勝ち)。マジック
ナンバーは足さず既存フィールドだけで判定する。テスト 2 本 + 変異 3 種 (年齢順へ戻す / 重要度の
向きを逆転 / 全部警告のとき最新を捨てる) で red を確認。

**「保持」と「表示」の食い違いも解消した (2026-08-13)**。実際に描かれる枚数は `boxLines` の
描画予算 (`max(page/2, toastBoxLines)`) が握るので、追い出しだけ重要度を見ても狭い端末では
「保持しているのに重要な通知だけ画面に出ない」状態が残っていた (レビュー実測: popup 高さ ≤20 で
後続 1 枚、≤28 で後続 2 枚あると最古が描かれない)。

重要度の判定を `toastItem.important()` の 1 箇所に切り出し、**追い出し (`evictOne`) と描画予算の
選別 (`boxLines`) がそれを共有する**形にした。予算に入らないときは重要でない (成功/進行中) 枚を
先に落とし、同じ重要度なら古い方から落とす。⚠️ 残す枚の並びは元のまま (上が新しい) —
重要な枚を上へ繰り上げるとスタックの読み方が崩れるため。テスト 1 本 + 変異 2 種 (選別をやめる /
規則を逆転) で red を確認。

`toastStackMax` が保持数であって表示数ではないこと自体は変えていない (窓の高さに対する占有を
抑える上限として別の役割がある)。揃えたのは「落とす順の規則」。

**P3 の追記漏れが実際に起きていたので補充した**。`spinnerActive` はその後
「演出は列挙せず `tickInterval` から導出する」形へ変わり (演出の登録先を 1 箇所に集約)、
非同期源として `issuesOv.loading()` / `statusOv.fetching()` が後から加わっていた。
`TestBrowseSpinnerActiveSources` のテーブルにはこの 2 つが無く、issue 本文が
「捕まえられない」と書いていたケースがそのまま実現していた。2 行足し、それぞれを
常に false にする変異で red を確認した。

**以下は 2026-07-25 時点の記録 (P1 / P3 / P4 の完了内容)。**

- **P1 完了**: `buildPanelBoxImpl` の `shadow bool` / `b boxBorder` / `border string` を
  `panelBoxStyle{shadow, glyphs, color}` に畳んだ。呼び出しは内部 3 箇所のみで公開ラッパー
  (`buildPanelBox` / `buildShadowPanelBox`) のシグネチャは不変なので外への波及なし
- **P4 完了**: `usageBoxChrome` (usage_overlay.go) → `shadowBoxChrome` (box.go) へ移動・改名。
  値は 5 のまま。toast が usage overlay の定数を借用する結合を切った
- **P3 完了 (案 B)**: `TestBrowseSpinnerActiveSources` を追加し、14 の非同期/アニメ源を 1 つずつ
  立てて `spinnerActive()` が true になることを table で網羅。OR の 1 項 (`m.scrollAnim`) を
  削って実際に FAIL することを確認済み (mutation テスト)。
  ⚠️ **ただし issue 本文の「追記漏れが test 失敗として現れる」は不正確**: 新しい源を足して
  `spinnerActive` への追記を忘れるケースは、テーブルへの追記も同時に忘れるため検出できない。
  このテストが守るのは「既存の項の削除・条件反転」の回帰と、源の一覧のドキュメント化。
  案 A (登録式) を採らなかった理由も同じ (登録漏れに問題が移るだけ = 複雑性の移動)
- **P2 未着手** (上記 trigger 待ち。素朴なキュー化は「後勝ち」設計の逆転になるため不可)
