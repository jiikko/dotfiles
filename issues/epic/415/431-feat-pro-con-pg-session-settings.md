# 431 (feat): pro-con の PG / 係の session 用の設定 (hook と規約を絞る)

起票日: 2026-09-24

親: [415](415-design-claude-pm-worker-orchestration.md) / 実測の出典: [425](done/425-research-claude-bg-remaining-measurements.md) の結果 6

## 概要

`claude --bg` で起こした session にも、ユーザーの hook と規約 (`_claude/rules/` など) が全部効く。425 の実測では:

- 1 語を返すだけの session で、起動時の context が約 13 万 token (cache_creation 107,001 + cache_read 26,079)。PG・係を 1 体起こすたびにかかる
- Stop hook (`_claude/hooks/issue-progress-check.sh`) が、その session の触っていない issue を「更新漏れの疑い」と言い、
  Read → Edit → EnterWorktree → Edit まで進ませた (権限の確認で止まり、書き込みは起きていない)。どの commit を「関わった」と数えたかは未確認

役割 (426 の決定 6: PG / テストの係の要約役 / 調べる係 / レビューの係) ごとに、要る hook と規約だけを載せた設定で起動する。

## 対応方針 (叩き台)

- `claude --bg --settings <file>` で役割ごとの設定を渡す。どこまで絞れるか (ユーザーの `~/.claude/` の規約・hook を外せるか、足すだけか) を先に実測する
- 役割ごとに要るものを決める: PG は worktree・commit・テストの規約は要る / 調べる係とレビューの係は読み取りだけなので編集系の規約は要らない
- 絞った設定で、起動時の context の token を 425 と同じ方法で測り直す (前後を比べる)
- Stop hook の issue 進捗チェックは、PG では外すか、PG が触った issue だけを見る形にする

## 下調べ (2026-09-24。`claude --help` を読んだだけで未計測。Claude Code 2.1.281)

- `--bare`: hook・auto-memory・CLAUDE.md の自動の読み込みを飛ばす最小のモード。🚨 **認証は ANTHROPIC_API_KEY か apiKeyHelper だけで、
  サブスクリプションの OAuth / keychain を読まない**。今の使い方 (サブスクリプション) では PG に使えない
- `--setting-sources <user,project,local>`: 読む設定の出どころを選ぶ。`project,local` にすれば、ユーザーの設定 (`~/.claude/settings.json` =
  `_claude/settings.json`。hook はここ) を外せる見込み。**規約 (`~/.claude/CLAUDE.md` と `~/.claude/rules/`) がこれで外れるかは未確認** (設定と別の経路の可能性)
- `--restricted`: ユーザー・project・local の設定を無視し、Bash などコードを動かすツールと WebFetch を外す (`--tools` で戻せる)。
  読むだけの係 (調べる係・レビューの係) の候補
- `--exclude-dynamic-system-prompt-sections`: 機械ごとの部分 (cwd・git status 等) を最初の発言へ移し、session をまたいで prompt のキャッシュを使い回しやすくする。PG を何体も起こすときの候補
- `--system-prompt` / `--append-system-prompt`: PG の規律 (AskUserQuestion を使わない、pro-con のコマンドで質問する 等。426 の決定 2) を渡す口

計測の手順 (週の利用枠がリセットされてから。2026-09-24 の時点で 96% 使用済み。1 回 約 13 万 token): `claude -p --output-format json --model haiku "OK とだけ答えて"` を、
何も付けない / `--setting-sources project,local` / それに `--exclude-dynamic-system-prompt-sections` を足す、の 3 通りで回し、
usage (input / cache_creation / cache_read) と、Stop hook が走ったか (transcript の hook のレコード) を比べる

## 計測 (2026-09-24 / Claude Code 2.1.281 / haiku / `~/dotfiles` から `claude -p --output-format json "OK とだけ答えて"`)

