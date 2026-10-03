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
  (一覧の並びに頼らない。古い run が先頭に来た原因は分かっていないが、headSha を照合していれば原因を問わず取り違えない)
  - 🚨 **`--commit` は 40 桁の完全な SHA でしか一致しない**。短縮 SHA を渡すと、エラーにならず空の一覧が返る
    (2026-10-03 に実測: `gh run list -c <8 桁> -w Tests` は 0 件、同じ commit の完全な SHA では 1 件)。
    受け取った commit は `git rev-parse` で完全な SHA に展開してから渡す。そうしないと「まだ起動していない」と誤診して待ち続ける
  - 照合は `--jq` の中ではなく bash 側に置く。今の偽 gh (`tests/bin/test_ci_log.sh`) は `--jq` を解釈せず、
    絞り込み後の行を環境変数 (`GH_FAILED_ROWS` / `GH_RECENT_ROWS`) でそのまま返し、分岐は `--limit 40` / `--limit 60` の文字列で行う。
    照合を `--jq` に埋めると fake は照合を素通りし、照合を外す変異でも緑になる。`--commit` の経路には、`--json` の生の JSON を返す分岐を fake に足す
  - `--branch` は付けない (`-c` だけで絞れる。併用したときの挙動は未実測)
- 該当する run が無いときは、rc≠0 で「まだ起動していない / paths filter で起動しない」を区別できるように出す
  (paths filter で起動しない workflow を待ち続けないように)。同じ commit に複数の run がある
  (re-run の attempt / push と workflow_dispatch が両方起動) ときの返り方は未実測。実装の前に測って、どれを返すかを決める
- cancel を追う待ちは、このオプションを使えば `gh run watch` と組み合わせて数行で書ける。待ちのループ自体を ci-log に入れるかは、
  実装時に決める (入れるなら上限時間と「判定不能」の出力を持たせる)
- `--commit` が無いという古いコメント (`bin/ci-log` の失敗 run を集める箇所) は、実装の前に gh の版を確かめて直す。今の headSha の局所フィルタ (`--limit 40` / `--limit 60`) を
  `--commit` に寄せられるかも見る (件数の上限から run が漏れる形が消える)
  (`--commit` と `--limit` の関係、つまり server 側で絞ってから件数を切るのかは未実測)
- 入口の更新: `.claude/rules/use-ci-log-for-ci-inspection.md` の主なオプションと、ci-log の usage コメント

## 受け入れ条件

- [x] commit と workflow を渡すと、その commit の run の ID だけを返す (headSha を照合している)
- [x] 短縮 SHA を渡しても完全な SHA に展開して引く
- [x] 該当が無い / gh が失敗したときに rc≠0 で区別できる
- [x] `tests/bin/test_ci_log.sh` に、古い run が一覧の先頭に来る fake を置いても正しい ID を返すケースがある
- [x] rule と usage に載っている

## 関連ファイル

- `bin/ci-log`
- `tests/bin/test_ci_log.sh`
- `.claude/rules/use-ci-log-for-ci-inspection.md`

## 進捗

- 2026-10-03: 起票
- 2026-10-03: 反証レビュー (sonnet 1 体、読み取りのみ) を通した。事実の記述 (run 33894864496 の素性・Tests の workflow が 1 つ・
  gh 2.101.0 の `--commit`・ci-log のコメントと `--limit`・fake の作り) は反証されなかった。重複する issue は無かった (610 は別件)。
  指摘 2 件を反映した: 短縮 SHA で `--commit` が黙って空になる (自分でも再現) / 照合を `--jq` に置くと今の fake では検査できない。
  未実測の点 (re-run・workflow_dispatch の複数 run / `--branch` との併用 / `--commit` と `--limit`) は方針に未実測と書いた。
  古い run が先頭に来た原因の候補は、裏の取れるものが無かった。
  起票の commit の message は「反映済み」と書いたが、編集の失敗で反映されていなかった。反映はこの後の commit で入れた
- 2026-10-03: 実装した (commit「feat(ci-log): -i で commit と workflow を名指しして run の ID を返す (634)」)
  - `ci-log -i <workflow> [<commit>]`: commit を `git rev-parse --verify` で完全な SHA にし、`gh run list --commit <sha> --workflow <名前>` で引いて、
    bash の側で headSha を照合した最初の ID を返す。rc: 0 = ID / 4 = run がまだ無い (push 直後・paths filter) / 5 = GitHub に無い
    (push していない) / 1 = gh の失敗 (workflow の名前の誤りを含む) / 2 = 引数の誤り
  - push されたかは `gh api repos/{owner}/{repo}/commits/<sha>` で GitHub に聞く (HTTP 422 = 無い)。ローカルの追跡 ref は使わない
  - 既定の経路 (HEAD の失敗 run を集める) も `--commit "$head_sha"` で絞るようにし、「`--commit` は無い」という古いコメントを直した
  - 実測 (本物の gh 2.101.0): push 済み → ID (headSha を照合して一致) / paths filter で起動しない Karabiner Lint → rc=4 / push していない HEAD → rc=5。
    直近 200 件の run に re-run (attempt>1) と、同じ commit で同じ workflow の重複は 0 件 (複数あれば作成が新しい方を返し、stderr で数を言う)。
    re-run は同じ databaseId のまま attempt が増えるので、watch は最新の attempt を見る (敵対的レビューの確認)
  - テスト 28 件 (14 ケース × bash 5 / 3.2)、変異 11 本すべて red。`make test-lint` / `make test` rc=0
  - 自分で書いた `「$id_wf」` (bash 3.2 の全角の罠) を、633 の lint と `/bin/bash` で走るテストの両方が捕まえた
- 2026-10-03: 敵対的レビュー (opus、読み取りのみ) を 2 周。全数勘定:
  - 1 周目: 取り違えと既定の経路の退行は壊せなかった。指摘 4: 待ち方の例に `--exit-status` が無い (run が失敗しても rc=0) /
    push していない commit も rc=4 で「待てば来る」と区別できない / 「workflow の名前の誤り」は rc=4 ではなく rc=1 / テストの空振り。すべて直した
  - 2 周目: 指摘 3: 例の `gh run watch "$(ci-log -i …)"` は rc を捨てる → `id=$(…); rc=$?` で受ける形に直した /
    git の失敗で rc=5 と嘘をつく、URL や tag だけの push・追跡 ref が古いと rc=5 → 判定の根拠を GitHub の API に変えて解消 /
    remote で巻き戻した後の追跡 ref で rc=4 → API に変えたので解消
  - 3 周目は回していない: 2 周目の修正の各分岐 (API の 200 / 422 / それ以外) は、本物の gh と偽物の両方で直接測り、変異で固定したため
  - 検査していない (記録): 偽の gh は `--jq` と `--limit` を解釈しないので、`sort_by(.createdAt) | reverse`・`--limit 20`・既定の経路の jq の
    headSha の照合は、変異しても緑のまま。本物の gh は `--commit` でサーバ側で絞り、新しい順で返すので実害は小さい。
    HTTP 422 の判定は gh の英語のメッセージの文言に頼る (gh は翻訳しない。文言が変わると rc=1 側に倒れ、「push していない」とは言わない)
- 2026-10-03: CI (5e22e0ed) の Lint (run 37085718059) / Tests (run 37085718073) が success。待つのに `ci-log -i` 自身を使った。Tests のログに `[ok] tests/bin/test_ci_log.sh`。done へ
