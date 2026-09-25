# 471 (feat): 重い処理 (make test 等) を、pro-con の外の session・PG が自分で走らせる分も含めて直列にする

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの質問 (2026-09-25): 「作業者レーンの中のカードが make test を実行しているけど、リソースを占有する系は、直列になるように調整できているの？」。
**pro-con の中では直列になっているが、外と PG の自前の分は直列になっていない**。

## 今の形 (2026-09-25 20:43 に ps で確かめた)

- ✓ PG が `pro-con card run <カード> -- <コマンド>` で頼んだものは、テストの係が 1 本ずつ流す (426 の決定 5。占有の資源 `store.RunResource = "テスト"`。
  カードに「待ち: テスト の順番待ち (N 番目)」と出る)
- ✗ **pro-con の外の session**: 同じ時刻に repo 全体の `make test` が 2 本走っていた。1 本はテストの係 (C-010 の worktree)、もう 1 本は dotfiles-7d の session が自分の worktree で回したもの。
  pro-con からは見えず、順番に入らない
- ✗ **PG が自分で走らせる検証**: `bin/mutate-verify` (変異ごとに go test を回す) を、4 本の PG がそれぞれ並べて走らせていた。PG への指示 (`dispatcher.Prompt`) は
  「make test・ビルド・実機 E2E など時間のかかるコマンド」だけを card run に回すよう言っている
- 結果: 14 コアのマシンでロードアベレージ 25 (1 分)。時間に敏感なテストは負荷の下で落ちうる (2026-09-25 の codex_fanout の flaky は負荷の下で落ちた形)
- 資源の種類は「テスト」1 つだけ。repo ごと・種類ごと (iOS の xcodebuild・シミュレータ・実機 E2E) の占有は分けていない (`no-concurrent-spm-build-during-xcodebuild.md` の形は他の repo で要る)

## 対応方針 (候補)

- テストの係が走らせるコマンドを、ホストの排他 (`bin/lockman with <dir> -- <cmd>`。dotfiles にある、ディレクトリ単位でセッションをまたいで排他を取る CLI) で包み、
  人・外の session も同じ lock を取る形にする (例: dotfiles の `make test` そのものが lock を取る)。取る lock のディレクトリ (repo ごと / 種類ごと) を決める
- PG の変異の検証 (`bin/mutate-verify`) を card run に回すか、同時に走る数の上限を持たせる (PG の数と CPU のコア数から)。回すと PG の作業が遅くなるので、どちらかを実測で決める
- 資源の種類を repo の設定で足せるようにする (「テスト」「xcodebuild」「実機」)。カードの待ちに種類を出す

## 関連

- 426 (決定 5: テストの係) / 455 (待ちの表示) / `bin/lockman` / 2026-09-25 の dogfooding (440)
