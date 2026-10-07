# 661 (chore): zundamon-kaisetsu の CI の遅いテストを軽くし、done の issue から lint に転用できる規律を足す

> 🚨 **担当中: dotfiles-4d**（2026-10-07〜）

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

- [ ] 爆弾のテスト
- [ ] lint
