# 513 (research): pro-con の性能の悪そうな箇所の監査 (2026-09-26)

起票日: 2026-09-26

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-26、カード C-071): 「pro-con でパフォーマンスの悪そうな箇所を探して issues ディレクトリに書き出して」。

pro-con (`src/pro-con` と、それが使う `src/tuikit` / `src/procsup`) の性能の悪そうな箇所を探し、見つけたものを issue に書き出す。
**監査なのでコード (`src/`) は直さない** (ほかのカードとぶつからないように)。形は 479 (リソースリークの監査) に倣う。

## やること

- 観点 (例): dispatcher の Tick ごとの仕事 (ファイルの読み直し・外部コマンドの起動・JSON の解析)、画面の描画 (フレームごとの割り当て・文字幅の計算・
  全カードの走査)、transcript・events.jsonl・runs のように伸び続けるファイルの読み方、socket の知らせの頻度と大きさ、見張り・取り込みの係・要約の係が起こす
  `claude -p` と `git` の回数
- 🚨 **性能の主張は計測で裏を取る** (perf-claims-need-measurement)。「遅そう」だけで issue にしない。
  計測は module を `mktemp -d` へ写した中の go test / benchmark で行い、本物の pro-con・本物の state dir・本物の claude の session には触らない
  (本物に対しては ps・ファイルの大きさを読むだけ)
- 走査した母集合を機械で数えて本文に書く (479 と同じ)。見つけたものを「issue にしたもの / 記録のみ / 却下」に分けて全数を勘定する
- 1 件 1 issue で起票する (`perf` の型)。issue にした番号と、記録のみにした理由をこの本文に書き戻す

## 既知のもの (重ねて起票しない)

- 478 cards.json が終えたカードも持ち続け毎回読み直す / 494 アニメのフレームの割り当てと GC /
  502 claude agents の一覧を画面と dispatcher がそれぞれ起こす / 503 transcript の末尾を毎回読み直す / 504 relay が 83KB のフレームを秒 8 回書く
  (どれも `issues/epic/415/done/`)
- 479 (リソースリークの監査) の「記録のみ」

## 結果 (2026-09-26、master 473f5ec7)

- 走査: 読み取り専用のサブエージェント 3 体に領域を分けて読ませた (dispatcher・store 系 / 画面・live・relay・tuikit / 外部コマンド・見張り・片付け)。
  母集合は非テストの Go で pro-con 113 (うち割り当てから漏れたのは `configcmd.go` と `e2ecmd.go` の 2 本。どちらも 1 回きりのコマンド) / tuikit 25 のうち画面が使う 21 / process_supervisor 1
- 計測: module を `mktemp -d` へ写し、本物の `cards.json` の写し (54 枚・296KB) で benchmark した。本物に対しては ps・ファイルの大きさ・画面の観測ログ
  (`framelog.tsv`。494 の口) を読んだだけ
- 本物の様子 (21:14): 画面 (`pro-con` の TUI) は CPU 約 3〜4% (ps の CPU 時間の差分。作業中の PG が居てスピナーが回っている)。dispatcher は 8 分で 0.9 秒、
  見張り (`monitor`) は 3.5 分で 0.09 秒。画面の観測ログでは View が p50 1.6ms / p90 4.8ms / p99 12.8ms (2,207 回)、Update はどの種類も平均 0.1〜0.5ms

### 全数勘定: 候補 16 = issue にした 3 (2 件に束ねた) / 記録のみ 12 / 却下 1

候補はサブエージェントの 15 (dispatcher・store 系 6 / 画面系 4 / 外部コマンド系 5) と、本物の画面の CPU の観測 1。

#### issue にしたもの

- [528](../528-perf-pro-con-tick-rereads-record-after-busy-day.md): 完了して 24 時間以内のカードが溜まる日は、Tick 1 回が 25ms・11.7MB (478 の見送りの前提が崩れた)。
  「1 Tick で記録を十数回読む」「状態の変わったカードごとに `store.Update` が全体を読み書きする」の 2 候補をこの 1 件にまとめた
- [529](529-perf-pro-con-diff-panel-rebuilds-all-rows-every-view.md): 差分の板は描くたびに全行 (最大 5000 行) を組み直す (1 描画 5.6ms・3.6MB。板を閉じた画面は 0.6ms)

#### 記録のみ (影響が小さい・意図した形・測れていない)

- **画面の常時 3〜4% の CPU はほぼ View** (スピナーで毎秒 10 回 × 本物の p50 1.6ms)。プロファイル (本物の記録の写し・200x55) では View の約半分が
  `overlayCursor` → `softSide` / `splice` の文字列の切り貼り (`ansi.Truncate` / `StringWidth`)、`lanes()` の並べ替えが約 1/4
  (`lanes()` は 1 回 17µs だが、Update の後と View で 6 回以上呼ばれる。`gauge` / `tabBar` も別々に全カードを舐める。この 3 候補と観測をこの項に束ねた)。`splice` / `fit` の作り直しは 494 が見送り済みで、その trigger (揺れの 1 コマが 5ms 超) には当たっていない。
  trigger: 画面の CPU が 10% を超える / 観測ログの View の p50 が 5ms を超える
- **見張りの衝突の検査は、同じ repo の未取り込みのカードの組 (k²) ごとに `git merge-tree`** (`monitor/monitor.go` の `conflicts`)。commit の組で覚えるので、
  git を起こすのは HEAD が動いたときだけ (1 commit で最大 k 回)。本物の見張りは 3.5 分で CPU 0.09 秒。trigger: 同じ repo で動くカードが 10 枚を超える
- **`pro-con worktree clean` は消す 1 件ごとに判定の材料を丸ごと取り直す** (`wtclean/clean.go` の `Clean` / `CleanSessions` → `worktreeInputs`: `claude agents` 約 0.15 秒 + `lsof` 約 0.2 秒 +
  git 数本。所要はコードのコメントの実測で、今回は測っていない)。「取り直さずに消さない」は安全のための意図した形で、人が叩く 1 回きりのコマンド。
  trigger: 片付けが 1 回に数十件になり、待ちが気になる
- **`diskuse` は worktree と transcript を全部たどって大きさを足す** (`diskuse/diskuse.go` の `Size`)。コメントに「worktree 数十個で数秒」とある (意図して受けている)。
  呼ぶのは `pro-con du` と設定画面を開いたとき (周期では呼ばない)
- **dispatcher は 30 秒ごとに `zsh -c` で新しい版の有無を見る** (`dispupgrade.go` の `upgradeCheckEvery`)。1 回の所要は測っていない。カード数に依らない固定の費用
- `pro-con screen --follow` は 200ms ごとに、追う 1 画面の生死を見るのに全画面の lock を開く (`screencmd.go` の `stillOpen` → `relay.List`)。画面は普段 1〜2 個
- `register` のカード × session の二重ループ / `dispatch` の順番待ち (`card.HeldBy` の線形探索) / `tagScreen`: どれも 528 の Tick の bench の中に含まれ、単独では目立たない
- 進み具合の係の `findIssue` は `issues/` を毎回たどる (`dispatcher/progress.go`)。30 秒ごと・裏の goroutine で、Tick を止めない

#### 却下

- `wake.Broadcast`: 開いている画面へ 8 バイトを送るだけ

既知の 478 / 494 / 502 / 503 / 504 は走査の前に除いた (候補に数えていない)。528 は 478 の見送りの再評価で、重ねた起票ではない。

## 関連

- 479 (リソースリークの監査。形の前例) / 460 (脆さの監査)
