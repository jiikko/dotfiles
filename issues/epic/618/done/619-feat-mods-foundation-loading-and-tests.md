# 619 (feat): mods の土台 — 置き場所・読み込みの配線・テスト・入口の文書

起票日: 2026-10-02

epic [618](../pending/618-design-claude-code-mods-migration.md) の子。620〜624 の前提。

## 概要

mod を dotfiles で持つための共通部分を作る。中身のある mod は 620 以降で足す。

## 対応方針

1. **置き場所**: `_claude/mods/<name>/` (plugin 1 つ = ディレクトリ 1 つ。`.claude-plugin/plugin.json` / `hooks/hooks.json` / `hooks/register.ts(x)`)
2. **読み込み**: `_claude/settings.json` の `env` に `CLAUDE_CODE_PLUGIN_DIRS` を書く。値は親フォルダの `~/dotfiles/_claude/mods` 1 つ
   (子の mod を全部読むことを実測したので、mod ごとに並べる案から変えた)。
   hook と同じく `~/dotfiles` の実体パスから読まれる (worktree で編集しても、master へ push して `~/dotfiles` へ pull するまで効かない。
   `.claude/rules/worktree-per-session.md`)
3. **読み込まれる経路の実測** (618 の前提の 1 項目目): 何もしない mod (`session.start` で `$.ui.log` するだけ) を置き、
   対話 / `claude -p` / pro-con の PG で読まれるかを確かめる。
   - 🚨 Claude のセッションの中 (Bash ツール) から測ると、親の環境の `CLAUDE_CODE_PLUGIN_DIRS` が子に渡り、「読まれる」と誤って緑になりうる。
     各経路を `env -u CLAUDE_CODE_PLUGIN_DIRS` 付きと無しの両方で回し、settings の env から読まれたのか、親の環境から読まれたのかを分ける
   - PG は今もユーザーの hook が走らない (618 の前提)。PG の dispatcher で `CLAUDE_CODE_PLUGIN_DIRS` を外すか、載せると決めるかを決め、決めたほうをテストで固定する
4. **生成物を無視する**: engine は mod を読むたびに `<mod>/.claude-plugin/types/` (この build の API と、マシンごとに中身が違う MCP の型) を書き出す (reference.md「The types are the reference」)。
   `~/dotfiles` の共有の working tree に毎回 untracked が増えないよう、`_claude/mods/*/.claude-plugin/types/` を `.gitignore` に足す
5. **テスト**: `claude plugin validate` と `claude plugin test` (`*.test.ts`) を `make test` から回す。`tests/claude/` に、
   `_claude/mods/*/` を自動で見つけて両方を回す bash のテストを 1 本置く。mod が 0 件なら失敗にする
   (`verify-execution-not-just-exit-code.md`: 対象 0 件は失敗)。mod ごとにも、`claude plugin test` の出力から実行されたテストの件数を取り、0 件なら失敗にする
   (`*.test.ts` の無い mod が rc=0 で通るかは未確認なので、件数で見る)
   - CI (macOS runner) に `claude` が入っていない (`.github/workflows/` に install の行が無い)。CI でどうするかを決める:
     runner に入れて必須にするか、CI では対象外と明示するか。判定不能を緑として扱う形にはしない
6. **入口の文書**: `_claude/CLAUDE.md` か `docs/` に「mod の置き場所・読み込み・テスト・settings の hook との分担 (618 の表)」を書き、
   `docs/README.md` の索引に 1 行足す (`new-tool-requires-entrypoint-docs.md`)

## 受け入れ条件

