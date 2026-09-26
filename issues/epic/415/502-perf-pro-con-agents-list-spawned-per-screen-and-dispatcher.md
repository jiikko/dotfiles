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
- [x] 画面が dispatcher の一覧を使う (commit「pro-con: 画面は dispatcher が書いた一覧と出力の末尾を読む (502・503)」):
  dispatcher は tick で一覧を取って登録した後に `store.Seen` (`seen.json`: 一覧と、pro-con が起動した session の出力の末尾) を書く
  (`dispatcher/doing.go` の `publishSeen`)。画面の `refresh` は、これが `seenFresh` (15 秒 = tick 3 秒 + 一覧の上限 10 秒) より新しければ使い、
  自分では `claude agents --json` も transcript も読まない。古い・無い (dispatcher が回っていない・一覧を取れない) ときは今までどおり自分で読む。
  「pro-con が起動した session」の判定は `live.OwnedSessions` に寄せ、画面と dispatcher が同じものを使う。
  テスト: `TestRefreshUsesFreshSeenFromDispatcher` (新しい / 古い) と `TestTickPublishesSeenForScreens`。`bin/mutate-verify` で
  画面が記録を使わない / 古さを見ない / tick が書かない / 外の session の出力も載せる / 出力の末尾を切らない の 5 本が red
- [ ] 実機で 15 秒間の起動数を測り直す (画面と dispatcher が新しいビルドで起動し直した後。見込みは dispatcher の分だけ = 画面の数によらず約 0.33 回/秒。未実測)
- [x] 敵対的レビュー (opus、読み取り専用 1 体ずつ 3 周。502〜504 まとめて):
  1 周目 P2 (中継: 書かない描き直しを「続いている」に数えて操作の中継が 1 秒遅れる) と P3 (seen.json の時刻が未来 / dispatcher が一覧を取れない tick で画面が前の一覧を
  理由なしに使う / テストの差し替えの競合 / 一覧の出所を区別しないテスト / 取れない tick の検査が無い) を直した (commit「pro-con: 502〜504 の敵対的レビューの指摘を直す」)。
  2 周目 P3 (知らせの読み直しは一覧を取り直さないので、記録が古くなっても前の一覧を使う / Seen の時刻が一覧を取る前の時刻) を直した
  (commit「pro-con: 知らせの読み直しでも古くなった dispatcher の一覧を使い続けない (502)」)
- 記録のみ (2 周目 P3。元からある形): dispatcher も画面も一覧を取れないと、画面は一覧を捨ててから取り直すので PG の様子が消えるのに、理由の文は
  「PG の様子は古いまま」になる (`live.go` の `refresh`)。前は dispatcher の一覧で最長 15 秒埋まったが、今は dispatcher の失敗 1 回で入る
- [x] 敵対的レビュー 3 周目: P1 / P2 なし。P3-1 (Seen の時刻を書く時点にした変更をテストが守っていない) は `TestTickPublishesSeenAtWriteTime` で固定
  (変異: tick の頭の時刻に戻す → red)。修正はテストだけなので周回はここで閉じた
- 記録のみ (3 周目 P3-2。頻度は未確認): tick の後半の処理と次の tick の頭までの間が 15 秒を超えると、箱の依頼を適用した直後の知らせの読み直しで
  記録がもう古く、画面が claude agents (最長 10 秒) を待ってからカードを出す。1 回で止まり、正しさは壊れない
- 残り: `AttachCommand` (attach の直前の照合) は今も画面が自分で一覧を取る (操作のたびの 1 回で、3 秒ごとではないので残す)
