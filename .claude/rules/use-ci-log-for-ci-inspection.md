# CI ログの確認は `bin/ci-log` を使う (gh の多段手順を手で組まない)

## ルール

- **この repo で GitHub Actions の失敗を調べるときは `bin/ci-log` を使う**。`gh run list` → 失敗 run の id 特定 → `gh run view <id> --log-failed` を手で叩き分けない
- 既定 (`ci-log` 引数なし) は **HEAD コミットに紐づく全 run のうち失敗したものすべて**の失敗ログを出す。1 回の push で複数 workflow (Lint / Bench / Tests / src_* 等) が同時に落ちても取りこぼさない
- 主なオプション: `ci-log <run-id>` = 指定 run の失敗ログ / `ci-log -a <run-id>` = 全 job ログ (成功含む) / `ci-log -l` = run 一覧
- **CI の完了を待つときは、待つ run を `ci-log -i <workflow> [<commit>]` で名指しする** (その commit の run の ID を、headSha を照合して返す。既定は HEAD)。
  `id=$(ci-log -i Tests); rc=$?` で rc を受けてから `gh run watch --exit-status "$id"` で待つ
  (`gh run watch "$(ci-log -i Tests)"` と引数の中で展開すると rc が捨てられる。`--exit-status` が無いと run が失敗しても rc=0 で終わる)。
  `gh run list --limit 1` のように「最新」で選ぶと、別の commit の run を拾うことがある (2026-10-02 に 2026-09-04 の run の success を拾いかけた。issue 634)。
  rc=4 は「run がまだ無い」(push 直後 / paths filter で起動しない) なので上限つきで再試行する。rc=5 は GitHub に無い (push していない) commit で、待っても来ない。
  **issues/ だけの commit では、push の Tests / Lint は起動しない** (paths-ignore)。その commit で待つのは `Issues`
  (Tests は毎時の schedule の run が master の最新として走らせることはある。下の項)
  - 🚨 **名指しで待った run が緑でも、報告の前に引数なしの `ci-log` を 1 回打ち、その commit の全 run の失敗を見る**。名指しは
    「待つ対象」を決めるためのもので、名指ししなかった workflow (paths 付きの検査など) については何も言わない (retro 689: 3 本を名指しで
    待って緑を見続け、`unused` の赤を 10 本見逃した)
- 🚨 **HEAD が緑でも「直前までが緑」ではない**。paths filter 付きの workflow は、その paths に触らない
  commit では起動しないので、赤いまま残った 1 つ前の commit が HEAD の緑に隠れる。`ci-log` は
  引数なしのとき「HEAD から辿れる commit における各 workflow の最新 run」も見て、まだ緑になって
  いない workflow を 🚨 付きで報告する (そのあと成功していれば黙る)。**この行が出たら、HEAD が
  緑でも直していない失敗が残っている**
- 🚨 **特定の workflow の結果を根拠にするときは run を名指しで確認する**。「HEAD が緑」は
  「HEAD で起動しなかった workflow」については何も言っていない (`gh run view <id> --json headSha` で
  どの commit を建てたかまで見る)
- **Tests と Bench は push の run が次の push に取り消され続けることがある** (cancel-in-progress)。その間は毎時 17 分の
  schedule の run (push と別の concurrency の group) が master の最新を 1 本走らせるので、検証したい commit を祖先に含む
  schedule の run を探して待つ (`gh run list --workflow Tests --event schedule`)。最新の commit を本体の job が既に検証していれば、
  schedule の run は本体を走らせない (緑なら gate だけで緑、赤なら gate だけの run を取り消す。`scripts/ci_schedule_gate.sh`。issue 1000)。
  取り消された schedule の run は検証ではないので読まない (`ci-log -i` は他に run があれば飛ばす)

## なぜ

この repo は push ごとに複数 workflow が並走する (`.github/workflows/` に Lint / Bench / Tests / src_glogx 等)。`gh run list --limit 1` で最新 run を 1 件だけ見ると、**同じ push で落ちた別 workflow を見落とす** footgun がある。`ci-log` は headSha で束ねて失敗 run を全部拾うのでこれを構造的に防ぐ。手順を毎回考え直さない (確認方法の一本化)。

## 保守

- `bin/ci-log` が rename / 削除されたらこのルールも直す (乖離を残さない)。実体は `bin/ci-log` が真の出典で、使い方の詳細はスクリプト冒頭の usage コメントを見る
