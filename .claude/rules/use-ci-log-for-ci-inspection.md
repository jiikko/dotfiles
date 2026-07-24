# CI ログの確認は `bin/ci-log` を使う (gh の多段手順を手で組まない)

## ルール

- **この repo で GitHub Actions の失敗を調べるときは `bin/ci-log` を使う**。`gh run list` → 失敗 run の id 特定 → `gh run view <id> --log-failed` を手で叩き分けない
- 既定 (`ci-log` 引数なし) は **HEAD コミットに紐づく全 run のうち失敗したものすべて**の失敗ログを出す。1 回の push で複数 workflow (Lint / Bench / Tests / src_* 等) が同時に落ちても取りこぼさない
- 主なオプション: `ci-log <run-id>` = 指定 run の失敗ログ / `ci-log -a <run-id>` = 全 job ログ (成功含む) / `ci-log -l` = run 一覧

## なぜ

この repo は push ごとに複数 workflow が並走する (`.github/workflows/` に Lint / Bench / Tests / src_glogx 等)。`gh run list --limit 1` で最新 run を 1 件だけ見ると、**同じ push で落ちた別 workflow を見落とす** footgun がある。`ci-log` は headSha で束ねて失敗 run を全部拾うのでこれを構造的に防ぐ。手順を毎回考え直さない (確認方法の一本化)。

## 保守

- `bin/ci-log` が rename / 削除されたらこのルールも直す (乖離を残さない)。実体は `bin/ci-log` が真の出典で、使い方の詳細はスクリプト冒頭の usage コメントを見る
