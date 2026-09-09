# bug: 「手書き列挙」と「3 桁決め打ち」で守っている検査が、実体とずれて無音で射程を失っている

起票日: 2026-09-07
カテゴリ: bug
優先度: 中（どれも**壊れたときに黙る**形。今すぐ実害が出ているのは YAML の 13 本）
出典: /audit broken-code 2026-09-06。数はすべて私が機械で数え直した

共通の構造: **母集合を人が書く / 桁数を決め打つ**ので、後から足したものが検査から静かに漏れる。
3 件を 1 つの issue にまとめたのは、**直し方が同じ（実体から導出する）**ため。

## ① `Makefile:YAML_FILES` が手書き列挙で、13 本が yamllint の対象外

```
YAML_FILES := theme/colors.yml pre-commit-config.yml .github/dependabot.yml ... （14 本を手書き）
```

実在する yml/yaml は **27 本**（vendor 除く）。列挙に無いのは **13 本**:

```
.github/actions/ensure-toolchain/action.yml
.github/workflows/_go-project.yml
.github/workflows/doctor.yml
.github/workflows/src_doctor.yml
.github/workflows/src_termsafe.yml
src/{disassemble_excel,doctor,glogx,lockman,parallel-each,schedkeys,termsafe}/.golangci.yml
zshlib/tmux-window-name.yaml
```

**射程の違いを分けて書くこと**（過大に読ませないため）:

- **workflow 4 本**は `actionlint`（`make test-actionlint`）が意味レベルで見るので、
  漏れているのは **yamllint の構文/スタイル検査だけ**
- **`src/*/.golangci.yml` 7 本**は誰も lint していない。壊れれば `golangci-lint` が落ちるので
  **無音ではない**が、`run.allow-parallel-runners: true` の欠落のような**意味の誤り**は
  [Makefile:494 の注記](../../Makefile)どおり **flaky に見える形**で出る
- **`zshlib/tmux-window-name.yaml`** は vendor 設定で、どの検査も通っていない

**直し方**: `find` で導出し、除外したいものだけを理由つきの allowlist に置く（列挙の向きを反転させる）。

## ② issue 番号の抽出が 3 桁決め打ちで、1000 番以降を静かに落とす

```
tests/issues/test_issue_numbers_unique.sh:37   find ... -name '[0-9][0-9][0-9]-*.md'
_claude/hooks/issue-progress-check.sh:62        grep -oE '\(([0-9]{3}([,/ ]+[0-9]{3})*)\)'
_claude/hooks/issue-progress-check.sh:65        find ... -name '[0-9][0-9][0-9]-*.md'
```

現在 309 番まで到達しており、**このペースなら 1000 番は遠くない**。到達した日に起きるのは:

- 一意性ゲートが 1000 番以降を**抽出集合から落とす**（重複が検出されなくなる）
- Stop hook の「関わった issue」判定が 1000 番以降を**認識しない**（機構ごと無音で死ぬ）

`issues/README.md` の採番規約は「3 桁ゼロ埋め」と書いているが、
**上限を宣言しているわけではない**（4 桁になったら規約側も直す必要がある）。

**直し方**: `[0-9]{3,}` へ。あわせて README に「4 桁になったらこの 3 箇所を直す」と書く…
のではなく、**最初から桁数に依存しない形にする**（規約の書き換えを覚えていることに依存させない）。

## ③ `discover_shell_scripts.sh` の「未登録は構造的に発生しない」が偽

ヘッダは自己修復機構を主張しているが、実際には `LINT_DIRS` の外に shell script が
**3 本**あり、`zshlib` の zsh 例外は「登録漏れなら SC1071 で loud に落ちる」という主張が
**実ファイルでは成立しない**（監査の指摘。私は本数のみ再確認）。

**直し方**: 主張をヘッダから消すか、主張どおりに動く形にする。
[`comment-no-restate-enforced.md`](../../_claude/rules/comment-no-restate-enforced.md) の逆で、
**実装が強制していないことを「強制されている」と書いている**のが問題。

## 受け入れ条件

- [x] ①: yamllint の対象を `find` から導出し、除外は理由つき allowlist にする。
      **対象 0 件を失敗にする**（[`verify-execution-not-just-exit-code.md`](../../_claude/rules/verify-execution-not-just-exit-code.md)）
- [x] ②: 3 箇所すべてを桁数非依存にする（`grep -c` で数えてから「3 箇所」と書く）
- [x] ②: **変異検証** — 4 桁の issue ファイルを fixture に置き、桁数決め打ちに戻すと red
- [x] ③: ヘッダの主張と実装を一致させる
- [x] 各検査が**集約経路から実行され、その出力行が出る**ことを確認する

## 関連

