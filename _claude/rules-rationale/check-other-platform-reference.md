# Platform 特有のバグ調査では、必ず他 platform に動いている参照実装がないか確認する — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/check-other-platform-reference.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## なぜ (起源: DualNote iOS #030 IME バグ, 2026-05-23)

iOS の UIViewRepresentable wrapper が壊れていたとき、iOS だけ見て delegate cycle 仮説に固執し 3 回試行して全て外した。forge の専門家は macOS の**同機能の動いている wrapper** と構造比較を一発で実施し、真因 (双方向 Binding と `becomeFirstResponder()` 同期呼出という構造的差分) を 5 分で特定した。
