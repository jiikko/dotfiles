# 1077 (feat): yomiyasu の check (検査の記録と最終稿の照合) を Go で作り直す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-12〜）

起票日: 2026-11-12

## 概要

本家の `yomiyasu_check.py` (5,639 行) を `ja-writing check` として作り直す。最終稿に lint を、原文と最終稿に diff をまとめて掛ける道具。
入力の写し・レポート・記録を状態フォルダに書き、後で最終稿が記録と同じかを照らす。返すのは「検査を実行した」であって、意味の保持や読みやすさの合格ではない (本家の docstring)。

## 本家が持つもの (v3.1.0)

- subcommand: `run` (省くと run)・`verify`・`start`・`export`・`end`
  - `run`: lint と diff を時間の上限つきの子プロセスで動かす。資料があればプロセスの中で照らす。結果は状態フォルダ (指定しないときは git の最上位の `.yomiyasu/`) に書く
  - `verify`: 最終稿のハッシュを記録と照らす。`--replay`・`--since`・`--timeout` を付けると lint と diff を動かし直す
- オプション (subcommand の全体): `--doc-type` `--fact-gaps` `--fail-on` `--final` `--json` `--material` `--materials` `--new-run` `--no-record` `--original`
  `--reader` `--replay` `--run-id` `--session` `--since` `--source` `--stance` `--state-dir` `--stdin` `--timeout`
- 記録 (JSON。`ensure_ascii=True`・`indent=1`) に入るもの
  - 道具のファイルの SHA-256
  - 語の一覧 7 本 (nominalization・intensifiers・feelings・hedges・katakana-terms・thin-evaluation・renyo-nouns) と fact-gaps.txt の SHA-256
  - 動かした Python の情報 (`python.version`・`implementation`・`unicode_version`)。`verify` は `unicode_version` が今と違えば、似かた (NFKC と一般カテゴリ) を求め直さない
- JSON は、ほかに `sort_keys` と `separators=(",", ":")` の組と、指定なしも使う。記録を読む `json.loads` が 3 か所ある
- 状態フォルダを `.gitignore` で扱う規則、書けないときの案内、書き込みを途中の状態で残さない処理 (`_write_atomic`)

## 決めること (着手のときに、本家のコードを読んでから)

- **記録の互換**: Go の check が書く記録を、本家の check の `verify` が読める形にするか (逆向きも)
  - `tools` の SHA-256 は本家では Python のファイルの値で、Go のバイナリの値は意味が違う
  - `python` の欄に Go の情報を入れると事実と違う
  - 互換を持たない場合は、記録の `format` かバージョンの欄で区別し、相手の記録を読んだときに「別の道具の記録」と知らせる
- **子プロセスか関数呼び出しか**: 本家は lint と diff を子プロセスで動かして時間の上限を掛ける。Go では、1073 の `Main(ctx, env)` を関数として呼ぶこともできる。
  上限を子プロセスで掛ける理由は、止まらない照合を止めること。これを Go の取り消し (context) で同じ強さで守れるかを確かめてから選ぶ。
  regexp2 の照合は context を見ないので、関数で呼ぶなら照合が止まらない場合の扱いを決める
- 記録の中の時刻・ホスト・パスと `python` の欄は、1073 の「揃わないことを認める範囲」の表の枠で置き換えて比べる。その中身をここで決める

## 決めたこと (2026-11-15。着手前の調査の地図は tmp/yomiyasu-gap-2026-11-12/check-survey/map-check.md)

本家 `yomiyasu_check.py` はトップレベルの def 203・class 13・`re.` の呼び出し 25 (後読み・先読みは 0)。`json.dumps` 13 か所 (indent=1 が 9、sort_keys と詰めた区切りが 3、ensure_ascii だけが 1)、`json.loads` 3 か所、子プロセスを起こす所 1 か所。
diff の群 AE (`ground` 7555〜7957・`fact_gaps` 9370〜9475・`detector_summary` と `report_parts` 11126〜11160) は check が呼ぶので、diff の package の公開関数として 1077 で作る (群 J・M の内部の型を使うため)。

- **(a) 記録の互換: 互換にする (ユーザー決定 2026-11-15)**。`tools` の欄に本家 3.1.0 のファイルの SHA-256 を書き (道具の `.py` 4 本と、語の一覧 `references/lists/*.txt` 7 本 (`_LIST_NAMES`。check.py 139・918〜930)。fact-gaps は `--fact-gaps` のときだけ。値は `oracle/upstream.sha256` にある。一覧が欠けたとき (キーが無い・開けず null) の規則も本家から写す)、`python` の欄には Go の実際の値を書き、`format` は本家と同じにする。`skill` は常に `found: false`。
  - 根拠 (本家の verify のコードの読み: `format` 4325・`python` の欄 3805 は型だけ見る・tools の sha256 4427・`unicode_version` は `_recheck_problem` 4016。調査の係が本家 Python 3.15.0 の記録を書き換えて本家の verify に読ませた実験の出力は残っておらず、出力では裏を取れていない。語の一覧の sha を変える形は試していない): 知らないキーを足す・`python` を Go の値 (go1.26.0 / Go) にする・`unicode_version` を変える・tools の version の文字列を変える、はどれも executed。tools の sha256 を変えると tool_changed (rc 1)、`format` を変える・`python` を消すと invalid_record
  - 採らなかった案: format を変えて互いに断る形 (本家が書いた `.yomiyasu/` を Go が読めず、Python を正解にした突き合わせができない)・Go だけが両方読む形 (Go の記録を本家の verify に判定させられない)
  - 失うもの: `tools` の欄の意味が「実際に動いた道具」から「答えを合わせた本家のファイル」にずれる。本家の `.py` がある環境では skill_changed の注記が付く (状態は変わらない)。逆向きに Go の verify が本家の記録を読むと、SKILL.md のハッシュを出せないので skill_changed が常に付く。誰が書いたかは `python.implementation` に残る
  - 失うもの (2): tools の sha を本家の値に固定するので、Go の挙動を直しても古い記録が tool_changed にならない (本家は `.py` を直せば `_recheck_problem` 4019 と verify 4427 で古い記録を弾けた)。Go の挙動のバージョンは `python.implementation` の文字列 (例: `Go ja-writing <バージョン>`) に入れ、この限界を README に書く
  - `python.version` と `python.implementation` が本家と違うことは、README の「揃わない範囲」の表に 1 行足す
