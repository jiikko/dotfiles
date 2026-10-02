# 619 (feat): mods の土台 — 置き場所・読み込みの配線・テスト・入口の文書

> 🚨 **担当中: Claude code mods migration design (epic 618 を順に)**（2026-10-02〜）

起票日: 2026-10-02

epic [618](618-design-claude-code-mods-migration.md) の子。620〜624 の前提。

## 概要

mod を dotfiles で持つための共通部分を作る。中身のある mod は 620 以降で足す。

## 対応方針

1. **置き場所**: `_claude/mods/<name>/` (plugin 1 つ = ディレクトリ 1 つ。`.claude-plugin/plugin.json` / `hooks/hooks.json` / `hooks/register.ts(x)`)
2. **読み込み**: `_claude/settings.json` の `env` に `CLAUDE_CODE_PLUGIN_DIRS` を書き、`~/dotfiles/_claude/mods/<name>` を並べる。
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

- [ ] `_claude/mods/` に何もしない mod が 1 つあり、`make test` で validate と test が走ったことを出力で確かめた
- [ ] 対話 / `claude -p` / PG の 3 経路で、`env -u CLAUDE_CODE_PLUGIN_DIRS` の有無ごとに、読まれた・読まれなかったの実測を本文に書いた
- [ ] PG に mod を載せるか外すかを決め、dispatcher の起動の環境をテストで固定した
- [ ] mod を読ませた後、`git status` に `.claude-plugin/types/` が出ないことを確かめた
- [ ] 入口の文書と索引を足した
- [ ] CI での扱い (必須 / 対象外の明示) を決めて本文に書いた
- [ ] 3 経路の結果を 618 の表と 620〜623 の着手条件に反映した

## 進捗

- 2026-10-02: 起票
