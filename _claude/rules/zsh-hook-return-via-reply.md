# zsh の hook (precmd/preexec) から呼ぶ関数は stdout でなく REPLY で返す

## ルール

- **precmd / preexec / zle widget など「毎プロンプト・毎コマンド走る経路」から呼ぶ自作関数は、結果を `print` でなく `REPLY` に入れて返す**。呼び出し側は `$(...)` を使わず `f arg; x="$REPLY"` の形で受ける
- `$(f arg)` は 1 回ごとに **subshell を fork** する (実測 0.42ms/回 vs 直呼び 0.03ms/回)。hook はユーザー操作のたびに走るので、ここでの fork はそのまま体感レイテンシになる
- **hook 本体には `local REPLY` を置く**。zsh は動的スコープなので、呼び出し先の `REPLY=` 代入はこの local に入り、グローバル `REPLY` (他の hook / zle widget / `read` が使う) を汚さない
- ループ内で呼ぶ関数は特に優先して REPLY 化する (行数 × 呼び出し数だけ fork が増える)

## なぜ (起源: dotfiles 2026-07-25 の実測)

同じパターンで 3 箇所が遅かった。いずれも「関数が正しく動いていた」ので気づかれていなかった:

| 箇所 | 症状 | 修正後 |
|---|---|---|
| `_tmux_load_yaml` (YAML 35 行 × key/value を `$(trim)`) | 70 fork = **35.9ms**。zprof で対話シェル初期化の 55% (pane を開くたび) | 1.6ms |
| `_tmux_preexec` (`$(extract_command)` 等 3 回) | 毎コマンド **1.18ms** | 0.083ms |
| プロンプトの `vcs_info` (git を数回 fork) | 毎プロンプト **16.9ms** | `.git/HEAD` 直読みで 0.05ms |

fork は「1 回 0.4ms」に見えて安いが、hook は 1 操作あたり複数回 × 1 日に数千操作走る。しかも
プロファイルを取るまで見えない (機能は正しく動くため)。

## やること / やらないこと

- ✓ hook 経路の関数は `REPLY` 返し + 直呼び
- ✓ hook 本体に `local REPLY` を書いて閉じ込める
- ✓ 「stdout ではなく REPLY で返す」ことを関数の直上コメントに 1 行書く (次に触る人が `print` を足し戻さないように)
- ✗ hook の中で `$(...)` / `` `...` `` を使う (関数呼び出し・`date`・`tmux show-option` 等も同じ)
- ✗ REPLY 返しの関数に `print` も併記して「両対応」にする (hook から呼ぶと端末を汚す)
- ✗ 呼び出し側の `local REPLY` を忘れる (グローバルを書き換えて別の hook を壊しうる)

## 例外

- hook の外 (対話コマンド・setup スクリプト・テスト) から呼ぶだけの関数は stdout 返しでよい。REPLY 返しの関数をテストから使うときは `f arg; print -r -- "$REPLY"` で受ける
- 外部プロセスが本質的に必要なもの (`tmux set-option` によるスタンプ等) は fork を消せない。その場合は**呼ぶ頻度を落とす** (throttle) 方向で設計する

## 関連

- `zshlib/_tmux_window_name.zsh` / `zshlib/_git_prompt.zsh` — 実装例 (前者は REPLY 契約、後者は fork ゼロで git 状態を読む例)
- `tests/zshrc/bench_zsh.sh` — `prompt_lag` metric が hook の恒常コストの回帰ゲート。REPLY 化の効果もここで測る
- [`comment-no-restate-enforced.md`](comment-no-restate-enforced.md) — コメントに残すのは「実装で強制できない制約」だけ。REPLY 契約は lint で守れないためコメントに書く側
