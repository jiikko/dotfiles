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
