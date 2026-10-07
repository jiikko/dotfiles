# 620 (feat): issue 規約の注入を、mod でシステムプロンプトの節へ上げる (守られる率が上がるかを先に測る)

> 🚨 **担当中: obaket の session (peer-inbox / ratelimit-warn を作った Fable)**（2026-10-07〜）

> 2026-10-02 に「移さない」で決着 → **2026-10-07 に再開**: 再開の trigger (managed settings が無くなり `prompt.compose` が user の mod に届く) を同日の測り直しで確認した (618 の進捗)。下の計測計画を回す。

起票日: 2026-10-02

epic [618](618-design-claude-code-mods-migration.md) の子。619 の後。epic で最初に着手する 1 本 (ただし移す前に仮説を測る)。

## 概要

`_claude/hooks/issue-rules-inject.sh` は、`issues/` を持つ repo のセッションで、SessionStart の `additionalContext` として
`_claude/issue-rules.md` と `_claude/issue-rules.d/*.md` (3 本) を注入している (settings に 4 行。issue 401)。
この注入は `<system-reminder>` として届き、CLAUDE.md と同じ拘束力を持たない。そのため `_claude/CLAUDE.md` に
「注入された規約に従う」義務を 1 行残している。

mod の `prompt.compose` なら、同じ文をシステムプロンプトの節 (`scope: 'session'`) として足せる。

## 仮説 (未検証。この issue はまずこれを測る)

「システムプロンプトの節にすれば、今の `<system-reminder>` の注入より規約が守られる」。**根拠は無い**。

- rules が守られている理由は、文面よりも置き場所と名目 (「ユーザーの指示。既定の振る舞いより優先し、必ず従う」の枠で、行動の前から全文が在る) にあると見ている。
  plugin が足した節がどの名目でモデルに見えるかは確かめていない。システムプロンプトの中にあっても「ユーザーの指示」と同じ扱いになる保証は無い
- 文面を移しても拘束力は足されない。変わるのは置き場所と名目だけで、強まるか弱まるかは測るまで分からない。モデルの自己申告 (「守ります」) は証拠にしない

### 計測 (A-B)

- **腕**: A = 今の SessionStart の注入 / B = mod の節 / **C = 規約なし** (対照。C と A で差が出ない項目は、既定の振る舞いで守られているので判定から外す)。
  同じ規約の文面、同じモデル (ふだん使う main のモデル。haiku で代用しない)
- **隔離した config で回す**: 本番の `~/dotfiles/_claude/settings.json` から hook を外すと、並行中の全セッションから規約が消える
  (hook は実体パスから起動し、worktree の編集は効かない)。`ISSUE_RULES_FILE` を空のファイルへ向ける手も使えない (「規約を読めなかった」と注入され、規約を指す文が B に混ざる)。
  そこで 3 腕とも `CLAUDE_CONFIG_DIR` か HOME を差し替えた隔離した config で回し、settings の写しの hook の行だけを腕ごとに変える。B の mod は `--plugin-dir` でだけ載せる
  (本番の mods フォルダには置かない)。隔離の下で認証 (Keychain) が通るかは未確認なので、先に 1 本で確かめる
- **CLAUDE.md の文を腕でそろえる**: `~/.claude/CLAUDE.md` の「Issue管理」は、注入に「この CLAUDE.md と同じ拘束力で従う」を与え (A だけが後押しされる)、
  「注入が見当たらないときは正本を Read する」とも言う (B と C は規約を Read しやすい)。隔離した config の CLAUDE.md では、この節を 3 腕で同じ中立の文にするか外す。
  `issue-rules*.md` を Read した run は記録して別に集計する
- **Stop hook の差し戻しより前で判定する**: Stop の `issue-progress-check.sh` (進捗を書かせる) と PostToolUse の `git-state-verify.sh` / `next-claim-push.sh` (未 push を知らせる) は、
  出口で項目を強制して腕の差を消す。進捗と push の項目は、最初に Stop が来た時点の状態で判定し、block の有無は stream-json から記録する
