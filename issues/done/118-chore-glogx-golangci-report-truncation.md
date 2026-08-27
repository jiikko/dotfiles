# 118 chore: golangci の指摘が既定 3 件で打ち切られ、一括退行の修正に往復が要る

起票日: 2026-08-27 / 出典: lint-from-done 監査 / priority: low

## 事実

`src/glogx/.golangci.yml` の `issues:` セクションは `uniq-by-line: false` を
**「せっかくの強制が別 linter の陰で false green になるのを防ぐ」**という理由つきで設定している。
一方 `max-same-issues` は未設定で、golangci の既定 **3** が効いている。

**実測 (監査が変異検証)**: `issues/done/038-refactor-glogx-push-anim-use-timenow-seam.md` の退行を
再現するため `tui.go` の `timeNow()` 呼び出し **20 箇所**を `time.Now()` に戻して forbidigo を回すと:

| config | 報告件数 |
|---|---|
| 現行 (既定 3) | **3 件** |
| `max-same-issues: 0` | **20 件** |

## ⚠️ これは false green ではない (実験で確認済み)

除外ルールは**上限の前**に適用される。「forbidigo 除外の `_test.go` に 5 件 + `tui.go` に本物 1 件」
を作った probe では、本物 1 件が正しく報告された。**`make lint` は赤のまま落ちる。**

実害は「20 箇所直すのに lint を 7 往復させられる」= **報告の不完全さ**であって、退行の素通りではない。
だから priority は low。

## 対応

`issues:` に `max-same-issues: 0` を 1 行 (必要なら `max-issues-per-linter: 0` も)。
`uniq-by-line: false` の隣に、同じ理由づけのコメントを添える。

---

## 併せて: `padSpaces` への置換残りが 2 件 (lint ルール化は見送り)

`issues/done/047-perf-glogx-frame-alloc-amplification.md` が
`strings.Repeat(" ", n)` → `padSpaces(n)` (無確保ヘルパ) の置換を 3 箇所で行ったが、
**package main の production に 2 件残っている**:

- `src/glogx/render.go` (SGR 復元の pad)
- `src/glogx/tui.go` (dim 描画の左詰め)

**ruleguard ルール化は見送った**。理由: repo 全体の `strings.Repeat(" "` は 12 件で、
うち真の対象は上記 2 件のみ。残り 10 件は `padSpaces` の実装本体 (1) / テスト (2、うち 1 件は
`padSpaces ≡ strings.Repeat` の同値テスト) / `padSpaces` を参照できない別パッケージ (6)。
**例外設定 4 エントリを足して 2 件を守る**費用対効果が見合わない。
なお ruleguard で書けば元の 3 行を実際に flag できることは監査が検証済み
(この判断を覆すなら、その実験結果は再利用できる)。

→ 2 件はこの issue のついでに手で直せば済む。

---

## 対応 (2026-08-28)

### (1) 報告の打ち切り

`max-same-issues: 0` と `max-issues-per-linter: 0` を足した。**実測で確認**
(監査が述べた形をそのまま再現。`tui.go` の `timeNow()` 20 箇所を `time.Now()` へ戻す):

| config | forbidigo の報告 |
|---|---|
| 既定 (未設定) | **4 行** (指摘 3 + 集計 1) |
| `max-same-issues: 0` | **21 行** (指摘 20 + 集計 1) |

### (2) `padSpaces` の置換 — 見送りの前提が 106 で崩れていた

118 を起票した時点では「別パッケージから `padSpaces` を参照できないので例外が 4 つ要る」
という理由で ruleguard 化を見送っていた。**その前提は issue 106 で崩れている** —
`PadSpaces` が leaf パッケージ `termwidth` へ移り、全パッケージから参照できるようになった。

そこで残っていた `strings.Repeat(" ", n)` を**全部**置換した (main 2 / issues 4 / usage 2)。
非テストで残るのは実装本体 (`termwidth.go`) だけになり、ruleguard ルール
`padViaPadSpaces` の例外は **テストと実装本体の 2 つ**で済むようになった。

テストを例外にするのは、期待値の組み立てに `strings.Repeat` が要るため
(`frame_alloc_test.go` は「`PadSpaces` ≡ `strings.Repeat`」の同値検証そのものを持っており、
置換すると自己言及になる)。

### ⚠️ golangci-lint のキャッシュに騙されかけた

ルールを足した直後の実行が **`0 issues`** を返したので「例外なしで通った」と読みかけたが、
`cache clean` してから走らせると**違反 3 件**が出た (テスト 2 + 実装本体)。

**ルールを新設した直後の緑は、キャッシュ由来かもしれない。** 発火の確認は
`cache clean` の後に行い、さらに production へ違反を入れて名指しで落ちることまで見た
(`box.go:55:13: ruleguard: 空白の連結は termwidth.PadSpaces(leftGap) を使う`)。

### 検証の履歴 (誤った測り方も残す)

最初は ruleguard の違反を 5 箇所作って `max-same-issues` の効果を測ろうとしたが、
**設定の有無で件数が変わらなかった** (どちらも 5)。原因は ruleguard のメッセージが site ごとに
異なり「同一 issue」として畳まれないため。**同一文面が出る forbidigo** で測り直して上表を得た。
