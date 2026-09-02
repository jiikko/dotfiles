# 141 refactor: brew の依存導入ブロックが 3 つの workflow に重複している

起票日: 2026-08-29 / 種別: refactor / 優先度: P3 / 出典: 自分の後始末

## 事実

CI を macOS へ移した (issue 133) とき、「存在を確かめ → 無いものだけ brew で入れ → 最後に
もう一度確かめる」というブロックを **3 箇所に書いた**:

- `.github/workflows/tests.yml` (CI_COMMANDS_* を読む版)
- `.github/workflows/lint.yml` (zsh / jq / ruby / yamllint)
- `.github/workflows/bench.yml` (tmux / zsh)

加えて「brew の bash 5 を PATH 先頭に出す」ブロックも tests.yml / lint.yml /
`.github/actions/run-bench/action.yml` の 3 箇所にある。

CLAUDE.md「コード変更時の自律改善」は**同じ変更を 2 箇所にコピペするのを禁止**しており、
これは規約違反を自分で作った形。

## なぜ直すか (行数ではなく複雑性で)

単なる行数の重複ではなく、**変更時の touch 箇所が 3 になっている**のが問題。実際に想定される
変更: formula 名の写像を足す (`bats → bats-core` / `gtimeout → coreutils` が既にある) /
検証の仕方を変える / brew の環境変数を足す (`HOMEBREW_NO_AUTO_UPDATE` 等、issue 133 の
敵対レビュー R3 で未対応として残っている)。今はどれも 3 箇所を直す必要がある。

## 対応案

`.github/actions/` に composite を 1 本切り出す (`run-bench` が既にこの形)。入力は
「必要なコマンドの空白区切り」だけにし、**formula 名の写像も composite 側に集約**する。

## ⚠️ 注意

- bash 5 の導入は `run-bench` composite に既に入っている。**二重に入れない**
- `tests.yml` だけは対象コマンドを Makefile (`CI_COMMANDS_*`) から sed で読む step を持つ。
  そこは残し、**導入部分だけ**を共通化する (出典を Makefile に保つ設計は issue 073 の結論)

## 受け入れ条件

- [ ] formula 名の写像が 1 箇所になる
- [ ] 3 つの workflow すべてが CI で緑
- [ ] **実行された証拠**を確認する (composite の出力が各 job のログに出ること。
      `verify-execution-not-just-exit-code.md`)

---

## 対応 (2026-08-29): `ensure-toolchain` composite へ集約

`.github/actions/ensure-toolchain/` を新設し、**6 job すべてがこれを使う**形にした。

- 入力は `commands` (空白区切り、空でもよい) だけ。**bash 5 の導入と検証も同じ action が行う**
- **formula 名の写像 (`bats → bats-core` / `gtimeout → coreutils`) が 1 箇所になった**
- `${{ }}` を run に直接埋めない / 文字種の検証 / 「brew install の成功ではなく `command -v` で
  実在を確認」も 1 箇所に集約された (これまで写しごとに微妙に違っていた)

| 呼び出し元 | commands |
|---|---|
| tests.yml (heavy / rest) | `${{ steps.deps.outputs.commands }}` (Makefile の `CI_COMMANDS_*` が出典。**この step は残した**) |
| lint.yml | `zsh jq ruby yamllint` |
| bench.yml bench-nvim / bench-zsh | `zsh` |
| bench.yml bench-tmux | `tmux zsh` |
| bench.yml bench-glogx | (空。予算 checker のために bash だけ揃える) |

`run-bench` composite が持っていた bash の導入は**外した** — 呼び出し側の job で揃うので、
残すと brew install が二重になる。漏れても予算 checker 側の bash 4+ ガードが loud に落とす
(二重化は意図的)。

`brew install --quiet bash` の出現数: **3 → 1**。

### 受け入れ条件

- [x] formula 名の写像が 1 箇所になった
- [x] 3 つの workflow (6 job) すべてが CI で緑
- [x] 実行された証拠を確認した

### CI 確認 (2026-09-02, commit 48db380)

`gh run view --log` で 3 run すべてを確認した。

| run | workflow | 結果 |
|---|---|---|
| 33601182513 | Tests (heavy / rest) | success |
| 33601182590 | Lint | success |
| 33601182500 | Bench (glogx / nvim / tmux / zsh) | success |

**実行された証拠** — 6 job すべてのログに composite の step
`Ensure required commands (...)` と、その末尾の実在確認出力が出ている
(`verify-execution-not-just-exit-code.md`。緑という結果ではなく出力で判定した):

```
dotfiles-tests (rest)  Ensure required commands (tmux zsh make bats gtimeout)
  tmux = /opt/homebrew/bin/tmux
  bats = /opt/homebrew/bin/bats          ← 写像 bats → bats-core が効いている
  gtimeout = /opt/homebrew/bin/gtimeout  ← 写像 gtimeout → coreutils が効いている
  bash = /opt/homebrew/bin/bash (GNU bash, version 5.3.15(1)-release)
dotfiles-lint          Ensure required commands (zsh jq ruby yamllint)
dotfiles-tests (heavy) Ensure required commands (zsh make)
bench-glogx            Ensure required commands ()        ← 空入力でも bash 5 は揃う
bench-nvim / bench-zsh Ensure required commands (zsh)
bench-tmux             Ensure required commands (tmux zsh)
```

`brew install --quiet bash` の残存箇所も 1 件 (`ensure-toolchain/action.yml:30`) のみで、
`run-bench` 側は二重導入にならないことを grep で確認した。

### 残課題

なし。
