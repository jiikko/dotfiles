# 775 (refactor): bin の繰り返しを寄せる (整数の検査を bin/lib/uint.sh に・自分の実体の置き場を readlink -f に)

> 🚨 **担当中: dotfiles-63**（2026-10-15〜）

起票日: 2026-10-15

## 概要

ユーザー依頼 (2026-10-15): 「bin ディレクトリの中で繰り返し使われているイディオムがあれば、抽出したのち合理的な場所へ集約してほしい」。
src の重複の調査 (773) の bin 版。read-only のサブエージェント 1 体 (opus) が洗い出し、main が本文と実測で確かめた。

## 母集合の数え方

- bin/ の 42 本。shebang と行数を一覧にした。Go の起動スクリプト 17 本 (`bin/lib/go_autobuild.zsh` に寄せ済み。各 3〜8 行) と、zshlib を起こすだけの 6 本 (各 2 行) は全文を読んだ
- 残りの bash / sh 14 本は観点ごとに grep して当たった箇所を読んだ。観点: dirname / BASH_SOURCE / readlink・die / usage・command -v・mktemp / trap・runtimeout・lockf・y/N・色・jq / python3・tmux show・XDG_*・数値の検査 `[!0-9]`
- 寄せ先の候補として bin/lib の 6 本・scripts/lib・tests/lib を読んだ (bin に README / CLAUDE.md は無い)

## 寄せたもの

1. **「数字だけの整数か + 桁の上限」の検査 (4 か所)** → `bin/lib/uint.sh` の `uint_ok <値> <最大の桁数>` (rc 0 / 1 数字でない / 2 桁が多すぎる)。
   POSIX の case で書き、bash 3.2・bash 5・zsh・sh のどれからも読める。エラー文と rc は呼ぶ側に残した (テストが字面で見ている)
   - codex-fanout の `validate_timeout` (9 桁) / codex-run の `-t` (9 桁) / `bin/lib/claude_bin.sh` の `_claude_bin_version_ok` (7 桁) / mutate-verify の `--timeout` (7 桁)
   - **不具合が 1 つ直った**: mutate-verify は `#!/bin/bash` (3.2) なのに `[!0-9]` を使い、ja_JP.UTF-8 で全角の「１２」を通していた (2026-10-15 に実測)
   - claude_bin.sh の「7 桁を超えると runtimeout が rc=125」は誤りだった。runtimeout が拒むのは 9223372036 秒 (MaxInt64 / 1e9) を超える値
     (実測: 4294967295 は rc=0、9999999999 は rc=125)。理由は uint.sh に 1 か所で書いた。各道具の上限 (7 / 9 桁) は挙動を変えないため据え置き
2. **自分の実体の置き場の解決 (symlink を辿る 5 行のループ 3 か所)** → `readlink -f` の 1 行 (macOS 12.3 以降の BSD readlink。27.0.1 で確認)
   - codex-fanout・mutate-verify・mutate-verify-list (issue 585 で「codex-fanout と同じ形」とコピーされたもの)
   - 辿っていなかった codex-run も同じ解決にした (symlink で別の dir から起動すると、兄弟の codex-fanout を探し外していた)
3. 入口: ルートの CLAUDE.md の「対象プラットフォーム」の節に、bin のスクリプトの定型として 1 行足した

## 寄せないもの (理由)

- **Karabiner の「restore 済みの記録」** (backup / restore_karabiner_config.sh の `record_restored` と `_zshrc` の `_dotfiles_check_watch` のパス): 寄せても source の行が増えるだけで行数はほぼ減らず、
  読む側の _zshrc は sh の lib を読まない。エラー文は意図して違えている (restore 側は「設定はコピーしたが」)
- **ffmpeg 系の「必要なコマンドがあるか」** (concat_movies / repair_avi_vorbis_audio.sh / total_duration): 3〜6 行で、文言 (日本語 / 英語) も揃っていない。寄せるなら文言の統一と同時に
- **die / usage**: 終了コードの約束が違う (codex-run / codex-fanout は 1、mutate-verify / -list は 2、skill-eval は 64、gopls は呼ぶ側が指定)。どれも 1 行
- **ヘッダのコメントを usage として出す 1 行 (7 本・4 種類)**: どこで止めるかはファイルごとのヘッダの形に合わせてある。lib にすると source の行が増える
- **mktemp + trap rm**: 各 2 行。skill-eval と mutate-verify の trap は bash 3.2 の rc や中断時の報告先 (fd 3) を担う独自のもの
- **`unset CDPATH` (12 本)・`set -euo`**: 1 行で、`check_cdpath_before_cd_capture.sh` が既に強制している
- **zshlib を起こす 2 行 (6 本)**: go_autobuild のような機構は要らない
- **gopls の実体の解決** (sh。hop の上限とハードリンクの `-ef` の比較) と **tmux の shim** (実体を絶対パスで固定。`_claude/rules/path-shim-must-resolve-real-binary.md`): 意図して別の作り
- **skill-eval・concat_movies・total_duration・kernel-alloc-watch の `dirname "$0"`**: symlink で起動する経路が無い (未確認) か、自分を再実行するだけで無害。readlink -f に揃えるかは次に触るとき
- scripts/ の `[!0-9]` 約 15 本は tmux が出す値が相手で、ユーザーの入力ではない (対象外)

## 進捗

- [x] 実装 (commit:「refactor(bin): 整数の検査を bin/lib/uint.sh に、自分の実体の置き場を readlink -f に寄せる」/「docs: bin の定型…をルートの CLAUDE.md に書く」)
- [x] テスト: `tests/bin/test_uint.sh` (bash 3.2・bash 5・zsh・sh × 12 件と、mutate-verify が全角の --timeout を拒むこと)。既存の codex_fanout.bats・codex_run.bats・
  test_mutate_verify.sh (48 ケース。symlink / PATH / dir ごとの symlink のケース 43 を含む)・test_mutate_verify_list.sh・test_claude_bin.sh・test_skill_eval.sh は rc=0
- [x] 変異: uint.sh の列挙を `[!0-9]` に戻す → test_uint.sh が /bin/bash と /bin/sh の「１２」「1２」で red
- [ ] 敵対的レビュー
- [ ] make test
