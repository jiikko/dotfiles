# 130 refactor: ローカル入口に残る fail-fast (test-changed / test-go / トップレベル test)

起票日: 2026-08-28 / 出典: issue 109 の敵対的レビュー / 優先度: P3 (CI には影響しない)

## 事実

issue 109 で `test-runtime` / `test-runtime-rest` / `test-lint` / `run_tests` / `test-bats` を
集約へ変えたが、**同じ短絡が残っている入口が 3 つある**。いずれも CI からは呼ばれないので
P3 だが、**ローカルの方が CI より弱い**状態になっている。

| 箇所 | 形 | 隠れるもの |
|---|---|---|
| `Makefile` の `test:` | `test: test-lint test-runtime test-src` | lint が 1 つ落ちた日は**テストが 1 本も走らない** |
| `Makefile` の `test-go-lint` / `test-go` | `src/*` を `\|\| exit 1` で回す | 最初のプロジェクトの失敗が残り 4 つを隠す (CI は `src_*.yml` でプロジェクト別なので無傷) |
| `scripts/test_changed.sh:203-211` | `for` ループ 3 連が全て `\|\| exit 1`。最終行は `make -C "$d" lint test` の **1 make に 2 goal** | 触った範囲の検証が途中で止まる。`lint` が落ちると `test` が走らない |

## なぜ 109 で直さなかったか

109 のスコープは「CI で bats が隠れる」で、上記はいずれも **CI から呼ばれない**
(`tests.yml` は `test-discovered-heavy` / `test-runtime-rest` を、`lint.yml` は `test-lint` を、
`_go-project.yml` は lint / test を別ジョブで叩く)。

ただし `make test` は**コミット前ゲートとして常用される入口**なので、
「lint が 1 つ落ちた日はテストが 1 本も走らないまま赤を見る」形は体験として悪い。

## 対応

`run_all_targets` (109 で追加済み) をそのまま使えば `test:` は 1 行で直る。
`test-go-lint` / `test-go` と `test_changed.sh` は失敗を集めてから返す形へ。

⚠️ `test:` を集約にすると **lint が落ちていてもフルスイートが走る**ので所要時間が伸びる。
「lint が落ちたら即止まってほしい」という運用の方が好ましい可能性があるので、
**着手時にユーザーへ確認すること**。
