---
name: claude-assets-review
description: Claude Code の資産 (settings の hooks・rules・skills・agents・CLAUDE.md) を、最新の公式ドキュメントと手元の claude のバージョンに照らして評価し、使っていない機能・公式の推奨とのずれ・効き目の薄い設定を、優先度と「変えると失うもの」つきの改善提案として返す。対象の dir を引数で指定できる (省略時は dotfiles の _claude)。読み取りだけで、直すのはユーザーが選んだ提案を issue にした後。「Claude の資産を評価して」「claude 系の設定を見直して」「hooks や skills を公式ドキュメントと照らして改善点を出して」「claude-assets-review」で発火。CLAUDE.md・README と実体のずれを直すのは claude-md-refresh、常時読み込みを縮めるのは claude-md-compact。
---

# Claude Assets Review

Claude Code の資産を公式ドキュメントと手元の claude のバージョンに照らし、改善の提案を返す。**この skill は何も直さない**。
提案のうちユーザーが選んだものを issue にし、実装はその issue で行う。

## 引数

```
/claude-assets-review                     # dotfiles の _claude (global 層)
/claude-assets-review ~/repo/.claude      # repo の .claude
```

引数を省いたときの対象は `$HOME/dotfiles/_claude`。作業の dir は `skill-tmpdir claude-assets-review` が返す dir (以下 `$W`)。

## Step 0: 公式ドキュメントを読み直す (バージョンが変わったときだけ)

`claude --version` を `inventory.sh` の冒頭のコメントに書いたバージョンと比べる。**同じなら Step 1 へ進む**。
違うなら、sonnet の read-only のサブエージェント (`claude-code-guide`) にドキュメントを読ませ、新しい欄・event・設定を挙げさせる。
指示には次を入れる:

- 読む所: `https://code.claude.com/docs/llms.txt` を入口に、memory・hooks・skills・sub-agents・settings・plugins・best-practices
- 「読み取りのみ。どこにも書かない」。ページが途中で切れたら、切れた位置より後の項目を「未確認」と書かせる
- 欄の名前を正確に (camelCase か kebab-case か) 返させる

返ってきた一覧は main が検閲してから `inventory.sh` の `KNOWN_SKILL_KEYS`・`KNOWN_AGENT_KEYS`・`FEATURES` と冒頭のバージョンに反映する
(反映は issue を立てて行う。この skill の手順の中では直さない)。反映されるまでは、inventory の「公式の一覧に無い欄」を、検閲した一覧と照らし直してから判定に使う。**ドキュメントは仮説**で、提案に使う機能は Step 2 でバイナリに在ることを確かめる。

## Step 1: 事実を数える

```sh
~/.claude/skills/claude-assets-review/inventory.sh <claude dir の絶対パス> >"$W/inventory.txt" 2>"$W/inventory.err"; echo rc=$?
```

rc=2 は dir の取り違えか jq が無い (stderr を読む)。対象が dotfiles の `_claude` なら、常時読み込みの字数は
`scripts/check_instruction_budget.sh` で測る (150k 字の上限との差を出す道具が既にある。数え方を写さない)。

## Step 2: 観点ごとに評価する

`inventory.txt` を読み、下の表の観点ごとに「今」を書く。**提案に使う機能は、inventory の末尾の機能名の件数が 0 でないことを確かめる**
(表に無い機能なら `strings "$(readlink -f "$(command -v claude)")" | grep -c <名前>` で確かめる)。

