# 1113 (research): ja-writing の実装を audit skill の全タイプで監査する

起票日: 2026-11-26

## 概要

ja-writing の実装を、audit skill の 23 タイプで 1 つずつ直列に監査した (ユーザー指示 2026-11-26。直接実行。test-cleanup と test-helpers は skill どおり書き換えてよい)。
発見は 3 件で、それぞれ issue にした (1114・1115・1116)。ほかに、起票しない記録が 3 件ある (下の「起票しない記録」)。

## 対象と読み方

- 対象: `src/ja-writing` (Go)・`bin/ja-writing`・`bin/ja-textlint`・`_claude/hooks/ja-textlint-on-edit.sh`・`_claude/hooks/ja-writing-hook.sh`・`_claude/skills/ja-writing/textlint/rules/`
- Go の本体の大きさ (テストを除く): 約 9.7 万行。うち生成した表 (`internal/pyunicode`、約 2.5 万行) と本家の記録を取る道具 (`oracle`、約 2 万行) を除く手で書いた部分が約 5 万行。テストは約 1.8 万行
- 全部を 1 行ずつは読んでいない。タイプごとに、module 全体を母集合にした機械の道具 (deadcode・dupscan・go test -list・依存の一覧) と、そのタイプの壊れ方が出る形の grep で読む場所を絞って読んだ。どこまで見たかは下の表に書く
- 前提: ja-writing は本家 yomiyasu と同じ出力 (標準エラーと rc まで) を返すことが契約 (src/ja-writing/README.md の「揃わないことを認める範囲」)。本家と同じ挙動は不具合として数えず、本家との差と、Go の外の部分 (入口のスクリプト・hook・dotfiles への組み込み・検査の作り) を見た
- 同じタイプの過去の監査: `issues/audit-log` に ja-writing を対象にした行は無い

## タイプごとの結果

| タイプ | 結果 | 見た範囲と方法 |
|---|---|---|
| broken-code | 0 件 | 入口のスクリプト 4 本を全部読んだ。check が lint と diff を子として起こす境界 (`check/children.go`) を読んだ。settings.json の hook の timeout と `--print-config` の値を 5 イベントで突き合わせた (一致。`tests/claude/test_ja_writing_hook.sh` が見ている) |
| dead-code | 1 件 → 1116 | `deadcode` (module の全部の `main` から) で 115 件。テストの道具の `oracle` の 56 件を除く 59 件が製品の package にあり、全部がテストからだけ呼ばれる入口と下請け |
| error-handling | 0 件 | production の Go 180 ファイルで値を捨てる行 (行頭の `_ =`・`_, _ =`) 21 か所を読んだ (ほかに `strings.Cut` の使わない戻り値が 2 か所)。どれも本家と同じ扱いだとコメントに書いてある (`check/hook.go` の `fail` と `appendErrorLine` は「本家の except BaseException」の写し) |
| false-green | 1 件 → 1115 | `//go:build !race` のテスト 14 本と `NORACE_CHECKS` 13 本を突き合わせた。記録を読むテストに、空振りを止める検査 (記録 0 件・件数の下限) が多数ある (テストのファイルで「0 件」か `len(g.…)` の比べに当たる行が 38 行) |
| concurrency | 0 件 | check の登録とロックは専用のテストがある (`TestConcurrentStartLeavesOneActiveRegistration` など)。hook-errors.log の書き込みに排他が無い (下の記録) |
| resource-leaks | 0 件 | 子は時間切れで Kill してから Wait で回収し、孫を作らない。`bin/ja-textlint` の npm のキャッシュの残り (下の記録) |
| security | 0 件 | 外から来る値 (hook の tool_input のパス・PreToolUse の gh のコマンド・語の一覧の正規表現) は、位置引数で渡す・試す数の上限 (`PATTERNS_MAX_COST`) と 1 秒の上限で抑える。npm ci は `--ignore-scripts` |
| performance | 0 件 (新しく測っていない) | issue 1079 で 10 周の計測とプロファイルを回した直後。残りの候補 (どれも全体の 5% 未満) は 1079 の「結果」にある |
| design | 0 件 (提案 1 件、下の記録) | module の中の import の向きを `go list` で並べた。輪は無い |
| responsibility | 0 件 | hook を check に置いた理由は README にある (テストの道具を共有する)。ファイルの分け方は本家のファイルに揃えてある |
| duplication | 0 件 | dupscan (節の下限 12) で ja-writing を含む写しの組は 0。ja-writing の関数が索引に入っていることを、既知の名前 (`popenEncode`・`AdditionRisks`) で確かめた |
| polymorphism | 0 件 | host (`claude-code`・`codex`) とイベントの分岐は本家が決めた閉じた集合なので数えない |
| leaky-abstraction | 0 件 | interface は `check.ToolFiles` など少数で、具象の名指しの分岐は見つからなかった |
| encapsulation | 0 件 | 所有者の外で対の値を更新する形は見つからなかった |
| entry-parity | 1 件 → 1114 | 同じ操作の入口: `bin/ja-textlint`・hook `ja-textlint-on-edit.sh`・`bin/ja-writing lint`。語の一覧の探し方が `bin/ja-textlint` (cwd) と hook (ファイルのフォルダ) で違う。使い捨ての repo で実測した |
| ui-components | 対象外 | 画面を持たない CLI |
| ux | 0 件 | 利用者に見える文は、本家と同じ文 (契約) と `ja-textlint`・hook の案内。案内に食い違いは見つからなかった |
| test-cleanup | 0 件 (消したテストなし) | `func Test…(t *testing.T)` の 267 本 (行頭の `func Test` は 271 本で、差は TestMain など) のうち、assert を持たないものは 0 本 (`t.Error` 系・helper への `t` の受け渡し・`t.Run` の有無で数えた) |
| test-helpers | 0 件 (寄せたものなし) | dupscan はテストの写しも数え、ja-writing のテストに写しの組は無い |
| dependency | 0 件 (npm は未確認) | Go の依存の過不足は `make lint` の `go_module_graph.sh` (`go mod tidy -diff`) が止める。npm の依存の既知の脆弱性は `npm audit` (ネットワークを使う) を回していないので未確認 |
| ci | 0 件 | `tests/bin/test_ja_textlint.sh`・`tests/claude/test_ja_textlint_on_edit.sh`・`tests/claude/test_ja_writing_hook.sh` が CI の Tests の run 45803891070 で走ったことをログで確かめた。Go のテストが読むファイルは module の中だけで、`src_ja-writing.yml` の paths に入っている |
| lint-from-done | 0 件 | 今回の発見から機械で止められる形は 1115 の直し方そのもの (一覧を手で書かない) |
| general | 上の 3 件 | |

