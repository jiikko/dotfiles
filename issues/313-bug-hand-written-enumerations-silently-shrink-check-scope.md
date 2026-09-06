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
  [Makefile:494 の注記](../Makefile)どおり **flaky に見える形**で出る
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
[`comment-no-restate-enforced.md`](../_claude/rules/comment-no-restate-enforced.md) の逆で、
**実装が強制していないことを「強制されている」と書いている**のが問題。

## 受け入れ条件

- [ ] ①: yamllint の対象を `find` から導出し、除外は理由つき allowlist にする。
      **対象 0 件を失敗にする**（[`verify-execution-not-just-exit-code.md`](../_claude/rules/verify-execution-not-just-exit-code.md)）
- [ ] ②: 3 箇所すべてを桁数非依存にする（`grep -c` で数えてから「3 箇所」と書く）
- [ ] ②: **変異検証** — 4 桁の issue ファイルを fixture に置き、桁数決め打ちに戻すと red
- [ ] ③: ヘッダの主張と実装を一致させる
- [ ] 各検査が**集約経路から実行され、その出力行が出る**ことを確認する

## 関連

- issue 310 / 311（同じ「検査装置が壊れている」ファミリー）
