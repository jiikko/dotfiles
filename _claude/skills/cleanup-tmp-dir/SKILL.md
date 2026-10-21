---
name: cleanup-tmp-dir
version: 2.0.0
description: 一時領域 (/private/tmp の Claude Code の作業場所・$TMPDIR・dotfiles の tmp/) に溜まったものを bin/tmpclean で数え、消す候補を種類ごとに見せて、ユーザーが承認した plan の古くて使用中でないものだけをゴミ箱へ移す (dotfiles の tmp/ は別の承認を取って make clean-tmp で消す)。「/tmp を掃除して」「一時ファイルを片付けて」「TMPDIR が溜まっていないか見て」「cleanup-tmp-dir」で発火。消す前に必ず承認を取る。
---

# cleanup-tmp-dir — 一時領域の掃除

一時領域に件数が溜まると、起動時の削除が長引いて再起動が遅くなるとされる (`_claude/CLAUDE.md`「一時ファイルの配置」)。
判定と移動は **`bin/tmpclean` だけが行う** (本体と守りの一覧・検出しないものは `src/tmpclean/README.md`)。

🚨 **一時領域を自分で `rm` / `mv` / `find -delete` しない**。生きているセッションの scratchpad を消すとそのセッションが壊れ、
見分けは tmpclean が層を重ねて行う。浅い場所を直接消す形は PreToolUse の hook (`_claude/hooks/deny-tmp-area-rm.sh`) が止める。

| 種類 | 場所 | 扱い |
|---|---|---|
| `session` | Claude Code の scratchpad (`/private/tmp/claude-<uid>/<project>/<session-id>/`) | tmpclean。既定は 7 日より古いもの |
| `tmpdir` | `$TMPDIR` の直下 | tmpclean。既定は 3 日より古いもの |
| `repo-tmp` | dotfiles の本体 checkout の `tmp/` | `make clean-tmp` (既定 30 日)。dotfiles 以外の repo の `tmp/` は容量を見せるだけ |

`$TMPDIR` の直下の空の `TemporaryDirectory.*` (Swift のツールチェーンが残す。issue 851) は、この skill を通さずに SessionStart hook
(`_claude/hooks/tmpdir-swift-sweep.sh`) が 1 日 1 回 `tmpclean sweep` で消す。手順 2 の数で `TemporaryDirectory.` が多いときは、
先に `~/.cache/dotfiles-tmpdir-sweep/sweep.log` で sweep が走っているか (`rc=` と `removed=`) と、`last.json` に `limit` があれば
それ (消すのを止めている一番古い Swift のツール) を見せる。

## 手順

1. **置き場を決める**: `skill-tmpdir cleanup-tmp-dir` が返す絶対パスを `<D>` とし、以降はリテラルで書く
2. **数える**: `tmpclean count > <D>/count-before.txt 2>&1` (読み取りのみ。手順 6 で前後を並べるので残す)。あわせて `du -sh <本体>/tmp`
   (`<本体>` は `<D>` の 3 つ上の dir)。dotfiles (`<本体>/scripts/clean_tmp.sh` が在る) なら `make -C <本体> clean-tmp DRY_RUN=1` も取る。
   それ以外の repo では `make clean-tmp` を打たず、容量を見せるだけにする (同名の target が `DRY_RUN` を解釈するとは限らない)
3. **候補を作る**: `tmpclean plan -kinds session,tmpdir -out <D> > <D>/plan-out.txt 2>&1; echo rc=$?` (何も動かさない)。候補は 1 件 1 行で出るので
   ファイルに落とし、`id:` と `plan:` の行、`candidates.<種類>:` の件数、`kept (<理由>): <件数>` を読む。`lsof` の全体を取るので 30〜60 秒かかる。
   rc≠0 なら止まった理由が出力の `止めた:` の行にある (rc=2 = 生きている claude を見分けられない等、rc=3 = 実行環境の失敗)。理由を伝えて先へ進まない
4. **見せて承認を取る**: AskUserQuestion で「この plan (ID と種類ごとの件数) を移してよいか」を聞く。選択肢は「移す」と「移さない」。
   「移す」の説明に **ID・種類ごとの件数・失うもの・移し先** を書く:
   - 失うもの: 終わったセッションの scratchpad (`--resume` で再開したときの作業ファイル)、`$TMPDIR` の古いテストの残骸とアプリの一時ファイル
   - 移し先: `~/.Trash/tmpclean-<日時>-<ID>/` (Finder から戻せる。容量はゴミ箱を空にするまで減らない)
   - 所要: 件数 × 0.3 秒 (1 件ごとに `lsof` を取り直す)。🚨 数万件なら数時間かかる (12 万件で約 8 時間。2026-10-21 実測)。
     `$TMPDIR` と scratchpad だけなら、再起動 (起動時に macOS が消す) の方が早いと添える
   - `repo-tmp` も扱うなら**別の質問**にする。🚨 こちらはゴミ箱を経由せず**消す** (戻せない) と説明に書く。`make clean-tmp DRY_RUN=1` の件数と、
     `grep -rn 'tmp/' <本体>/issues <本体>/_claude` で issue や doc が指しているパスを見せる。plan の ID に結び付かないので、承認の直後に打つ

   **回答が無い・取れない・「移さない」なら、何も移さずに終える**。種類を絞りたいと言われたら、`-kinds` を変えて手順 3 からやり直す (plan を手で書き換えない)
5. **移す**: 承認された plan だけを `tmpclean apply -plan <plan のパス> -approve <ID> -json > <D>/apply-<ID>.json 2> <D>/apply-<ID>.err; echo rc=$? > <D>/apply-<ID>.rc`。
   候補が数百件を超えるなら Bash ツールの `run_in_background` で走らせ、完了の通知の後に `.rc` と `.json` を読む (1 件ごとに `lsof` を取り直すので
   1 件 0.3 秒ほど。通知そのものは rc を保証しない)。plan は作ってから 1 時間で使えなくなる: rc=2 で「plan が古い」と出たら手順 3 から作り直し、
   新しい ID で承認を取り直す (古い ID の承認を流用しない)。
   `repo-tmp` は別の質問で承認されたときだけ、打つ直前に `make -C <本体> clean-tmp DRY_RUN=1` を取り直して見せた一覧から増えていないかを確かめ、
   `make -C <本体> clean-tmp DAYS=30` を打つ (増えていたら、増えた分を見せて承認を取り直す)
6. **報告する**: apply の終了コードで読む。0 = 走り切った / 1 = 移せなかったものがある (JSON の `failed`) / 2 = 前提の検査で止めた (何も移していない) /
   3 = 実行環境の失敗 / 4 = 途中で止めた (一部は移した。止まった理由は JSON の `stopped`)。3 と 4 は何を移したかを `moved-<ID>.jsonl` で確かめる。
   plan を読む段で止まったとき (古い plan・ID の不一致) は `.json` が空で、理由は `apply-<ID>.err` の `止めた:` の行にある。
   移した件数 (`moved`)・残した理由ごとの件数 (`kept`)・移し先を出し、`tmpclean count` を取り直して `count-before.txt` と前後を並べる。
   何をどこへ移したかは `<D>/moved-<ID>.jsonl`。手で消すのが続くようなら、溜めている元 (テストの後始末の漏れ) を 1 行で添える
