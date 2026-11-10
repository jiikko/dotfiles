# 1050 (feat): 長い検証を Bash ツール 1 回の中の素の `&` で背景に回す形を hook で止める

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-10〜）

起票日: 2026-11-09

## 概要

`_claude/rules/verify-execution-not-just-exit-code.md` の「長い検証 (make test 等) は、道具 1 回の中の素の `&` で背景に回さない」(retro 1030 で足した規範) が、
規範だけでは止まっていない。2026-11-09 のセッション (retro 1049) で、`( make -C src/dupscan lint test > log 2>&1; echo rc > rc ) &` を打ち、
完了の通知が来ないまま同じ worktree のファイルを編集した。走っていた run は古い版を検証していたので、pid を探して止め、回し直した。

## 詳細

素の `&` で起こすと、完了の通知が来ず、ハーネスの停止 (TaskStop) も効かない (規範の本文にある理由。今回は `ps` の cwd と command 行で自分の run を探して止めた)。
加えて、走っている間に同じ worktree を編集しても何も言われない。issue 1041 の「make test の途中で木が変わった」の報告は、ルートの `make test` (`test-guarded`) にしか掛からず、
`make -C src/<module> …` や `go test` を素の `&` で起こした run には効かない (今回の形)。

## 対応方針 (案)

- PreToolUse(Bash) の hook で、長い検証のコマンド (`make` / `go test` / `bash tests/…` / `mutate-verify` / `mutate-verify-list` / `bin/skill-eval`) の後ろに、
  引用と heredoc の外で `&&` でない単独の `&` が続く形を deny し、ハーネスの背景実行 (`run_in_background`) へ案内する
- 判定は字面で見る粗いものにする (既存の `_claude/hooks/deny-piped-push-then-destroy.sh` と同じ作り。脅威モデルと検出しない形を hook の冒頭に書く)
- 判定の単位: heredoc の本文と引用を落とした文字の上で、`;` `&&` `||` `|` 改行で割った各コマンドを見る。長い検証の語を含むコマンド (またはそれを囲む `( … )` / `{ …; }`) の直後が単独の `&` なら止める。
  `make x; sleep 1 &` のように、`&` が別のコマンドに付いている形は止めない
- 止めない形: `&&`・`|&`・`>&`・`&>`・`>&2`・`2>&1`・`&>/dev/null`・引用の中の `&`・短いコマンドの `&` (sleep やサーバの起動など。長い検証の語が無いもの)・
  `nohup` / `setsid` / `disown` を付けた形 (`verify-execution-not-just-exit-code.md` の「上限後の出力経路が保証されないなら、最初から detach し、終了コードを run 固有のファイルへ書く」は
  規範が認める detach。止めると規範どおりの手順まで塞ぐ)
- `_claude/hooks/serial-run-wrap.sh` との順番: 同じ PreToolUse(Bash) で、長い検証の語の前に `serial-run -- ` を差し込む (updatedInput の書き換え)。
  deny する hook は、差し込みの前後のどちらの字面でも同じ判定になるようにし (`serial-run -- make` も長い検証の語として読む)、settings.json での並びと、
  deny と書き換えが同時に出たときにどちらが効くかを、実装の前に headless の session で測る
- deny ではなく案内 (additionalContext) に留める案も比べる: deny は確実に止めるが、上の detach の例外を書き損じると規範どおりの手順を止める。案内は止まらない (retro 1030 の後、規範だけで止まらなかったのと同じ形になる)
- テストは tests/claude/ に置き、止める形・止めない形の両方を並べ、変異 (検出の語を外す・`&&` の除外を外す) で red を確かめる

## 関連

- `_claude/rules/verify-execution-not-just-exit-code.md` の「非同期・background の完了も「成果物」で判定する」
- retro 1030 (規範の起源)・retro 1049 (2 回目)・issue 1041 (make test の途中で木が変わったことの報告)
- `_claude/hooks/deny-piped-push-then-destroy.sh` (同じ作りの hook)・`_claude/hooks/serial-run-wrap.sh` (同じ語を見て書き換える hook)

