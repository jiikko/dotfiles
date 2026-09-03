# 212 test: `TestExecKillsGrandchildOnCancel` は負荷次第で vacuous になりうる (決定論化したい)

起票日: 2026-09-03
出典: issue 205 の red team (opus、攻め口⑤) → **反証レビュー (opus) で主張を訂正**
重要度: P3 (**テストは現に機構を守っている**。決定論でないだけ)
関連: `src/doctor/runner/runner_test.go` の `TestExecKillsGrandchildOnCancel` /
`src/doctor/runner/runner.go` の `cmd.Cancel` / `_claude/rules/avoid-wall-clock-assertions.md`

## 🚨 起票時の主張は誤りだった (2026-09-03 に訂正)

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

## 適用ログ (2026-09-03)

commit `?`。

判定を「marker が無いこと」から**孫の生存という状態**へ変えた。3 段: 孫の pid を読めるまで
待つ (= 確かに生まれた) → cancel → その pid が死ぬまで待つ。孫が生まれなければ pid が読めず、
合格でも不合格でもなく **判定不能として落ちる**。

### 🚨 書き直しの途中で旧版より弱いテストを作りかけた

孫の pid を `(echo $$ …)` で取っていたが、**サブシェルの中の `$$` は POSIX では起動シェル
(= 直接の子) の pid** で、孫の pid ではない (実測: 60619 が親 sh、孫は 60621)。そのため
検査対象が直接の子になり、`Kill(-pgid)` → `Kill(pid)` の退行を**素通しした**。
旧版の marker 方式は同じ変異で FAIL していたので、明確な弱体化だった。
親から見た `$!` で取る形に直した。

### 変異検証 (production 側に当てる)

```
Kill(-pgid) → Kill(pid)   FAIL
Setpgid を外す             FAIL
cmd.Cancel を既定へ戻す      FAIL
孫を作らない形             判定不能として FAIL (緑にしない)
```

🚨 起票時の「無条件で緑になる false green」は**反証レビューが否定した**もので、本文は
その訂正を反映済み (production の変異では現に red になる)。残った本物の弱点
「孫が fork される前に cancel が届くと vacuous」だけを直した。

### 閉じる前の最終ゲート (opus、2026-09-03) — 弱体化なし

- macOS の `/bin/sh` は **bash 3.2.57** (dash ではない)。`(exec sleep 30) & echo $!` の `$!` は
  実測でサブシェル (exec 後の sleep) の pid で、`ppid` は `sh` = **確かに孫**。
  bash 3.2 / zsh の両方で同じ (dash は macOS に無いので未確認)
- `Kill(-pgid)` → `Kill(pid)` の変異はビルドが通り、テストは red
  (`孫 (pid=61614) が cancel 後も生きている`)
- `processAlive` の pid 再利用は**偽 FAIL 方向のみ**で偽 PASS を作らない

この issue は**単独で閉じてよい**と判定された。
