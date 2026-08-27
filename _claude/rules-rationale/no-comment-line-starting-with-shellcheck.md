# shell script の散文コメント行を `# shellcheck` で始めない（directive 誤認で SC1072） — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/no-comment-line-starting-with-shellcheck.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## なぜ（起源: dotfiles `scripts/discover_shell_scripts.sh`, 2026-07-16 実測）

散文コメント「`# shellcheck が SC1071 で、…`」（複数行コメントの折り返し 2 行目）を書いたところ directive 誤認で SC1072/SC1073 になり `make test-shellcheck` が落ちた。エラー文言は「directive の構文が壊れている」としか言わず、**原因が散文コメントの折り返し位置だとは気づきにくい**（directive を書いた覚えがないのに directive エラーが出る）。

なお、**正しい directive の後ろに理由コメントを付けるのは問題ない**: `# shellcheck disable=SC2086 # 理由` は valid。禁止するのは「directive でない散文」が `# shellcheck` で始まる形だけ。