- **(b) 子プロセスか関数呼び出しか: 自分自身 (`os.Executable()`) を `lint --json` / `diff --json` の子として起こし、締め切りで kill する**。本家が子で止めるのは止まらない照合を止めるためで、同じ強さで守れるのはこの形だけ
  - 2 つの案を分けるのは「止まらない照合を kill できるか」で、cwd は子の `exec.Cmd` の Dir・Env で本家の `Popen` (1521) と同じに指定できる (関数で呼ぶ案の弱点だが決め手ではない)
  - Go の事実: `lint.Main` (lint/lint.go)・`diff.Main` (diff/diff.go) は ctx を捨てている、regexp2 の `MatchTimeout` は使っていない、`visibility.InputPathProblem`・`ReadInput` はプロセスの cwd を見る (関数で呼ぶなら `os.Chdir` が要る)、Go の致命的なエラーは recover できない
  - 採らなかった案: 関数 + goroutine (止まらない照合が CPU を取り続け、致命的なエラーで記録が残らず、cwd を共有する)・関数 + 取り消しを lint・diff・pyre・difflib に通す (1073〜1076 で全件突き合わせたコードを書き換え、突き合わせをやり直す)
  - 失うもの: 実行ファイルが ja-writing であることに頼る。`os.Executable()` が ja-writing なのは main から起動したときだけで、check の test と 1078 の hook の package (とその test) から呼ぶとテストバイナリになる。子を起こす口を差し込める形にし、test は TestMain の再 exec で ja-writing の Main を走らせる。`exec_boundary_test` の許可表を変える。実行中にバイナリが入れ替わると親子のバージョンがずれうる (未確認)
  - 親の中で動く `ground`・`fact_gaps` に時間の上限が無いのは本家と同じ。SIGHUP は check の中で受けて子を kill し、128+n で終わる (main.go の `signal.NotifyContext` は INT と TERM だけなので、受け方が二重にならないよう実装で整理する)
- **(c) 記録の突き合わせの置き換え**: 置き換えるのは実行 ID (`--run-id` で固定できるなら置き換えない)・時刻・ミリ秒・パス (cwd・HOME・TMPDIR の接頭辞)・`python.version`・`python.implementation`・案内の文のコマンド名。置き換えないのは `python.unicode_version` (どちらも 17.0.0。ずれは赤で見る)・tools の SHA-256 ((a) の不変条件)・語の一覧を使わない入力の `report_sha256` と `text_sha256`。パスから作る索引のキーは、ハッシュの値でなく `_place` の文字列で比べる。`record_digest` は置き換える前に自分の式で検算してから外す
- 推奨のまま決めた実装の選択: Windows の枝は写さない・書き込みとロックは本家の規則に合わせる (`src/atomicfile` に寄せるかは差を数えて決める)・casefold の表は Python から生成する・文面の定数は 1 ファイルにまとめる・記録の形の検査は表にして 1 つの検査器で回す

### 実装の分け方 (1 本 = 実装 → opus の敵対的レビュー → 修正)

1. 部品: 小道具・パスと状態フォルダの場所 (casefold の表・F_GETPATH)・似かた・(a) の tools の定数・pyjson への足し。関数の記録
2. diff の群 AE と、JSON から文を作る口: `ground`・`fact_gaps`・`detector_summary`・`report_parts`・`diff("", "")` の公開口。関数の記録。check が lint と diff から使う口を全部公開する: diff の `diff`・`report`・`fail_on_count`・`extract_numbers`・`STANCES`・`MATERIALS_FORMAT_VERSION`・`DIFF_FORMAT_VERSION` (diff 166) と非公開の `_shows_rewritten_only` (check 2409)・`_hunk_heading` (2659)・`_LIST_PROBLEMS` (2746)、lint の `render_report`・`render_findings`・`LINT_FORMAT_VERSION` (check.py の `_diff_module()` 1027・`_lint_module()` 1024 が取るもの)
3. 書き込み・入力・記録の読み・verify (replay 以外)。oracle に「同じ作業フォルダで複数回起動して状態フォルダの木を取る」記録を足し、本家が書いた状態フォルダで Go の verify を比べる
4. run: 子の実行 ((b))・run の文面と保存・replay・run と verify の引数・シグナル。重ければ 4a (子の実行・run・出力) と 4b (replay・`--since`) に分ける
5. start / end / export と登録 (ロック・`_managed_limit`・1078 の hook が呼ぶ公開の関数)

## 受け入れ条件

