# bug: `_dotfiles_check` の結果ファイルが共有の固定名で、並行シェル間で所有権が入れ替わる

起票日: 2026-09-06
カテゴリ: bug
優先度: 中
出典: /audit resource-leaks 2026-09-06（forge Minimum+）。3 エージェントが独立に検出

## 何が起きているか

`_zshrc:735-790` の `_dotfiles_check_*` は、非同期チェックの結果を
**`${XDG_CACHE_HOME}/dotfiles/${name}-check.result`** という**シェルをまたいで共有の固定名**に置く。
一方 `_dotfiles_check_pending` は**シェルごとのカウンタ**なので、母集合とカウンタが一致しない。

壊れ方は 2 経路ある。

**① 消費側が他シェルのぶんまで食う** — `_dotfiles_check_notify` の glob は
`*-check.result(N)` で、**自分が投げたぶんかどうかを見ていない**。
他シェルの result を読んで `rm` し、自分のカウンタを減らす。

```zsh
for f in "${XDG_CACHE_HOME:-$HOME/.cache}/dotfiles"/*-check.result(N); do
  msg="$(<"$f")"; command rm -f "$f"      # 誰のぶんかを見ていない
  (( _dotfiles_check_pending-- ))         # 自分のカウンタだけ減る
done
```

**② 生成側が他シェルの未読を消す** — `_dotfiles_check_watch` は bg 起動の**前に**
`command rm -f "$f"` する（:773）。新しいシェルを開くたび、先行シェルの未消費 result が 1 つ消える。

## 実害（過大に書かないこと）

- **通知そのものは失われない**のが主経路（消費した側のシェルには表示される）。
  ずれるのは**表示先のシェル**
- カウンタが 0 まで落ちないと `precmd` hook が解除されず**残り続ける**（1 プロンプトあたり
  readdir 1 回。シェル寿命で有界なので実害は小さい）
- 経路 ② では**通知が 1 つ落ちる**ことがある（これが medium を支えている根拠）
- 🚨 「`~/.claude` のリンク漏れを伝える唯一の経路」は**誤り**。同期の `_dotfiles_check_claude_links`
  （`_zshrc:800`）と SessionStart hook `claude-links-sync.sh` が別に持つ

## 推奨対応（3 点セット。部分適用は別の穴に化ける）

1. 結果ファイル名を **`${name}-check.$$.result`** へ
2. `notify` の glob を**自分の `$$` のぶんへ絞る**
3. `zshexit` で自分のぶんを掃く

さらに、カウンタを持ち回るのをやめて「**自分が登録したファイル名の配列が空になったら hook を外す**」
形にすると、母集合とカウンタの不一致が構造的に起きなくなる。

## 🚨 `$$` 化の副作用（着手前に設計を決めること）

今は固定名 2 本（setup / karabiner）なので**上限が構造的にある**。`$$` 化すると
「途中で死んだシェルぶんの result」が pid の数だけ溜まる = **リークを直す修正が別のリークを作る**。

- `zshexit` の掃除は「合わせて」ではなく**必須要件**
- それでも `zshexit` が走らない死に方（SIGKILL / 端末強制終了）が残る
- 掃除を足す時点で**破壊的操作の新設**なので、対象を `*-check.<digits>.result` パターンに限定する
  （[`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) §0-A）

## 🚨 採ってはいけない修正案（却下理由）

- **「pending カウンタをやめて result が 0 件であることを自己解除条件にする」**: そのまま実装すると
  **通知が永久に出なくなる**。`_dotfiles_check_watch` は bg 起動前に `rm -f $f` しており result が
  生えるのは `shasum` の後なので、起動直後の最初の precmd で 0 件 → 即 hook 解除。
  しかも「result を置いてから notify を呼ぶ」形のテストでは**緑のまま通る**（fixture が退行から
  不可視 = [`mutation-verify-new-tests.md`](../_claude/rules/mutation-verify-new-tests.md) の典型）。
  成立させるには「watch 登録時に pending marker を先に作り bg が上書きする」形が要る
- **「pid が生きていない古い `*-check.*.result` を掃除する」**: 報告された実害に対して重すぎ、
  新しい破壊的経路を 1 本増やすうえ pid 再利用の窓を持ち込む。掃除は `zshexit` に閉じる

## 検証

`$$` を差し替えた 2 つの偽シェルを立てて、①片方の result をもう片方が消費しないこと
②`watch` の登録が他方の未読を消さないこと、を見る。**変異検証**は glob の絞り込みを外すと red。
