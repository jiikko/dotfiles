# 1000 (chore): push の多い時間帯に、CI の Tests と Bench が cancel-in-progress で何時間も完走しない

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-30〜）

起票日: 2026-10-30

## 概要

retro 999 の P-2。`.github/workflows/tests.yml` と `bench.yml` は `concurrency: group: ${{ github.workflow }}-${{ github.ref }}` と
`cancel-in-progress: true` を持つ。dotfiles には並行セッションが数分おきに push するので、master の Tests / Bench は次の push で
取り消される。push の間が 10〜15 分 (Tests の 1 run の所要) より短く続く間は、完走する run が途切れる。

## 実例 (2026-10-29〜30)

- 975 の commit cd0fc2ea2 から後の 8 commit の Tests / Bench の run がどれも取り消された (15:33〜16:03 UTC)。取り消された run の
  再実行 (`gh run rerun`) も、同じ group の新しい run に即取り消された
- cd0fc2ea2 を含む commit で完走した run (c0ff0f9d4、16:23 UTC) を見るまで約 2 時間かかった
- 971 でも同じ形で 3 回取り消され、再実行して完走させた
- 検証の規則 (verify-execution の「取り消された run は、それを含む後の commit の run で確かめる」) どおり祖先で探しても、push が
  詰まって続く間は完走した run が無い。間が空けば完走する (2026-10-30 00:32〜02:01 UTC の Tests は成功 4・取り消し 4。所要は約 10〜15 分)

## 対応方針 (未決。判断が要る)

候補:
1. master への push では取り消さない (`cancel-in-progress: ${{ github.ref != 'refs/heads/master' }}`)。全 push の run が走るので、
   runner の時間と待ち行列が増える (Tests の 1 run は約 10〜15 分。push が数分おきなら列が伸び続ける)
2. 取り消しは残し、一定の間隔 (例: 1 時間ごと) に schedule で master の最新を 1 本走らせる。今の workflow に `schedule:` は無いので新しい
   trigger になる。schedule の run の `github.ref` は既定のブランチなので、今の group (`${{ github.workflow }}-${{ github.ref }}`) のままだと
   push の run と同じ group で取り消される。group に `github.event_name` を入れるなどして明示的に分ける。検証したい commit を含むかは祖先で見る
3. 取り消された run が一定の本数続いたら、次の run を取り消さない (実装が複雑。採るなら 1 か 2 で足りないときだけ)

決めるときは、runner の時間 (GitHub の無料枠・macOS runner の倍率) と、検証を待つ側の待ち時間の両方を見る。

## 関連ファイル

- `.github/workflows/tests.yml`・`.github/workflows/bench.yml` の concurrency
- `.claude/rules/use-ci-log-for-ci-inspection.md`・`_claude/rules/verify-execution-not-just-exit-code.md` (取り消された run の扱い)

## 進捗

- [x] 起票時の反証レビュー (sonnet、読み取りのみ): 「どの commit も完走しない」は言い過ぎ (間が空けば完走する) → 直した。
  候補 2 の schedule の run は group を明示的に分けないと取り消される → 書き足した。候補 1 の式は有効

- [x] 方針を決める: 候補 2 (ユーザー指示「1000 は候補 2 でやって」2026-10-30)
- [x] 実装 — commit「ci: push が続いて Tests / Bench が完走しない間も、毎時の schedule の run で master の最新を検証する (issue 1000)」
  - tests.yml・bench.yml に毎時 17 分の schedule。concurrency の group は schedule と push で分ける
  - schedule-gate (`scripts/ci_schedule_gate.sh`): その commit を本体の job が既に検証したか (job の一覧で、schedule-gate 以外の job が
    success / failure で終わった run があるか) を見る。緑なら gate だけで緑、赤なら gate だけの run を取り消す、未検証なら本体を走らせる。
    走っている途中の run は数えない (push が続く場面で次の push に取り消されるため)。問い合わせの失敗は本体を走らせる
  - bin/ci-log -i は取り消された schedule の run を飛ばす。引数なしの ci-log の窓を 60 → 200 本
  - runner は GitHub の hosted (repo は public で課金なし)。gate も規約どおり xcode-27 に置いた
- [x] テストと変異 (10 本 red)・repo 全体の make test rc=0・本物の GitHub に判定を読み取りで打って確かめた (直近 8 commit で本体が緑で
  走り切ったのは 1 つだけ)
- [x] 敵対的レビュー (opus) 3 周。直したもの・受けたものは commit message に書いた。受けた: schedule の起動は負荷で遅れたり落ちたりしうる /
  偽の gh は jobs の filter=latest を模していない
- [ ] push の後の最初の schedule の run を本物の Actions で見る
  - 2026-10-30 15:33 時点で未起動: 14:17・15:17 とも schedule の run が 0 件 (`gh run list --event schedule` が `[]`)。tests.yml・bench.yml とも state=active、master の定義に `cron: '17 * * * *'` がある。新しく足した cron は最初の起動が遅れることがあるので、次に schedule の run が出たときに gate の判定 (ログの ci_schedule_gate の行) と本体の走り方を見る。同じ時間帯に push の Tests は 3 本続けて完走していた (05:50Z・06:05Z・06:23Z)