- [ ] `run` / `verify` / `start` / `export` / `end` が入力の集まりで本家と同じ出力と rc を返す。違ってよいのは 1073 の表で置き換えた字だけ
- [ ] `verify --replay` が lint と diff を動かし直し、本家と同じ結果を返す
- [ ] 状態フォルダの選び方・書けないときの振る舞いが本家と同じ
- [x] 上の「決めること」の 3 つを決め、理由をこの issue と README に書く (issue には「決めたこと」に、README には check の節に書いた。2026-11-15)
- [ ] subcommand ごとに最低 1 つ、実装を外して落ちることを確かめた (`mutate-verify`)
- [ ] `make test-dupscan-gate` が通る

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_check.py`
- 1075 (lint)・1076 (diff)・1078 (hook が check を読む)

## 再開の手順 (2026-11-15 14:40 に止めた。OS の再起動のため)

1 本目 (部品) の実装と、途中までのレビューの指摘 5 件の修正を push した所で止めた。

- **済んだもの**
  - 1 本目の実装 (下の「1 本目」の commit 6 本)
  - 途中までのレビューの指摘 5 件の修正 (下の表の「扱い」)。commit は「test(ja-writing): check のパスの記録に FIFO・ハードリンク・HOME 無しを足す」と「docs(ja-writing): README の diff の記録の件数と、check の揃わない範囲 (HOME 無し・不正なバイトの読み) を直す」。記録 `check-base.json` は 35 → 37 case で、既存の case の消失・変化は 0 (python で照合)
  - push 前の 4 つの検査 (`make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate`) は、修正の後の木ですべて rc 0
- **残り (この順に)**
  0. ~~止めた時点の push の CI を見る~~ 済んだ (2026-11-20。引数なしの `bin/ci-log` で HEAD 0ff48f1be に失敗した run は無かった)
  1. 修正の確かめの続き: わざと壊した形 7 本 (一覧は `tmp/yomiyasu-gap-2026-11-12/check-wf1b/list.sh`。`bin/mutate-verify-list` に渡す) のうち、`fifo-no-nonblock` だけ red (rc 0) を確かめた所で止めた。→ 済んだ (2026-11-20。残り 6 本 samefile-no-inode・samefile-skip-path-compare・samefile-stat-error-true・home-unset-skip・home-unset-no-set-check・home-unset-no-passwd を `bin/mutate-verify-list` で当て、6 本とも rc 0 = 想定どおり red。P2-2・P2-3 の「壊した形は未確認」もこれで確かめた)
  2. ~~2 回目の敵対的レビュー~~ 済んだ (2026-11-20。下の「1 本目の敵対的レビュー (2 回目)」)。以下は当てたときの指示の写し: 1 本目の全部 (起点 23f638d6f から 2 本目の前まで) に、もう一度 opus の敵対的レビューを当てる。前のレビュー (`check-wf1/redteam.md`) は途中で止まっていて、lint/・diff/ のテスト・共有の部品 (pyjson の設定・pyunicode の表) を変えたことで lint と diff の答えが変わっていないか・大文字小文字の表の使い所を攻めていない。済んだ範囲 (関数の fuzz・語末の Σ・パス・`_record_digest`) は繰り返さない
  3. そのレビューの指摘を直す: P2・P3 とも直した (2026-11-20)
  4. 2 本目 (diff の群 AE と、JSON から文を作る口) へ進む。以降は上の「実装の分け方」の順。2 本目と 3 本目の途中までは master に入った (下の「2026-11-20 の並列の作業で進めた 2 本目と 3 本目」。worktree は残っていない)。2 本目の残りと 3 本目の verify (`--replay` を除く) も 2026-11-20 の続きで master に入れた (下の「2026-11-20 の続き」)。4 本目の前半 (run) も master に入れた (同じ節)。次は 4 本目の後半 (`verify --replay` と `--since`)
- **進め方** (ユーザー指示 2026-11-15): 係 (実装・レビュー・修正) は **opus で 1 体ずつ直列** に起こす (fable は使わない・並べない)。1 本 = 実装 → 敵対的レビュー → 修正。係が動いている間は main agent も計測やテストを重ねない。worktree は 1 つ作ってそこで作業し、push は `git push origin HEAD:master` の後に `scripts/pull_main_checkout.sh`。CI は `bin/ci-log -i` で src/ja-writing・Tests・unused・Bench を名指しで待ち、最後に引数なしの `bin/ci-log`
- **使ったもの** (どれも手元の本体 checkout の `tmp/` にあり、git には入らない。消えていたら作り直す):
  - 着手前の調査の地図: `tmp/yomiyasu-gap-2026-11-12/check-survey/map-check.md`
  - 本家の写し: `tmp/yomiyasu-gap-2026-11-12/src/skills/yomiyasu/scripts/` (本家 db78b9bc5)
  - 1 本目の作業の dir: `tmp/yomiyasu-gap-2026-11-12/check-wf1/` (inventory.md・redteam.md・レビューの道具 `rt.xd1FIg/`) と、修正の dir `check-wf1b/` (わざと壊す一覧 list.sh と途中の結果 mut.out)
  - 係への指示の台本: Claude Code の session の `workflows/scripts/ja-writing-1077-check-wf1b.js` (修正 → レビュー → 修正。レビューと修正の係は opus を指定済み)

### 1 本目の敵対的レビュー (途中まで。opus)

本家と違う答えを返す入力は、本番の経路では作れなかった (関数の fuzz 82,535 件・語末の Σ を全コードポイント 1,112,064 字・パス 1,873 件・`_record_digest` 1,500 件、どれも食い違い 0)。壊した形 52 本のうち red 46・緑 6 (P2 の 3 本と、答えが変わらない 3 本)。

| 指摘 | 場所 | 中身 | 直し方 | 扱い |
|---|---|---|---|---|
| P2-1 | `check/paths_darwin.go` の F_GETPATH の open | `O_NONBLOCK` を外しても緑。記録に FIFO が無い。書き手のいない FIFO を `--final` に渡すと open で止まる (本家は止まらない。FIFO で本家と Go は一致) | path の fixture に `mkfifo` を足し、`_disk_path`・`_place`・`_case_insensitive` を通す | 直した (記録に mkfifo を足し、`O_NONBLOCK` を外す退行は check の単体のテスト (止まりの上限つき) で赤にする。壊した形 `fifo-no-nonblock` で red を確認) |
| P2-2 | `check/paths.go` の `SameFile` | `os.SameFile` の枝を `return false` にしても緑。記録にハードリンクが無い (今の Go の答えは本家と同じ) | path の fixture に hardlink を足し、`_same_file(F.txt, hard.txt)` を記録する | 直した (記録に hardlink を足した。壊した形は未確認) |
| P2-3 | `check/paths.go` の `HomePath` | HOME が無いときの枝を壊しても緑。記録の HOME 15 通りに「無い」が無い (HOME を消して本家と比べた 480 件は一致) | 記録の HOME に unset を足す (結果は利用者のホームなので置き換えるか、`~` で始まらないことだけを比べる) | 直した (HOME が無いときは、結果そのものでなく `posixpath.isabs` と basename を比べる。壊した形は未確認) |
| P3-1 | `src/ja-writing/README.md` 8 行目 | diff の CLI の記録の件数が 8,146 のまま (正しくは 8,154) | 8,154 に直す | 直した |
| P3-2 | `check/tools.go` の `DirFiles.Tool` | 不正なバイトの読み方が本家と違う (`"3.1\xe3\x81"` は本家 `3.1�`・Go `3.1��`、`"3.1\xed\xa0\x80"` は本家 `3.1���`・Go は単独のサロゲート)。使うのは base_recording_test.go だけで、本番は `Released` | 本番で使わないなら README の「揃わない範囲」の行に 2 例を書く | 直した (README の「揃わない範囲」に 2 例を書いた) |

答えが変わらない書き換え (採らない): `HomePath` の 2 つの `~` の検査はどちらか一方で足りる (本家も同じ二重の形)・`CaseInsensitive(probe)` を `(real)` にしても同じファイルシステムの中では同じ答え。
未確認リスク: 大文字小文字を区別するファイルシステム・EACCES の getcwd・NUL を含むパス。共有の部品 (pyjson の設定・pyunicode の表) を変えたことで lint と diff の答えが変わっていないかは、レビューでは見ていない (push 前の `make -C src/ja-writing lint test` は lint と diff の記録の試験を含めて rc 0)。

### 2026-11-20 の並列の作業で進めた 2 本目と 3 本目

2 本目と 3 本目を別の worktree で同時に進めた (ユーザー指示 2026-11-20: 40 分で並列を最大にして weekly の枠を使い切る。それまでの「opus で 1 体ずつ直列」はこの回だけ外した)。
どちらも敵対的レビューを通していないので master に入れていない。worktree は消さずに残した (detached。commit は下の hash)。再開するときは、各 worktree で `git rebase origin/master` してから続ける。

- **2 本目 (途中まで master に入れた。2026-11-20)**: check が lint と diff から使う口の公開と、子の JSON の形の誤りの扱い。敵対的レビュー (opus) を 4 周当てた
  - 入れた口: diff の `DetectorSummary`・`ReportParts`・`Report`・`ShowsRewrittenOnly`・`HunkHeading`・`FailOnCount`・`Stances()`・`ListProblems()` (Go は語の一覧を埋め込むので常に空)・`DiffFormatVersion`・`MaterialsFormatVersion`・`DetectorKeys`。lint の `RenderReport`・`RenderFindings`・`LintFormatVersion` (CLI の出力も同じ実装を通す)。`pyjson.Object` の `Field`・`Clone`・`Delete`、`visibility.WordsLabelOf`・`ContextLabelOf`、`pyunicode.Upper`
  - **決めたこと: 子の JSON の形の誤りは、本家と同じく error にする** (verify が本家の書いた記録も読むため)。口は `(…, error)` を返す。条件は「本家が例外 ⇔ Go が error」で、例外にならない呼び出しは戻り値も一致させる (例外の型と文は比べない)。Python の演算と例外は `internal/pyops` に写し、例外は panic で運んで口の `pyops.Catch` で error に戻す (自分で投げた例外以外の panic は投げ直す)
  - 記録 `report-shape.json`: 基準 23 個 (diff 20 組・lint) の欄を 1 か所ずつ書き換えた全部 (書き換え 46,279・呼び出し 359,126・本家の例外 20,290) を、書き換えごとの digest で持つ (1.07MB)。食い違いは `go run ./oracle/cmd/golden -shape-detail '<基準>:<番号>'` で調べる (README)。代表を選んで残す形は、レビューの周ごとに束ね方の穴 (path の 2 段 → kind・subkind → hunk の tag 等) が出たのでやめた。diff の shape のテストは race で 42 秒かかるので `NORACE_CHECKS` で race なしで回す
  - レビューで直したもの: 1 周目 = 形の誤りで error を返さない・panic する、`FailOnCount` の wrapper のテストの穴、`Object` の写し。2 周目 = 記録の束ね方・severity の大文字化 (`ß` など)・float に収まらない int の OverflowError・int と float の比べの精度・`FailOnCount` の途中の数。3 周目 = 記録の束ね方・`sum` (CPython の補正つきの和。本物を oracle にした 3,017 件の記録 `internal/pyops/testdata/sum.json`)・NaN のコメントの理由。4 周目 = 記録の束ね方 (全件にして閉じた)。CLI の出力 (lint・diff・report) は、3 通りの比べ (988 呼び出し・語の一覧・1.9MB の diff) で master と変わらなかった
  - 直さないもの: NaN の扱い (list の == の identity・並べ方)。本家にも Go の子にも nan を作る式が無い (割り算は分母 0 を先に除き、float() は check の引数で 0<v<inf に限る)。理由と再提起の条件 (NaN を含む子の JSON が実際に出る経路) は `pyops.Equal` の直前のコメント
  - 4 周目の後は、記録を全件にして代表を選ぶ処理を無くした直しなので、5 周目のレビューは当てていない (直しの後に 2〜4 周目の変異 `tone-lookup-default`・`stiffness-sentence-linewhere`・`hunkheading-delete-oldend-or` が全部 red)
  - 検査: 最後の木で `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` が rc 0。root の `make test` は赤 2 本 (`tests/githooks/test_pre_push.sh` は master でも同じく赤・`tests/zshrc/av1ify/test_av1ify_vfr_e2e.sh` は bin の道具を PATH から呼んだ検査。どちらも src/ja-writing の外で、この変更は触っていない)
  - 残り: `ground` (本家 7844〜。約 400 行)・`fact_gaps` (9430〜) の写しと記録・`diff("", "")` の公開口・`extract_numbers` の dict の口。記録は 1 か所ずつの書き換えで、2 か所以上を同時に壊す形と list の 2 番目以降の要素は試していない
- **3 本目 (書き込みと記録の読みまでを master に入れた。2026-11-20)**
  - 入れた関数: 書き込み (本家 465〜800・2859 の `_mkstemp`・`_replace`・`_write_atomic`・`_raise_if_hidden`・`_still_placed`・`_place_new_file`・`_write_state_files`・`_add_state_files`・`_ensure_state`・`_write_problem`) と記録の読み (864〜890 の `_read_record_status`・`_read_record`)
  - 決めたこと: `src/atomicfile` は使わない (一時ファイルの名前が本家の `.tmp-<16進>` と違い、`_write_problem` の文が変わる)。`os.Rename` でなく `syscall.Rename` (宛先がフォルダのとき Go は EEXIST を返し、本家の EISDIR と文が変わる)。状態フォルダの親は `os.MkdirAll` でなく pathlib と同じ手順で作る (親の途中がファイルのとき、`MkdirAll` は ENOTDIR・本家は FileExistsError で文が違う)
  - 記録 `check-base.json` を 37 → 42 case・2,172 呼び出しにした (呼んだ後の状態フォルダの木を取る op `tree`・記録の読みのファイルの集まり・`os.strerror(0〜120)`)。記録で見つかった違い 2 つを直した: 上の `MkdirAll` と、`visibility/paths.go` の `strerror` の手書きの表 (EEXIST が無く "Unknown error: 17"・EOPNOTSUPP の文・0 の文の誤り。lint と diff の誤りの文も直る) を Python の `os.strerror` を写した配列にした。比べはテストの中で umask を 022 に固定する (記録の mode は 022 のもの)
  - 敵対的レビュー (opus): P1・P2 なし。本家と並べた書き込み 37 通り (リンク・読めないフォルダ・大文字小文字・NFD・uchg・umask 000/022/027/077)・FAT のボリューム (hard link を作れない FS で O_EXCL に回る経路)・8 プロセスの同時の用意 40 回・記録の読みの壊れた入力 (サロゲート・BOM・NaN・5000 桁・入れ子 5 万段) は全部一致。P3 5 件を直した: 記録の比べが umask に依存 (固定した。`S_IMODE` と同じく 0o7777 を見る)・空の記録の読み直しと EACCES の読み直しと Chmod の分岐にテストが無い (待ちを差し替え口にしてテストを足した)・`hiddenError` と相対パスの `AbsPath` は macOS / 本番から届かない (コメントに書いた)。`stillPlaced` は本物の FS で決まって起こせないので、テストせず理由を `placeExclusive` に書いた
  - 揃わない範囲 (README): 入れ子が 20 万段の JSON は本家が RecursionError・Go が error
  - 検査: 最後の木で `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` が rc 0。root の `make test` は `tests/githooks/test_pre_push.sh` だけ赤 (master でも同じく赤。この変更は触っていない)
  - 続き (2026-11-20。master に入れた): 入力の読み (本家の `_Input` 1033・`_read_stdin_bytes` 1076・`_make_input` 1095・`_read_named` 1104・`_material_marker` 1131・`_split_material` 1145・`_split_stdin` 1177・`_path_problem` 1239・`_read_inputs` 1254・`_Target`/`_read_target` 3399〜3439。`check/input.go`) と記録の形の検査 (`_record_shape` 3735・`_fact_gaps_shape` 3718・`_words_shape` 3897。`check/record_shape.go`)
    - 決めたこと: 入力の読みの誤りの文と標準入力の読みは `internal/pycli` (`OpenProblem`・`NotUTF8File`・`NotUTF8Stdin`・`ReadStdinBytes`) に寄せ、lint・diff の `pycli.ReadInput` も同じものを使う。形の検査は項目を本家と同じ順の表 `recordRules` に並べ、`RecordShape` 1 本で回す。本家が TypeError で落ちる記録 (検査の名前や fact_gaps の kind に list・dict) は Python 3.15 と同じ文の error を返す
    - 記録 `check-base.json` を 49 case・35,361 呼び出しにした (入力の読み 425・形の検査は形の合う記録 2 つの欄を 1 か所ずつ替えた 14,789 件)。driver に `$namespace`・op `stdin`・`objects` を足した
    - 敵対的レビュー (opus): 本家と違う答えは 1 件 (P2)。checks の name を 2 つとも NaN にした記録で、本家は nan を 1 つに数える (json.loads が同じ object を返し set は identity を先に見る) のに Go は別に数え、形の誤りを 1 件見落とした。直した (commit「fix(ja-writing): 記録の形の検査で、検査の名前の nan どうしを 1 つに数える」。期待値は本家の答え。変異 `nan-distinct` で red)。pycli に寄せたことによる lint・diff の退行は、master と並べた標準入力 6 通り × 4 コマンドで rc・出力とも一致。check の入力の読み 71 通り・形の検査 65 通りと 2〜4 か所を変えた 4,000 件のファジングは本家と一致。形の検査の変異 10 本は直した後の木で当て直して全部 red
    - 揃わない範囲 (README): 閉じた標準入力 (`<&-`) は本家が rc 2、Go は空の入力 (lint・diff・check とも)。Go の runtime が起動の時点で閉じた fd 0 を /dev/null で開き直すので `</dev/null` と区別できない (Go 1.26 で実測)
    - 検査: 最後の木で `make -C src/ja-writing lint test`・`make test-unused-excluding-tests` が rc 0。root の `make test` は `tests/githooks/test_pre_push.sh` だけ赤 (master でも同じ)
  - 残り: verify (replay 以外。本家 3442〜4700 の約 1,200 行: `_Found`・`_find`・`_verify_record`・`_recheck_problem`・`_summary_problems`・`_verify` など)・本家が書いた状態フォルダを Go の verify に読ませる記録とその逆向き・`_touch`・`_ensure_dir` を `_save` と一緒に戻す

### 2026-11-20 の続き (opus で 1 体ずつ直列。作業の跡は本体の `tmp/1077-cont-1120/`)

- **2 本目の残り (master に入れた)**: diff の `Ground`・`FactGaps`・`Diff` (引数なしの口)・`ExtractNumberDicts` (`diff/ground.go`・`factgaps.go`・`public.go`)。commit「check が diff から使う ground・fact_gaps・diff()・extract_numbers の口を写し…」と「ground・fact_gaps の記録に、写しの分かれ目を通る入力を足す」
  - 記録 `diff-ground.json` 875 case (ground 1,777・fact_gaps 1,707・extract_numbers 81 呼び出し。check が書く `json.dumps(ensure_ascii=True, indent=1)` の字も比べる)。本家の行の網羅は 333 行中 332 (残り 1 行は一覧の読みで先に検査するので届かない)。わざと壊した形 40 本が red
  - 敵対的レビュー (opus): 本家との食い違いは差分のファジング 37,323 件で 0。記録の入力の不足で緑のまま通るわざと壊した形が 7 本 (強調の後の単位・脚注の中の数・コードの中の設定の行の NFKC と数の鍵・同じ文の 2 つ目の当たり・確度の語に挟まれた範囲の単位・範囲の後に続く数) → 入力 15 件を足して 7 本とも red
- **3 本目の verify (`--replay` を除く。master に入れた)**: `check/verify.go`・`verify_summary.go`・`verify_cli.go` と `Main` の verify の配線。本家の run で作った状態フォルダ (op `snapshot`) に本家の verify を当てた記録 `check-verify.json` (18 case。main の呼び出し 352) と stdout・stderr・rc (`--json` も) を比べる。本家の verify の行の網羅は 795 行中 780 (残りは `--replay` 11・export の `_return` 2・届かない 2)
  - 敵対的レビュー 1 周目 (opus): 本家との食い違いは手の入力 209 件と欄のファジング約 2 万件で 0。記録の穴 6 本 (同点の並べ方・`--material` の順・真偽の件数・壊れた created_at・report の `./runs/`) → op `snapshot` に `ties` を足して case を 3 つ足し 6 本とも red。P3 (int64 を超える回の番号の名前で判定が invalid から executed に変わる) は本家に揃え、回の番号を任意の大きさで扱う
  - 2 周目 (直した差分に絞った opus): 本家との食い違いは約 470 件で 0。回の番号を字の順で比べる退行など 9 本が緑 → `attemptNo` を `cmp` でしか比べられない struct にし (字の比べ・`==`・`slices.Sort` はコンパイルが止める)、回 9 と 10・先頭の 0・巨大な回の読める記録の case を足して 9 本とも red
  - 4 本目で足すもの: 本家の run / start / end は回の番号に +1 する (`numbers[-1] + 1`・`_reserve_attempt`。yomiyasu_check.py 1411・1416・1426・2796)。`attemptNo` に big の加算を足す (使わないうちは unused が止めるので足していない)。逆向き (Go が書いた記録を本家の verify が読む) の記録も 4 本目で作る
  - 揃わない範囲 (README): `-h` の折り返しを幅 80 に固定・内部エラーの例外の型と文とトレースバック
- 検査: 各 push の前の木で `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` が rc 0。root の `make test` は `tests/githooks/test_pre_push.sh` だけ赤 (master でも同じ)
- **4 本目の前半 (4a = run。master に入れた)**: `check/children.go`・`run_link.go`・`run_checks.go`・`run_output.go`・`run_save.go`・`run_cli.go` と、新しい package `entry` (subcommand の表とプロセスの入口 `entry.Run`。`main.go` はそれを呼ぶだけ)。commit は「check の run を写す (子の lint と diff を締め切りで kill・資料の照らし・出力・保存・シグナル)」から「止めた後も残る verify の読みが、名前付きの結果 err を return と競って書き換えない」までの 20 本
  - 決めたこと: 子の実行ファイル・時刻・経過の時計・乱数・一時フォルダの置き場を `check.Runtime` にした (`MainWith`)。テストの子は、本番と同じ `entry.Run` を通るテストのバイナリにした。回の番号の +1 は `attemptNo.next` (big の加算)
  - 記録 `check-run.json` (15 case・489 呼び出し)。出力と、Go が書いた状態フォルダの木を本家の木と比べる (python.version と implementation の字だけ印にする)。子の代わりの shell script (op `pyexe`) で、時間切れ・終了コード・シグナル・JSON の形の誤り・起動できない形などを作る。TMPDIR が使えない形は case `run-tempdir`
  - 逆向き: Go が書いた形の木 (goize) に本家の verify を当てた 18 呼び出しを記録し、Go の verify の出力と比べる
  - 本家の run の関数 (docstring を除く 1,406 文) のうち 1,272 文を通る。通らない文は、シグナルと例外の枝 (Go の単体のテストで見る)・armed/ の予約 (5 本目)・競合でしか起きない名前の衝突・入力を作れなかった枝
  - 敵対的レビューは 4 周当てた (opus。証拠は `tmp/1077-cont-1120/p4art*/`、直しの跡とわざと壊した形の表は `p4afix*/mutations.md`)
    - 1 周目: 差分実行を約 120 case 当てた。見つかったのは次の 3 件と P3 3 件
      - Kill を SIGTERM に変えても全テストが緑だった (テストの子は SIGTERM で死ぬが、本番の子は SIGTERM を捨てていた)
      - `--stdin` を読む間のシグナルで止まらない
      - TMPDIR が無い・ファイルのとき、`--no-record` の run が rc 3 になる (本家は rc 0)
    - 1 周目の直し: テストの子を `entry.Run` に揃えた。シグナルの受け方は subcommand ごとに本家に揃えた
      - lint・diff・hook はシグナルを受けず、そのシグナルで終わる。check は SIGINT・SIGTERM・SIGHUP を受けて 128+n で終わる (起動時に無視されていたものは受けない)。lint・diff がシグナルを捨てていたのは 1073 の骨組みからそのままで、意図した決定の記録は無かった
      - `tempfile.gettempdir` の候補の選び方を写した
      - 環境変数を Env から読む。Env.Cwd がプロセスの cwd と違えば rc 3 で止める (1078 の hook は run・verify を CLI として呼ばないので、この守りは 1078 を壊さない。2 周目で確かめた)
      - Ground の panic で子を止めてから流し直す。一時フォルダの片付けで chflags を外す
    - 2 周目: 見つかったのは次の 2 件と P3 3 件
      - 書き手の居ない FIFO を `--material` / `--final` に渡すと、シグナルで止まらず、状態フォルダを書くか verify の結果を出してから 143 になる
      - 無視して起動されたシグナルを受けない分岐を外しても緑だった
    - 2 周目の直し: 入力と記録を開く・読む所を 1 つの口 `untilStopped` に寄せた。Env の環境変数を 1 つの並び (Environ) にして、空の値と「無い」を分けた。片付けを本家の `rmtree(onexc=again)` の写しにした
    - 3 周目: 見つかったのは次の 2 件と P3 5 件
      - 2 回目の直しで「テストから作れない」として、消しても緑のまま受け入れた形 (verify の出力の直前の確かめを消す) が、読み終わった瞬間に Canceled を返す ctx を使えば作れた
      - 読み終わった直後に止められると、誤りの文を出してから止まる
    - 3 周目の直し: `untilStopped` が読み終わった後にも ctx を見て、止められていれば結果を捨てる。子に渡す環境を「同じ名前は先のもの」で畳んだ。片付けの途中で消えた中身と、無い根を本家に揃えた (python3.15 で実測)。本物の check を動かすテストの環境を、プロセスから継がずに組んだ (直す前は、同じ退行で repo の `.yomiyasu` に実際に書いた)
    - 4 周目: P1 は無かった。P3 が 2 件
      - 止めた後に残る読みの goroutine が、verify の名前付きの結果 err を return と競って書く (`-race` で DATA RACE) → 閉包を局所の変数に書く形に直した。直した後は 3 回とも race 無し
      - 一時フォルダの根を symlink に差し替えられた形の本家との差 → README に書いた
    - どの周も、直したものをわざと壊して red を確かめた。緑のまま残したのは、Kill を SIGTERM に変える形だけ (lint・diff がシグナルで終わるようになり、本番の経路では Kill と区別できない。等価)
  - 揃わない範囲 (README に書いた)
    - python の欄
    - `armed/` がある run は rc 3
    - シグナルを受けて止まる場所。保存の途中では止まらない
    - 本家の lint・diff が SIGINT で出すトレースバック
    - 起動時に無視された SIGTERM
    - 片付けで symlink に差し替えられた形 (同じ利用者と root しか差し替えられず、守る相手がいないため揃えない。ほかの利用者が一時フォルダに書ける形になったら揃える)
  - 確かめていないこと
    - 負荷が高いときの時間切れ
    - 実行中に実行ファイルが入れ替わったとき
    - `ground` が例外になる本物の入力
    - 「シグナルで本家は何も出さない」は本家の構造を読んだもので、記録はしていない (シグナルの時点を driver で決められない)
    - `--final` の FIFO は run では rc 2 で揃う。verify では untilStopped を通る
  - 検査: 最後の木で `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` が rc 0。root の `make test` は `tests/githooks/test_pre_push.sh` だけ赤 (master でも同じ。`issues/` の hook の一覧に `test_issue_links_awk` が無い)
  - 4b に回したもの: `verify --replay` と `--since` の動かし直し

### 1 本目の敵対的レビュー (2 回目。2026-11-20。opus)

攻めた所: 共有の部品 (pyjson の設定・pyunicode の表・pystr.Float / DecimalInt) を変えたことで lint と diff の答えが変わるか・大文字小文字の表の使い所・lint と diff のテストを弱めたか。前回の済んだ範囲は繰り返していない。生存 2 件 (P2 1・P3 1)。

| 指摘 | 場所 | 中身 | 扱い |
|---|---|---|---|
| P2 | `internal/pystr/pystr.go` の `Float` | Go 1.26.0 の `strconv.ParseFloat` は、整数部の後ろに 0 が 800 個を超えて並び指数で打ち消す入力を誤る (`"1"+"0"*800+"e-800"` が 0.1、`n=1000` で 1e-201、Python はどれも 1.0)。`check.Timeout` がこの写しを使うので `--timeout` の答えが本家と違った。記録の floats は 4 字程度の数字しか無く、緑のまま通っていた | 直した (commit「fix(ja-writing): float(str) の写しが、仮数の 0 が 800 個を超えて並び指数で打ち消す入力を Python と違う値に読むのを直す」)。仮数を「0.<先頭と末尾の 0 を除いた数字>e<指数>」に書き直してから渡す。pystr.json の floats に長い仮数 36 件 (n = 799・800・801・1000) を書き足した (既存の 4,000 件とほかの欄は変わらないことを python で照合)。壊した形: 書き直しを外す → red。末尾の 0 を残す → 緑 (「0.」で始まる形なら末尾の 0 が 800 個を超えても ParseFloat は正しい。72 件を Python と比べて一致。削るのは数字の並びを短くするためだけで、答えは変わらない) |
| P3 | `internal/pycli/parse.go` の `FailOnWord` | lint と diff の `--fail-on` の小文字化は `strings.ToLower` + İ の近似のまま。check の `FailOn` は `pyunicode.Lower`。`--fail-on ΑΣ` の誤りの文が本家・check は `ας`、lint・diff は `ασ` (README の「揃わない範囲」に既知の差として書いてある) | 直した (commit「fix(ja-writing): lint と diff の --fail-on の小文字化を Python の str.lower の表で行い、check と 1 つにする」)。`FailOnWord` を `pyunicode.Lower` で引き、check の `FailOn` はそれを呼ぶ。README の「揃わない範囲」の 2 項を消した。`internal/pycli/parse_test.go` に Python 3.15.0 の出力 6 件 (語末の Σ・`ΑΣ_Β`・İ・Unicode 16 の字) を写した。lint と diff の CLI の記録は全件そのまま通る (記録に Σ の入力は無い)。壊した形: 近似に戻す → red。CLI の記録には足していない (足すと本家の記録の作り直しが要る) |

壊せなかった観点: 23f638d6f..HEAD で lint/・diff/ のファイルの変更は 0 件、足した部品は lint と diff から呼ばれていない (grep で 0 件)、既存で変わったのは `normalize.go` の二分探索を関数に出した 1 か所 (同じ処理)。本家の `casefold` / `swapcase` / `lower` の使い所と Go の表は 1 対 1。1 本目で lint と diff のテストの assert の緩和・記録の差し替えは無い。
未確認: runs/ に UTF-8 でない名前があるときの並び順 (APFS では作れない)。

## 進捗

### 1 本目 (部品。2026-11-15)

実装の分け方の 1 本目 (部品) だけを入れた。2 本目以降 (diff の群 AE・書き込みと入力・run・verify・start / end / export) は未着手。`ja-writing check` の `Main` はまだ stub。

- commit (5 本。本家の関数の一覧と本家の行は作業の中の `check-wf1/inventory.md`。入れた関数は `check/check.go` の package の冒頭のコメントの対応表と README の check の節にある)
  - 466928418 str.casefold・lower・swapcase を全コードポイントの記録から作った表で写す (`internal/pyunicode/case.go`・`case_gen.go`・`testdata/golden/casemap.json`。語末の Σ の規則は Python が性質を出さないので「`<字>Σ` と `a<字>Σ` の小文字化」から cased と case_ignorable の範囲を求めた)
  - 1ef27942c float(str) の受ける形と十進数字の値 (`pystr.Float`・`pystr.DecimalInt`・`pyunicode.DecimalValue`。pystr.json に floats 4,000 文字列、casemap.json に十進数字 770 字を書き足した)
  - 73af63624 pyjson の名前つきの設定 `Indent1`・`Compact`・`Plain` (本家 check の json.dumps の 3 通り)
  - 32eeb994b check の部品 (`check/util.go`・`paths.go`・`similar.go`・`tools.go`)・`visibility` の os.path・pathlib の写しの公開 (`pathexport.go`)・記録 `check-base.json`・README の check の節
  - f8f6578c1 コメントから issue の番号を外し (test-comment-provenance)、`src/PACKAGES.md` を作り直した (test-go-packages-index)。コードは変えていない
- 記録: `check-base.json` 35 case・1,786 呼び出し (大きさ 150 KB)。小道具 (ハッシュ・実行 ID 80 個・記録の digest 15・状態の行 23・環境の値・時刻 63 と --since 50・--fail-on 37・--timeout 72)・似かた (compact 52・similarity 24・source_like 21。しきい値 0.53 の前後は 2 字組の数を決めた入力)・パス (fixture 6 本・931 呼び出し。実行 ID の比べの fixture は別に 6 本・63 呼び出し。大文字小文字だけ違う綴り・NFD・シンボリックリンクと輪・読めないファイル・消えた作業フォルダ・HOME の値 15 通り・環境変数の値 16 通り・`.git` の置き場)・tools の欄 (`_HERE` を fixture に差し替えた 9 本・本物の 1 本)
  - 全部 Go が本家の digest と一致 (`TestBaseMatchesPythonRecording`。呼び出し数と例外の数も関数ごとに比べる。手元の race で check パッケージ 1.3 秒)。casemap は 3 つの表 × 全コードポイント (サロゲートを除く 1,112,064 字)・文字列 58 本・十進数字 770 字、floats は 4,000 文字列 (受けた 535・受けない 3,465) を全部比べた。pystr.json と casemap.json は書き足しだけ (git diff の削除行 0)
  - 手元の race (`go test -race -json`) の所要: check 1.3 秒・internal/pystr 1.5 秒・oracle 11.8 秒・internal/pyunicode 40 秒 (うち全コードポイントの 2 テストが 10 秒と 24 秒)。CI の上限 600 秒の半分を超えないので `NORACE_CHECKS` には足していない
- わざと壊した形 32 本 (`bin/mutate-verify-list`。全部 commit f8f6578c1 の木で、rc 0 = 期待の red。一覧は作業の `check-wf1/mut/list.sh`・結果は `run3.out`)。壊した所 (群ごと)
  - 小道具 12: 末尾の改行を全部外すところを `TrimSpace` に・末尾に改行を付けない・実行 ID の長さ 64 → 65・予約名の大文字小文字の区別・`--fail-on` の重複を外さない・`--timeout` の 0 を受ける・`--since` の時差の上限 (+24:00)・秒 60 を受ける・`_stamp` の書式から Z を外す・record_digest を自分自身の欄ごと計算・状態の行の attempt 0 を「-」にしない・環境の値の `${` を断らない
  - 似かた 4: しきい値を `>=` から `>` に (同点 0.53)・min を max に (比べの向き)・同点で後の資料が勝つ・書式の文字 (Cf) を外さない
  - パス 8: casefold しない・NFC にしない・大文字小文字の判定で swapcase を使わない・HOME がルートだけのときを断らない・`.git` の名前・環境変数の `${` を断らない・絶対パスを整えない・実行 ID の大文字小文字の比べを casefold から `strings.ToLower` に
  - tools の欄 4: ENOTDIR を「無い」にしない・キーはあるが dict でない値を「無い」にしない・バージョンの行の検索から MULTILINE を外す・定数の SHA-256 の 1 字違い (upstream.sha256 との検査)
  - 標準ライブラリ 4: 語末の Σ の判定の向き・swapcase に Σ の規則を使わない・`float()` の ASCII の `\x1c` を空白に数える・十進数字の値の mod 10 → 9
  - 1 回目は 3 本 (fail-on-dedupe・home-path-root-check・tools-notdir-absent) が構文エラー (使わなくなった import と変数。rc 5) だったので、壊し方を直して当て直し、最後の木で 32 本とも rc 0
- 本家と揃わなかった所: なし (記録の比べは全部一致。ただし `python.version` と `python.implementation` の欄を揃えない決定は README の「揃わない範囲」の表に足した)
- 確かめていないこと
  - 大文字小文字を区別するファイルシステム (手元に無い。`_case_insensitive` が偽を返す枝は `/` と数字だけのパスで通した)・`HOME` が無くパスワードデータベースから決める枝・`EACCES` で読めない作業フォルダの `strerror` の文・`_disk_path` の NUL (本家は ValueError)・`SKILL.md` がある場合の `_skill_info` (ja-writing は常に found=false に決めた)
  - `DirFiles` の `decode("utf-8", "replace")` (バージョンの値に不正なバイトが入る入力)
  - まだ写していない関数 (`_materials_format_version`・`_version_line`・`_utc_now`・`_file_sha256`・`_label`) は後の本が足す
- 検査 (最後のコードの編集 = f8f6578c1 の木。rc は全部 0): `make -C src/ja-writing lint test`・repo の root で `make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` (ログは作業の `check-wf1/fin-*.log` と `testlint2.log`)
  - commit ごと: 466928418 は lint・test・unused が rc 0 (dupscan gate は赤。`CaseRow` と `caseLookup` の写し 2 件。32eeb994b が直した)。1ef27942c は lint・unused が rc 0 と pystr・pyunicode・oracle のテストだけ (`make test` の全体は回していない)。73af63624 は pyjson のテストだけ。32eeb994b は lint・test・unused・dupscan が rc 0 だが、`make test-lint` が赤 (コメントの issue の番号 3 行と PACKAGES.md。f8f6578c1 が直した)
