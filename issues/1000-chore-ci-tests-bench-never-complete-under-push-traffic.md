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

- [ ] 方針を決める
