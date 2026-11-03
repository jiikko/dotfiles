---
name: cleanup-tmp-dir
version: 2.1.0
description: 一時領域 (/private/tmp の Claude Code の作業場所・$TMPDIR・dotfiles の tmp/) と、Xcode の DerivedData・Apple container に溜まったものを数え、どれを対象にするかを聞いてから、消す候補を種類ごとに見せる。ユーザーが承認した plan の古くて使用中でないもの (DerivedData は持ち主の消えたもの) だけを bin/tmpclean でゴミ箱へ移す (dotfiles の tmp/ は make clean-tmp、Apple container は container prune で、それぞれ別の承認を取って消す)。「/tmp を掃除して」「一時ファイルを片付けて」「TMPDIR が溜まっていないか見て」「DerivedData を片付けて」「container を掃除して」「cleanup-tmp-dir」で発火。消す前に必ず承認を取る。
---

# cleanup-tmp-dir — 一時領域・DerivedData・Apple container の掃除

一時領域に件数が溜まると、起動時の削除が長引いて再起動が遅くなるとされる (`_claude/CLAUDE.md`「一時ファイルの配置」)。
判定と移動は **`bin/tmpclean` だけが行う** (本体と守りの一覧・検出しないものは `src/tmpclean/README.md`)。

🚨 **一時領域を自分で `rm` / `mv` / `find -delete` しない**。生きているセッションの scratchpad を消すとそのセッションが壊れ、
見分けは tmpclean が層を重ねて行う。浅い場所を直接消す形は PreToolUse の hook (`_claude/hooks/deny-tmp-area-rm.sh`) が止める。

| 種類 | 場所 | 扱い |
|---|---|---|
| `session` | Claude Code の scratchpad (`/private/tmp/claude-<uid>/<project>/<session-id>/`) | tmpclean。既定は 7 日より古いもの |
| `tmpdir` | `$TMPDIR` の直下 | tmpclean。既定は 3 日より古いもの |
| `repo-tmp` | dotfiles の本体 checkout の `tmp/` | `make clean-tmp` (既定 30 日)。dotfiles 以外の repo の `tmp/` は容量を見せるだけ |
| `derived-data` | Xcode の DerivedData (`~/Library/Developer/Xcode/DerivedData/<名前>-<hash>/`) | tmpclean。持ち主 (info.plist の WorkspacePath) が消えたものだけ。古さでは選ばない (判定は `src/tmpclean/README.md`) |
| `container` | Apple container (`container` CLI) の停止中の container と使われていない image | `container prune` / `container image prune`。**volume は消さない** (手順 5) |

🚨 **DerivedData を手で消さない・container の volume を一括で消さない**。DerivedData は持ち主の在るものを消すと次のビルドが全部やり直しになり、
見分けは tmpclean が持つ。container の volume は cache ではなくデータ (DB の中身など) のことがある (2026-11-03 にこの機械で
`monolink-mysql-data` を確認。`container volume prune` は container から参照されていない volume を全部消す)。

`$TMPDIR` の直下の空の `TemporaryDirectory.*` (Swift のツールチェーンが残す。issue 851) は、この skill を通さずに SessionStart hook
(`_claude/hooks/tmpdir-swift-sweep.sh`) が 1 日 1 回 `tmpclean sweep` で消す。手順 2 の数で `TemporaryDirectory.` が多いときは、
先に `~/.cache/dotfiles-tmpdir-sweep/sweep.log` で sweep が走っているか (`rc=` と `removed=`) と、`last.json` に `limit` があれば
それ (消すのを止めている一番古い Swift のツール) を見せる。

## 手順

1. **置き場を決める**: `skill-tmpdir cleanup-tmp-dir` が返す絶対パスを `<D>` とし、以降はリテラルで書く
2. **数える**: `tmpclean count > <D>/count-before.txt 2>&1` (読み取りのみ。手順 7 で前後を並べるので残す)。あわせて `du -sh <本体>/tmp`
   (`<本体>` は `<D>` の 3 つ上の dir)。dotfiles (`<本体>/scripts/clean_tmp.sh` が在る) なら `make -C <本体> clean-tmp DRY_RUN=1` も取る。
   それ以外の repo では `make clean-tmp` を打たず、容量を見せるだけにする (同名の target が `DRY_RUN` を解釈するとは限らない)。
   DerivedData は `du -sh ~/Library/Developer/Xcode/DerivedData` (`tmpclean count` は数えない。dir が無ければ「無い」と書く)。
   Apple container は `command -v container` が在るときだけ、`container system status` が running なら `container system df`・`container ls -a`・
   `container image ls`・`container volume ls` を `<D>/container-before.txt` に取る (どれも読み取りのみ)。running でなければ
   「container の service が止まっている」と書いて数えない (数えるために `container system start` を打たない)
3. **何を対象にするかを聞く**: AskUserQuestion (`multiSelect: true`) で、手順 2 の数と容量を添えて対象を選ばせる。選択肢は
   「一時領域 (scratchpad・`$TMPDIR`)」「Xcode の DerivedData (持ち主の消えたもの)」「Apple container (停止中の container・使われていない image)」と、
   dotfiles なら「dotfiles の `tmp/`」。無いもの (DerivedData の dir が無い・container が無い・service が止まっている) は選択肢に出さない。
   ここで選ばれたものだけを以降で扱う。何も選ばれない・回答が無いなら、何も消さずに数えた結果だけを報告して終える
