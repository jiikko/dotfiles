# issue に着手するときは `issues/next/` へ移して claim し、その移動だけを即 push する

> **トリガー型ルール。** 「この issue をやろう」と決めて最初のファイルを触る直前に発動する。
> **複数マシン / 複数セッションが同じ repo を触る前提**でだけ意味を持つ規律。

## ルール

- **着手する前に `git fetch` して、その issue が既に `issues/next/` に居ないか見る**。
  居たら**別のセッションが着手済み**なので、勝手に始めない (別の issue へ回るか、本人に聞く)
- **着手を決めたら `issues/next/` へ移し、その移動だけを pathspec で commit して即 push する**。
  claim は **push されて初めて claim になる** — ローカルに留めている間、他マシンからは
  「誰も着手していない issue」に見える
- **claim の commit に他の変更を混ぜない**。混ぜると push できない事情 (レビュー待ち・検証中) に
  claim が巻き込まれ、宣言だけが遅れる
- **push できないときは黙って進めない**。remote が進んでいるなら `git pull --rebase` してから
  push する。それでも push できない事情があるなら、**着手前にユーザーへ一言伝える**
  (claim できていない = 衝突しうる、という情報が要る)
- 完了したら `issues/next/` から `issues/done/` へ移す (next に置きっぱなしにしない)

## なぜ

起源: dotfiles, 2026-09-02。別マシンのセッションと**同じ retro (164) の切り出しを同時にやり**、
同じ趣旨のルール追記が 2 本できて rebase で衝突した。どちらも正しく仕事をしていたのに、
**片方の成果は捨てるかマージし直すしかなかった**。根拠・実例は
`~/dotfiles/_claude/rules-rationale/claim-issue-in-next-and-push.md` に置く
(起動時には読まれない。ルールを疑う・改訂するときに読む)。

## 強制手段 (hook が一部を持つ)

- **PostToolUse(Bash) hook** `_claude/hooks/next-claim-push.sh` が、`issues/next/` への移動を
  含む Bash コマンドを検出したら「claim を単独 commit して push したか」を注入する
  (配線: `_claude/settings.json`)。**hook が見えるのは Claude が Bash で動かした移動だけ**で、
  glogx の issues viewer の `n` キー (Go 側で移動する) は Bash を通らないので発火しない
- hook は**注意を出すだけ**で、push を強制しない。「fetch してから着手する」「他の変更を
  混ぜない」も hook では強制できないため、本 md が正本のまま残る
  ([`comment-no-restate-enforced.md`](comment-no-restate-enforced.md) の区分)

## やること / やらないこと

- ✓ 着手前に `git fetch` し、`issues/next/` に既に居ないか見る
- ✓ claim の移動だけを pathspec commit して即 push する
- ✓ push できない事情があるなら、着手前にユーザーへ伝える
- ✓ 完了したら next から done へ移す
- ✗ ローカルで claim して push しないまま作業を始める (claim になっていない)
- ✗ claim の commit に実装や他の issue の変更を混ぜる
- ✗ 既に next に居る issue を、確認せずに横から始める

## 例外

- **ユーザーがその場で指示した単発の作業** (「この issue やって」) は、claim を経ずに始めてよい。
  ユーザーは自分がどのマシンで何を頼んだか知っているため。ただし**長くかかる**と分かったら、
  途中でも next へ移して push しておくと他マシンからの二重着手を防げる
- 自分しか触らないと分かっている repo (単一マシン運用) では不要

## 関連

- [`commit-with-pathspec.md`](commit-with-pathspec.md) — 「claim だけを commit する」ための
  pathspec 規律と、他セッションの変更を巻き込まない作法
- [`parallel-write-agents-need-worktree-isolation.md`](parallel-write-agents-need-worktree-isolation.md) —
  **同じ working tree** を複数主体が書く問題。本ルールは **同じ issue 列**を複数マシンが
  処理する問題 (working tree は別なので worktree 分離では防げない)
- `docs/issues-viewer-spec.md` — `next/` の元々の意味 (glogx の `n` が付ける「次にやる」目印)。
  本ルールはその状態語彙に「着手中の claim」という第二の意味を重ねている
