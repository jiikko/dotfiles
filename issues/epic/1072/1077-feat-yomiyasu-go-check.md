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
- [ ] 上の「決めること」の 3 つを決め、理由をこの issue と README に書く (issue には「決めたこと」に書いた。README は実装の 1 本目で書く)
- [ ] subcommand ごとに最低 1 つ、実装を外して落ちることを確かめた (`mutate-verify`)
- [ ] `make test-dupscan-gate` が通る

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_check.py`
- 1075 (lint)・1076 (diff)・1078 (hook が check を読む)

## 進捗

- (未着手)
