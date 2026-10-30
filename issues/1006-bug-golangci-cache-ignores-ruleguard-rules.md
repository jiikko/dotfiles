# 1006 (bug): golangci-lint の結果キャッシュが ruleguard の規則のファイルの中身を鍵に入れず、規則の変更が手元で効かない

> 🚨 **担当中: dotfiles-2e (lintcache)**（2026-10-30〜）

起票日: 2026-10-30

## 概要

ruleguard の規則のファイル (`src/<module>/gorules/rules.go`) は、合成した設定からパスで指されるだけで、中身が golangci-lint の結果キャッシュの鍵に
入らない。手元では、規則を足しても既にキャッシュ済みのパッケージに新しい規則が効かず黙って緑になり、壊れた規則で一度回すと、戻しても
`empty rule set` の失敗が出続ける (issue 1005 で踏んだ)。CI は結果のキャッシュを持ち回らないので起きない。

## 詳細 (実測。2026-10-30、golangci-lint v2.15.0、treefiler、run ごとに別の空の GOLANGCI_LINT_CACHE)

| 実験 | 同じキャッシュ | 空のキャッシュ |
|---|---|---|
| E1: 規則 (rawViaSetRaw) を足す前の rules.go と違反で回した後、規則を足して回す | 違反を見つけない (0 件) | 見つける (1 件) |
| E2: 読み込みに失敗する規則で回した後、rules.go を戻して回す | `empty rule set` が 89 件残る | 0 件 (通る) |
| E3: .golangci.local.yml の forbidigo に禁止を 1 行足す | 効く (8 件) | 効く (8 件) |
| E5: 合成した設定のトップに知らない欄を足し、値を変える | 効かない (鍵に入らない) | — |
| E6: ruleguard の設定の disable に存在しない group 名を入れ、値を変える | 効く (1 件。エラーも規則を外すことも無い) | — |

linter の設定は鍵に入るが、設定がパスで指すだけの ruleguard の規則のファイルの中身は入らない。CI は結果のキャッシュを持ち回らないので起きない (手元だけ)。

## 対応

- `scripts/golangci_ruleguard_key.rb`: 合成した設定の ruleguard の rules (go-critic と同じく glob。先頭の . のファイルも当てる) のファイルの中身の
  sha256 を、disable に `dotfiles-rules-<16 桁>` として入れる。既存の disable は残し、2 度通しても重ならない。欄の名前は大文字小文字を問わない。
  golangci-lint が置き換える変数 (`${base-path}` など) を含む・1 つも当たらない rules は rc=2 で止める (固定の値を hash すると鍵が中身に追いつかない)
- `scripts/golangci_lint.sh` が golangci_config.rb の後に通す (set -euo pipefail の下)
- 一覧と入口: check_go_project_lanes.sh の共有の script・26 本の workflow の paths (52 か所)・scripts/test_changed.sh の写像・src/README.md の雛形と説明・
  scripts/CLAUDE.md
- テスト: tests/scripts/test_golangci_ruleguard_key.sh (14 項目)、test_golangci_lint_pinned.sh の fixture に ruleguard の規則を置き、渡した設定に鍵が入ることを見る

## 進捗

- [x] 直した入口で E1・E2 をやり直した: 同じキャッシュで新しい規則が効き (1 件)、壊れた規則を戻せば通る (rc=0)
- [x] 敵対的レビュー (opus、3 周)。1 周目 P2 2 件・P3 4 件: rules の glob と変数で鍵が中身に追いつかない・test_changed.sh の写像の漏れ・
  結果を捨てる配線を見逃す・2 つ目のファイルを無視する退行を見逃す・README の雛形 → 直した。2 周目 P3 4 件: 1 つ目のファイルだけを hash する変異を
  見逃す・変数の分岐を区別しない・隠しファイルで go-critic とずれる・欄の大文字小文字 → 直した。3 周目 低 3 件: 既存の Disable の欄のテストの穴 → 直した
- [x] 変異で red を確認 (bin/mutate-verify): golangci_lint.sh から鍵のスクリプトの行を消す・結果を捨てる (`>/dev/null`) / 中身を hash しない /
  glob を展開しない / 1 つ目のファイルだけ / 隠しファイルを当てない / 変数の分岐を消す / 欄の名前を大文字小文字で区別する / disable の欄を固定の名前で引く

## 記録 (直さない)

- 規則のファイルが dsl.ImportRules で取り込む別の規則の束は鍵に入らない (今の 3 本の規則はどれも dsl しか import しない)
- `rules` と `Rules` のように大文字小文字だけ違う欄が同じ map に 2 つあると、golangci-lint の読み込み (viper) が run ごとにどちらを残すかを揺らす
  (今回の差分の前からある。直すなら golangci_config.rb の重複の検査を大文字小文字を区別しない比較にする)
- `**`・`{a,b}` は Ruby だけが展開する (Go の Glob は解釈せず「no file matching」で lint ごと落ちるので、黙って緑にはならない)
