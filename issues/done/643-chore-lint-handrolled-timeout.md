# 643 (chore): 時間の上限付き実行を runtimeout 以外で新しく書いたら lint で落とす

起票日: 2026-10-07

## 概要

issue 640 で、上限付きの実行 (coreutils の `timeout` / `gtimeout`、背景起動 + kill の手組み) を `bin/runtimeout` に寄せた。
新しく同じ形を書かれるのを止めているのは CLAUDE.md の 1 行だけで、機械の検査が無い。
`make test-lint` に入る検査 `scripts/check_handrolled_timeout.sh` を足す。

## 対応方針

- 検出する形 (脅威モデル: うっかり昔の書き方で書くこと。意図的な迂回は対象外):
  - `timeout` / `gtimeout` をコマンドとして呼ぶ・`command -v` で探す (macOS に無く、無いと黙って上限なしになる。bench_zsh.sh がこの形だった)
  - `( sleep N; kill … ) &` の見張り (scripts/check_assert_reaches_exit.sh の旧 run_probe の形。pid の再利用で別のプロセスを撃つ)
- 検出しない形はスクリプトの冒頭に書く。意図的な例外は行内の `handrolled-timeout: allow` (理由を添える)
- 対象 0 件・発見の失敗は緑にしない (既存の check_pipefail_grep_q.sh と同じ規律)。fixture のテストで検出することを固定する

## 関連ファイル

- `scripts/check_handrolled_timeout.sh` (新設) / `tests/scripts/test_check_handrolled_timeout.sh` (新設) / `Makefile` (test-lint)
- `scripts/lib/shell_code_lines.awk` (新設。heredoc とコメントの読み方の共有) / `scripts/check_pipefail_grep_q.sh` / `scripts/check_test_sleeps.sh`
- issue 640

## 進捗

- [x] 検査の実装と test-lint への配線 — chore(lint) の commit
- [x] fixture のテストと変異 — 同じ commit

## 結果 (2026-10-07)

- 検査: `scripts/check_handrolled_timeout.sh`。検出は gtimeout / timeout を探す / timeout を呼ぶ (オプション・前置きの代入・heredoc の開始行の前後も) /
  `( sleep; kill )`・`( sleep && kill )`・`{ sleep; kill; }` の見張り。検出しない形 (変数経由・`kill -0` の待ち・複数行の見張り・.bats) はヘッダ
- 旧の発見力: issue 640 の前の tree に当てると、640 で直した箇所を落とす (`bench_zsh.sh` の黙った縮退・`run_probe` の見張り・
  hook のテストの timeout / gtimeout の探索・`test_go_autobuild.sh` の素の `timeout 5`)
- heredoc とコメントの読み方を `scripts/lib/shell_code_lines.awk` に寄せた。`check_test_sleeps.sh` の `heredoc_tag` (here-string と算術を除く、強い方の実装) を
  移して 3 つの検査で共有し、pipefail 検査と本検査は「終端のタグの行が後ろに在るときだけ heredoc とみなす」形で読む
  - **pipefail 検査の既存の穴を塞いだ**: 旧実装は here-string の `<<<` を heredoc の開始と読み、9 本のファイルの後半を黙って検査していなかった。
    塞いだら本物の `| grep -q` が 6 か所出たので直した (`tests/claude/test_warn_discarding_checkout.sh` 5 / `scripts/check_go_project_lanes.sh` 1)
  - `check_test_sleeps.sh` は HEAD の版を正解役に tests/ と Go 全体で出力が一致 (敵対的レビュー 2 周目で実測)
- 変異で red: 各検出の規則 (gtimeout / 探す / 呼ぶ / 見張り) / 例外の印 / コメント行の除外 / heredoc の除外 / 終端の先読み / 開始行の前半・後半 /
  `&&` の見張り / `{ }` の見張り / オプションの値。緑のまま: here-string の除外を外す (タグの読み取りで `<` が区切りになり、もともと heredoc にならない。
  check_test_sleeps.sh から移した分岐で挙動は変えていない)
- 検証: worktree で `make test-lint` 緑 / `tests/scripts/test_check_handrolled_timeout.sh` / `test_check_test_sleeps.sh` 40 件

## 敵対的レビュー (2026-10-07、read-only のサブエージェント、opus、2 周)

- 1 周目 P1 1 / P2 2 / P3 3。全部採用: here-string で後半を読み飛ばす (上) / heredoc の開始行の前半を見ていない / 見張りの `&&`・`{ }`・算術 /
  timeout のオプションの値・`${T:-5}`・絶対パスの gtimeout / 発見の find の rc を捨てていた (pipefail 検査も同じだったので両方直した) /
  行の途中のコメント・文字列の中の gtimeout は偽の red (安全側。ヘッダに記録)
- 2 周目 P1 0 / P2 0 / P3 3。採用: 開始行のタグより後ろも検査する / `<<` を含まない行は走査しない (pipefail の awk 部分が 0.77 → 2.86 秒になっていた)。
  記録のみ: 複数行の引用符の中の `<<EOF` の後ろに本物の heredoc がある形は、間の行が黙って抜ける (行をまたいで引用符を追う設計変更が要る。
  repo に該当なし。shell_code_lines.awk の「検出しないもの」)。直した 2 点は判定を足さず実測で確かめたので 3 周目は回さない

## 残タスク

- なし
