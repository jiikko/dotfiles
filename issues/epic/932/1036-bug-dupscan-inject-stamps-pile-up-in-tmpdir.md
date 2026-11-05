# 1036 (bug): dupscan の注入の hook が、重複を防ぐ印を $TMPDIR に 1 件ずつ置き、消さないので溜まる

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-06〜）

起票日: 2026-11-06

## 概要

`_claude/hooks/inject-dupscan-candidates-on-new-func.sh` は「同じ session × 名前 (型) には 1 回だけ注入する」ための印を、`$TMPDIR` に空ファイル (`claude-dupscan-inject-<session>-<名前>`) として名前ごとに作り、消さない。
