# 834 (bug): コミット本文で git log の区切りを偽装すると、偽の行で d を押したとき git show --output= で任意のファイルが上書きされる (P1)

> 🚨 **担当中: air5 (glogx)**（2026-10-19〜）

起票日: 2026-10-19

## 概要

`src/glogx/internal/gitdata/gitlog.go` の `parseLog` は、git log の出力をレコード区切り `\x1e` とフィールド区切り `\x1f` で切る。
コミット本文 (%B) は作者が自由に書けるので、本文にこの 2 文字を入れると、本物のレコードの途中から**偽のコミット行**を作れる。
SHA の欄は検査しないので、偽の SHA に `--output=<path>` と書ける。
その行で `d` を押すと `LoadCommitDiff` が `git show --stat --patch ... <sha>` を `--end-of-options` 無しで起動し、
git がこれをオプションとして読んで、show の出力を `<path>` に書く (上書き)。

## 詳細

- 経路: `tui.go` の `openDiff` → `fetchDiffCmd` → `gitdata.LoadCommitDiff(sha)` → `extcmd.RunGitTimeout("show", ..., sha)`
- 偽装の形: 本文に `\x1f\x1e<偽SHA>\x1f<h>\x1f<subject>\x1f…` (フィールド 9 個分) を書く。本物のレコードはフィールド数を満たしたまま途中で切れ、残りが別のコミットとして解析される。件名・作者も攻撃者が決める
- 書かれる中身は HEAD の `git show` の出力 (メッセージと diff)。HEAD が攻撃者のコミットなら中身も攻撃者が決められる。相対パス (`../../.zshrc`) も通る。rc ファイルを上書きしたとき実行に繋がるかは**未実測**
- 同じ根の副作用: 本文 (`-p` のときはファイル内容の `0x1E` でも) に `\x1e` があるだけで、フィールド数が合わず `parseLog` がエラーを返し、glogx が起動できない。コードのコメントは「病的ケース」としているが、入力を攻撃者が決められる前提では起動不能 (DoS) になる
- 発火条件: 攻撃者のコミット (`git commit --cleanup=verbatim` などで制御文字を残せる) が表示範囲にある (PR ブランチの checkout・他人が push した repo の pull)。**TUI が起動する** (`main.go` の `runLog` は、取得が要らず一画面に収まると静的表示で終わる)。ユーザーがその偽の行に合わせて `d` を押す
- 偽の行は既定の表示でも選べる: `render.go` の `VerbatimLines` はコミット数が合わないと nil を返し、`RenderLines` が `mediumLines` へ戻って偽のレコードにも選べるヘッダを作る。`openDiff` は `m.commits[m.cursor].SHA` を使う。`--oneline` も全レコードを行にする (codex の反証レビューで確認)
- 再現 (監査の中で実施): scratchpad に細工したコミットを持つ repo を作り、`go test -overlay` で本物の `LoadCommits(LogQuery{MaxCount: 10}, false)` を通すと 3 行が返り、2 行目が `SHA="--output=<scratchpad>/pwned.txt" Subject="Forged subject" Author="Mallory"`。続けて `LoadCommitDiff(その SHA, false)` は err=nil を返し、`pwned.txt` (502 バイト) ができた
- glogx の中で git の argv に SHA を入れるのは `LoadCommitDiff` だけ (`"show"` / `"log"` に SHA を渡す呼び出しを grep。GraphQL 側は `%q` で引用)。glogx の外 (pro-con など同じ区切りで git log を読むもの) は**未確認**

## 対応方針

1. `LoadCommitDiff` を `git show ... --end-of-options <sha>` にする (argv に外部由来の値を置く他の git 呼び出しも同じ形に揃える)。オプション注入はこれで止まる
2. **レコード境界そのものを直す** (作者が書けない形で区切る。例: 本文を含まない `%H` だけの一覧を別に取り、件数と SHA で照合する / NUL 区切り)。hex の検査は**補助にしかならない**: 実在する SHA を書いた偽のレコードは hex の検査を通る (codex の反証レビューの指摘)。「偽の行が出ない」はこの 2 でしか満たせない
3. 本文の中の `\x1e` で起動不能にしない (2 で同時に解ける)
4. 回帰テスト: 偽装した本文 (偽 SHA に `--output=` と実在の SHA の 2 通り) を持つ repo で、偽の行が出ないこと・`d` でファイルが作られないこと・起動できること

## 関連ファイル

- `src/glogx/internal/gitdata/gitlog.go` (`parseLog` / `LoadCommitDiff`)
- `src/glogx/internal/extcmd/git.go` (`RunGitTimeout`)
- `src/glogx/tui.go` (`openDiff`)

## 進捗

- [x] 1〜4 (2026-10-19。commit「glogx: 全体監査 (844) の issue 834〜843 を直す」)
  - 1: `LoadCommitDiff` は SHA の形 (完全な 16 進) を先に見てから `git show ... --end-of-options <sha>` (`showCommit`)。2 段のどちらかが外れても止まる
  - 2・3: レコードと欄の区切りを起動ごとの乱数の nonce 入り (`\x1e<nonce>\x1e` / `\x1f<nonce>\x1f`。`newLogFormat`) にし、作者が本文に書いた区切りでは切れない。SHA の欄は 16 進で検査する。VS16 の正規化は分割の後に欄ごと
  - 4: 回帰テスト `TestIntegrationForgedRecordInMessage` (偽の SHA に `--output=` と実在の SHA の 2 通り) ほか `TestParseLogIgnoresSeparatorsWrittenByAuthor` / `TestParseLogRejectsNonHexSHA` / `TestParseLogNormalizesVS16AfterSplitting`
- 変異: 6 本 (16 進の検査・`--end-of-options`・固定の区切り・分割前の VS16 の正規化など) がすべて red
- レビュー: codex の実装レビュー 3 本 (指摘は反映済み)