- **課題**: 隔離した一時 repo で、規約に触れる作業を headless で頼む (例: 「この不具合を issue に起票して」「この issue を完了にして」)。頼む文には規約の中身を書かない
  - 置き場所は 3 腕とも scratchpad (`/private/tmp/...`) 配下の同じパス (`~/dotfiles/tmp/` に置くと dotfiles の CLAUDE.md が祖先として読まれる)。git root と直下の `issues/` を持たせる (注入の条件)
  - remote はローカルの bare にする (本物の dotfiles に push できない形)
  - `next/` を持つ形も用意する (`next/` が無い repo では claim と「完了前に目印を消す」が規約上も適用されず、両腕 0 差に数えられる)。置く既存の issue は規約に沿った見本になるので、その影響を C で測る
- **コマンド**: 書き込みの道具を許可して回す (`.claude/rules/worktree-per-session.md` の観測用のコマンドは書き込みの道具を外しているので、A-B には使わない)。
  各 run で「issue ファイルが 1 つ以上できた」ことを判定の前提にし、できなかった run は判定不能として数える
- **測るもの**: 機械で判定できる規約の項目だけ。ファイル名の形 (`NNN-<type>-<slug>.md`)・`起票日:` の行・human の `期限:` の書式・採番した後すぐ commit したか・
  done へ移す前に目印を消したか、など。項目ごとに守った / 破ったを数える。判定は生成物と git の状態から読み、モデルの返答の文面からは読まない
- **回数と打ち切り**: 腕ごとの回数を始める前に決めて本文に書く。本物のセッションを起こすので、先に `ratelimit` で 5h 枠を見る。1 回の token を測ってから総数を決める
- **B で節が実際に足されたかを run ごとに確かめる**: `-p` では `/context` が使えず (`/context` は送信しない `analysis` の描画)、読み込みの失敗は json / stream-json の run では
  debug log にしか出ない (reference.md「Developing one」)。mod が `prompt.compose` (trait `print`) で節を足したことを、session_id ごとの印か debug log の行で確かめ、
  確かめられない run は判定から外す。節の名目 (どう見えるか) は、対話のセッションで `/context` を 1 回見て本文に書く
- **判定**: B が A より守られる率が上がったときだけ、下の対応方針へ進む。差が無い・下がるなら、移さずに閉じる (618 の表を「移さない」に直す)。
  差の大きさと回数を本文に書き、少ない回数で「上がった」と書かない

## 対応方針 (計測で B が上回ったときだけ)

- `_claude/mods/issue-rules/`: `prompt.compose` で `next(e)` の `sections` の最後に 1 節を足す
- issues/ の在りかの判定は TS で書き直さない。`_claude/hooks/lib/issue-hooks.sh` の `issue_hook_resolve_dir` は git root・`issue/` と `issues/` の両方・入れ子 1 段・
  `node_modules` / `vendor` の除外を持っている。判定を出力する入口を lib に 1 本置き、`$.process.run` で呼ぶ (621 と同じ方針)
- 中身は今と同じファイルを読む (正本は `_claude/issue-rules.md` のまま。mod に写さない)。
  **読むのは `session.start` で 1 回だけ**にし、結果を `$.state` に置く。`prompt.compose` は描画のたびに発火する (`/context` の解析でも) ので、
  そこでファイルや script を読むと、セッションの途中で規約ファイルが pull で変わった・script が一時的に失敗した、で節の文が変わり、prompt cache を外す
- 切り替えは 1 回で行う: mod を入れる commit で、settings の `issue-rules-inject.sh` の 4 行を外す (二重に注入しない)。
  ただし 619 の実測で mod が読まれない経路 (PG 等) があれば、その経路では settings の hook を残す

## 失敗モード

`issue-rules-inject.sh` は、規約を読めないときに黙らない設計にしている (冒頭の 🚨。「規約が届かないことがこのフックの壊れ方」)。
mod は失敗すると黙ってスキップされる。移すと、規約の無いセッションが気づかれずに続く形が新しく生まれる。

- `_claude/CLAUDE.md` の「注入された規約に従う」の 1 行は**残す** (mod が止まっても、規約を読みに行く手がかりになる)
- mod が規約を足せなかったとき (ファイルを読めない・判定の入口が失敗した) は、節の代わりに「規約を読めなかった」と書いた節を足すか `$.ui.toast` を出す。黙って何も足さない形にしない
- mod そのものが読み込まれなかったときに気づく手段を決める (例: settings 側に、mod の読み込みの印が無ければ警告だけを出す小さな hook を置く)。置かないなら、その判断と理由を書く
  - 🚨 印を `$.store` に置かない。`$.store` はセッションをまたいで残るファイルなので、一度 mod が動いた後は、mod が読まれなくなっても古い印で警告が永久に出ない。
    印は **session_id ごと**にする
  - 印を調べる時点は、mod の `session.start` より後に来る点 (最初の UserPromptSubmit 等) にする。SessionStart の settings の hook と mod の `session.start` の順序は未確認

