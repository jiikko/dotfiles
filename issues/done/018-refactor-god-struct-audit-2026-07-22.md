# 018 refactor: 残存 God struct の監査と抽出候補 (2026-07-22)

## 背景

glogx の `browseModel` から 4 サブコンポーネント (usageOverlay / diffOverlay / actionModal /
jobDetailOverlay) を抽出したのを機に、src 配下の Go プロジェクト全体で「まだ巨大クラス
(God struct) が残っていないか」を多エージェントで監査した (依存マップ → 抽出候補の敵対的判定 →
統合)。本 issue はその結論と、次に手を入れる価値がある抽出候補・逆に「抽出しない」と判断した
ものの記録。

**評価原則** ([`verify-design-intent-before-refactor.md`](../_claude/rules/verify-design-intent-before-refactor.md)):
リファクタの目的は「複雑性を下げる」こと。行数分割は複雑性の**移動**にすぎず不可。抽出価値は
「認知負荷 (読む時の jump 数 / 変更時の touch 箇所)・結合・重複・状態の局所化」が実際に下がるかで
判定する。「切り出さない (現状維持)」も正当な結論。

## 監査対象と全体判定

| struct | 場所 | フィールド | 判定 |
|---|---|---|---|
| `browseModel` | src/glogx/tui.go | 47 | **完了。新規抽出の価値なし** |
| `model` | src/parallel-each/tui.go | 49 | overlay パターン未適用。抽出候補あり |
| `Runner` | src/parallel-each/runner.go | 32 | 並行プリミティブを一部切り出せる |
| ~~`browseModel`~~ | ~~src/glog/tui.go~~ | ~~37~~ | **削除済み** (glog を廃止・commit 40d4a28) |

どれも「行数だけの God」ではなく、大半は不可分なドメイン/並行不変条件の写像。

## 抽出で複雑性が実際に下がる候補 (優先順)

いずれも着手時は glogx と同じ **test-first (characterization test で振る舞いを固定 → 抽出 →
差分ゼロ確認)** で行うこと。着手前に各候補をコード再確認し、複雑性が実際に下がるかを再判定する
(下記は監査時点の評価)。

### P1: Runner `pauseGate` の抽出 (clean win) — ✅ 完了 (commit b30304b)

- 対象フィールド: `pauseMu` / `paused` / `pauseCh` (runner.go:248-250)
- 本物の可逆条件ゲート。他 29 フィールドへの参照ゼロで自己完結し、単体テスト可能な並行
  プリミティブとして切り出せる。32 フィールドの Runner から最もリスク低く抜ける。
- 実施: pause_gate.go へ切り出し。stop シグナルは `waitUntilResumed(done)` へ引数注入し stopCtx
  結合を持たない。`wakePause` は冗長だったので `wake()` に畳んで Runner から削除。単体テスト 5 本
  (-race・mutation で load-bearing 確認)。

### P2: Runner `resultLogWriter` (2 フィールド再スコープ) — ✅ 完了 (commit b30304b)

- 対象フィールド: `resultMu` / `resultLog` (runner.go:227-228) の **2 つだけ** (logDirAbs/width は
  別責務なので含めない)。
- 型が内部で thread-safe になり、Runner 側はロックを意識せず呼べる。race を推論すべき箇所が
  3 → 0 に減る。
- 実施: result_log.go へ append / rewriteExcluding / close を切り出し。初期 open (cfg.Fresh の
  分岐) は Start に残し、開いた handle + path をラップ。単体テスト 7 本 (-race・mutation 確認)。

### P3: Runner `dispatchQueue` (条件付き・中〜小)

- 対象フィールド: `queueMu` / `queue` / `queueWake` / `nextIndex` (runner.go:262-265)
- commit-by-index 不変条件の局所化が主便益。**境界を queue 操作で止めること** (dispatcher の
  select 送信まで型へ飲み込むと worker pool と癒着して fusion に転落し「移動」になる)。

### P4: parallel-each `exportOverlay` (条件付き・小)

- 対象フィールド: `otherMenu` / `exportInput` / `exportBuf` / `exportTargetDir` (tui.go:98-101)
- **`tea.Cmd` → `exportResultMsg` 方式で `setFlash` を親に戻すこと** (flash sink を mutating 注入
  すると型が閉じず「移動」に降格する)。

## 「大きいが分割 = 移動」で現状維持が正しいもの (再提案しない)

将来の audit / レビューで同じ分割提案が再生成されるのを防ぐため記録する。

- **glogx `panel-frame`** (panelSHA / panelCursor / panelPollSeq / panelRefresh): panelSHA が CI
  マップ (details/statuses) の索引キーそのもの・etaBasis が全コミット走査・detailMsg が CI マージと
  パネル寿命判定を融合・panelRefresh が共有 refresh ロック。抽出は結合を型境界へ露出させるだけ。
  コード内コメント (tui.go の browseModel 定義近傍) に残置理由を文書化済み。
