# shell script の散文コメント行を `# shellcheck` で始めない（directive 誤認で SC1072）

## ルール

- **shellcheck の lint 対象ファイルでは、散文コメントの行を `# shellcheck` という語で始めない**
- shellcheck は `# shellcheck` で始まるコメントを directive（`disable=` / `source=` / `shell=` 等）として解析し、続く語が key=value でないと **SC1072 (Expected '=' after directive key) + SC1073 の error** になり lint 全体が落ちる
- 日本語コメントは「shellcheck が〜」「shellcheck は〜」のように主語として文を始めやすく、**複数行コメントの折り返し位置次第で行頭に来て踏む**（1 行に収まっていた文が、折り返し調整で偶然 `# shellcheck` 始まりの行を生む）
- 回避: 語順を変える／「SC1071 (shellcheck) で〜」のように言い換える／折り返し位置を変えて `#` 直後に shellcheck が来ないようにする

## なぜ（起源: dotfiles `scripts/discover_shell_scripts.sh`, 2026-07-16 実測）

散文コメント「`# shellcheck が SC1071 で、…`」（複数行コメントの折り返し 2 行目）を書いたところ directive 誤認で SC1072/SC1073 になり `make test-shellcheck` が落ちた。エラー文言は「directive の構文が壊れている」としか言わず、**原因が散文コメントの折り返し位置だとは気づきにくい**（directive を書いた覚えがないのに directive エラーが出る）。

なお、**正しい directive の後ろに理由コメントを付けるのは問題ない**: `# shellcheck disable=SC2086 # 理由` は valid。禁止するのは「directive でない散文」が `# shellcheck` で始まる形だけ。

## やること / やらないこと

- ✓ 散文コメントで shellcheck に言及するときは行頭（`#` 直後）を避ける
- ✓ 実際の directive（`disable=` 等）は従来どおり使う（後置の理由コメントも可）
- ✓ SC1072/SC1073 が出たら、directive の typo だけでなく「散文コメントの行頭」を疑う
- ✗ `# shellcheck が…` / `# shellcheck は…` のような散文行（折り返しで生まれるものを含む）

## 関連

- 起源のコード内注記: dotfiles `scripts/discover_shell_scripts.sh`（同じ罠の行内再発防止コメントを残している。[`pending-issue-rationale-in-code.md`](pending-issue-rationale-in-code.md) の「実装で守れない制約はコード直近に残す」の実例）
