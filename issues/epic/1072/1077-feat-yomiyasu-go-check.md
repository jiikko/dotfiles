# 1077 (feat): yomiyasu の check (検査の記録と最終稿の照合) を Go で作り直す

起票日: 2026-11-12

## 概要

本家の `yomiyasu_check.py` (5,639 行) を `yomiyasu check` として作り直す。最終稿に lint を、原文と最終稿に diff をまとめて掛ける道具。
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

## 受け入れ条件

- [ ] `run` / `verify` / `start` / `export` / `end` が入力の集まりで本家と同じ出力と rc を返す。違ってよいのは 1073 の表で置き換えた字だけ
- [ ] `verify --replay` が lint と diff を動かし直し、本家と同じ結果を返す
- [ ] 状態フォルダの選び方・書けないときの振る舞いが本家と同じ
- [ ] 上の「決めること」の 3 つを決め、理由をこの issue と README に書く
- [ ] subcommand ごとに最低 1 つ、実装を外して落ちることを確かめた (`mutate-verify`)
- [ ] `make test-dupscan-gate` が通る

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_check.py`
- 1075 (lint)・1076 (diff)・1078 (hook が check を読む)

## 進捗

- (未着手)
