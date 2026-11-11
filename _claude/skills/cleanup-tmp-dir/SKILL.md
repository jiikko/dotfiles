---
name: cleanup-tmp-dir
version: 2.3.0
description: 一時領域 (/private/tmp の Claude Code の作業場所・$TMPDIR・dotfiles の tmp/) と、Xcode の DerivedData・Apple container・使っていない git worktree に溜まったものを数え、どれを対象にするかを聞いてから、消す候補を種類ごとに見せる。ユーザーが承認した plan の古くて使用中でないもの (DerivedData は持ち主の消えたもの) だけを bin/tmpclean でゴミ箱へ移す (dotfiles の tmp/ は make clean-tmp、Apple container は container prune、worktree は bin/wt remove か git worktree remove で、それぞれ別の承認を取って消す)。「/tmp を掃除して」「一時ファイルを片付けて」「TMPDIR が溜まっていないか見て」「DerivedData を片付けて」「container を掃除して」「使っていない worktree を片付けて」「cleanup-tmp-dir」で発火。消す前に必ず承認を取る。
---

# cleanup-tmp-dir — 一時領域・DerivedData・Apple container・worktree の掃除

一時領域に件数が溜まると、起動時の削除が長引いて再起動が遅くなるとされる (`_claude/rules-rationale/global-claude-md.md` の「本文から移した実例」)。
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
| `worktree` | `~/src` と `~/dotfiles` の下の repo (言われればその dir) の使っていない git worktree。worktree 自体は根の外にもある | repo の道具で消す (`bin/wt remove` / `git worktree remove`)。数え方と分け方は下の「worktree の数え方と分け方」 |

🚨 **DerivedData を手で消さない・container の volume を一括で消さない**。DerivedData は持ち主の在るものを消すと次のビルドが全部やり直しになり、
見分けは tmpclean が持つ。container の volume は cache ではなくデータ (DB の中身など) のことがある (2026-11-03 にこの機械で
`monolink-mysql-data` を確認。`container volume prune` は container から参照されていない volume を全部消す)。

🚨 **worktree のディレクトリを `rm -rf` で消さない・`--force` を付けない**。登録が残るうえ、未コミットの変更と、どの branch / remote にも無い
commit を確かめずに消す。消すのは repo の道具に任せ、判定は下の「worktree の数え方と分け方」で行う。

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
   「container の service が止まっている」と書いて数えない (数えるために `container system start` を打たない)。
   worktree は下の「worktree の数え方と分け方」で数え、`<D>/worktrees.txt` に 1 件 1 行で残す
3. **何を対象にするかを聞く**: AskUserQuestion (`multiSelect: true`) で、手順 2 の数と容量を添えて対象を選ばせる。選択肢は
   「一時領域 (scratchpad・`$TMPDIR`)」「Xcode の DerivedData (持ち主の消えたもの)」「Apple container (停止中の container・使われていない image)」
   「使っていない git worktree (件数と容量)」と、dotfiles なら「dotfiles の `tmp/`」。無いもの (DerivedData の dir が無い・container が無い・
   service が止まっている・worktree が 0 件) は選択肢に出さない。AskUserQuestion の選択肢は 4 つまでなので、5 つになるときは worktree を 2 問目
   (「使っていない git worktree も対象にするか」。選択肢は「対象にする」「対象にしない」) に分ける。
   ここで選ばれたものだけを以降で扱う。何も選ばれない・回答が無いなら、何も消さずに数えた結果だけを報告して終える
4. **候補を作る**: 一時領域か DerivedData が選ばれていたら `tmpclean plan -kinds <種類> -out <D> > <D>/plan-out.txt 2>&1; echo rc=$?` (何も動かさない)。
   種類は一時領域なら `session,tmpdir`、DerivedData なら `derived-data` (両方なら `session,tmpdir,derived-data`)。どちらも選ばれていなければ plan は作らない。候補は 1 件 1 行で出るので
   ファイルに落とし、`id:` と `plan:` の行、`candidates.<種類>:` の件数、`kept (<理由>): <件数>` を読む。`lsof` の全体を取るので 30〜60 秒かかる。
   rc≠0 なら止まった理由が出力の `止めた:` の行にある (rc=2 = 生きている claude を見分けられない等、rc=3 = 実行環境の失敗)。理由を伝えて先へ進まない。
   ただし `止めた:` が claude の cwd・project の行 (`session` を止める判定。`src/tmpclean/README.md` の「判定不能で止める」) なら、
   止まるのは `session` だけなので、`-kinds` から `session` を外して作り直してよい。手順 5 の質問に、`session` を外した理由 (止めた claude の pid と cwd) を書く。
   worktree が選ばれていたら、下の「worktree の数え方と分け方」で、候補・別に聞くもの・実体の無い登録・残すものに分ける (tmpclean は使わない)
