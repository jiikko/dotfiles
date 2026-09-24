# issue に着手するときは `issues/next/` に目印 (symlink) を置いて claim し、その目印だけを即 push する

> **トリガー型ルール。** 「この issue をやろう」と決めて最初のファイルを触る直前に発動する。
>
> **適用条件: 作業中の repo に `issues/next/` または `issues/epic/<name>/next/` が実在するときだけ。**
> 無ければこのルール全体を読み飛ばしてよい (`next/` を作ることが opt-in。hook も同じ条件で無効化する)。

## ルール

- **着手する前に `git fetch` して、その issue が既に global または所属 group の `next/` に居ないか見る**。
  居たら別のセッションが着手済みなので、勝手に始めない (別の issue へ回るか、本人に聞く)
  - 🚨 **fetch は「着手を決めた直前」にもう一度打つ** (セッション冒頭の結果は古い)
  - 🚨 **`ListAgents` に他セッションが居るなら、本人へ 1 回聞く**。claim は push されるまで next に現れないので、
    生きているセッションへの照会が唯一の即時性のある手段
- **着手を決めたら `next/` に目印を置き、目印とバナーだけを pathspec で commit して即 push する**。
  claim は **push されて初めて claim になる**
  - 🚨 **目印は symlink で、issue ファイルは動かさない** (issue 263。rename すると本文の相対リンクが切れる)

    ```sh
    ln -s ../NNN-slug.md issues/next/NNN-slug.md && git add issues/next/NNN-slug.md
    # issues/NNN-slug.md のタイトル直下に `> 🚨 **担当中: <セッション名>**（YYYY-MM-DD〜）` を書いてから
    git commit -F - -- issues/next/NNN-slug.md issues/NNN-slug.md <<'M'
    claim: issue NNN に着手
    M
    git push
    ```

  - **目印の形は `../<同名>` に固定** (glogx はそれ以外を目印として読まない。`tests/issues/test_next_links_valid.sh` が検査する)。
    glogx の issues viewer の `n` は目印とバナーを 1 組で書き、解除では両方を外す (push は人が行う)
  - 旧運用 (ファイルそのものを `next/` へ移す) は新しい claim では使わない。既に `next/` に実ファイルとして居る issue は
    そのまま完了まで持ってよく、完了時は `next/` から `done/` へ移す
- **group issue (`issues/epic/<name>/`) の claim / 完了 / 保留は group 内の `next/` / `done/` / `pending/` へ** (issue 291。
  global の `issues/done/` へ出すと epic 所属が消える)
- 🚨 **担当者バナーを本文の冒頭 (最初の `## ` より前) に書き、目印と同じ commit で push する**。`next/` の目印は
  `next/` を見る入口にしか届かない。バナーの無い claim は `tests/issues/test_next_claims_have_banner.sh` が落とす (issue 403)
- **claim の commit に他の変更を混ぜない** (混ぜると push できない事情に claim が巻き込まれる)
  - 🚨 **push はブランチ単位**なので、他に未 push の commit があれば一緒に飛ぶ。**飛ばしてよいかを先に確かめ**、
    確かめ方は `git log --oneline origin/master..HEAD` の**出力をそのまま**相手へ見せる (自分で数えた件数はずれる)。
    飛ばせないなら、claim できていないことをユーザーへ伝えてから着手する
- **push できないときは黙って進めない**。`git pull --rebase` してから push し、それでも無理なら着手前にユーザーへ伝える
- 完了したら **目印 (symlink) を消してから** done へ移す (残すと dangling で CI が落ちる)
  - 🚨 **手でやらない。dotfiles では `scripts/issue_done.sh <NNN>` に寄せる** (移動・目印削除・本文の相対リンクと
    他 issue からの参照の張り直しを 1 コマンドで行い、リンク検査が落ちたら戻す)
- **`git pull --rebase` が衝突したら claim を優先して片付ける**。相手が同じ issue を触っていたなら二重着手の証拠なので、
  相手の claim を尊重して別の issue へ回る。rebase は `--continue` か `--abort` で必ず閉じる
- **二重着手が起きてしまったら、捨てる側のレビュー指摘を残す側の実装に当て直してから捨てる**

## なぜ

起源: dotfiles, 2026-09-02。別マシンのセッションと同じ retro (164) の切り出しを同時にやり、片方の成果を捨てるしかなかった。
根拠・実例は `~/dotfiles/_claude/rules-rationale/claim-issue-in-next-and-push.md` に置く (起動時には読まれない。ルールを疑う・改訂するときに読む)。

## 強制手段 (hook が一部を持つ)

- **PostToolUse(Bash)** `_claude/hooks/next-claim-push.sh`: `next/` への `ln -s` / 移動を含む Bash コマンドを検出したら
  「claim を単独 commit して push したか」を注入する。Claude が Bash で動かした移動しか見えない
- **UserPromptSubmit** `_claude/hooks/next-claim-unshared.sh`: 毎プロンプトで、他マシンから見えない claim (未コミット /
  未 push) があれば「push してよいか」をユーザーへ伺わせる (glogx の `n` で付けた claim もここで拾う)。
  自動 push は採らない (他の未 push commit も飛ぶため)
- hook は注意を出すだけで、jq が無いと無音で死に、宛先が変数・相対パスの移動は検出できない。
  **規律の正本はこの md** ([`comment-no-restate-enforced.md`](comment-no-restate-enforced.md) の区分)
- 🚨 **照会に答えられないセッションがある** (`to` を取る `SendMessage` を持たず `ccd_session_mgmt` しか無いセッションは原理的に返信できない)。
  沈黙は「空いている」と読まれるので、返信できないと分かったらユーザーへ上げる

## 例外

- **ユーザーがその場で指示した単発の作業** は claim を経ずに始めてよい。長くかかると分かったら途中でも claim して push する
- 自分しか触らないと分かっている repo (単一マシン運用) では不要

## 関連

- [`commit-with-pathspec.md`](commit-with-pathspec.md) — 「claim だけを commit する」ための pathspec 規律
- [`parallel-write-agents-need-worktree-isolation.md`](parallel-write-agents-need-worktree-isolation.md) —
  同じ working tree を複数主体が書く問題。本ルールは同じ issue 列を複数マシンが処理する問題
- `docs/issues-viewer-spec.md` — `next/` の元々の意味 (glogx の `n` が付ける「次にやる」目印)
