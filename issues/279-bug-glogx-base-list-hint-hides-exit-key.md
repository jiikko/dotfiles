# bug: 基底一覧の hint が 174 桁で、既定幅では出口 `q: 終了` が常に見えない

起票日: 2026-09-06
カテゴリ: bug
優先度: 高（既定の画面・既定の幅で、抜ける手段の案内が消えている）

## 何が起きているか

`tui.go:hintLine` の既定分岐（全画面ビューアが出ていない = 通常の git log 一覧）の hint は
**固定文字列**で、`fitHintItems` を通らない:

```go
hint := "j/k: 移動  Enter: CI job  d: diff  o: ブラウザ  p: PR  P: PR 状態  y: URL コピー  b: push  u: pull  i: issues  U: usage  R: 残量  C: update  D: doctor  w: 警告コピー  q: 終了"
```

出口 `q: 終了` が**末尾**にあり、幅が足りないと真っ先に切られる。

## 実測

```
基底一覧 hint の表示幅 = 174

端末  91: 予算  89 / 出口 'q: 終了' が残る = false
端末 120: 予算 118 / 出口 'q: 終了' が残る = false
端末 176: 予算 174 / 出口 'q: 終了' が残る = true
端末 200: 予算 198 / 出口 'q: 終了' が残る = true
```

**端末 176 桁未満では出口が案内から消える。** 一般的な端末幅（80〜120）では常に消えている。

`docs/glogx-ui-guide.md` の hint 節は「抜ける手段（`q` / 閉じるキー）は必ず残す」を
**無条件で**規定しており、この形はその規定に反する。

## 同じ形が他に 2 本ある（issue 264 の数え上げが漏らしていた）

`issues/done/264` は固定文字列の hint を **3 本**（issues 本文 / commit diff / status pager）と
数え上げて `fitHintItems` へ寄せたが、**その数え上げに入っていない固定文字列 hint が 3 本残っている**:

| 場所 | 表示幅 | 症状 |
|---|---:|---|
| `tui.go:hintLine` の既定分岐（基底一覧） | **174** | 端末 176 未満で出口が消える（= ほぼ常時） |
| `ratelimit_dashboard.go:ratelimitDash.hint` | 70 | `hint()` は **width 引数を持たない**。最小サポート幅 `frameMinWidth=60`（予算 58）で切り詰め。端末 30〜47 桁で出口 `R/q/esc/h: 閉じる` が完全に消える |
| `tui.go:hintLine` の `prStatusOv.visible()` 分岐 | 52 | 端末 52 桁未満で出口 `P/q/h: 閉じる` から切れる |

他の 3 ビューア（`doctorView.hint` / `statusView.hint` / `issuesView.hint`）はいずれも
**`hint(width int)`** を受け取って `fitHintItems` を通す。`ratelimitDash.hint()` **だけ**が
シグネチャからしてその規律の外にある。

🚨 rlDash は全画面かつ `rlDashSwallow` で裏へキーを通さないので、出口が案内から消えると
**画面いっぱいのダッシュボードから抜ける手段が分からなくなる**。

## `fitHintItems` に寄せるだけでは足りない（優先度は席を予約しない）

`status_view.go:fitHintItems` の doc は「優先度 1 に『抜ける手段』を置けば、幅が狭くても
それだけは残る」と書いているが、**優先度は採る順序を決めるだけで席を予約しない**。
敵対的な実測:

- **入る項目が 1 つも無いとき、元の並びの「最後」を返す**。この極狭フォールバックを
  守っているテストは 1 本も無い（`return items[len(items)-1].text` → `return ""` の変異で
  **全スイート GREEN**、`go build` 通過を確認）
- `doctorView.hint` は末尾に `x: N 件を実行`（prio 2）を append しうるので、
  **選択中は出口ではなく `x: …` が出る**
- `detailOv` の末尾は `Y: 詳細コピー`（prio 5）なので、出口 `Enter/h/q: 戻る`（prio 1）が消える
- `diffOv` / job パネルが無事なのは末尾がたまたま出口だから = **構造ではなく並び順に依存**
- さらに手前でも壊れる: doctor の出口は `D/q/esc: 閉じる`（17 桁）、最短項目は
  `j/k: 移動`（9 桁）。**幅 9〜16 では出口が落ちて `j/k` だけが残る**
  （`hint_width_test.go` の doctor ケースは幅 20 しか見ておらず、この帯は無検査）

## 発火条件

- 基底一覧: 端末幅 < 176。**既定の画面・一般的な幅で常時**
- rlDash: 端末幅 < 56 で出口が消える（切り詰め自体は < 72 から）
- prStatusOv: 端末幅 < 52
- 極狭フォールバック / 並び順の問題: 上記の各条件
- **silent に壊れる**: 3 本とも参照するテストが 0 件。CI も lint も止めない
  （`grep '毎分自動更新|PR をブラウザで開く' --include='*_test.go'` は 0 件）

## 推奨対応

**個別に固定文字列を詰め直す（264 と同じ手当て）を繰り返さない。** 3 度目なので、
生成契約を型で閉じる:

1. `hintLine` が呼ぶ**全経路を `hint(width int) string` に揃え**、生成は必ず `fitHintItems` を通す
   （`ratelimitDash.hint()` → `hint(width int)`、prStatusOv と基底一覧の文字列リテラルは
   `hintItem` 表へ。出口は優先度 1）。これで「幅を無視した hint」を**表現不可能**にする
2. `fitHintItems` を「**優先度 1 の項目を先に予約する**」形に直す
   （prio 1 が入らない幅なら prio 1 を単独で返す）。または doc を
   「優先度は採る順序であって残ることを保証しない」に訂正する。どちらにせよ
   **極狭フォールバックに変異で red になるテストを付ける**
3. ゲート側は issue 280 で扱う（列挙表 → 走査 / レジストリ）

## 反証の試み

`src/glogx/CLAUDE.md` / `README.md` / `docs/glogx-ui-guide.md` の hint 節 /
`issues/done/` の 264・201・155 / 全 `*_test.go` を探索。264 の数え上げに 3 本が
入っていない事実はあるが、**除外した・意図的であるという記述はどこにも無い**。
ui-guide にも基底一覧の免除は無い。

🚨 `issues/done/201` は当該サイトを表に載せて
「`ratelimit_dashboard.go` | 70 桁 | 現状 OK（ただし無検査）」と記録しており、
**無検査であることは既知**。ただし 201 が設計した走査型ゲート（→ 280）が入っていれば
検査対象になっていたサイト。

## 関連

- 280（hint 幅ゲートが 201 の設計どおりでない件）
- `issues/done/264`（固定文字列 hint 3 本を寄せた。数え上げが不完全だった）
- `issues/done/201` / `issues/done/155`（同じ失敗モードの過去 2 回）
- `issues/done/242`（`fitHintItems` の既知リスク。prio が 1..7 の範囲外の項目。本件とは別）
