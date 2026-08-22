# 095 retro: av1ify のクリップボード入力を実装したセッション (2026-08-22)

起票日: 2026-08-22
種別: retro
関連: [094](094-human-verify-av1ify-clipboard-input.md)

やったこと: `av1ify` / `av1c` の引数なし呼び出しでクリップボードを入力に取り、一覧表示 +
[y/N] 確認を挟む経路を追加した（6973d99）。以下は踏んだ点と改善案。

## 1. 自作の検証ハーネス（`script -q`）が「機能側のバグ」に見える偽陰性を作った

実 TTY での動作確認に `printf 'y\n' | script -q /dev/null zsh -c ...` を使ったところ、
**確認プロンプトが「中止しました」で終わり exit 130** になった。パイプが閉じた時点で
`script` が pty へ EOF を送り、`read` が EOF を受けて空回答扱いになったため。
出力だけを見ると「y を入れたのに中止される = 実装のバグ」に見える形だった。

`python3` の `pty.fork()` でプロンプト文字列を待ってから `y` を書く driver に替えたら
一発で通った（一覧 → 確認 → dry-run 実行、exit 0）。

**切り出し先の提案**: `_claude/rules/` に新規ルール
「**対話プロンプトの検証は `cmd | script` でなく pty driver（プロンプトを待ってから書く）で行う**」。
既存の [`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md)
節 2 は「検査できなかったのに緑」を扱っており、今回は逆向き（**検証できなかったのに赤**、
しかも赤の原因がハーネス側）なので、同じ節に 1 段落足す形でも足りる。判断はユーザーに委ねる。

## 2. `zshlib/_av1ify.zsh` は shellcheck 側なので zsh 固有構文が通らない

`${${x##...}%%...}` のネスト展開（SC2299）と `${(Q)x}`（SC2296）で `make test` が落ちた。
Makefile 冒頭に「同じ .zsh でも `zshlib/_av1ify.zsh` は sh 互換で shellcheck 側」と
書いてあるのに、書く前に読んでいなかった。`(Q)` は既存箇所と同じ `disable=SC2296` で通し、
ネストは一時変数に分解した（分解した方が読みやすくもなった）。

**切り出し先の提案**: 却下（Makefile のコメントが既に正本で、読めば分かる。ルールを増やすより
「触るファイルが lint のどちら側かを最初に見る」で足りる）。

## 残課題

- なし（094 の人手確認は別 issue として open）
