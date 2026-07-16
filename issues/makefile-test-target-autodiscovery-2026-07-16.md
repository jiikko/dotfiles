# Makefile の test-* ターゲットを find ベース自動発見にし test_registration.sh を廃止する

**trigger 待ち**: 2026-07-16 時点で並行セッションが `tests/nvim` を作業中 (test_nvim.sh / test_ambiwidth.sh が dirty・`tests/nvim/lib/` が未追跡)。Makefile のテスト列挙を今動かすとそのセッションのテスト追加・登録と衝突するため、**その作業の commit が落ち着いてから着手する**。

## 動機

Makefile の test-nvim / test-tmux / test-zshrc / test-setup がテストスクリプトを 1 行 1 ファイルで列挙しており (test-zshrc は 32 件、4 ターゲット合計 43 件 = 43 行)、テスト追加のたびに「スクリプトを書く + Makefile に登録する」の 2 箇所 touch が必要。登録漏れ (死蔵テスト) は meta テスト `tests/test_registration.sh` が検出しているが、find ベースの自動発見にすれば **「登録」という概念自体が消え、死蔵テストは「検出される」から「発生し得ない」になる** (検出より強い構造的解決)。列挙 43 行と meta テスト 1 本 (41 行) の両方が消えるため、複雑性の移動ではなく削減になる。

## 根拠: テストの判別は機械決定できる

`tests/test_registration.sh` 自身が `find tests -type f \( -name 'test_*.sh' -o -name '*.bats' \) ! -name '*helper*'` で全テストを権威的に列挙している = **命名規約 (test_*.sh) だけでテストか否かを機械決定できる**ことを meta テスト自身が証明している。手動リストが不可避な lint リスト (下記「対象外」) とは前提が異なる。

## ⚠️ 対象外: lint リスト (SHELLCHECK_FILES / ZSH_SYNTAX_FILES) は wildcard 化しない

`SHELLCHECK_FILES` / `ZSH_SYNTAX_FILES` の手動列挙は**意図的設計** (commit 8dd0fae, 2026-07-16)。shellcheck 可否は shebang / 拡張子から機械決定できない (同じ .zsh でも `_av1ify.zsh` は shellcheck 側 / `_concat.zsh` は zsh -n 側) ため、リストは手動維持し網羅だけを `tests/test_lint_coverage.sh` で守る。**本 issue の自動発見化を lint リストへ波及させないこと** (将来の audit が「同様に wildcard 化せよ」と誤指摘したらこの節を根拠に棄却する)。

## 対応方針

1. **dispatcher 方式** (per-directory find への単純置換にしない): 実行本体は **`tests/` 全体を走査する 1 ターゲット**にする。
   ```make
   # test-runtime が使う実行本体。tests/ 全体を走査するので、新ディレクトリ tests/foo/ を
   # 作ってもここが自動で拾い、ディレクトリ単位の死蔵も構造的に発生しない。
   test-discovered:
   	@find tests -type f -name 'test_*.sh' ! -name '*helper*' | sort | \
   		while IFS= read -r t; do echo "[run] $$t"; "$$t" || exit 1; done
   ```
   - **per-directory find 4 本への置換だと、現行 registration (tests/ 全体を再帰検索) より守備範囲が狭くなる**: 将来 `tests/foo/` を新設したときどのターゲットにも拾われず、ディレクトリ単位の死蔵という新しい穴が開く。dispatcher が tests/ 全体を権威的に走ることでこれを防ぐ
   - test-nvim / test-tmux / test-zshrc / test-setup は**人間の選択実行用の便宜フィルタ**として残す (`find tests/nvim ...` の同形ループ)。test-runtime の実行経路は dispatcher に一本化されるため、便宜フィルタの漏れは死蔵を生まない
   - ループは `for t in $$(find ...)` (コマンド置換の単語再分割で空白パスに壊れる) ではなく **`find | while IFS= read -r` 形式** (test_lint_coverage.sh の discover() と同形)。ファイル名に空白・改行を置かない前提は既存 registration / Makefile 列挙と同じ。`test_*.sh` は実行ビット必須 (これも現行の直接実行と同じ前提)
   - `echo "[run] $$t"` でどのテストで落ちたか可視化する (現在の 1 行 1 コマンド形式と同等の失敗特定性を保つ)
   - ヘルパー除外は registration と同じ規約 (`test_*.sh` のみ実行対象・`*helper*` 除外。helper は `lib/` か非 `test_` 名で置く)
2. **test-bats**: `tests/_ensure_cli_with_brew.bats` の hardcode も自動発見化する。registration は `*.bats` もカバーしているため、**registration だけ消して bats の hardcode を残すと新規 .bats が silently dead になる**。具体化: 対象は `tests/` 全体の `*.bats` を find (`while read` 形式で 1 ファイルずつ `bats` に渡す)、`bats` 未インストール時の skip は現行踏襲、発見 0 件時も skip 扱い (bats は引数なしだとエラーになるため)
3. **test_registration.sh の廃止**: `test-runtime` の依存から `test-registration` を外し、ターゲットと `.PHONY` エントリを削除、`tests/test_registration.sh` を削除
4. **stale 参照の掃除**: `tests/test_lint_coverage.sh` 冒頭コメントの「(test_registration.sh の lint リスト版)」を更新する (claude-md-maintenance の構造的乖離防止)

## trade-off / 検証事項

- **実行順が変わる**: 現在の列挙順 (非アルファベット順・グループ単位) → `find | sort` 順 (グループ間の順序も消える)。テスト間に順序依存がない前提。受け入れ条件の full green で検証し、順序依存が見つかったらそのテストを直す (順序依存自体が潜在バグ)
- **WIP テストが即実行される**: `test_*.sh` を置いた瞬間 CI 対象になる。現在 registration の ALLOWLIST は空 = 除外の実需要ゼロ。将来必要になったら命名規約 (非 `test_` 名に退避) で除外する
- **影響範囲**: `test-registration` の参照は Makefile 内 (`.PHONY` / `test-runtime` 依存 / ターゲット定義) と test_lint_coverage.sh:3 のコメントのみ。CI は `make test-runtime` (tests.yml) 経由なのでワークフロー側の変更は不要。**README.md:34 の test-runtime 説明** (「aggregate runtime target (syntax + zshrc + nvim + tmux + setup)」) は構成が変わるため同 PR で更新する

## 受け入れ条件

- `make test` full green (ローカル)。CI (tests.yml の `make test-runtime`) も green
- dispatcher (`find tests -name 'test_*.sh' ! -name '*helper*'`) が拾うテスト集合が、廃止前の Makefile 列挙 43 件 + registration の検出対象と一致すること (移行時に 1 回突き合わせる)。`.bats` も同様に突き合わせる
- README.md:34 の test-runtime 説明を新構成に更新済みであること
- 実装後に codex レビュー (レビュー方針どおり)
