# 502 (perf): `claude agents --json` を画面と dispatcher がそれぞれ 3 秒ごとに起動する (常に CPU 約 0.2 コア)

起票日: 2026-09-26

## 概要

session の一覧 `claude agents --json` (node のプロセス) を、dispatcher の tick と、開いている画面ごとの読み直しの両方が 3 秒ごとに起動している。
同じ一覧を 2 か所 (画面が増えれば 3 か所以上) で取り直していて、1 回が CPU 約 0.3 秒・RSS 184MB。画面の描画と同じマシンの CPU を取り合う
(494 のカクつきの背景の 1 つの可能性。未確認)。

## 実測 (2026-09-26、Claude Code 2.1.283、Apple Silicon)

| 何を | 値 |
|---|---|
| `claude agents --json` 1 回 (`/usr/bin/time -l`、3 回) | real 0.17 s / user 0.14 s / sys 0.13〜0.18 s / 最大 RSS 184 MB、出力 3.6 KB |
| 15 秒間に起動された数 (`ps` を約 30 ms ごとに見て pid を数えた。短いものは取りこぼしうる) | 9 回。親は画面 (`pro-con --join`) と dispatcher がほぼ半々 |
| 合計 | 約 0.6 回/秒 × CPU 0.3 秒 ≈ **常に 0.2 コア**。画面を 1 つ足すごとに約 0.1 コア増える |

## 詳細

- 画面: `live/live.go` の `Start` の ticker (`Interval` = 3 秒) → `Refresh` → `refresh(ctx, true)` → `b.list` (= `execList`)。
  dispatcher に起こされたとき (`kick`) は一覧を取り直さない
- dispatcher: `dispatchercmd.go` の `serve` が `dispatcherInterval` (3 秒) か wake ごとに `Tick` → `dispatcher.go` の `tick` → `d.List`。
  依頼を置くたびの wake でも回るので、操作の多いときは 3 秒より頻繁になる
- 画面が一覧から使っているのは、pro-con が起動した session の status / pid と、作業中のカードの PG の一覧 (`Consumers`) だけ

## 対応方針 (案)

- dispatcher が取った一覧を、ほかの「集めた様子」と同じく派生の記録 (`store.LoadDerived` の仲間) に書き、画面はそれを読む
  (473 / 469 の「画面は ps も git も transcript の全体も読まない」と同じ向き)。画面が自分で `claude agents --json` を起動するのは
  dispatcher が回っていないときだけにする
- 画面が自分で一覧を取るのは `refresh` (3 秒ごと) と `AttachCommand` (attach の直前の照合。3 秒ごとではない) の 2 か所。
  dispatcher が止まっている表示 (`ui/view.go` の `dispatcherStopped`) は一覧ではなく dispatcher の状態ファイルを見ている (反証レビューで確認)。
  寄せた後も、派生の記録が古い (dispatcher が回っていない) ときは画面が自分で取る (正しさを dispatcher に預けない)
- 効果は「15 秒間の起動数」と「画面プロセス + 子の CPU 時間」で before / after を測る

## 関連ファイル

- `src/pro-con/live/live.go` — `Start` / `refresh` / `execList`
- `src/pro-con/dispatchercmd.go` — `serve` / `dispatcherInterval`
- `src/pro-con/dispatcher/dispatcher.go` — `tick` / `List`
- `src/pro-con/agents/agents.go`

## 進捗

- [x] 実測 (上の表)
- [x] 反証レビュー (読み取り専用のサブエージェント 1 体): 起動の経路と周期は反証されず。`dispatcherStopped` が一覧を使うという注記は誤りだったので直した
- [ ] 対応方針の要否と形を決める
