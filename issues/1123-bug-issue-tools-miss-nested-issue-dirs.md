# 1123 (bug): 採番・claim・完了の道具が入れ子の issues/ (obaket の macOS/issues/) を見ない

起票日: 2026-11-28

## 概要

`scripts/issue_next_number.sh`・`scripts/issue_claim.sh`・`scripts/issue_done.sh` は repo 直下の `issues/` だけを対象にする。
hook の lib (`issue_hook_resolve_dir`) が見る入れ子の 1 段 (`<root>/*/issues`) は数えず、obaket の `macOS/issues/` の issue を採番・claim・完了できない。
