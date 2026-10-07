# Claude Code の mods (関数 hook の plugin) を dotfiles で持つ

Claude Code 2.1.287 の mods を、settings の hook (`_claude/settings.json` の `command`) と並べて持つための約束。
どの hook を mod へ移すか / 移さないかの判断の正本は epic issue 618、土台の実測は issue 619 に置く。
API の正本は plugin-authoring skill が書き出す `claude-code.d.ts` で、**early access なのでリリースごとに変わる**。
初めて触るなら、全体像と変更の手順を [`claude-mods-guide.md`](claude-mods-guide.md) で先に読む。

## 🚨 このマシンでは mod に届かないイベントがある — **2026-10-07 に消えた (条件付き)**

> **2026-10-07 (claude 2.1.292) の測り直し**: 619 の probe (`prompt.compose` / `prompt.context` を足した 15 イベント) を `--plugin-dir` で載せ `claude -p` (Bash 1 回) で回すと、
> **15 イベント全部に印が付き、debug log に `bypassed by` は 0 本**。理由は debug log の
> `cc-plugin-sec-default@builtin not seated: no managed settings and not a Team or Enterprise organization (max)` で、
> 下の表の原因だった managed settings (`/Library/Application Support/ClaudeCode/managed-settings.json`) が**無くなっている** (Broken symlink or missing file)。
> つまり下の表は「managed settings があるとき」の挙動で、**今のこのマシンでは `classic.*` / `prompt.compose` / `prompt.context` / `prompt.section` も user の mod に届く**。
> 🚨 managed settings が戻れば (組織のポリシー配布) 表のとおり素通しに戻るので、これらの口に依存する mod は「届かないときに何が起きるか」を持つ (epic 618「mod が黙って止まったときの扱い」)。
> 測り直しの手順は下 (probe は 619 の本文)。下の表は、戻ったときの参照用に残す。

managed settings (組織のリモートの設定) のあるマシンでは、組み込みの `cc-plugin-sec-default` が一番外側に座り、user の tier の mod の
一部のイベントを呼ばずに素通りさせる。debug log (`--debug-file`) に `<plugin>: <event> bypassed by cc-plugin-sec-default (tier user); beneath runs` と出るだけで、
mod の側からは何も起きないように見える (2.1.287 で実測。issue 619)。

| 届く | bypass される |
|---|---|
| `session.start` / `prompt.submit` / `turn.start` / `turn.complete` / `tool.call` / `tool.describe` / `session.end` / `ui.render` (AbovePrompt) / `$.ui.status` | `classic.*` (測ったのは SessionStart・UserPromptSubmit・PreToolUse・PostToolUse・Stop) / `prompt.section` / `prompt.compose` / `prompt.context` |

- 帰結: mod からシステムプロンプトと、最初のメッセージの context ブロック (CLAUDE.md の枠) に文を足せない。settings の hook のイベントを `classic.<Event>` で受けることもできない。
  プロンプトの隣にモデル向けの block を足す `prompt.submit` の `context` は届く (置き場所は settings の hook の注入と同じメッセージの側)
- `--plugin-dir` で載せても `CLAUDE_CODE_PLUGIN_DIRS` で載せても tier は `user` で同じ
- 測り直し方: 各イベントで印を書くだけの probe の mod (コードは issue 619 の「このマシンでは user の mod に届かないイベントがある」) を `--plugin-dir` で載せ、`--debug-file` の `bypassed by` の行を見る。Claude Code の更新や
  managed settings の変更で変わりうるので、届かない前提に頼る設計 (ここに書いた表) は更新のたびに疑う
- 未確認のリスク: 組織が sideload (`--plugin-dir` / `CLAUDE_CODE_PLUGIN_DIRS`) を禁じるポリシーを入れると、`CLAUDE_CODE_PLUGIN_DIRS` を持つ claude は
  起動時にエラーで止まりうる (2.1.287 の二進を静的に読んだだけ。回復の案内は出る)。そうなったら settings の env の行を外す
- user の settings を読む裏の `claude -p` (ratelimit の `claude -p /usage` 等) でも mods が走る。mod は `session.start` の
  `e.isInteractive` / `e.surface` で、対話のセッションにだけ効かせる
  - 🚨 **ただし desktop が起こすセッションは `-p` / SDK と同じ作り**で、型定義 (`SessionStartInput` / `session.attach`) によると、開始時は
    `surface` が null・`isInteractive` が false・`$.session.surfaces()` (名簿) が空で、desktop は最初の描画の要求で名簿に載る (実機の値は未観測)。
    `session.start` や `turn.complete` で「desktop / 対話か」を名簿・`e.isInteractive` で聞くと、desktop では帯が一度も作られない (issue 625)。
    desktop が起こしたセッションかは、起動時に決まる環境変数 **`CLAUDE_CODE_ENTRYPOINT=claude-desktop`** (`$.env.get`) で聞く。描くかどうかは描画のたびの `e.surface` で決める (名簿でなく描く先そのもの)。
    🚨 この環境変数は子の `claude -p` にも**継承される** (desktop の Bash から起こした `claude -p` で `claude-desktop` を実測)。desktop 以外の -p も真になりうるので、起動を待たせる処理 (issue-band の数え直し) は裏で走らせ、描画は `e.surface` で決める
    テストの fixture も、この初期状態 (`surface: null` / `isInteractive: false` / 名簿空 / 環境変数あり) から始める
  - 見張り: 対話の CLI は `CLAUDE_CODE_PLUGIN_DIRS` のフォルダを見張って mod を読み直すが、desktop (長命の headless セッション) は `CLAUDE_CODE_PLUGIN_DIR_WATCH=1` を env に持つときだけ見張る
    (plugin-authoring の reference.md による。未実測)。見張っていない desktop のセッションは、mod を直しても**新しいセッションを開くまで旧版のまま**