5. **見せて承認を取る**: AskUserQuestion で「この plan (ID と種類ごとの件数) を移してよいか」を聞く。選択肢は「移す」と「移さない」。
   AskUserQuestion は 1 回に 4 問までなので、下の別の質問と合わせて 4 問を超えるなら、worktree の問いを次の呼び出しに分ける。
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
     🚨 `container image ls` はシステムの image (container を起動する `vminit`。名前は `container system property ls` の `[vminit]`) を出さないが、
     `system df` の image の数には入り、`prune --all` はそれも消す (2026-11-05 に 1.5.0 で実測)。数が合わないときは、表に出ていない image として `vminit` を名指しする
     **volume は選択肢に出さない**。`container volume ls` の名前と `container system df` の容量を見せ、「データを持つことがあるのでこの skill では消さない。
     要らないと確かめたものは `container volume rm <名前>` で 1 つずつ消す」と書く
   - 手順 3 で worktree が選ばれていたら**別の質問**にする。🚨 こちらもゴミ箱を経由せず**消す**。候補 (と実体の無い登録) の一覧は、repo・path・
     最後に触った日・容量の表にして質問文に出し、選択肢は「全部消す」と「消さない」にする (選択肢は 4 つまでなので候補を 1 件ずつ並べない。
     一部だけ消したいときは Other に path を書いてもらう)。失うものは「ディレクトリと worktree の登録。`bin/wt` の repo では、その worktree の
     Xcode の DerivedData も。中身の commit は branch か remote に在る」と書く。残すものは理由ごとの件数を添える。
     どの branch / remote にも無い commit を持つものは**さらに別の問い**にし、commit の数と subject、origin で同じ題目が別の commit で取り込まれているかを
     添えて、「`attic/<ディレクトリ名>` の branch に残して消す」と「残す」から選ばせる。branch を作らずに消す選択肢は出さない (branch は容量を取らず、
     `bin/wt remove` は参照の無くなる commit を持つ worktree を `--discard-commits` 無しでは消さない)

   **回答が無い・取れない・「移さない」「消さない」「残す」なら、その質問の分は何もしない**。種類を絞りたいと言われたら、`-kinds` を変えて手順 4 からやり直す (plan を手で書き換えない)
6. **移す**: 承認された plan だけを `tmpclean apply -plan <plan のパス> -approve <ID> -json > <D>/apply-<ID>.json 2> <D>/apply-<ID>.err; echo rc=$? > <D>/apply-<ID>.rc`。
   候補が数百件を超えるなら Bash ツールの `run_in_background` で走らせ、完了の通知の後に `.rc` と `.json` を読む (1 件ごとに `lsof` を取り直すので
   1 件 0.3 秒ほど。通知そのものは rc を保証しない)。plan は作ってから 1 時間で使えなくなる: rc=2 で「plan が古い」と出たら手順 4 から作り直し、
   新しい ID で承認を取り直す (古い ID の承認を流用しない)。
   `repo-tmp` は別の質問で承認されたときだけ、打つ直前に `make -C <本体> clean-tmp DRY_RUN=1` を取り直して見せた一覧から増えていないかを確かめ、
   `make -C <本体> clean-tmp DAYS=30` を打つ (増えていたら、増えた分を見せて承認を取り直す)。
   Apple container は別の質問で承認された操作だけを、打つ直前に `container ls -a` と `container image ls` を取り直して、見せた一覧から
   消える対象が増えていないかを確かめてから打つ (増えていたら、増えた分を見せて承認を取り直す)。出力は `<D>/container-prune.txt` に取り、rc を見る。
   worktree は承認されたものだけを 1 件ずつ、消す直前に未コミットの変更と中で動くプロセスを取り直してから消す (増えていたら消さずに報告する)。
   `--force` と `--discard-commits` は付けない。
   `bin/wt` が使える repo は `bin/wt -C <repo> remove <path>` (repo 固有の後片付けの `.wt-teardown` と DerivedData の削除もする)、それ以外は
   `git -C <repo> worktree remove <path>` (`<repo>` は手順 2 で `worktree list` を取った repo)。「branch に残して消す」が承認されたものは、先に
   `git -C <repo> branch attic/<ディレクトリ名> <HEAD の SHA>` を作る (同じ名前の branch が既に在って作れなければ、消さずに報告する)。
   実体の無い登録は `git -C <repo> worktree prune`。rc≠0 (submodule の中身がある・途中の rebase 等) なら消さずに理由を報告する。出力は `<D>/worktree-remove-<名前>.log`
7. **報告する**: (plan を移したときは) apply の終了コードで読む。0 = 走り切った / 1 = 移せなかったものがある (JSON の `failed`) / 2 = 前提の検査で止めた (何も移していない) /
   3 = 実行環境の失敗 / 4 = 途中で止めた (一部は移した。止まった理由は JSON の `stopped`)。3 と 4 は何を移したかを `moved-<ID>.jsonl` で確かめる。
   plan を読む段で止まったとき (古い plan・ID の不一致) は `.json` が空で、理由は `apply-<ID>.err` の `止めた:` の行にある。
   移した件数 (`moved`)・残した理由ごとの件数 (`kept`)・移し先を出し、`tmpclean count` を取り直して `count-before.txt` と前後を並べる。
   何をどこへ移したかは `<D>/moved-<ID>.jsonl`。手で消すのが続くようなら、溜めている元 (テストの後始末の漏れ) を 1 行で添える。
   DerivedData を移したなら、容量はゴミ箱を空にするまで減らないと添える。Apple container を消したなら `container system df` を取り直して前後を並べる。
   worktree を消したなら、消した path が無いこと・`git -C <repo> worktree list` から消えたこと・作った `attic/` の branch を確かめ、消した件数と
   容量の合計、残した branch を出す (`attic/` の branch は push していない。要らなくなったら消すと添える)