## 確かめること

- [ ] (計測とは別の、移した後の確認) 規約の節が注入されるかを、`issues/` の在る cwd / 無い cwd の両方で見る。手順は `.claude/rules/worktree-per-session.md` の
      `claude -p --model haiku --output-format stream-json ...` の形 (書き込みの道具を外した観測用。答えは stream-json の先頭から読む)
- [ ] subagent に規約が入るかを見る (今は入らない。入るなら挙動が変わるので、本文に書く)
- [ ] セッションの途中で規約ファイルを編集しても、節が 1 バイトも変わらないこと (cache を外さない)。`$.session.usage()` の `cache_read_input_tokens` で確かめる
- [ ] 「気づく手段」を、mod あり → 警告なし / mod なし → 警告あり、の両方で同じ手順で確かめた (同じマシンで、mod を一度動かした後に外した状態で)
- [ ] mod を外した状態 (読み込まれない) で同じ A-B を取り、何が起きるかを見た (上の「気づく手段」が働くか)
- [ ] `tests/claude/test_issue_rules_inject.sh` を移行後の形に合わせて直す (settings の hook を残す経路があるなら残す)

## 決着 (2026-10-02): 移さない — このマシンでは仮説の腕 (システムプロンプトの節 / CLAUDE.md の枠) を mod で作れない

> 決着したので、上の「対応方針」「失敗モード」「確かめること」の未チェックの項目は無効 (移すときの手順として残してある)

619 の実測で、managed settings のあるこのマシンでは、組み込みの `cc-plugin-sec-default` が user の tier の mod の
`prompt.compose` / `prompt.section` / `prompt.context` / `classic.SessionStart` を呼ばずに素通りさせることが分かった
(claude 2.1.287。debug log の `issue-rules-ab: prompt.compose bypassed by cc-plugin-sec-default (tier user); beneath runs`)。
腕 B (システムプロンプトの節) は作れないので、A-B は取らずに閉じる。
`prompt.submit` は mod に届き、`context` に「プロンプトの隣でモデルだけが読む block」を足せる (型定義) が、置き場所は今の A (hook の注入) と同じメッセージの側なので、
「置き場所を上げれば守られる」という仮説の腕にはならない (この腕は測っていない)。

- 確かめた手順: 腕 B / D (D = 規約を `claudeMd` の context ブロック、CLAUDE.md と同じ「指示ファイル」の枠に足す。仮説の「置き場所と名目」を直接試す腕として足した) の mod を書き、
  本物の `issue-rules-inject.sh` を 4 回呼んで A と同じ文 (14,937 字) を `session.start` で得た。`claude -p --model haiku --setting-sources project,local --plugin-dir <mod>` で起こすと、
  `session.start` は動いたが、`prompt.compose` / `prompt.context` の hook は一度も呼ばれず (入口で書く印が無い)、モデルも規約を見ていないと答えた。debug log に上の bypass の行
- `--plugin-dir` でも `CLAUDE_CODE_PLUGIN_DIRS` でも tier は `user` で同じ。`--setting-sources` を付けない普通の起動でも同じ
- 計測の前提として、`--setting-sources project,local` + `--settings` に腕ごとの hook を渡せば、本番の settings に触らずに隔離できることも確かめた
  (SessionStart の hook が走り、注入がモデルに届いた)。再開するときはこの形を使う
- **再開の trigger**: このマシンから managed settings が無くなった / `cc-plugin-sec-default` が user の tier の `prompt.*` を通すようになった
  (Claude Code の更新で変わりうる)。確かめ方: probe の mod (各イベントで印を書くだけ) を `--plugin-dir` で載せ、`--debug-file` の `bypassed by` の行が消えたかを見る。
  その時点で、上の計測計画 (A / B / C / D の腕) をそのまま使う
