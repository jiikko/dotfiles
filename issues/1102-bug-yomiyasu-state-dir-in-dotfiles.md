# 1102 (bug): 状態フォルダを渡さずに check を dotfiles の中で動かすと ~/dotfiles/.yomiyasu ができる

起票日: 2026-11-21

## 概要

ja-writing の `check` と本家の `yomiyasu_check.py` は、状態フォルダを同じ順で決める (Go は `check/paths.go` の `StateDirFor`、本家は `state_dir_for`)。

1. `--state-dir`
2. `YOMIYASU_STATE_DIR`
3. 作業フォルダから上へ辿り、`.git` (フォルダでもファイルでもよい) が最初に見つかったフォルダの `.yomiyasu/`
4. どこにも `.git` が無ければ、作業フォルダの `.yomiyasu/`

dotfiles の本体の checkout の中 (`~/dotfiles/tmp/...` を含む) で 1 と 2 を渡さずに動かすと、記録は `~/dotfiles/.yomiyasu/` に入る。
worktree (`~/wt-*`) の中で動かすと、worktree の根にある `.git` ファイルで止まり、その worktree の根に `.yomiyasu/` ができる。

issue 1077 の作業 (2026-11-20〜21) で、本体の `~/dotfiles/.yomiyasu/` ができた。

- 2026-11-20 17:34 に作られ、同じ日の 20:21 に中身が足された。実装の係が状態フォルダを渡さずに check を試した
- 2026-11-20 23:52 に作られ、11-21 04:00〜04:02 に中身が足された。4b と 5 本目の係が、本家の行の網羅を測る使い捨ての run を `tmp/1077-cont-1120/p5/cov/run.*` で動かした
- 中身はどれもテストの文の残りで、git には入らない (中の `.gitignore` が `*`)。ユーザーの指示で 2 回とも手で消した
- 別の経路として、Go のテストが `os.Environ()` をそのまま受け継ぎ、`TMPDIR` が repo の中だと repo の `.yomiyasu` に書く形もあった。これは 1077 の commit「本物の check を動かすテストの環境変数を、プロセスから継がずに組む」で直してある

## なぜ困るか

- 何も知らせずに書くので、気づくまで残る (git status に出ない)
- 係への指示に「`--state-dir` を必ず渡す」と書いても、網羅の計測や driver の直接の実行で破られた。人が覚える規律では止まらない

## 対応方針 (案。どれにするかは着手のときに決める)

1. dotfiles の `.gitignore` に `.yomiyasu/` を足す。書かれることは止めず、git に入る事故を二重に防ぐだけ (今も中の `.gitignore` が防いでいる)。効き目は小さい
2. driver (`src/ja-writing/oracle/py/call_function.py`) が、作業根 (`ROOT = os.getcwd()`) の上に `.git`・`.yomiyasu-words`・`.yomiyasu` があれば起動を拒む。判定は Runner の使い捨ての dir の `oracle/runner.go` の `markerAbove` と同じにする
   - `YOMIYASU_STATE_DIR` を外へ向けてから起こす形は採らない。記録の case が自分でこの変数を外したり書き換えたりし (`oracle/checkrun_cases.go`・`oracle/checkbase_path_cases.go` の `checkStateDirEnvCase`)、`manageFlowCase` などは状態フォルダを渡さずに動かし、fixture の中の `.git` の横にできる `.yomiyasu` を前提にしているので、事故は止まらず、通る行も変わる
   - 網羅の計測の `cover.py` は各係が tmp に書いた使い捨てで、直す先として残らない。網羅を測る道具を残すなら、同じ判定を入れて tracked に置く
3. dotfiles の wrapper (`bin/ja-writing`) で、check が決めた状態フォルダが dotfiles か worktree の根の `.yomiyasu` なら警告を出す。本家との互換を崩さないよう、本体ではなく wrapper に置く

## 関連

- issue 1077 (本文の「事故」の行)
- 状態フォルダの選び方の正本: `src/ja-writing/check/paths.go` の `StateDirFor` と、本家の `state_dir_for`
- issue 1077 の本文と `oracle/runner.go` の `upstreamMarkers` のコメントは「git の最上位」と書いているが、実際はいちばん近い `.git` で止まる。この issue に着手するときに直す
