# 620 (feat): issue 規約の注入を、mod でシステムプロンプトの節へ上げる (守られる率が上がるかを先に測る)

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

- **腕**: A = 今の SessionStart の注入 / B = mod の節 (settings の hook は外す)。同じ規約の文面、同じモデル (ふだん使う main のモデル。haiku で代用しない)
- **課題**: 隔離した一時 repo (`issues/` だけを持つ。remote は無いか、ローカルの bare。本物の dotfiles に push できない形) で、規約に触れる作業を headless で頼む
  (例: 「この不具合を issue に起票して」「この issue を完了にして」)。頼む文には規約の中身を書かない
- **測るもの**: 機械で判定できる規約の項目だけ。ファイル名の形 (`NNN-<type>-<slug>.md`)・`起票日:` の行・human の `期限:` の書式・採番した後すぐ commit したか・
  done へ移す前に目印を消したか、など。項目ごとに守った / 破ったを数える。判定は生成物と git の状態から読み、モデルの返答の文面からは読まない
- **回数と打ち切り**: 腕ごとの回数を始める前に決めて本文に書く。本物のセッションを起こすので、先に `ratelimit` で 5h 枠を見る。1 回の token を測ってから総数を決める
- **名目の確認**: B で、節がどういう形でモデルに届いているかを `/context` か debug log で見て、本文に書く
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

- [ ] headless の新規セッションで A-B を取る (`issues/` の在る cwd / 無い cwd)。手順は `.claude/rules/worktree-per-session.md` の
      `claude -p --model haiku --output-format stream-json ...` の形。答えは stream-json の先頭から読む (Stop hook が最後の返答を差し替えるため)
- [ ] subagent に規約が入るかを見る (今は入らない。入るなら挙動が変わるので、本文に書く)
- [ ] セッションの途中で規約ファイルを編集しても、節が 1 バイトも変わらないこと (cache を外さない)。`$.session.usage()` の `cache_read_input_tokens` で確かめる
- [ ] 「気づく手段」を、mod あり → 警告なし / mod なし → 警告あり、の両方で同じ手順で確かめた (同じマシンで、mod を一度動かした後に外した状態で)
- [ ] mod を外した状態 (読み込まれない) で同じ A-B を取り、何が起きるかを見た (上の「気づく手段」が働くか)
- [ ] `tests/claude/test_issue_rules_inject.sh` を移行後の形に合わせて直す (settings の hook を残す経路があるなら残す)

## 進捗

- 2026-10-02: 起票
