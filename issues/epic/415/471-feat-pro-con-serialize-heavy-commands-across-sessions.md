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

## 決めたこと: 取る lock のディレクトリ (2026-09-25)

- **`<git の共通ディレクトリ>/pro-con-locks/<種類>/`**。今の種類は `test` 1 つ (`dispatcher/runner.go` の `runLockDir`)
  - 共通ディレクトリ (`git rev-parse --git-common-dir`) は**同じ repo のどの worktree から見ても同じ**なので worktree をまたいで 1 つの lock になり、
    **repo が違えば別の lock** になる (repo ごとの直列)。`.git` の中なので working tree に lock の残骸 (`.lockman/`) を置かず、`git status` も汚さない
  - 種類を分けるとき (xcodebuild・シミュレータ・実機) は `<種類>` の段を足す。資源の種類の設定化 (対応方針の 3 つ目) はその段に名前を渡す形になる
- **pro-con の外から同じ lock を取る形** (人・外の session。lockman は置き場が在ることを要求するので `mkdir -p` が要る):

  ```sh
  d="$(git rev-parse --path-format=absolute --git-common-dir)/pro-con-locks/test" && mkdir -p "$d" && lockman with "$d" -- make test
  ```

- 🚨 **`lockman with` は再入不可**。テストの係が lock を取って `make test` を走らせるので、`make test` 自身が同じ lock を取ると自分で自分を締め出す。
  `make test` の側で取らせるときは、テストの係から「取った」を env で渡して二重に取らない形が要る (残り)

## 進み具合

- ✓ **テストの係のコマンドを `lockman with` で包んだ** (`ExecRunner{Lockman: "lockman"}`。e2e は lock を取らない)
  - `lockman with` は待たずに抜ける (`--wait` は acquire にしか無く、保持中なら rc=121 で子を起こさない)。テストの係はそれを結果にせず、
    カードを列の先頭で「テスト (pro-con の外が使用中) の順番待ち」にして **30 秒後に取り直す** (`deferRun` / `runLockRetry`)。PG は再開しない・要約もしない
  - rc=121 は「実行の bash が自分の pgid を書いていない」ときだけ lock の busy と読む (頼まれたコマンドが 121 で抜けたのと取り違えない)
  - lockman は子を別のプロセスグループに置く。bash が書いた pgid からそのグループも撃ち、bash が抜けた後に残った子を止める (前からの約束を lock の下でも保つ)
  - 取り消し・時間切れでは lockman 本体ではなく bash のグループを SIGKILL する (lockman を SIGKILL すると解放されず、TTL の 30 分まで repo の次の実行を止める。
    敵対的レビューで再現)。lockman が子を起こす前に失敗したら、rc に関わらずコマンドの結果にしない。子の後の 122 / 125 は「lockman の rc かもしれない」と添えて渡す
  - 待ちの上限は 1 時間 (`runLockGiveUp`)。超えたら lockman の言い分 (中身を読めない lock 等) を添えて「実行できなかった」で PG を再開する (黙って永久に待たない)
  - 待たせた頼みはカード + コマンドで覚える。取り下げ・頼み直しで印を持ち越さない
  - 検査: `dispatcher/runlock_test.go` (本物の lockman を build し、一時の repo + worktree で外の保持者を作る) と `runner_test.go` の
    `TestRunWaitsWhileRepoLockIsHeldOutside` / `TestRunGivesUpWaitingForRepoLock` / `TestRunBlockIsForgottenWhenRequestIsDropped`。
    変異 13 本 (lock を取らない = 直す前の形 / busy の判定 / pgid の確かめ / グループの停止 / env を渡さない / 結果にしない分岐 / 取り直しの間隔 / 履歴 /
    取り消しで lockman を撃つ / lockman の失敗を結果にする / 上限なし / 待ちの印の持ち越し / 同一性からコマンドを外す) をすべて `bin/mutate-verify` で red にした
- 残り:
  - 🚨 **dispatcher が落ちている間にカードが作業中の列を離れると、残った実行 (lockman + bash) を誰も止めない** (killStale を呼ぶのは「作業中かつ実行中の印」と削除の 2 か所だけ)。
    前からある「残った bash」の問題だが、lock を持つので **repo 全体の次の実行を止める**ものになった (敵対的レビュー。コードからの推論で未再現)。子が終われば解放される
  - 頼まれたコマンドが同じ lock を取る形 (`lockman with <同じ dir> -- …`) を含むと、内側が rc=121 で抜けてコマンドの失敗として PG に渡る (再入。下の入口と一緒に手当てする)
  - 外の session・人が lock を取る入口 (`make test` 自身が取る / 取る道具を置く)。上の再入の手当てと一緒に
  - PG の変異の検証 (`bin/mutate-verify`) の扱い (対応方針の 2 つ目)
  - 資源の種類を repo の設定で足す (対応方針の 3 つ目)。今の順番待ちは repo をまたいで 1 本 (lock は repo ごとなので、別の repo の lock が空いていても待つ)
  - 保持者 (`lockman status` の label・host) をカードの待ちに出すか

## 関連

- 426 (決定 5: テストの係) / 455 (待ちの表示) / `bin/lockman` / 2026-09-25 の dogfooding (440)

## 実例 (2026-09-25)

- C-010 (457) のテストの係の make test で `tests/tmux/test_log_kill_command.sh` の 1 本だけが落ちた (rc=2)。単独では master でも C-010 のブランチでも 3/3 ずつ緑。
  同じ時間帯のロードアベレージは 25 (14 コア)。負荷で落ちるテストが、PG の作業の合否を 1 回ぶん無駄にした
- C-011 (458) のテストの係の make test でも、`tests/zshrc/test_dotfiles_check_result_ownership.sh` の 1 件だけが落ちた (rc=2)。単独では 5/5 と 1/1 緑。負荷で揺れるテストの 2 本目
