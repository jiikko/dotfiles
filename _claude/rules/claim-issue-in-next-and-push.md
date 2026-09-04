# issue に着手するときは `issues/next/` へ移して claim し、その移動だけを即 push する

> **トリガー型ルール。** 「この issue をやろう」と決めて最初のファイルを触る直前に発動する。
>
> **適用条件: 作業中の repo に `issues/next/` または `issues/epic/<name>/next/` ディレクトリが
> 実在するときだけ。**
> 無ければこのルール全体を読み飛ばしてよい (仕事の repo のように `issues/` を持たない repo では
> 何もしない)。運用を始めたい repo では global または group 内の `next/` を作ることが opt-in になる。
> hook 側も同じ条件で自分を無効化する。

## ルール

- **着手する前に `git fetch` して、その issue が既に global または所属 group の `next/` に居ないか見る**。
  居たら**別のセッションが着手済み**なので、勝手に始めない (別の issue へ回るか、本人に聞く)
  - 🚨 **fetch は「着手を決めた直前」にもう一度打つ。** セッションの最初に取った結果は古い。
    実測 2026-09-04 (6 セッションが同じ backlog を消化した日): 見落とし / 衝突が **7 回**起き、
    その 1 件は「自分の fetch が古く、push 済みの claim を見落とした」形だった
  - 🚨 **`ListAgents` に他セッションが居るなら、next を見るだけでなく本人へ 1 回聞く。**
    claim は **push されるまで next に現れない**ので、生きているセッションへの照会が
    唯一の即時性のある手段 (同日実測: 未 push の claim による二重着手が 1 件、
    調整中に横から完走されたのが 1 件)
- **着手を決めたら `issues/next/` へ移し、その移動だけを pathspec で commit して即 push する**。
  claim は **push されて初めて claim になる** — ローカルに留めている間、他マシンからは
  「誰も着手していない issue」に見える
- **group issue (`issues/epic/<name>/`) の claim は、その group 内の `next/` (`issues/epic/<name>/next/`) へ移す。完了したら global `issues/done/` へ移す。** group 内に `done/` / `pending/` は作らない。
- **claim の commit に他の変更を混ぜない**。混ぜると push できない事情 (レビュー待ち・検証中) に
  claim が巻き込まれ、宣言だけが遅れる
  - 🚨 **push はブランチ単位**なので「claim の commit だけを push」はできない。他に未 push の
    commit があるなら、それらも一緒に飛ぶ。**飛ばしてよいかを先に確かめる** (飛ばせないなら、
    claim できていないことをユーザーへ伝えてから着手する)
- **push できないときは黙って進めない**。remote が進んでいるなら `git pull --rebase` してから
  push する。それでも push できない事情があるなら、**着手前にユーザーへ一言伝える**
  (claim できていない = 衝突しうる、という情報が要る)
- 完了したら global issue は `issues/next/` から `issues/done/` へ、group issue は所属 group の `next/` から global `issues/done/` へ移す (next に置きっぱなしにしない)
- **`git pull --rebase` が衝突したら、claim を優先して片付ける**。claim の commit は
  「1 ファイルの rename だけ」なので衝突しても解決は自明 (相手が同じ issue を触っていたなら、
  それは**二重着手が起きている証拠**なので、続けずに相手の claim を尊重して別の issue へ回る)。
  rebase を途中で放置しない — `--continue` か `--abort` のどちらかで必ず閉じる

## なぜ

起源: dotfiles, 2026-09-02。別マシンのセッションと**同じ retro (164) の切り出しを同時にやり**、
同じ趣旨のルール追記が 2 本できて rebase で衝突した。どちらも正しく仕事をしていたのに、
**片方の成果は捨てるかマージし直すしかなかった**。根拠・実例は
`~/dotfiles/_claude/rules-rationale/claim-issue-in-next-and-push.md` に置く
(起動時には読まれない。ルールを疑う・改訂するときに読む)。

## 強制手段 (hook が一部を持つ)

- **PostToolUse(Bash) hook** `_claude/hooks/next-claim-push.sh` が、global または group 内の `next/` への移動を
  含む Bash コマンドを検出したら「claim を単独 commit して push したか」を注入する
  (配線: `_claude/settings.json`)。**この hook が見えるのは Claude が Bash で動かした移動だけ**で、
  glogx の issues viewer の `n` キー (Go 側で移動する) は Bash を通らないので発火しない
- **UserPromptSubmit hook** `_claude/hooks/next-claim-unshared.sh` が、その穴を埋める:
  毎プロンプトで、global または group 内の `next/` の claim が**他マシンから見えない状態** (未コミット、または
  commit 済みだが未 push。後者は issue 249 で足した) なら
  「push してよいか」をユーザーへ伺わせる。人が `n` で付けた claim もここで拾う。
  **使い分け**: 移動した瞬間に Claude 自身へ促すのが前者、取りこぼした claim を後から
  人に伺うのが後者。**押した瞬間の自動 push は採らない** (push はブランチ単位なので、
  他の未 push commit も一緒に飛ぶ。飛ばしてよいかは人しか判断できない)
- 🚨 **jq が無い環境では hook が丸ごと無音で死ぬ** (`command -v jq || exit 0`)。検出が消えても
  何の兆候も出ないので、claim の規律は最終的に**この md を読む人が守る**もので、hook はその補助
- 🚨 **静的検査なので宛先が変数・相対パスだと検出できない**
  (`D=issues/next; git mv x "$D/"` / `cd issues && git mv x next/`)。取りこぼすより出す側へ倒す方針
- hook は**注意を出すだけ**で、push を強制しない。「fetch してから着手する」「他の変更を
  混ぜない」も hook では強制できないため、本 md が正本のまま残る
  ([`comment-no-restate-enforced.md`](comment-no-restate-enforced.md) の区分)

## やること / やらないこと

- ✓ 着手前に `git fetch` し、global または所属 group の `next/` に既に居ないか見る
- ✓ claim の移動だけを pathspec commit して即 push する
- ✓ push できない事情があるなら、着手前にユーザーへ伝える
- ✓ 完了したら global `issues/done/` へ移す
- ✗ ローカルで claim して push しないまま作業を始める (claim になっていない)
- ✗ claim の commit に実装や他の issue の変更を混ぜる
- ✗ 既に global または所属 group の next に居る issue を、確認せずに横から始める

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
