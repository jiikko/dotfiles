# 997 (ux): Go の testdata の壊れた fixture で gate が毎回「解析が不完全」と出る

起票日: 2026-10-30

## 概要

dotfiles の `src/subproc/boundarycheck/testdata/broken/broken.go` は、検査の道具のテストのためにわざと構文を壊したファイルである。
dupscan はこれも Go のファイルとして読むので、`dupscan gate src` (make test・CI・セルフレビュー) の 1 行目は毎回
`新しい重複 0 (読めたファイルの中で)・解析が不完全 (… 読めない構文のファイル base 1 / head 1 …)` になる (2026-10-30 の自分の gate でも出た)。

読めない構文のファイルを 1 行目に出すのは [929](929-bug-dupscan-gate-lost-file-parse-errors.md)・[959](959-research-dupscan-dogfood-feedback-obaket-1307-part3-2026-10-29.md) で決めた形で正しい。
困るのは、壊れていて当然のファイルが 1 つあるだけで、1 行目の状態の語が毎回「解析が不完全」になること。件数 (`base N / head N`) は出るので増えれば数字は変わるが、状態の語が常に同じなので読み流される。
[972](972-research-dupscan-dogfood-glogx-tui-split-2026-10-29.md) の F2 で `index -file` の側は「関係しうるファイルだけ」に直したが、gate と report は直していない。

## 発火条件

Go の `testdata` の下に読めない構文の `.go` があり、gate / report の起点に入る (dotfiles の `src` を起点にするとき毎回)。`go build` / `go vet` は `testdata` を読まないので、そこが読めないことは製品の写しの検査と関係しない。

## 対応方針

- 変えるのは **text の 1 行目の状態の語だけ**: 読めないファイルが全部 Go の `testdata` の下 (go のツールが読まない置き場) なら、状態を「新しい重複なし」にし、
  後ろに `・読めない testdata N (go が読まない置き場。-strict-analysis では数える)` と出す。testdata の外に 1 つでもあれば今までどおり「解析が不完全」
- **rc・json (`analysis_incomplete` / `*_files_with_errors`)・`-strict-analysis` は変えない**。[929](929-bug-dupscan-gate-lost-file-parse-errors.md) は
  この fixture で strict が rc=2 になることを検証済みの挙動として記録し、「既存の rc 契約は黙って変えず」とした (反証レビューの P2)。契約は保ったまま、人が読む語の雑音だけを減らす
- report の 1 行目 (`読めない構文があるファイル N`) も同じ語の扱いにする
- 採らない案: testdata を既定で読まない (972 の F2 で退けた形。読めないことを黙らせる) / gate の「確かめられなかった変わった単位」で状態を決める (929・959 の「読めないファイルがあるのに なし と言い切らない」を、testdata 以外にも緩めることになる)
- Swift には同じ慣習が無いので変えない

## 受け入れ条件

- [x] testdata の下の壊れたファイルだけがある repo で、gate の 1 行目が「新しい重複なし」で、testdata の数が別に出ることをテストにする (直す前に赤)。json の `analysis_incomplete` と strict の rc=2 は変わらないことも見る
- [x] testdata の外の壊れたファイルは今までどおり「解析が不完全」になる (対照)

## 進捗

