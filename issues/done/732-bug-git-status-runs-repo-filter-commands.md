# 732 (bug): git status が開いた repo の `.git/config` の filter (clean) のコマンドを実行する

起票日: 2026-10-12

## 概要

treefiler (と glogx・pro-con) は `subproc.GitCommand` (`gitHarden` = `-c core.fsmonitor=false --no-optional-locks`) で git を起こす。issue 692 で fsmonitor は
塞いだが、**同じ脅威モデルの残りとして `filter.<name>.clean` (と `smudge` / `process`) が `git status` で実行される**。

## 詳細 (再現)

実測 2026-10-12 (git 2.55.0 / macOS。main が再現):

```sh
git init r && cd r && echo a > f && git add f && git commit -m i
git config filter.x.clean "touch <外>/PWNED; cat" && echo '* filter=x' > .gitattributes
echo b > f && touch -t 202001010000 f        # stat の古いファイルを 1 つ作る
git -c core.fsmonitor=false --no-optional-locks status --porcelain=v1 -z --branch --ignored
# → <外>/PWNED ができる
```

- 発火: `.git` ごと渡された repo (tarball・zip・コピー) を treefiler / glogx の F で開いたとき (treefiler は開いた直後と 3 秒ごとに status を取る)。
  `git clone` では `.git/config` が届かないので、clone した repo では起きない
- `filer/git.go` の `fetchRepo`・`filer/diff.go` の diff も同じ経路。glogx 本体・pro-con の git 呼び出しも `subproc` を通るので同じ
- 期待: 他人の repo を開くだけで、その repo の設定のコマンドが走らない (692 と同じ不変条件)

## 対応方針 (案)

- status / diff を「中身を読むだけ」にする: `-c filter.<name>.clean=` は名前を列挙できないので、attribute を無効にする方向 (`GIT_ATTR_SOURCE` / `core.attributesFile`・
  `--no-textconv` は status に無い)。git のどの設定で filter を止められるかを先に実測する (未確定)
- 止められないなら「所有者が自分の repo だけ status を取る」・「信頼する repo だけ」の割り切りを 692 の記録と合わせて決める (ユーザーの判断)
- 直したら 692 の `TestGitDoesNotRunConfigCommands` 相当に filter の場合を足す

## 関連

- issue 692 (fsmonitor を塞いだ)・737 (この監査)・`src/subproc/subproc.go` の `gitHarden`

## 進捗

- 2026-10-12: 起票 (監査 737)
- 2026-10-12 (5f): 対応した。ユーザーの判断で、ローカルの filter を使う repo の表示の誤りは割り切る
  - 実測 (git 2.55.0): `-c filter.<名前>.clean=` / `process=` / `smudge=` で filter のコマンドは走らない。required=true なら `required=false` も要る
    (無いと status が「clean filter failed」で rc=128)。🚨 `process=` は空でも「設定済み」と読まれ、global の clean まで呼ばれなくなる
    (ローカルで定義された鍵だけを上書きする理由)。attributes の側は `attr.tree` / `GIT_ATTR_SOURCE` で作業ツリーの .gitattributes は止まるが
    `.git/info/attributes` は止まらない (tarball では届くので不可)。include したファイルの鍵は scope が local と報告される
  - 実装: `subproc.GitRepoArgs` / `GitRepoCommand` (src/subproc/gitfilter.go)。`git config --show-scope -z --get-regexp '^filter\.'` で列挙し
    (+6ms。status は 28ms)、scope が system / global / command 以外で定義された filter.<名前>.{clean,smudge,process} を -c で上書きする
    (信頼できる scope に同じ鍵があればその値。git-lfs は global に置くので動く)。名前に = があると -c で上書きできないので err (表示が出ないだけ)
  - 当てた所 (表示のために作業ツリーを読む呼び出しだけ): treefiler の fetchRepo (status)・gitDiffCommand (diff)、glogx の loadWorktreeStatus・
    loadWorktreeDiff (`runGitReadTimeout`)。4 か所とも `--ignore-submodules=dirty` も付けた (submodule の中は子の git が submodule 自身の設定を読み、
    上書きが届かない)。当てないもの: add / commit (lfs --local の repo で本体を書き込む)・pro-con (自分が登録した repo だけを触る。692 の merge driver と同じ)
  - テスト: subproc (clean / required / process / include / 空の名前 / 点を含む名前・global の filter が残る・= の名前で err・filter が無い repo)、
    treefiler と glogx (clean / process・submodule)。どれも対照として素の起動で罠が走ることを先に見る。fixture は mtime を過去に打つ
    (index と同じ秒だと racy の扱いでたまにしか filter を通らない)
  - 変異 13 本が red: 呼び出し 4 か所を GitArgs に戻す・treefiler の status を cwd の repo で引く・required=false を出さない・定義されていない鍵まで空にする・
    = の検査を外す・local を信頼する・空の名前を捨てる・`--ignore-submodules=dirty` を 4 か所で 1 本ずつ外す
  - 敵対的レビュー (opus) 1 周目: 採用 — 空の名前の filter が漏れる (P1)・submodule の中の filter (P1)・diff の結果を見ていないテスト (P3)。
    別 issue — partial clone の遅延 fetch で core.sshCommand が走る (739 に起票。main が status で再現)。割り切り (ユーザー判断) — ローカルの filter の repo で
    偽の変更が出る (P2)。記録のみ — = を含む名前は err で表示が出ない (コマンドは走らない側。`GIT_CONFIG_COUNT` なら上書きできるが親から継いだ COUNT との合成が要る)
  - 2 周目 (opus): P1 / P2 無し。記録のみ (表示の変化) — 中身だけ変わった submodule に印が出なくなった (commit の変化 = gitlink の M は従来どおり出る)・
    `submodule.<名前>.ignore=all` の submodule で commit の変化が出るようになった (CLI の指定が設定に勝つ。安全側)。2 周目の修正は無いので打ち切り
  - 4 module (subproc・treefiler・glogx) の lint と test・root の `make test-lint` が緑