| 起動のしかた | 起動時の token (cache_creation + cache_read) | hook | 規約 (instructions) |
|---|---|---|---|
| 何も付けない | 107,403 + 14,053 = 121,456 | SessionStart が走る | 載る |
| `--setting-sources project,local` | 95,696 + 17,890 = 113,586 (約 1 割減) | 走らない | 載る |
| それに `--exclude-dynamic-system-prompt-sections` | 96,583 + 16,985 = 113,568 | 走らない | 載る |
| PG 用の `CLAUDE_CONFIG_DIR` (空のディレクトリ) | 起動しない (`Not logged in · Please run /login`、rc=1。モデルは呼ばない) | — | — |

- **一番大きいのは規約の注入** (transcript の `instructions` の添付、約 14 万文字): `_claude/rules/` の全ルール (`~/.claude/rules/` の link) と、
  `~/dotfiles/CLAUDE.md` / `.claude/rules/` (project のもの)。ユーザーの分は設定ディレクトリ (`~/.claude/`) から読まれ、`--setting-sources` では外れない
- `--setting-sources project,local` で hook (SessionStart・Stop) は外れる (hook の添付が消えた)。Stop hook の問題 (425 結果 6) はこれで止まる見込み
- ~~次の手: PG 用の設定ディレクトリ (`CLAUDE_CONFIG_DIR`) を作り、PG に要るルールだけを link して起動する~~ → 採らなかった (下の「再計測」「対応」節)。
  ログインの人の操作 [433](done/433-human-login-pro-con-pg-config-dir.md) は不要として閉じた

## 再計測 (2026-09-26 / Claude Code 2.1.282。`claude -p "/context"` はモデルを呼ばずに読み込まれる規約を列挙する = 枠を使わない)

worktree `pc-c-031` から。「Memory files」は `/context` の見積もり:

| 起動のしかた | Memory files | 載るもの |
|---|---|---|
| 何も付けない | 95.8k | `~/.claude/CLAUDE.md` + `_claude/rules/` の User 34 本 + project 3 本 + MEMORY.md |
| `--setting-sources project,local` (今の PG) | 15.3k | `~/.claude/CLAUDE.md` (8.5k。**Project として**) + project 3 本 + MEMORY.md (1.2k) |
| それに `--settings '{"claudeMdExcludes":["~/.claude/CLAUDE.md"],"autoMemoryEnabled":false}'` | 5.6k 未満 | project 3 本だけ |
| `--settings` で `disableAllHooks` + `claudeMdExcludes` に「3 本以外の `_claude/rules/*.md`」(user は読む) | 22.5k | 指名した User 3 本 + project 3 本 (Skills / Custom agents も user の分が戻る) |

- 🚨 **2.1.282 では `--setting-sources project,local` で `~/.claude/rules/` は外れている**。上の「計測」節 (2.1.281) の「規約は外れない」と食い違う。
  版の差か `-p` と `--bg` の差かは未確認。ただし pro-con が `--bg` で起こした PG (C-031) の context にも User の rules は載っていない
- `~/.claude/CLAUDE.md` が残るのは、祖先 `/Users/koji` の `.claude/CLAUDE.md` として Project 扱いで拾われるため (見込み。`/context` の Type が Project)
- `claudeMdExcludes` (picomatch の glob / 絶対パス。User・Project・Local に効く) と `autoMemoryEnabled` は `--settings` (flag) からも効く。
  **`CLAUDE_CONFIG_DIR` もログイン (433) も要らずに規約を絞れる**。460 の「projects の置き場がずれる」問題も起きない
- `disableAllHooks` を `--settings` で渡したときに hook が本当に止まるかは未確認 (`/context` に hook は出ない)

## 🚨 今の PG は `~/.claude/rules/` を読んでいない (2026-09-26 に判明。このカードでは戻さない)

- `--setting-sources project,local` を付けた時点で、PG (と PM) には **`_claude/rules/` のルールが 1 本も載らない**。
  `mutation-verify-new-tests` / `commit-with-pathspec` / `verify-execution-not-just-exit-code` / `adversarial-review-own-safeguards` なども含む。
  載るのは `~/.claude/CLAUDE.md` (rules への索引の表を含むが、指す先は読まれていない) と repo の `CLAUDE.md` / `.claude/rules/` だけ