- [x] `_claude/mods/` に何もしない mod が 1 つあり、`make test` で validate と test が走ったことを出力で確かめた (`[ok] tests/claude/test_claude_mods.sh`、出力に `✓ canary: claude plugin test (2 件)`)
- [x] 対話 / `claude -p` / PG の 3 経路で、`env -u CLAUDE_CODE_PLUGIN_DIRS` の有無ごとに、読まれた・読まれなかったの実測を本文に書いた (下の「実測」。プロセスの環境の経路)
- [x] settings の `env` から読まれる経路の実測 (push して `~/dotfiles` へ pull した後に測った。下の「実測」の S1 / S2 / S4)
- [x] PG に mod を載せるか外すかを決め、dispatcher の起動の環境をテストで固定した (外す。`roleEnv`。TestPersistentSessionProfile / TestHaikuPassesSettings / TestUsageCmd)
- [x] mod を読ませた後、`git status` に `.claude-plugin/types/` が出ないことを確かめた (engine 自身が types/ に `*` の .gitignore を置く。repo の `.gitignore` にも足した)
- [x] 入口の文書と索引を足した (`docs/claude-mods.md`、`docs/README.md`、root の `CLAUDE.md` の `_claude/` 節)
- [x] CI での扱い (必須 / 対象外の明示) を決めて本文に書いた (claude の要る検査は CI で exit 77。settings の配線の検査は CI でも走る)
- [x] 3 経路の結果を 618 の表と 620〜623 の着手条件に反映した

## 実測 (2026-10-02 / claude 2.1.287。子は `env -i` の最小の環境から起こし、Claude の中の環境変数を持ち込まない)

canary (`_claude/mods/canary`) の印 `loaded` の有無で読んだ。経路ごとに新しいディレクトリを `DOTFILES_MOD_CANARY_DIR` で渡した (各 1 回)。
表の記号は測った run の名前: I = 対話、P = `claude -p --model haiku`、G / E = `claude --bg --model haiku --setting-sources project,local --settings <PG と同じ>` を `~/dotfiles` で起動。
E1 / E2 は daemon が止まった状態から 2 つの client を続けて起こした組 (E1 = dirs あり → なし、E2 = なし → あり)。id は `claude --bg` が返した session の短い id (印の中の session_id の先頭)

| 経路 | プロセスの環境に `CLAUDE_CODE_PLUGIN_DIRS` あり | 無し (`env -u` 相当) |
|---|---|---|
| 対話 (隔離した tmux `-L` で起動) | 読む (I2) | 読まない (I1) |
| `claude -p` | 読む (P1。`CLAUDE_CODE_ENABLE_FUNCTION_HOOKS=1` を足しても同じ = P2) | 読まない (P0) |
| `claude --bg` + `--setting-sources project,local` (PG と同じ引数) | 読む (E1a / E2b) | 読まない (E1b / G0) |

- 親フォルダ (`_claude/mods`) を渡すと子の mod を読む (P3)。`~` も効く (P4)
- 🚨 `--bg` は daemon (`claude daemon run --origin transient`) が先に起こした spare を引き当てる。spare の環境は daemon を起こしたプロセスのもの。
  ただし mods を決める `CLAUDE_CODE_PLUGIN_DIRS` は client の値が効く: E2 (dirs なしの client が daemon を起こし、次に dirs ありの client) では
  dirs ありの session (f8009507) が読み込み、印は daemon の環境の `DOTFILES_MOD_CANARY_DIR` (先の client のディレクトリ) に書かれた。
  E1 (逆順) では、daemon の環境に dirs があっても dirs なしの client の session (ea3d3f81) は読まなかった
- 計測で起こした daemon は、session を止めた 5 秒後に自分で終了した (`idle 5s with no clients — exiting`)。`claude agents` に残っていた `pc-c-089` は 9/26 に終わった古い記録
- 親フォルダの中に壊れた mod (hooks module が parse できない) があっても、他の子は読まれる。`claude -p` の text 出力では stderr に `broken: hooks module did not load: ...` が出る
- 子のうち manifest の無いディレクトリは読まれない (debug log に `no manifest in <dir>` が出るだけ)。テストで失敗にした
- **settings の `env` からの読み込み** (push して `~/dotfiles` へ pull した後、プロセスの環境に変数を持たせずに測った):
  対話 (S2) と `claude -p` (S1) は読む。印の `pluginDirs` は settings に書いた文字列のまま (`~/dotfiles/_claude/mods`)。
  `--setting-sources project,local` (PG と同じ) では読まない (S4)。S1 の Bash の子の環境には `CLAUDE_CODE_PLUGIN_DIRS=~/dotfiles/_claude/mods` が継承されていた
  (→ Claude の Bash から起こした `claude -p` や pro-con も mods を読む。pro-con は `roleEnv` で落とす)

