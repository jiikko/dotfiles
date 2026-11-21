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
- 「git の最上位」と「いちばん近い `.git`」は、普通の repo と worktree では同じ場所を指す (worktree の根の `.git` ファイルは、その worktree の最上位にある)。違うのは repo でない場所の `.git` などの端の形だけで、本家の文書も「git の最上位」と書く。`oracle/runner.go` の `upstreamMarkers` のコメントは、正確な方の言い方に直した。issue 1077 の本文は本家の言い方のまま残す

## 進捗

- 2026-11-21 方針 2 を入れた。commit「本家を動かす driver を Runner を通さずに repo の中で起こしたら、指示を読む前に止まる」と「driver の止める処理が作業根そのものの印も見て、repo の根で直接起こしても止まる」
  - 原因を確かめた: 網羅の計測の台本 (`tmp/1077-cont-1120/p5/cov/cover.py`) は `call_function.py` を Runner を通さずに起こしていた。Runner (`newSandbox`) は使い捨ての dir の上に印があると前から止まるので、Runner を通らない経路が穴だった
  - `call_function.py` は、指示 (stdin) を読む前に、作業根とその上に `.git`・`.yomiyasu-words`・`.yomiyasu` があれば止まる (`refuse_under_upstream_markers`)。Runner は driver を起こす前に作業根へ何も書かないので、作業根そのものも見る (fixture の `.git` は確かめの後に op が作る)
  - テスト `oracle/sandbox_test.go` の `TestCallFunctionRefusesUnderUpstreamMarker`: 3 つの名前 × 印の場所 (作業根・2 段上・4 段上) と、対照 (作業根の中のサブフォルダの `.git` では止まらない)
  - わざと壊した形 4 本が red: 呼び出しを消す・作業根の 1 つ上から見る・上へ辿るのを 2 段で止める・`.yomiyasu` を外す
  - 本番の経路で止まらないこと: 記録 `check-verify.json` と `check-manage.json` を作り直して差分 0
  - 敵対的レビュー (opus) 1 周。指摘のうち P1 の 2 件 (repo の根で直接起こすと止まらない・2 段で止める退行をテストが通す) を 2 本目の commit で直した
- 採らなかった方針
  - 方針 1 (`.gitignore` に `.yomiyasu/` を足す): 中の `.gitignore` (`*`) が既に git に入るのを防いでいるので、効き目がほぼ無い
  - 方針 3 (wrapper の警告): issue 1078 で文章の検査を dotfiles に組み込むと、check が dotfiles や worktree の根に `.yomiyasu` を作るのは正しい使い方になる。警告は誤報になる
- 残るもの (受け入れる): `bin/ja-writing check` や本家の `yomiyasu_check.py` を状態フォルダの指定なしで repo の中で直接動かすと、今も `.yomiyasu` ができる (1 件目の事故の形)。これは本家どおりの仕様の動きで、止めると 1078 の使い方を壊す。試すときは `--state-dir` を渡すか、cwd を repo の外にする
- レビューの再現で OS の一時フォルダに残った dir (`$TMPDIR/tmp.EpG1Lb7GJ3`) は、issue 1104 と一緒に片付ける
