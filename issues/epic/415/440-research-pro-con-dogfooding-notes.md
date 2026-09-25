# 440 (research): pro-con を Claude が使って pro-con を開発する (dogfooding) — 手触りと改善案

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

Claude (対話の session) が PM の役をして、pro-con で PG を起動し、pro-con 自体の開発を回す。使ってみた手触りと改善案をここに書く
(2026-09-25 にユーザーが依頼: 「pro-con を Claude 自身が立ち上げて使った場合、手触りとか改善案とかを別の issue に書いて」)。

## やり方

- PM の役は Claude が CLI (`pro-con card add / plan / answer / close`) で行う (PM を pro-con が起こす仕組みはまだ無い = 437)
- 1 枚目のカードは 428 (attach 中の指示をカードに残す) の予定。`--limit 1`。PG は `claude --bg -w pc-c-NNN` の worktree で作業し、
  Claude がレビューして cherry-pick → make test → push する
- PG が pro-con のコードを書いている間、Claude は pro-con のコードを触らない (PM に徹する)

## 1 回目 (2026-09-25 16:40〜17:25。カード C-002 = issue 428。手で起動した dispatcher `--limit 1`)

経過: 依頼 → 分解 → PG 起動 (約 3 秒) → PG が約 14 分作業 → `pro-con card run C-002 -- make test` (約 6 分、rc=0) → 結果つきで再開 →
レビュー待ち → Claude (PM) が diff を読み、master へ cherry-pick → make test → push (84cfc4e3) → カードを完了。
PG は変異 13 本・敵対的レビュー 1 周を自分で回し、残り (実機の E2E 未実施・入れ替わりの穴・改行が詰まる) を 428 に正直に書いた。

### 手触り

- ✓ 依頼を置いてから記録に出るまで 1 秒未満 (socket で即時)。分解してから PG の起動まで約 3 秒
- ✓ テストの係が効いた: PG は make test を自分で回さず頼み、結果つきで再開された (PG の turn が長時間ブロックしない)
- ✓ PG は規律どおり自分のブランチまで push し、master には push しなかった
- 🚨 **終了で止まったものを「止まっていない」と数え続けた** (実バグ)。作業を終えた session は `claude stop` 後も state が `done` のまま pid 無し。
  → 01dbb3b0 で直した (止まった = pid 無し かつ working でない)。直した版の `--stop` は 1 秒で ok
- ✗ PM の CLI にカードを読む口が無い: `card add` は受付の箱の依頼 ID だけを返し、カード ID (C-002) が分からない。状態・PG の出力・質問は
  cards.json を python で読んだ。状態の変化も 5 秒ごとに読んで待った → [442](../441/done/442-feat-pro-con-card-list-show-wait.md)
- ✗ **PM がレビューで差し戻す口が無い**: 操作は add / plan / ask / answer / review / close / run / guide だけで、レビュー待ちのカードに
  直してほしい点を渡して PG を再開する操作が無い。今回は取り込めたが、直してほしい点 (下の「履歴の順」) を PG に返せなかった
- ✗ 完了にしたカードの PG の session が止まらない (state done・pid ありの idle のまま残る)。終了のときには止まるが、それまでプロセスが残る
- ✗ 朝に試した「ping」のカード (C-001) が PM 不在のまま「依頼」の列に残っている (437 の穴の実例)
- △ PG の再開後の session の名前が AI の付けた題 (`test success log`) になり、`claude agents` でどのカードの PG か分からない
- △ 手で起動した dispatcher は SIGTERM から抜けるまで 59 秒かかった (止める処理をもう 1 回試してから抜ける。確かめる段の約 30 秒)
- △ 状態の置き場に改名前の `daemon.lock` が残っている (害は無いが、どちらが今の lock か紛らわしい)
- △ `claude agents` の state に 425 の表に無い `done` が出た (作業を終えた session)

### 改善案

- [ ] レビューの差し戻し: `pro-con card rework <カード> "<直してほしい点>"` (レビュー待ち → 作業中へ戻し、同じ PG を回答つきで再開する) — 未起票
- [ ] カードを完了・却下したら、その PG の session を止める (dispatcher の close の適用で) — 未起票
- [ ] カードを読む口 → 442。外の Claude から覗く口 → epic 441
- [ ] PG の session の名前を再開の後も `pc-c-NNN` にそろえる (`--resume` で名前を付け直せるか要実測) — 未起票
- [ ] レビューで見つけた小さな点: attach の指示は打った時刻のまま履歴の末尾に足すので、履歴が時刻の順に並ばなくなりうる (428 の実装) — 未起票