- **glogx `scroll-glide`** (offsetShown / scrollAnim / scrollFrom / scrollFrame): 論理 offset を
  毎フレーム注入する必要があり非自律。scrollAnim が 9 箇所へ leak し切る結合が残る。
- **glogx `tmux-prefix-guard`** (tmuxPrefix / prefixPending / prefixNote): CI 結合はゼロだが規模が
  極小で、切る結合が無く局所化だけ。価値が小さい。
- **glogx の CI 取得状態機械 / viewport / push poll / PR キャッシュ / lines メモ化 / render config**:
  いずれも commits/statuses/offset を共有substrate として read/write する CI ブラウジングの
  ドメイン写像。抽出は全消費者への注入し直しで結合を切らない。
- **parallel-each `shutdownController` / `parChangeOverlay` / `filterableListView`**: それぞれ
  外部変異する stopping フラグ・value-receiver idiom への pointer overlay 混入・所有権が逆の
  recent/queue 融合で、型に閉じると「移動」になる。
- **Runner の worker pool / dedup set / run locks**: cross-goroutine teardown ordering 不変条件を
  共有。抽出はコールバック + 共有シグナルへの移動。

## speculative (trigger 待ち)

- **parallel-each の 6 サブフローを overlay idiom へ揃える**: exportOverlay 単体より構造的には
  正しい方向だが大改修。次に該当フローを機能追加で触るときの trigger 待ちとする (先回り分解は
  しない)。

## 監査メモ

- glogx の `browseModel` は overlay 4 型抽出済み + panel-frame 意図的残置 + ドメイン core で、
  **これ以上触る価値はない** (候補に挙がった scroll-glide / tmux-prefix も敵対的判定で「移動」と
  却下)。
- glog は本 issue 作成と同時に廃止 (未使用・glogx に一本化。commit 40d4a28)。監査当初は glog への
  overlay back-port が最優先候補だったが、削除により moot。

## 完了 (2026-07-25): P3 実施 / P4 却下 → クローズ

### P3: Runner `dispatchQueue` の抽出 — ✅ 完了 (commit 9b5e738)

`dispatch_queue.go` へ切り出し。queue 関連 5 フィールド (`queueMu` / `queue` / `queueWake` /
`nextIndex` / `live`) → 1 フィールド (`*dispatchQueue`)、`queueMu` を取る 12 箇所が runner.go から
消えた (1151 → 1053 行)。既存の Runner 側テスト 18 本は無改変で green + 型の単体テスト 10 本 (-race)。

境界は本 issue の指示どおり「queue 操作」で止めた (dedup / -F 追記 / worker への select /
stopCtx は Runner 側に残置。peek は停止シグナルを channel 引数で受ける = pauseGate と同型)。

着手時の再判定で分かった、監査時点の評価とのズレ 2 点:

- **有利だった点**: `live` は `SetLive` が「Start 前のみ」の契約で、実行中は不変。よって注入では
  なくキュー側の単一出典に移せた (従来は「無同期書き込み + `queueMu` 下読み」だったので同期も締まった)
- **不利だった点**: queue の**振る舞い**は既に Runner API レベルの 18 本 (`EnqueueFront...` /
  `RemovePending` / `PendingSnapshot` / `EnqueueRejectedAfterStop` 等) が押さえており、
  「切り出して初めてテストできる」P1/P2 とは状況が違った。よって実利は主に *locking の局所化*。
  ただし **peek → commit の間に `EnqueueFront` が割り込む経路** は直接突くテストが無く、
  今回それが単体テストで固定された (mutation: commit を位置照合へ退化させると 3 本 FAIL)

### P4: parallel-each `exportOverlay` — ❌ 却下 (現状維持)

着手前にコードを再確認して却下した。理由:

- 状態機械は 3 状態で、読み書きは `handleOtherMenuKey` / `handleExportKey` の **2 関数に既に
  閉じている** (tui.go の 27 参照はほぼこの 2 関数 + render 1 箇所)
- 外への結合は `setFlash` の 3 呼び出しだけ。型に閉じるには本 issue が指示するとおり
  `exportResultMsg` 経由が必須で、**メッセージ型 + Update の case を足して直接呼び 3 つを
  置き換える**形になる = 結合を切らずにホップを増やす「移動」
- 6 サブフローのうち 1 つだけ overlay 化しても一貫性は上がらない (全体を揃えるのは下記 trigger 待ち)

### クローズ理由

抽出価値のある候補 (P1/P2/P3) は完了、P4 は却下、残るのは speculative の
「parallel-each の 6 サブフローを overlay idiom へ揃える」だけ。これは**該当フローを機能追加で
触るときの trigger 待ち**で、先回りしない方針が正しいため本 issue は閉じる (trigger が来たら
新しい issue を起こす)。glogx `browseModel` は「これ以上触る価値なし」の判定を維持する。