- **PG に一部のルールを効かせるかは別の判断** (未決)。戻すなら「再計測」の 4 行目の形 (user を読み、`disableAllHooks` + `claudeMdExcludes` で許可リスト)。
  そのときは user の skills・agents・permissions も戻るのと、`--settings` の `disableAllHooks` で hook が本当に止まるかを本物の session で確かめる (枠を使う)

## 対応 (2026-09-26、C-031)

役割ごとの設定を、pro-con が `--settings` で渡す JSON にした (`src/pro-con/dispatcher/rolesettings.go`)。どの役割も `--setting-sources project,local` と組:

| 役割 | `--settings` | 外れるもの (`--setting-sources` で外れる分に加えて) |
|---|---|---|
| PG・PM (`ExecLauncher` の起動・再開) | `{"autoMemoryEnabled":false,"language":…}` | auto memory (MEMORY.md 1.2k)。`~/.claude/CLAUDE.md` は残す (git の禁止操作・レビュー方針) |
| 要約役・btw (haiku) | `{"autoMemoryEnabled":false,"claudeMdExcludes":["<家>/.claude/CLAUDE.md"]}` | `~/.claude/CLAUDE.md` (状態の置き場で haiku の数えで 7.7k) → Memory files 0 |

- `/context` で確認: PG の JSON で worktree から Memory files 14.1k (`~/.claude/CLAUDE.md` 8.5k + project 3 本) / haiku の JSON で状態の置き場から 0
- 調べる係・レビューの係は pro-con にまだ無いので設定も無い (作るときに足す)
- 🚨 **本物の session での起動時の token は測り直していない** (枠を使う)。`/context` の見積もりと、449 の実測 (今の PG の最初の要求 48〜50k。絞る前は約 12 万) が根拠

## 🚨 前提 (2026-09-25 の監査 460 で分かったこと。`CLAUDE_CONFIG_DIR` を採らなかったので今は当たらない)

- PG 用の `CLAUDE_CONFIG_DIR` を渡すと、PG の transcript は `$CLAUDE_CONFIG_DIR/projects` に書かれるが、pro-con は `~/.claude/projects` に決め打ちで読む
  (`src/pro-con/main.go` の 2 か所・`live.New`)。そのままでは見張り (`dispatcher.watch`)・落ちた回数 (`restartsSince`)・カードの表示・428 の attach の記録が**黙って止まる**。
  `CLAUDE_CONFIG_DIR` を使うなら、同じ変更で projects の置き場を合わせ、見つからないことを出来事に出す

## 受け入れ条件

- [x] 役割ごとの設定ファイルがあり、daemon がそれを渡して起動する (ファイルでなく `--settings` の JSON。「対応」節)
- [x] 起動時の context の token を、絞る前 (約 13 万) と比べた実測値がある (449: 最初の要求 48〜50k。今回の auto memory の分は `/context` の見積もりだけ)
- [x] PG が、自分の触っていない issue を Stop hook に促されて書き換えにいかない (`--setting-sources project,local` で user の hook が外れる。project に settings.json は無い)

## 関連ファイル

- `_claude/settings.json` (ユーザーの設定。ここを PG 用に写さず、PG 用は別ファイルにする)
- `_claude/hooks/issue-progress-check.sh`

## 進捗

- [x] 下調べと計測 (2026-09-24。上の 2 節)。`--setting-sources project,local` で hook は外れる / 規約は外れない
- [x] 再計測 (2026-09-26、`/context`)。2.1.282 では `--setting-sources project,local` で `~/.claude/rules/` も外れている / `claudeMdExcludes` は `--settings` から効く
- [x] 役割ごとの `--settings` (feat(pro-con): PG・PM と haiku に役割ごとの --settings を渡す)。433 は不要として閉じた
- [ ] **残り (未決)**: PG に `~/.claude/rules/` の一部を効かせるか (上の「今の PG は読んでいない」節)
- [ ] 未実測: 本物の session での起動時の token (今回の変更の後)
