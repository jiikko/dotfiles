# 425 (research): claude --bg の残りの挙動を実測する

起票日: 2026-09-24

親: [415](../415-design-claude-pm-worker-orchestration.md) の論点 2 / 5 / 8 / 11 と要件 14

## 概要

PG を `claude --bg -w` で動かす設計 (415 論点 2) のうち、測っていない挙動が残っている。
本物の PM / PG の backend ([427](../427-feat-pro-con-real-pm-pg-backend.md)) と論点の決定 ([426](426-design-pro-con-open-decisions.md)) がこれに依存する。

## 測ること

- [x] 完了と異常終了の区別 → **区別できる** (結果 1)
- [x] `claude rm` の安全側 → **消さない** (結果 2)
- [x] 実行中の bg session へ直接送れるか → **SendMessage で送れる。届くのは turn の区切り** (結果 3)
- [x] 質問で止めた session → **`waitingFor: "input needed"`。SendMessage では答えられない** (結果 4)
- [x] 再起動を越えるか → 人の操作 (マシンの再起動) が要るので [430](../430-human-verify-bg-session-survives-reboot.md) に切り出した。
  プロセスが死んだ場合は daemon が自動で再開することまでは確認した (結果 1)
- [x] 利用枠を機械で読む口 → **`claude -p "/usage"` がテキストで返す** (結果 5)
- [x] `TMUX` / `TMUX_PANE` を落とさない場合 → **測らない**。落とす前提で設計している (415 論点 5) ので結果が設計を変えない

## 結果 (実測 2026-09-24 / Claude Code 2.1.281 / haiku / `~/dotfiles` から起動。stdout・stderr・rc は分けて採った)

### 1. 終わり方は `claude agents --json --all` の `status` / `state` / `pid` で 4 通りに分かれる

| 終わり方 | pid | status | state |
|---|---|---|---|
| turn を正常に終えた | 残る (プロセスは終了しない) | `idle` | `blocked` (次の入力待ち) |
| API エラーで turn が落ちた (長い出力が `Output blocked by content filtering policy`) | 残る | `idle` | `failed` |
| `claude stop` | 無し | 無し | `stopped` |
| プロセスが死んだ (`kill -9`) | 数秒間 無し (`state` は `working` のまま) | 無し | `working` |

- 🚨 **プロセスが死んだ session は daemon が自動で再開する**。約 25 秒後に新しい pid で同じ session ID が `busy` に戻り、
  会話に次の文が足された: 「Continue from where you left off. Note: this session was automatically restarted after its process exited
  unexpectedly; the user has not sent a new message since the restart. Re-verify anything time-sensitive (branch state, running processes,
  prior partial work) before continuing.」 → 415 論点 5 の「落ちたら failed にして自動で再実行しない」とは合わない ([426](426-design-pro-con-open-decisions.md) へ)
- 「pid が無いのに `working`」は、死んでから再開されるまでの一時的な状態として読む

### 2. `claude rm` はフラグなしでは未 push の commit・未コミットの変更を持つ worktree を消さない

- 未 push の commit: rc=1、stdout に「kept … 1 unpushed commit … It exists on no remote」と、消すときの値
  (`--discard-unpushed <commit>@<worktree-id>`) を出す。stderr は空
- 未コミットの変更: rc=1、stdout に「The worktree has uncommitted changes」。フラグで消す手段は案内されない (agent view で ctrl+x 2 回)
- どちらも **session は止める** (`state: stopped`、pid 無し) が、worktree とブランチ `worktree-<name>` は残る
- 変更が無ければ worktree もブランチも消える (415 の既存の実測と同じ)

### 3. bg session も `ListAgents` に出て、`SendMessage` が届く

- `ListAgents` に `bg` として名前 (`-n` で付けた名前) つきで並ぶ
- **idle の session**: 送ると起きて処理する (2 秒で返答した)
- **busy の session**: 生成の途中には割り込まず、**今の turn が終わった直後に届いた**。途中にツール呼び出しがあればその区切りで届くはず (SendMessage の説明にある「次のツール呼び出しの区切りで取り込む」。ツールを使う turn では未確認)
- 実行中に `--resume` を使わなくても、追加オーダーと回答は SendMessage で届けられる (415 論点 8 / 11 の届け方の候補が増えた)

### 4. AskUserQuestion で止めると `status: waiting` / `waitingFor: "input needed"` になり、SendMessage では答えられない

- 権限の確認 (`waitingFor: "permission prompt"`) とは値で区別できる
- 答えを SendMessage で送っても、30 秒以上 `input needed` のまま会話に入らなかった (次の区切りまで積まれたまま)
- → **PG には AskUserQuestion を使わせず、質問を書いて turn を終えさせる**。idle になった session には SendMessage で答えが届く (結果 3)

### 5. 利用枠は `claude -p "/usage"` で読める

- rc=0、stdout にテキストで「Current session: 23% used · resets …」「Current week (all models): 92% used · resets …」「Current week (Fable): …」
- 行の文言を読むので、Claude Code の版が上がると崩れうる (読めなかったら「判定不能」として新規起動を止める側に倒す)
- statusline の stdin の JSON にも `rate_limits.five_hour` / `seven_day` の `used_percentage` / `resets_at` があるが、対話 session の statusline にしか渡らず、どこにも保存されていない

### 6. 設計に効く副作用 (測る予定ではなかったもの)

- 🚨 **PG にもユーザーの hook と規約が全部効く**。1 語を返すだけの session で、起動時の context が約 13 万 token だった
  (cache_creation 107,001 + cache_read 26,079。`_claude/rules/` などの注入)。PG 1 体ごとにこれがかかる
- 🚨 **Stop hook (`issue-progress-check.sh`) が PG に別の作業の issue を更新させようとする**。この実測の session は、自分が触っていない
  issue 425 (claim の commit だけがあった) を「更新漏れの疑い」と言われ、Read → Edit → `EnterWorktree` → Edit まで進み、権限の確認で止まった
  (書き込みは起きていない)。どの commit を「関わった」と数えたかは未確認
- この 2 点は本物の PG の backend ([427](../427-feat-pro-con-real-pm-pg-backend.md)) で、PG 用の設定 (hook を絞った `--settings`) を用意するかの判断材料になる

## 測り方の制約

- 本物の `claude --bg` を起動・停止するので**利用枠を使う**。モデルは haiku、1 項目 1 session で済ませる
- stdout / stderr / rc を分けて採り、Claude Code の版を併記する (`~/.claude/rules/measure-external-cli-streams-separately.md`)
- 再起動を越えるかは人間の操作 (再起動) が要る。そこだけ human に切り出すか、最後に回す

## 関連ファイル

- 415 の「`--bg` の実測」節 (論点 2 の中) が既に測った分

## 進捗

- [x] 実測 (2026-09-24)。結果は上の節。後片付け: 作った bg session 7 本と worktree 3 本・ブランチ 3 本を `claude rm` で消し、残骸 0 を確認
- [x] 再起動の確認は 430 (human) へ切り出し
