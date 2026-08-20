# 073 refactor: GitHub Actions workflow のコードスメル (2026-08-21)

起票日: 2026-08-21

`/audit` (2026-08-20) は `.github/workflows/` を対象に含めていなかったため、別途 8 ファイル
576 行を読んだ結果。**全体の水準は高い** (各 workflow に `permissions: contents: read` /
`concurrency` + `cancel-in-progress` / `timeout-minutes` が揃い、判断の理由がコメントで
残っている。paths filter を required check に登録しない罠まで明記済み)。以下は残っている
スメルで、いずれも main agent がファイルを読んで数えた事実。

## 1. actionlint だけバージョン未固定 — shellcheck で学んだ教訓の横展開漏れ (medium)

`lint.yml` は shellcheck を `SHELLCHECK_VERSION: v0.11.0` で固定し、その理由まで書いている:

> apt (ubuntu 24.04) は 0.9 系で、開発機 (brew = 0.11) と指摘が食い違い「手元 green なのに
> CI 赤」を生む (実例 2026-07-25)

ところが同じファイルの actionlint は `gh api .../releases/latest` で**最新を追う**。同じ
「上流のバージョンが上がると手元と CI がずれて突然赤くなる」経路が開いたままになっている
(repo 内で `VERSION:` による pin は shellcheck の 1 件だけ)。

副作用として、latest 解決のために `gh api` へ依存し、HTML エラーページ対策のリトライ 3 回 +
空 ver ガードという 12 行の配管を抱えている (実例のコメント: run 29542275908 の
`invalid character '<'`)。**バージョンを固定すればこの配管ごと消える**。

- 発火条件: actionlint の新リリースで新しい検査が入る / 既存検査が厳しくなる → 手元で
  `make test-actionlint` が通っていても CI だけ赤。あるいは gh api が HTML を返す日に
  リトライも尽きて lint 全体が落ちる
- 対応: `ACTIONLINT_VERSION` を pin し、リリースバイナリを直接取得する (shellcheck と同じ形)。
  「手元を上げたらここも上げる」の注記も shellcheck に倣って添える

## 2. `env: GIT_CONFIG_*` の 3 行が 5 ファイルに逐語重複 (low)

`_go-project.yml` / `tests.yml` / `lint.yml` / `bench.yml` / `karabiner.yml` の 5 ファイルすべてに
同じ 3 行 (`GIT_CONFIG_COUNT` / `GIT_CONFIG_KEY_0` / `GIT_CONFIG_VALUE_0`) がある。
コメント自身が「tests / lint / bench / karabiner も同方式」と重複を認識している。

**GHA の制約で完全な一元化はできない**: composite action は checkout 後にしか解決できず、
この env は checkout の `git init` に効かせる必要がある (コメントに記録済みの理由)。
現実的な改善は「値の出典を 1 つにする」= repo Variables (`${{ vars.GIT_DEFAULT_BRANCH }}`) を
参照する形にして、値の変更時に 5 箇所を直さなくて済むようにすること。行数は減らない。

- 優先度は低い (値が変わる見込みが薄い)。**着手 trigger**: 6 個目の workflow を足すとき、
  またはこの値を変更したくなったとき

## 3. heavy グループの apt パッケージが workflow 側にハードコード (medium)

`tests.yml` の matrix は heavy (`test-discovered-heavy`) に `packages: zsh make`、rest に
`tmux zsh make bats` を与えている。一方**どのディレクトリが heavy かは Makefile 側が出典**
(`CI_HEAVY_TEST_DIRS := tests/zshrc/av1ify tests/zshrc/concat`) で、コメントも
「グループ定義は Makefile 側に集約」と明記している。

つまり「グループの中身」と「そのグループが必要とする依存」が別ファイルに分かれている。
heavy 側に `bats` や `tmux` を使うテストを追加した瞬間、Makefile だけ直せば CI は
**command not found で落ちる** (workflow を直す必要があると気づけるのは CI が赤くなってから)。

- 発火条件: `CI_HEAVY_TEST_DIRS` に列挙されたディレクトリへ、bats / tmux 依存のテストを足す。
  現状の av1ify / concat は zsh のみなので今は踏まない
- 対応案: 依存も Makefile 側から出す (例: `make ci-packages-heavy` が必要パッケージを echo し、
  workflow はそれを読む)。あるいは両グループで同じパッケージを入れて差を消す
  (tmux/bats の ~60s を節約するために分けた経緯があるので、時間と引き換え)

## 4. `apt-get update` が bench.yml 内で 3 回 (low)

`bench.yml` は job ごとに `apt-get update` + install を書いており、同一ファイル内で 3 回。
job が別 runner なので機能的には正しい (共有できない)。ただし install するパッケージが
job ごとに違う理由はコメントに無い。composite action (`.github/actions/setup-nvim` が既にある)
と同じ流儀で `setup-deps` を切り出せるが、**得るものは行数だけ**なので優先度は低い。

## 攻めて見つからなかった範囲 (次回の起点)

- `permissions`: 全 workflow が `contents: read` のみ。過剰権限なし
- `concurrency`: 全 workflow にあり `cancel-in-progress: true`。caller/reusable の関係も
  caller 側で効くので `_go-project.yml` に無いのは正しい
- `timeout-minutes`: 全 job にあり。lint の 25 分は「cold compile が cancel されると
  cache-save が走らず悪循環」という実測に基づく (a43cbe8 / 40b2600)
- actions のバージョン: `checkout@v7` (10 箇所) / `setup-go@v7` (4 箇所) / `cache@v6` で統一。
  ばらつきなし
- script injection: `${{ }}` を `run:` の中で使っているのは `inputs.dir` (workflow_call の
  型付き入力) と `matrix.*` (workflow 内で定義した固定値) だけ。PR タイトル・ブランチ名などの
  攻撃者が制御できる値を `run:` へ埋めている箇所は無い
- `pull_request_target` / `secrets` の使用なし