- commit「fix(dupscan): 読めないファイルが Go の testdata の下だけなら、gate と report の 1 行目を「解析が不完全」にしない (issue 997)」
  - `lang.Adapter.ToolIgnores` (その言語のツールが読まない置き場。Go は `testdata` の下) と `langs.ToolIgnoredOnly` を足し、gate は base と head を合わせて、report は読めないファイルが全部そこにあるときだけ
    1 行目を `新しい重複なし …・読めない testdata N (go が読まない置き場。-strict-analysis では数える。-v で一覧)` にする。ファイルの一覧は `-v` でだけ出す
  - json の `analysis_incomplete` / `*_files_with_errors`・xcode・`-record`・`-strict-analysis` の rc=2 は変えていない (929 の契約)。strict のときは 1 行目も今までどおり「解析が不完全」 (敵対的レビューの P2: rc=2 で止めるのに先頭が「なし」だと失敗に見えない)
  - dotfiles の `bin/dupscan gate src` の 1 行目が `新しい重複なし (…)・…・読めない testdata 1 (…)` になったのを確かめた
  - テスト `testdata_errors_test.go` 3 本 (testdata だけ・testdata の外の対照・report)。変異 (狙ったテストだけが赤): gate の判定を外す / report の判定を外す / 置き場の判定で常に真 / strict の除外を外す。
    `ToolIgnoredOnly` の「空なら偽」を常に真にする変異は緑のまま (呼び出し側が 2 か所とも読めないファイル 0 件で先に抜けるので届かない。等価な変異)
  - `make -C src/dupscan lint test` rc=0
- 敵対的レビューで記録だけにしたもの: 文言の「testdata」「go が読まない置き場」は Go に決め打ち (`ToolIgnores` を 2 つ目の言語が使い始めたら文言を adapter から引く) /
  ~~near・sweep・callers の解析の警告は今までどおり testdata も出す~~ → 下の「探索のコマンドへの横展開」で揃えた

## 探索のコマンドへの横展開 (2026-10-30、ユーザー指示「探索コマンドの警告も揃えて」)

- commit「fix(dupscan): 探索のコマンドの解析の警告も、Go の testdata の読めないファイルを解析不完全に数えない (issue 997)」
  - `callers` / `def` / `refs` / `index` の stderr (`warnParseErrorsAny`): testdata の下の読めないファイルは解析不完全の全文にも「…を含まない」の 1 行にも数えない。
    問い合わせの名前を含むときだけ `読めない testdata N 個 (go が読まない置き場) に問い合わせの名前がある。そこの定義・呼び出しは欠けうる` の 1 行。
    名前で絞れない問い合わせ (`index -returns` だけ・`init` の部分文字列) では「名前がある」と言わない (敵対的レビューの P2)。`index -file` で名指ししたファイル自身は testdata の下でも全文
  - `search` / `context` / `evidence` の 1 行目の「構文解析失敗: N ファイル」は testdata を除いて数え、`(ほかに読めない testdata M)` を添える。`context` / `evidence` の「読めない構文を含むファイルが」の行も testdata を除く
  - `sweep` の先頭行は `読めないファイル N・読めない testdata M (go が読まない置き場)`
  - 変えていない: `near -similar` と `explain -compare` の `parse_error_files` (json の欄と同じ値)、各コマンドの json の stdout
  - dotfiles の実物: `index -name broken src` は今まで毎回出ていた「読めないファイル 1 個は問い合わせの名前を含まない」を出さなくなった (fixture の中身は `package` の 1 語)。`sweep src/subproc` の先頭行は `読めないファイル 0・読めない testdata 1`
  - テスト `query_testdata_errors_test.go` 2 本と、972 の `index_file_notice_test.go` の期待の更新 (名前を含まない testdata は 1 行も出さない)。
    変異 (狙ったテストだけが赤): testdata の分け方を外す / `index -file` の名指しを外す / 名前を含むときの 1 行を出さない / 名前で絞れないときの言い方を外す
  - `make -C src/dupscan lint test` rc=0
- 敵対的レビューで記録だけにしたもの: 起点そのものが testdata の中 (`callers Foo pkg/testdata/x`) でも testdata の扱いになる (「欠けうる」とは言うが「0 件は不存在の証明ではない」は付かない) /
  `search -file testdata/x.go` には名指しの経路が無い (`index -file` と揃っていない) / 読めないファイルが testdata の中と外に混ざる場合を固めるテストは無い (コードでは件数は外の分で正しい)。
  **再提起の条件**: testdata の中を調べる問い合わせで、警告が足りずに読み違えた実例が出たとき