## このマシンでは user の mod に届かないイベントがある (620〜625 に効く)

managed settings (組織のリモートの設定) があるマシンでは、組み込みの `cc-plugin-sec-default` が一番外側に座り (`seated outermost: this machine has managed settings`)、
user の tier の mod の一部のイベントを呼ばずに素通りさせる (debug log: `<plugin>: <event> bypassed by cc-plugin-sec-default (tier user); beneath runs`)。

| 届く | bypass される |
|---|---|
| `session.start` / `prompt.submit` / `turn.start` / `turn.complete` / `tool.call` / `tool.describe` / `session.end` / `ui.render` (AbovePrompt、対話) / `$.ui.status` | `classic.SessionStart` / `classic.UserPromptSubmit` / `classic.PreToolUse` / `classic.PostToolUse` / `classic.Stop` / `prompt.section` / `prompt.compose` / `prompt.context` |

測り方: 各イベントで印を書くだけの probe の mod を `--plugin-dir` で載せ、`claude -p` (Bash を 1 回使わせる) と対話 (隔離した tmux) で印と debug log を見た。
対話の probe は `session.start` で `$.ui.status`、`ui.render` (AbovePrompt) で文字列を返すだけの別の mod。probe (`-p` 用。manifest は `{"name":"evprobe",...}`、印の置き場は `EVPROBE_DIR`):

<details><summary>register.ts</summary>

```ts
import type { Register, EngineInterface } from 'claude-code'
const mark = async ($: EngineInterface, name: string) => {
  const dir = await $.env.get('EVPROBE_DIR')
  if (dir) await $.fs.write(`${dir}/${name}`, 'x\n')
}
export const register: Register = on => {
  on('session.start', async ($, e, next) => { await mark($, 'session.start'); return next(e) })
  on('prompt.submit', async ($, e, next) => { await mark($, 'prompt.submit'); return next(e) })
  on('prompt.section', async ($, e, next) => { await mark($, 'prompt.section'); return next(e) })
  on('turn.start', async ($, e, next) => { await mark($, 'turn.start'); return next(e) })
  on('turn.complete', async ($, e, next) => { await mark($, 'turn.complete'); return next(e) })
  on('tool.call', { tool: 'Bash' }, async ($, e, next) => { await mark($, 'tool.call'); return next(e) })
  on('tool.describe', async ($, e, next) => { await mark($, 'tool.describe'); return next(e) })
  on('classic.SessionStart', async ($, e, next) => { await mark($, 'classic.SessionStart'); return next(e) })
  on('classic.UserPromptSubmit', async ($, e, next) => { await mark($, 'classic.UserPromptSubmit'); return next(e) })
  on('classic.PreToolUse', async ($, e, next) => { await mark($, 'classic.PreToolUse'); return next(e) })
  on('classic.PostToolUse', async ($, e, next) => { await mark($, 'classic.PostToolUse'); return next(e) })
  on('classic.Stop', async ($, e, next) => { await mark($, 'classic.Stop'); return next(e) })
  on('session.end', async ($, e, next) => { await mark($, 'session.end'); return next(e) })
}
```

</details>

`classic.Notification` 等の未測定の classic も、測った 5 つが全部 bypass なので届かない前提で扱う (使う子 issue で実測する)。

## 敵対的レビュー (opus、read-only) の結果