## 進捗 (2026-11-10)

### 実装: 別の hook を足さず、serial-run の hook に deny を足した
- 「長い検証か」の判定を、`isHeavy` (src/serialrun/hook.go) が既に持っている。そのため、PreToolUse(Bash) の `serial-run hook` が scanner で「背景に回す単位が長い検証を含むか」を数え、素の `&` なら書き換えずに deny を返す (src/serialrun/background.go の `backgroundDenial`)
  - 語の一覧を別の hook に写さずに済む
  - 書き換え (updatedInput) と deny を同じ 1 本が出すので、対応方針 (案) の「hook 間の順番を headless の session で測る」は不要になった
- 長い検証: `isHeavy` の対象 (make test/lint・go test・swift build/test・xcodebuild・mutate-verify(-list)) に `bin/skill-eval` と `tests/` の下の `.sh` を足したもの (`longVerification`)。直列化の対象は変えていない
- 🚨 単位は zsh の規則で切る。Bash ツールの shell が zsh (`/opt/homebrew/bin/zsh`) だから
  - zsh の `a && b &` は b だけを背景に回す (`sleep 2 && true &` の後の `$SECONDS` が zsh -f で 2、bash で 0)
  - そのため `make test && echo ok &` は止めない
- 止めない形:
  - 長い検証のコマンド自身の nohup / setsid、直後の disown、zsh の `&|` / `&!`
  - 後ろに wait がある形
  - `2>&1` / `&>` / `|&`、引用や heredoc の中
- 検出しない形は background.go の冒頭に書いた (引用で包んだ `bash -c "make test" &`、一覧に無い前置き、wait / disown の位置の作為的な形 等)
- 入口: `_claude/hooks/serial-run-wrap.sh` の速い経路に `skill-eval` と `tests/` を足した。rule (verify-execution-not-just-exit-code.md) の該当の行と serialrun の README に hook が止めることを書いた
- commit:
  - feat(serial-run): 長い検証を素の & で背景に回す Bash を hook で deny する
  - 以下の fix 4 本と README の追随

### 検証
- 変異: mutate-verify-list で 20 本すべて red
  - 単位の切り方 (&& / ; / | の後の改行)、段の持ち上げ、制御構文の段
  - detach の判定 (単位ごと・引数の nohup・serial-run -- を剥がす)、disown / wait
  - skill-eval、-o の値、runHook の deny
- 入口: 入口の速い経路の語を外す変異で tests/claude/test_serial_run_wrap.sh が red
- 敵対的レビュー (opus) は 3 周:
  - 1 周目: zsh と bash の違いによる誤検出 (P2) と、素通り 5 件
  - 2 周目: `& wait` / `&|` の誤検出、パイプの空行、引数の nohup、サブシェル越しの disown
  - 3 周目: hook が書き換えた `serial-run -- nohup … &` を止める不整合
  - 再現したものは直し、作為的な wait / disown の位置の 2 件は §8 の stopping rule で検出しない形に書いた
- ルートの `make test` rc=0 (rebase 後。1 回目は errchkjson で lint が落ち、直して回し直した)

### 本番での観測 (push と `~/dotfiles` の pull の後、2026-11-10)
- `claude -p --model haiku` に Bash で 1 回打たせた (cwd は ~/dotfiles、本体は ea6eaa49a)
  - A: `make -C /nonexistent-1050 test > /dev/null 2>&1 &` → tool_result が is_error=true で、本文が hook の deny 文 (「長い検証 … 素の & で背景に回しています」)。実行されていない
  - B (対照): `make -C /nonexistent-1050 test > /dev/null 2>&1; echo rc=$?` → is_error=false で `rc=2` (存在しない dir で make が落ちた = 止めずに走った)
- 残りは無い