| 観点 | 見るもの | 提案の形と、変えると失うもの |
|---|---|---|
| 読み取りのみのはずの agent | `tools` / `disallowedTools` の無い agent のうち、名前と description がレビュー・監査・調査のもの | `disallowedTools: Edit, Write, NotebookEdit` を付ける。失うもの: その agent に直す仕事を頼めなくなる |
| 常時読み込みの量 | rules の本数・`paths:` の本数・大きい rule・budget の値 | `paths:` へ移す・rationale へ外す (`claude-md-compact` が担当)。**行動 (commit・テスト・push) で発火する rule は無条件のまま** (dotfiles の `CLAUDE.md` の決定)。`InstructionsLoaded` hook で実際に読まれた rule を記録してから決める案もある |
| サブエージェントの文脈 | 軽い調査に使う agent | `omitClaudeMd: true`。失うもの: rule で守っている作法 (tmp の置き場・読み取りのみの規律) が外れる。Explore 型に限る |
| SessionStart の matcher | matcher なしの組 | 起動時だけで足りる hook を `startup` に絞る。**絞る前に hook の中身を読み、何度走っても結果が変わらない (済んでいれば抜ける) なら提案しない**: 得るものがほぼ無く、`resume` で走らなくなる損だけが残る (2026-11-13 に dotfiles の 4 本で確かめた)。規約を注入する hook は compact の後にも要るので絞らない |
| tool の hook の `if` | `if なし` の本数が多い matcher | 案内だけの hook (`suggest-*`・`warn-*`) を `if` で絞る。**止める hook (`deny-*`) は、`if` が複合コマンド (`a && b`・パイプ・`sh -c`) を取りこぼさないかを実測するまで触らない** (取りこぼすと安全機構が無音で外れる)。効果は並列で数十 ms なので優先度は低い |
| 非公式の欄 | skills・agents の「公式の一覧に無い欄」 | 読まれずに無視されるだけで害は無い。消すかは好み。**公式の欄の綴り違い** (`allowed_tools` 等) なら Claude Code に読まれていないので優先して直す |
| skill の欄の活用 | `context`・`argument-hint`・`disable-model-invocation`・`model` を使う skill の数 | 重い調査の skill に `context: fork` (調べた中身が会話に残らない)、引数を取る skill に `argument-hint`。`disable-model-invocation` は、ユーザーのキーワードで起動させたい skill に付けると発火しなくなるので、副作用だけで目的のない skill に限る |
| 大きさ | description が一覧の上限を超える skill・500 行を超える SKILL.md | 先頭に使う場面を置く・詳細を同じ dir の別ファイルへ移す |
| compaction | `PreCompact` / `PostCompact` の hook の有無 | 起きた時刻と回数を記録すると、要約の事実を裏取りする場面を後から探せる |

表に無い所見も、ドキュメントの新機能と inventory の事実から出してよい。そのときも「今 / 案 / 失うもの」の 3 つを揃える。

## Step 3: 既にある決定と照らす

提案ごとに、同じ話を既に決めていないかを探す。探すのは issue (`grep -rln '<機能名か対象の名前>' issues/`。done と pending も)、
対象のファイルの中の「採らない」「見送った」のコメント、rule・`CLAUDE.md` の「ユーザー指示」「決定」。
issues/ の無い repo では、対象のファイルのコメントと rule・`CLAUDE.md` だけを見る。決定と食い違う提案は、決定を名指しして「変える理由」を書くか、提案から外して外した理由を 1 行残す。

## Step 4: 報告する

報告は `$W/report.md` に書き、会話には要約を返す。提案は効果の大きい順に並べ、1 件ごとに次を書く:

- **今**: inventory の数字か、ファイル名とシンボル名 (行番号では書かない)
- **案**: 何をどう変えるか
- **失うもの**: 今の挙動から消えるもの。使い心地が変わるなら、ユーザーが体験する場面を 1 つ添える
- **確かめたこと / 未確認**: バイナリに在るか・実測したか。ドキュメントで読んだだけなら「未確認」と書く

最後に、確かめられなかった範囲 (切れたページ・読んでいないページ・実測していない挙動) を書く。
どれを issue にするかをユーザーに聞き、選ばれたものを issue 運用の規約どおりに起票する (反証レビューを含む)。

## やらないこと

- 提案をこの skill の中で実装しない (選ばれた後の issue で行う。hook・settings の変更は動いている全セッションの挙動を変える)
- ドキュメントの記述だけを根拠に「この機能は使える」と書かない (バイナリに在ることを確かめる)
- 件数の多さ (agent 26 本・hook 15 本) だけで減らす提案をしない。何が良くなるかを書けないものは出さない