1 周目 (P1 なし / P2 3 / P3 9) と、修正した差分への 2 周目 (P1 なし / P2 2 / P3 5)。

- 採って直した: manifest の無いディレクトリを失敗に / settings の `env` の値をテストで固定 (claude が要らないので CI でも走る) /
  一時ファイルを mktemp + trap に / 枠を読む `claude -p /usage` (usage.go) も `roleEnv` に / test-changed で `_claude/mods/` と `_claude/settings.json` を
  tests/claude へ (`*.json` の写像より前。mod の中の .sh には shell の lint も) / 写像のテストの `*.json` の代表を `_claude/keybindings.json` に (新しい腕に吸われていた) /
  engine が書く `tsconfig.json` を ignore / 文書の主張を実測に合わせる (settings の env の経路は未実測と書く) / 消した mod の残骸で赤になったときの案内を文言に
- **反証した (実測で起きなかった)**:
  - 「`claude plugin test` が親の環境の `CLAUDE_CODE_PLUGIN_DIRS` の mod も読み、false green になる」: env 側に同名で中身の違う mod を置いても結果は変わらず、壊した側はそのまま赤。
    再提起するなら、env 側の mod の変更で `claude plugin test` の結果が変わる実例を添えること
  - 「engine が共有の `~/dotfiles` の `types/` を書き換え合い、見張っているセッションが読み直しを往復する」: 見張っている対話のセッションで `types/` に書いても読み直しは起きず、
    `hooks/register.ts` に書くと起きた (`hooks module canary@inline reloaded`)。中身はセッションごとに書き換わるが、ignore 済みで往復はしない
  - 「PG の孫 (PG の Bash から起こした `claude -p`) に `CLAUDE_CODE_PLUGIN_DIRS` が残る」: daemon を dirs ありの client が起こした後、dirs なしの client で起こした
    bg の session の Bash で `env` を読むと、変数は無かった (rc=1)
- 採らなかった: pass の件数と実行件数の突き合わせ (1 度入れて外した)。kit に skip / todo が無く (`test.skip` は undefined で rc=1)、rc の検査と同値で変異で赤を確かめられない
- 未確認のリスクとして残す (`docs/claude-mods.md`): 組織が sideload を禁じるポリシーを入れると、`CLAUDE_CODE_PLUGIN_DIRS` を持つ claude が起動時にエラーで止まりうる (二進の静的読み) /
  ratelimit の裏の `claude -p /usage` (user の settings を読む) でも mods が走る (621 / 622 の mod は `e.isInteractive` 等で絞る)
- 今日の make test で `tests/bin/test_kernel_alloc_watch.sh` (ロックを 20 秒待つ検査) が 2 回とも落ちた。単体では rc=0 で、この変更は bin/ tests/bin/ に触れていない
  (原因は未確認。並列実行と同時に走らせていた claude の負荷による時間切れを疑っている)

## 進捗

- 2026-10-02: 起票
- 2026-10-02: 起票と同じ日に、反証レビュー (sonnet 2 本) と敵対的レビュー (opus 2 本) の指摘で方針を改訂した。619 で、実測を `env -u CLAUDE_CODE_PLUGIN_DIRS` の有無で分ける・PG に mod を載せるか決めてテストで固定・`.claude-plugin/types/` の ignore・mod ごとのテスト 0 件を失敗に・CI の扱いを決める、を足した。採否と理由の一覧は親 618 の進捗
- 2026-10-02: 実装 — canary の mod・settings の env・`tests/claude/test_claude_mods.sh`・pro-con の役から外す (`roleEnv`)・入口の文書。経路の実測と、このマシンで mod に届かないイベントを上に書いた。敵対的レビュー 2 周の結果も上
- 2026-10-02: push して `~/dotfiles` へ pull し、settings の env からの読み込みを実測 (対話・`-p` は読む、`--setting-sources project,local` は読まない、Bash の子に継承される)。受け入れ条件がすべて埋まったので done
