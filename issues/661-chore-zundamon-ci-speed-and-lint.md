# 661 (chore): zundamon-kaisetsu の CI の遅いテストを軽くし、done の issue から lint に転用できる規律を足す

起票日: 2026-10-07

## 概要

audit の ci / lint-from-done の観点 (656 の続き)。

### ci (実行の記録で見た。run 37597077493、src/zundamon-kaisetsu)

- C1 (走るべきなのに走らない): 漏れなし。paths は module・`src/proctree`・`_claude/skills/zundamon-kaisetsu/**` (テストが読むテンプレートと立ち絵) を含む
- C3 (キャッシュ): setup-go のキャッシュはヒット、golangci-lint のバイナリもキャッシュから。問題なし
- **C4 (時間を食う step)**: test の job の `Test` が 123 秒 (go test -race が 110 秒)。手元の -race で 1 本ずつ測ると
  `TestImageBombStopsBeforeDecode` が 35.4 秒で、全体の約半分。中身は 20000x20000 の PNG を本当に符号化しているだけで、
  確かめたいこと (本体を展開する前に縦横で止める) には IHDR だけの PNG で足りる

### lint-from-done (649〜655・660 から)

- L1: 子孫を止める処理は `proctree` に寄せた (649)。zundamon の本体で `syscall.Kill` を直接書くと、グループ宛ての kill で止めたつもりになる形を再発させうる。
  今の本体の使用は main.go のシグナルでの死に直し 1 か所だけ (2026-10-07 に grep)
- L2: zundamon のテストは `t.Parallel` を使わない前提 (mp4 のテストがパッケージ変数 videoSize を書き換える。651)。今は CLAUDE.md の 1 行だけが守っている
- 見送り: 652 の ctxInterrupted (ctx を受け取る関数の中の interruptedErr) は字句では判定できない / 660 の ffmpeg の引数はテストで固定済み

## 対応方針

- 爆弾のテストを IHDR だけの PNG にする (本体を展開する退行なら IDAT が無いことで別のエラーになり、「大きすぎる」で止まらないので red のまま)
- zundamon の `.golangci.yml` に forbidigo: `syscall.Kill` (main.go とテストを除く) と `.Parallel` を禁止。変異で lint が落ちることを確かめる
- lint が止める制約は CLAUDE.md から再掲を外す

## 進捗

- [x] 爆弾のテスト — chore(zundamon-kaisetsu): CI の遅いテストを軽くし…
- [x] lint — 同じ commit と、敵対的レビューの指摘を直した commit

## 結果 (2026-10-07)

- `TestImageBombStopsBeforeDecode`: 縦横 20000x20000 を名乗る PNG を IHDR・1 行分の IDAT・IEND で組む (-race で 35.4 秒 → 0.00 秒)。
  手元の `go test -race` 全体は 68 秒 → 34 秒
- forbidigo: 本体の `syscall.Kill` (main.go は死に直しの行だけ許す) と `.Parallel` を禁止。CLAUDE.md の t.Parallel の再掲を外した
- 変異で red: 縦横の検査の前に本体を Decode する (エラーを返す / 捨てる。後者は 381MB の確保で落ちる) / renderMermaid・新しいファイル・main.go に
  グループ宛ての kill を足す (lint) / テストに t.Parallel を足す (lint)

## 敵対的レビュー (2026-10-07、opus 1 周)

- P2 (採用): IDAT が無いと確保量の検査が効かない → 1 行分の IDAT を足した
- P3 (採用): 除外の path の固定が無く domain.go が素通り・main.go 全体が除外 → path を固定し、死に直しの行だけ許す
- 記録のみ (脅威モデルの外): 別名 import の Kill / os.Process.Signal や Process.Kill (直接の子にしか届かない) / `.Parallel$` は無関係な x.Parallel
  (今は該当 0 件) にも当たる