## worktree の数え方と分け方

- **走査は `bash -c` で回す**。zsh で glob を書くと、無マッチが NOMATCH でコマンドごと落ち、0 件と区別できない
- **見つける**: 根の下の repo を列挙し (`find <根> -maxdepth 6 -name .git -type d -prune`。`.git` が dir のもの)、repo ごとに
  `git -C <repo> submodule foreach --quiet --recursive 'echo $toplevel/$sm_path'` で submodule の checkout も足す。repo と submodule ごとに
  `git -C <repo> worktree list --porcelain` を取り、**先頭の `worktree` の行 (main) 以外**を全部拾う。🚨 worktree は根の外にもある
  (`bin/wt` は `<main の親>/<repo 名>-wt-…` に作るので、dotfiles の worktree は `~/` の直下にできる)。根の下を `.git` ファイルで探す方法では見つからない。
  submodule の main は `.git/modules/<path>` の path で出るが、先頭の行なので数えない。`prunable` の行は実体の無い登録
- **登録の無い残骸**: 根の下の `.git` が**ファイル**で、`gitdir:` が `/worktrees/` を含み、その dir が無いもの (`find <根> -maxdepth 8 -name .git -type f`。
  `gitdir:` が相対 path なら `.git` ファイルの dir から解決してから見る。そのまま見ると在るものも残骸に数える)。
  登録が消えて実体だけ残った worktree で、git の道具では消せない。「残す」に入れて path と容量を報告する (消すかはユーザーが決める)
- **1 件ごとに数える** (読み取りのみ)。`<D>/worktrees.txt` に 1 件 1 行で、repo・path・DIRTY・UNIQUE・途中の操作・lock・最後に触った時刻・
  中で動くプロセス・容量・区分 (と、残すなら理由) を残す。`<repo>` は `worktree list` を取った repo (submodule ならその checkout) で、手順 6 でも使う:
  - 未コミットの変更 (DIRTY): `git -C <wt> status --porcelain | wc -l`
  - 消すと参照が無くなる commit (UNIQUE): `git -C <wt> rev-list --count HEAD --not --branches --remotes`。branch で作った worktree は、その branch が残るので 0
  - 途中の操作: `git -C <wt> rev-parse --git-dir` の下の `rebase-merge`・`rebase-apply`・`MERGE_HEAD`・`CHERRY_PICK_HEAD`・`REVERT_HEAD`・`BISECT_LOG`
  - lock: `worktree list --porcelain` の `locked` の行
  - 最後に触った時刻: `<git-dir>/index` の mtime。比べるのは epoch (`stat -f '%m'`) で、見せるのは `stat -f '%Sm'`
  - 中で動くプロセス: `lsof -d cwd -Fpcn` を 1 回取り、`n` の行が worktree の path と一致するか、path + `/` で始まるもの (前方一致だけだと、名前が続く
    隣の worktree を巻き込む)。見えるのは自分のユーザーのプロセスだけ
  - 容量: `du -sh <wt>`
  - UNIQUE が 0 でないものは、HEAD が origin の既定の branch に入っているか (`merge-base --is-ancestor HEAD origin/HEAD`) と、patch が同じ commit が
    origin に在るか (`git -C <wt> cherry -v origin/HEAD HEAD`。`-` が同じ patch) も見る。衝突を解いて取り込み直した commit は `+` になるので、
    issue 番号で `git log origin/HEAD --grep` も引く。`origin/HEAD` が無い checkout では `origin/master` か `origin/main` を名指しする
    (`git remote set-head` で作らない。repo の設定を書き換える)
  - repo か superproject の直下に `bin/wt` が在れば `bin/wt -C <repo> list` が同じ DIRTY・UNIQUE を出す (my-products の umbrella の道具)
- **分ける**:
  - 候補: DIRTY 0・UNIQUE 0・途中の操作なし・lock なし・中で動くプロセスなし・最後に触ってから 7 日以上
  - 別に聞く: UNIQUE が 0 でなく、ほかは候補の条件を満たすもの
  - 実体の無い登録: `git -C <repo> worktree prune --dry-run` に出るもの。消えるのは登録だけ
  - 残す (理由を見せる): DIRTY が 0 でない (消すには `--force` が要り、変更が消える)・途中の操作・lock・中で動くプロセス・7 日未満・
    登録の無い残骸・判定できない (コマンドが失敗した・index が無い等。理由に失敗したコマンドを書く)
- Claude のアプリが作った worktree (`<repo>/.claude/worktrees/<名前>`。branch は `claude/<名前>`) も同じ判定で扱う。消しても branch は残り、
  セッションを再開するとアプリが取り出し直す。30 日以上使っていないセッションのものは、アプリの「Clean up inactive sessions」でも消せる
