# 1044 (chore): 予算検査の「他の repo 層」の定数が実測から遅れ、検査が通るまま obaket のセッションが上限を超える

起票日: 2026-11-08

## 概要

`scripts/check_instruction_budget.sh` の `OTHER_REPO_RESERVE=39000` は手で置いた定数で、obaket/macOS のセッションの repo 層 (2026-11-08 実測 39,719 字) より小さい。
