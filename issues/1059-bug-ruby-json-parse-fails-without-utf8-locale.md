# 1059 (bug): LANG が空のシェルで、Ruby のスクリプトが日本語を含む JSON を読めずに落ちる (golangci_ruleguard_key.rb ほか)

起票日: 2026-11-10

## 概要

LANG・LC_ALL が空のシェル (Claude Code の Bash など) で `make lint` が `scripts/golangci_ruleguard_key.rb` の Encoding::InvalidByteSequenceError で落ちる。本文はレビューの後に入れる。