- issue 310 / 311（同じ「検査装置が壊れている」ファミリー）。**2026-09-09 に両方 done**。310 は「語の一致」の grep を捨てて**先頭トークンを見るトークン解析**（`_claude/hooks/lib/git_cmd_detect.sh`）へ寄せた — 本 issue の「手書き列挙が黙って射程を縮める」と同じ形（正規表現が列挙しきれない形を落とす）なので、着手時の参照実装にできる

## 進捗・結果（2026-09-09）

### 🚨 本文の数え直し（着手前に全数勘定した結果、issue の数字が違っていた）

| 本文の記載 | 実測 |
|---|---|
| 実在する yml/yaml は **27 本** | **25 本** |
| 手書き列挙は **14 本** | **13 本** |
| 列挙に無いのは **13 本** | **12 本** |

差の理由: 列挙されていた `src/parallel-each/.golangci.yml` は**既に存在しない**（本文の
漏れ一覧にも載っていた）。漏れの実体は workflows 4 本 + `.github/actions/ensure-toolchain` +
`src/*/.golangci.yml` **6 本** + `zshlib/tmux-window-name.yaml` = 12 本。

### ① yamllint の対象を導出へ反転

`scripts/discover_yaml_files.sh` を新設し、`Makefile:YAML_FILES` を
`$(shell scripts/discover_yaml_files.sh)` に。除外は `.git` / `vendor` / `node_modules` / `tmp`
だけで、**それぞれ理由をスクリプト内に書いた**。対象 0 件は `__DISCOVERY_FAILED__` を出して
非 0 で終わる。**13 → 25 本**。25 本すべて yamllint の error 0（warning 7 = `document-start`）で、
`make test-yaml` rc=0。

### ② 桁数の決め打ちを 3 箇所とも外した

`_claude/hooks/issue-progress-check.sh` の 2 箇所（subject の `[0-9]{3}` / `next/` の find）と
`tests/issues/test_issue_numbers_unique.sh` の find。`grep -c` で数えて 3 箇所であることを確認済み。
4 桁の fixture（`1001-*.md`）を置いて、旧パターンが 1 件・新パターンが 2 件拾うことを実測。

### ③ ヘッダの虚偽を直し、**穴自体も塞いだ**

ヘッダは「未登録は構造的に発生しない」「zsh 例外の登録漏れは SC1071 で loud に落ちる」と
書いていたが、**どちらも偽**だった:

- `LINT_DIRS` の外に 3 本（`mac/` 2 本、`src/glogx/tools/` 1 本）。3 本とも shellcheck 0 件
  だったので **`LINT_DIRS` に `mac` と `src/glogx/tools` を足した**（112 → 115 本）
- SC1071 が出るのは **zsh の shebang を持つファイルだけ**。`zshlib/*.zsh` は source される
  ライブラリで shebang が無く、`ZSH_SYNTAX_FILES` から漏れても bash として検査されて緑になる。
  **これは既知の穴として残し**、ヘッダとテストの「検出しない形」に明記した

### 新設した検査

`tests/scripts/test_enumerations_are_derived.sh`（7 件）。①②③ を 1 本でまとめ、
脅威モデルと「検出しない形」をヘッダに書いた。集約経路 `make test-dir DIR=tests/scripts` に
`[run] tests/scripts/test_enumerations_are_derived.sh` が出ることを確認。

### 変異検証（6 本、すべて red / baseline green）

| 変異 | 落ちた assert |
|---|---|
| YAML を手書き列挙へ戻す | Makefile の YAML_FILES が手書き列挙に戻っている |
| discover_yaml から workflows を除外 | yamllint の対象から漏れている（12 本を名指し） |
| hook の find を 3 桁へ戻す | パターンが 1 件しか拾わない |
| 一意性ゲートを 3 桁へ戻す | 同上 |
| subject の抽出を 3 桁へ戻す | commit subject の番号抽出が桁数に依存している |
| `LINT_DIRS` を元へ戻す | LINT_DIRS の外に shell script がある（3 本を名指し） |

🚨 **subject の変異は 1 回目は緑だった**。テストが hook の正規表現を**書き写して**いて、
コピーを検査していただけだったため。hook の実体から `grep -oE` のパターンを抜き出して
使う形へ直して red を確認した。

### 残タスク

- **スコープ外（既知の穴として記録）**: `zshlib/*.zsh` が `ZSH_SYNTAX_FILES` から漏れても
  `zsh -n` の対象外になるだけで loud に落ちない。塞ぐなら「発見した `*.zsh` は必ず
  `ZSH_SYNTAX_FILES` か shellcheck のどちらかに入る」を検査する形になる
- **検出しないと決めた形**: json / ruby の手書き列挙（件数が少なく増えたときに気づく）