- 腕 B / D に使った mod (`hooks/register.ts`。manifest は `{"name":"issue-rules-ab",...}`、hooks.json は `{"modules":["./register.ts"]}`)。入口ごとに印を書く版:

  <details><summary>register.ts</summary>

  ```ts
  import type { Register, EngineInterface } from 'claude-code'

  // 620 の A-B 計測用。腕 A (SessionStart の hook) と同じ文を得るため、本物の issue-rules-inject.sh を同じ引数で呼ぶ
  const HOOK = '/Users/koji/dotfiles/_claude/hooks/issue-rules-inject.sh'
  const ARGS = [[], ['issue-rules.d/claim-issue-in-next-and-push.md'], ['issue-rules.d/issue-creation-codex-review.md'], ['issue-rules.d/move-report-conclusions-to-issues.md']]

  let loading: Promise<string> | null = null
  const load = ($: EngineInterface, cwd: string) =>
    (loading ??= (async () => {
      const parts: string[] = []
      for (const a of ARGS) {
        const r = await $.process.run([HOOK, ...a], { cwd, stdin: JSON.stringify({ cwd }) })
        let t = r.stdout
        try { t = JSON.parse(r.stdout).hookSpecificOutput.additionalContext } catch {}
        if (t.trim()) parts.push(t.trim())
      }
      return parts.join('\n\n')
    })())

  const mark = async ($: EngineInterface, name: string, text: string) => {
    const dir = await $.env.get('ISSUE_RULES_AB_DIR')
    if (dir) await $.fs.write(`${dir}/${name}`, `${text.length}\n${text.slice(0, 120)}\n`)
  }

  export const register: Register = on => {
    on('session.start', async ($, e, next) => {
      const r = await next(e)
      try {
        const t = await load($, e.cwd)
        await mark($, 'start', t)
      } catch (err) {
        await mark($, 'start-error', String(err))
      }
      return r
    })
    on('prompt.compose', async ($, e, next) => {
      const r = await next(e)
      await mark($, 'compose-called', r.sections.map(x => x.id).join(','))
      if ((await $.env.get('ISSUE_RULES_AB_ARM')) !== 'B') return r
      const text = await load($, await $.session.cwd())
      if (!text) return r
      await mark($, 'composed', text)
      return { ...r, sections: [...r.sections, { id: 'issue-rules-ab:rules', text, scope: 'session' as const }] }
    })
    on('prompt.context', async ($, e, next) => {
      const r = await next(e)
      await mark($, 'context-called', r.blocks.map(x => x.name).join(','))
      if ((await $.env.get('ISSUE_RULES_AB_ARM')) !== 'D') return r
      const text = await load($, await $.session.cwd())
      if (!text) return r
      const i = r.blocks.findIndex(b => b.name === 'claudeMd')
      if (i < 0) return r
      await mark($, 'context', text)
      const blocks = r.blocks.map((b, j) => (j === i ? { ...b, text: `${b.text}\n\n${text}` } : b))
      return { ...r, blocks }
    })
  }
  ```

  </details>

- 618 の表の 620 の行は「移さない」に直した。`_claude/CLAUDE.md` の「注入された規約に従う」の 1 行と `issue-rules-inject.sh` はそのまま

## 進捗

- 2026-10-02: 起票
- 2026-10-02: 起票と同じ日に、反証レビュー (sonnet 2 本) と敵対的レビュー (opus 2 本) の指摘で方針を改訂した。620 を「システムプロンプトの節なら守られる」の仮説を A-B で測る issue に直し (3 腕・隔離した config・CLAUDE.md の文をそろえる)、規約は session.start で 1 回読む・印は session_id ごと、を足した。採否と理由の一覧は親 618 の進捗
- 2026-10-02: 決着 — 移さない。このマシンでは user の mod の `prompt.compose` / `prompt.context` / `classic.SessionStart` が bypass され、腕 B / D を作れない (上の「決着」)。A-B は取らずに閉じる

## 進捗 (再開後)

- 2026-10-07: 再開。隔離の前提を測り直した: `CLAUDE_CONFIG_DIR` を一時 dir にすると認証が通らない (`Not logged in`) ので、この形は使えない。
  代わりに `--setting-sources ""` + 腕ごとの `--settings` で回す。`--setting-sources ""` では `~/.claude/CLAUDE.md` も読まれない (haiku に「ぼやきポイント推奨」の有無を聞いて no) ので、
  「CLAUDE.md の文を腕でそろえる」は何もしなくても満たされる (3 腕とも global の CLAUDE.md なし。本番と違う点として記録)