## 今ある mod

一覧・各ファイルの役割・仕組み・変更の手順は [`claude-mods-guide.md`](claude-mods-guide.md) にある (入門と地図。ここは約束と実測)。

## 置き場所と読み込み

- 1 つの mod = `_claude/mods/<name>/` の 1 ディレクトリ。`.claude-plugin/plugin.json` / `hooks/hooks.json` / `hooks/register.ts(x)` と、`tests/*.test.ts(x)`
- `_claude/settings.json` の `env` に `CLAUDE_CODE_PLUGIN_DIRS=~/dotfiles/_claude/mods` を 1 つ書いてある。親のフォルダを渡すと子の mod を全部読むので、
  mod を足しても settings は書き換えない (2.1.287 で実測。プロセスの環境で渡した場合。`~` も効く)
  - 子のうち `.claude-plugin/plugin.json` の無いディレクトリは読まれない (debug log に `no manifest in <dir>` が出るだけ)。
    子の 1 本の hooks module が壊れていても、他の子は読まれる
  - settings の `env` からも読まれる (対話・`claude -p`。2.1.287 で実測)。`--setting-sources` から `user` を外した起動 (pro-con の役) では読まれない。
    値はセッションの Bash の子へ継承される (`~` のまま) ので、Claude の中から起こした `claude -p` も mods を読む
- 読まれるのは `~/dotfiles` の実体。hook と同じく、worktree で編集しても master へ push して `~/dotfiles` へ pull するまで効かない
  (`.claude/rules/worktree-per-session.md`)。対話のセッションはフォルダを見張っていて、`hooks/` などを保存すると mod を読み直す
- engine は mod を読むたびに `<mod>/.claude-plugin/types/` (その build の型) と、無ければ `<mod>/tsconfig.json` (types/ を extends するだけ) を書く。
  どちらも `.gitignore` 済み。types/ はセッションごとに中身 (MCP の型) が違い、共有の `~/dotfiles` で書き換え合うが、見張りは types/ を見ないので読み直しの往復にはならない (実測)

## 読み込まれる経路 (2.1.287 で実測。issue 619)

`_claude/mods/canary` は何もしない mod で、`DOTFILES_MOD_CANARY_DIR` が在るときだけそこへ印 `loaded` (session_id・surface・見えている `CLAUDE_CODE_PLUGIN_DIRS`) を書く。
経路を測り直すときは、子を `env -i` の最小の環境から起こし (Claude の中の環境変数を持ち込まない)、経路ごとに新しいディレクトリを渡して印を読む。

| 経路 | プロセスの環境に `CLAUDE_CODE_PLUGIN_DIRS` あり | 無し |
|---|---|---|
| 対話 (`claude`) | 読む | 読まない |
| `claude -p` | 読む (`CLAUDE_CODE_ENABLE_FUNCTION_HOOKS` は要らない) | 読まない |
| `claude --bg` (pro-con の役と同じ引数) | 読む | 読まない |

- `claude --bg` の session は daemon が先に起こした spare で、環境は基本的に daemon を起こしたプロセスのもの (例: `DOTFILES_MOD_CANARY_DIR`)。
  ただし `CLAUDE_CODE_PLUGIN_DIRS` だけは `claude --bg` を叩いた client の値に従う: daemon の環境にあっても client に無ければ mods は読まれず、session の Bash の環境にも残らない

## pro-con の役 (PG・PM・取り込みの係・haiku・枠を読む `claude -p /usage`) には載せない

役は `--setting-sources project,local` (枠を読む係は `""`) でユーザーの settings の hook と env を外して起動している (issue 431)。mods もそれに揃え、
dispatcher が claude を起こす環境から `CLAUDE_CODE_PLUGIN_DIRS` を落とす (`src/pro-con/dispatcher/launcher.go` の `roleEnv`)。
`--setting-sources` はプロセスの環境の値を落とさないので、dispatcher の環境にこの値があると、落とさない限りその起こし方のときだけ役に mods が載る。

## テスト

- `make test` が `tests/claude/test_claude_mods.sh` を回す。settings の env の値を確かめたうえで、`_claude/mods/*/` を自動で見つけ、各 mod に
  `claude plugin validate` と `claude plugin test` をかける。manifest の無いディレクトリ・mod が 0 件・テストが 0 件、はどれも失敗
  (mod を消した後に ignore 済みの生成物だけが残ったディレクトリも赤になる。そのときは消してよい)
- **CI では claude の要る検査を skip する (exit 77)**。runner に claude が無く、入れると毎回その時点の最新版になって、手元で動いている engine と版が揃わない。手元の `make test` が正本
- `make test-changed` は `_claude/mods/` と `_claude/settings.json` の変更で tests/claude を回す
- `claude plugin test` の `$` は engine そのもので、テストの `on` が engine の役 (plugin の下) に座る。engine の既定の答えは無いので、mod が呼ぶ `$` の口
  (`session.start` / `process.run` …) はテスト側で答える。`$` の口 (op のイベント: `process.run` / `session.id` …) は `{ value: … }` で包んで返す
  (素の値を返すと「result object ではない」として捨てられ、その口を呼んだ plugin の hook ごとスキップされる)。engine のイベント (`session.start` / `tool.call`) は結果の形のまま返す

## settings の hook との分担

- 守りの hook (deny / block) は移さない。**mod の hook は失敗すると黙ってスキップされ、chain が続く**。知らせは debug log か、見張っているフォルダなら transcript の 1 行だけ
- 移す mod は「読み込まれなかったとき何が起きるか」と、それを検出する手段を持つ (epic issue 618「mod が黙って止まったときの扱い」)
- どれを移すかの表は epic issue 618 にある。ここには写さない
