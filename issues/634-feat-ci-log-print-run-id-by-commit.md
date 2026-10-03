# 634 (feat): ci-log に「commit と workflow を名指しして run の ID を返す」オプションを足す

起票日: 2026-10-03

## 概要

CI の結果を待つたびに、「待つ対象の run の ID」を手で組み立てている。
組み立て方を間違えて、関係の無い run を待ったり、その結果を緑と読んだりしかけた。
`bin/ci-log` に、commit (既定は HEAD) と workflow の名前を渡すと、その commit の run の ID を返すオプションを足したい。
返す前に headSha を照合する。待つ側はこの ID を `gh run watch <id>` に渡すだけにする。

## 詳細

### 起きたこと (2026-10-02〜03、issue 630 / 633 の CI 待ち)

- `gh run list --branch master --workflow tests.yml --limit 1` で「最新の Tests の run」を取って待った。
  返ってきたのは 2026-09-04 の run 33894864496 (headSha fe3f0f11) で、待ちたかった commit の run ではなかった。
  その run は success だったので、そのまま読むと「緑になった」と誤って報告するところだった。
  run を ID で名指しして待ち直して気づいた
  - 33894864496 は別の workflow ではなく、同じ `tests.yml` (workflowDatabaseId 206485834) の古い run だった。
    `gh workflow list --all` で name が Tests の workflow は 1 つだけ (2026-10-03 に確認)
  - 2026-10-03 に同じコマンド (`--workflow Tests` / `--workflow tests.yml`) を打ち直すと、最新の run が先頭に返り、再現しなかった。
    **古い run が先頭に来た原因は未確認** (gh 2.101.0。一覧の並びか API 側の一時的な挙動かは切り分けていない)
- push のたびに、後続の push が前の run を cancel する (concurrency)。そのため「この commit の Tests」を待つときは、
  cancel されたら次の commit の run へ移る必要がある。毎回、`ci-log -l` の出力を awk で切って ID を拾うループを手で書いた
  (3 回書き、そのうち 1 回は判定式の誤りで完了前に抜けた)

### 今の ci-log でできること / できないこと

- `ci-log` (引数なし) は HEAD の失敗 run を出す。`ci-log -l` は直近 15 件の一覧を出す。`ci-log <run-id>` は指定 run のログを出す
- 「この commit のこの workflow の run の ID」を返す口は無い
- `bin/ci-log` のコメントに「gh run list に --commit は無いため headSha で局所フィルタする」とある。
  しかし gh 2.101.0 には `-c, --commit SHA` があり (`gh run list --help`)、今回 `gh run list --commit <sha>` で run が返った。
  コメントは今の gh と食い違っている (いつの版で入ったかは未確認)

## 対応方針

- `ci-log -i <workflow の名前> [<commit>]` (仮。名前は実装時に決める) を足す。
  commit の既定は HEAD。`gh run list --commit <sha> --workflow <名前>` で引き、**返す前に headSha を照合する**
  (一覧の並びに頼らない。上の古い run の取り違えは、headSha を照合していれば起きない)
- 該当する run が無いときは、rc≠0 で「まだ起動していない / paths filter で起動しない」を区別できるように出す
  (paths filter で起動しない workflow を待ち続けないように)。同じ commit に複数の run がある (re-run) ときは最新の attempt を返す
- cancel を追う待ちは、このオプションを使えば `gh run watch` と組み合わせて数行で書ける。待ちのループ自体を ci-log に入れるかは、
  実装時に決める (入れるなら上限時間と「判定不能」の出力を持たせる)
- `--commit` が無いという古いコメントは、実装の前に gh の版を確かめて直す。今の headSha の局所フィルタ (`--limit 40` / `--limit 60`) を
  `--commit` に寄せられるかも見る (件数の上限から run が漏れる形が消える)
- 入口の更新: `.claude/rules/use-ci-log-for-ci-inspection.md` の主なオプションと、ci-log の usage コメント

## 受け入れ条件

- [ ] commit と workflow を渡すと、その commit の run の ID だけを返す (headSha を照合している)
- [ ] 該当が無い / gh が失敗したときに rc≠0 で区別できる
- [ ] `tests/bin/test_ci_log.sh` に、古い run が一覧の先頭に来る fake を置いても正しい ID を返すケースがある
- [ ] rule と usage に載っている

## 関連ファイル

- `bin/ci-log`
- `tests/bin/test_ci_log.sh`
- `.claude/rules/use-ci-log-for-ci-inspection.md`

## 進捗

- 2026-10-03: 起票
