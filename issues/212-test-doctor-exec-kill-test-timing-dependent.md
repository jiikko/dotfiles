# 212 test: `TestExecKillsGrandchildOnCancel` は負荷次第で vacuous になりうる (決定論化したい)

起票日: 2026-09-03
出典: issue 205 の red team (opus、攻め口⑤) → **反証レビュー (opus) で主張を訂正**
重要度: P3 (**テストは現に機構を守っている**。決定論でないだけ)
関連: `src/doctor/runner/runner_test.go` の `TestExecKillsGrandchildOnCancel` /
`src/doctor/runner/runner.go` の `cmd.Cancel` / `_claude/rules/avoid-wall-clock-assertions.md`

## ⚠️ 起票時の主張は誤りだった (2026-09-03 に訂正)

初版は「**無条件で緑になる false green**」と書いたが、**反証レビューが production 側の変異で
red を実証**した。私も再現した:

```
baseline:                                    ok   doctor/runner  2.491s
Kill(-cmd.Process.Pid) → Kill(cmd.Process.Pid):  FAIL  (孫が cancel 後も生きて marker を書いた)
```

`Setpgid: true` の削除でも FAIL する (group leader でないので `Kill(-pid)` が ESRCH)。
つまり**このテストはプロセスグループごと殺す機構を実際に守っている**。

red team が緑にしたのは **production ではなくテスト側の遅延を 0 にした**結果で、
「機構が壊れても緑」の証明ではなかった。**変異の当て方が偽物だった**例
(`_claude/rules/mutation-verify-new-tests.md` の「変異は production の機構を戻す形にする」)。

## 残る本物の弱点 (これだけ)

判定が「marker が**無い**こと」なので、**孫が fork される前に cancel が届くと同じ緑**になる。
今は `time.Sleep(300 * time.Millisecond)` で fork を待っており、通常はまず間に合うが、
**CI が高負荷で 300ms 以内に fork が間に合わなければ、その run は何も検査していない**
(flaky red ではなく「flaky に無意味」)。緑の側からは観測できない。

## 直し方 (案)

**孫 pid を受け取ってから cancel し、pid の生死で判定する**。「孫が確かに生まれた」→
「cancel 後に死んだ」の 2 段にすると時計に依存しない。待ち側も時計にしない
(孫が pid をファイルへ書き、それを読めるまで待つ等)。

## テスト観点 (このテスト自身をどう検証するか)

- 変異は **production 側**に当てる: `Kill(-pgid)` → `Kill(pid)` / `Setpgid` の削除。
  どちらも現状 red になる (上の実測)。書き直した後も red のままであること
- 「孫が生まれる前に cancel が来た」場合に**緑ではなく判定不能として落ちる**こと
  (`_claude/rules/adversarial-review-own-safeguards.md` の「判定できなかったを緑にしない」)

## レビュー状態

red team (opus) → **反証レビュー (opus) が主張を否定** → 上記のとおり書き直した。
訂正後の主張 (タイミング依存で vacuous になりうる) は反証されていない。