4. **候補を作る**: 一時領域か DerivedData が選ばれていたら `tmpclean plan -kinds <種類> -out <D> > <D>/plan-out.txt 2>&1; echo rc=$?` (何も動かさない)。
   種類は一時領域なら `session,tmpdir`、DerivedData なら `derived-data` (両方なら `session,tmpdir,derived-data`)。どちらも選ばれていなければ plan は作らない。候補は 1 件 1 行で出るので
   ファイルに落とし、`id:` と `plan:` の行、`candidates.<種類>:` の件数、`kept (<理由>): <件数>` を読む。`lsof` の全体を取るので 30〜60 秒かかる。
   rc≠0 なら止まった理由が出力の `止めた:` の行にある (rc=2 = 生きている claude を見分けられない等、rc=3 = 実行環境の失敗)。理由を伝えて先へ進まない
5. **見せて承認を取る**: AskUserQuestion で「この plan (ID と種類ごとの件数) を移してよいか」を聞く。選択肢は「移す」と「移さない」。
   plan を作っていない (手順 3 で一時領域も DerivedData も選ばれていない) なら、この質問と手順 6 の apply は飛ばし、下の別の質問だけを聞く。
   「移す」の説明に **ID・種類ごとの件数・失うもの・移し先** を書く:
   - 失うもの: 終わったセッションの scratchpad (`--resume` で再開したときの作業ファイル)、`$TMPDIR` の古いテストの残骸とアプリの一時ファイル。
     `derived-data` を含むなら、消えた worktree・checkout のビルド成果物と索引 (持ち主が無いので、それを使うビルドはもう無い)。
     候補の dir 名と `du -sh` の合計を添える (DerivedData は 1 つ数 GB になる)
   - 移し先: `~/.Trash/tmpclean-<日時>-<ID>/` (Finder から戻せる。容量はゴミ箱を空にするまで減らない)
   - 所要: 件数 × 0.3 秒 (1 件ごとに `lsof` を取り直す)。🚨 数万件なら数時間かかる (12 万件で約 8 時間。2026-10-21 実測)。
     `$TMPDIR` と scratchpad だけなら、再起動 (起動時に macOS が消す) の方が早いと添える
   - 手順 3 で dotfiles の `tmp/` (`repo-tmp`) が選ばれていたら**別の質問**にする。🚨 こちらはゴミ箱を経由せず**消す** (戻せない) と説明に書く。`make clean-tmp DRY_RUN=1` の件数と、
     `grep -rn 'tmp/' <本体>/issues <本体>/_claude` で issue や doc が指しているパスを見せる。plan の ID に結び付かないので、承認の直後に打つ
   - 手順 3 で Apple container が選ばれていたら**別の質問**にする。🚨 こちらもゴミ箱を経由せず**消す** (戻せない)。`<D>/container-before.txt` から
     停止中の container の名前と、使われていない image の名前・容量を見せ、選択肢を分ける (`multiSelect: true`):
     「停止中の container を消す (`container prune`。volume に置いていない、container の中に書いたものも消える)」「タグの無い image を消す (`container image prune`)」
     「使われていない image を全部消す (`container image prune --all`。次に使うときに pull し直す。手元でビルドした image は build し直す)」。動いている container は対象にならない。
     **volume は選択肢に出さない**。`container volume ls` の名前と `container system df` の容量を見せ、「データを持つことがあるのでこの skill では消さない。
     要らないと確かめたものは `container volume rm <名前>` で 1 つずつ消す」と書く

   **回答が無い・取れない・「移さない」なら、何も移さずに終える**。種類を絞りたいと言われたら、`-kinds` を変えて手順 4 からやり直す (plan を手で書き換えない)
6. **移す**: 承認された plan だけを `tmpclean apply -plan <plan のパス> -approve <ID> -json > <D>/apply-<ID>.json 2> <D>/apply-<ID>.err; echo rc=$? > <D>/apply-<ID>.rc`。
   候補が数百件を超えるなら Bash ツールの `run_in_background` で走らせ、完了の通知の後に `.rc` と `.json` を読む (1 件ごとに `lsof` を取り直すので
   1 件 0.3 秒ほど。通知そのものは rc を保証しない)。plan は作ってから 1 時間で使えなくなる: rc=2 で「plan が古い」と出たら手順 4 から作り直し、
   新しい ID で承認を取り直す (古い ID の承認を流用しない)。
   `repo-tmp` は別の質問で承認されたときだけ、打つ直前に `make -C <本体> clean-tmp DRY_RUN=1` を取り直して見せた一覧から増えていないかを確かめ、
   `make -C <本体> clean-tmp DAYS=30` を打つ (増えていたら、増えた分を見せて承認を取り直す)。
   Apple container は別の質問で承認された操作だけを、打つ直前に `container ls -a` と `container image ls` を取り直して、見せた一覧から
   消える対象が増えていないかを確かめてから打つ (増えていたら、増えた分を見せて承認を取り直す)。出力は `<D>/container-prune.txt` に取り、rc を見る
7. **報告する**: (plan を移したときは) apply の終了コードで読む。0 = 走り切った / 1 = 移せなかったものがある (JSON の `failed`) / 2 = 前提の検査で止めた (何も移していない) /
   3 = 実行環境の失敗 / 4 = 途中で止めた (一部は移した。止まった理由は JSON の `stopped`)。3 と 4 は何を移したかを `moved-<ID>.jsonl` で確かめる。
   plan を読む段で止まったとき (古い plan・ID の不一致) は `.json` が空で、理由は `apply-<ID>.err` の `止めた:` の行にある。
   移した件数 (`moved`)・残した理由ごとの件数 (`kept`)・移し先を出し、`tmpclean count` を取り直して `count-before.txt` と前後を並べる。
   何をどこへ移したかは `<D>/moved-<ID>.jsonl`。手で消すのが続くようなら、溜めている元 (テストの後始末の漏れ) を 1 行で添える。
   DerivedData を移したなら、容量はゴミ箱を空にするまで減らないと添える。Apple container を消したなら `container system df` を取り直して前後を並べる