## 起票しない記録 (再提起するなら、ここの理由を崩す実測を添える)

- **hook-errors.log の書き込みに排他が無い** (`check/hook.go` の `appendErrorLine`): 読む → 先頭へ戻る → 書く → 切り詰める、の間に別の hook が書くと 1 行を失う。
  Go のコメントは本家の `except BaseException: pass` の写しだと書く。本家の `yomiyasu_hook.py` は repo に写しが無く、本家に排他があるかは確かめていない (README の「hook の揃わない範囲」も排他には触れない)。
  失うのは診断の行だけで、登録と検査の記録には触れないので起票しない。本家を読んで排他があったら、本家との差として起票する
- **`bin/ja-textlint` の npm のキャッシュが残る**: 入れかけ (`.staging.XXXXXX`) は npm ci の途中で止めると消されない (trap が消すのは作業フォルダだけ)。
  lock の hash ごとのキャッシュ (1 つ 133MB) は、古い hash のものを消す経路が無い。lock を変えた commit は今まで 1 回で、溜まる速さが遅いので起票しない。lock を上げる作業が増えたら再評価する
- **`os.path` と `OSError` の写しが `visibility` にある** (design の提案): `check` が `visibility.PathJoin` を 63 回呼び、標準ライブラリの写しの `internal/pycli` も `visibility` を import する。
  置いた理由は `visibility/pathexport.go` の冒頭 (規則を 2 つ持たない)。`internal/` に専用の package を切っても同じ目的は満たせるが、書き換えが 100 か所を超え、壊れても記録の突き合わせが落とす (気づかないうちに壊れない)。次に `visibility` の path の写しを触る機能が来たら、そのときに移すかを決める

## 進捗

- 2026-11-26: 23 タイプを直列に回した。発見 3 件を 1114・1115・1116 に起票した
- 2026-11-26: 4 本を反証レビュー (opus) に通し、直した。1116 の件数 (根の `main` だけから辿っていた。61 件 → 59 件)、1114 の直し方 (相対パス)、1115 の守る範囲 (skipped)、この issue の数と hook-errors.log の根拠の書き方
