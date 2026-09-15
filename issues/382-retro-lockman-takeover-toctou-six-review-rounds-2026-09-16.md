# retro: 引き継ぎの TOCTOU 修正と、敵対的レビュー 6 周 (2026-09-16)

起票日: 2026-09-16
カテゴリ: retro
対象セッション: [366](done/366-bug-lockman-stale-takeover-sometimes-has-two-winners.md) の消化
(副産物: [380](380-bug-lockman-renew-and-release-act-on-name-after-check.md) /
[381](381-bug-lockman-with-renew-latch-stops-renewal-forever.md) 起票、
[359](359-research-lockman-resource-leaks-perf-audit-2026-09-11.md) /
[362](362-bug-lockman-abandoned-timeout-goroutine-leaves-lock.md) /
[363](363-bug-lockman-with-signal-handler-installed-too-late.md) / `done/091` へ追記)

## 数字

| | |
|---|---|
| 敵対的レビュー | 6 周 (観点①②③ + 差分への 3 周)。採用 30 / 却下 1 / 振り分け 3 |
| 変異検証 | 各周で全 red を確認。最終形は 30 本以上 |
| commit | 7 本 |
| `make test` | 7 回 (すべて rc=0) |

## 反省・気づき

### 1. 🚨 「自分の修正が次の周の P1 になる」が 3 周連鎖した

- 1 周目: 回収機構が無く、目印を取った直後の死が掃除まで塞ぐ (**私が持ち込んだ可用性の退行**)
- 3 周目: 2 周目で足した溢れガードが、**その手前の wrap で素通り**されていた
- 4 周目: 3 周目の「Stat で照合してから開く」が **4 つの退行**を同時に作っていた
- 5 周目: 4 周目の診断が**とうに死んだ作成者**の申告で閾値を作っていた

`adversarial-review-own-safeguards.md` §7 (「修正した周はもう 1 周」) は**効いた**。
1 周で止めていたら、2〜5 周目の P1 はすべて残っていた。

**切り出し先の提案**: §7 に「実測: 安全機構の新設では 3 周連続で『前周の修正が P1 になる』が
起きた」を 1 行。**新規ルールは立てない** (発動点は §7 と同じ)。

### 2. 🚨 seam の位置で変異の検出力が変わる — 「破壊より前」でないと素通りする

fd 照合を守る seam を **2 回動かした**。「書き込みの直前」でも「照合の直後」でも、パス照合へ
戻す変異は**破壊がその点より前に済む**ので緑のまま通った。「打刻を得た直後・照合より前」まで
出して初めて red。

**切り出し先の提案**: `mutation-verify-new-tests.md` へ「seam でレースを再現するテストは、
seam を**変異後の破壊的操作より前**に置く。後ろだと変異が緑で通る」を 1 項。

### 3. 🚨 変異の green 化が「テストが消えていた」ことの canary になった

`TestTakeoverClaimGraceBounds` を書き換えるとき、その後ろに追記していたテストとヘルパーを
巻き込んで消していた。気づいたのは**前回 red だった変異が green に転じた**とき。

**切り出し先の提案**: `mutation-verify-new-tests.md` へ「前回 red だった変異が green に転じたら、
実装ではなく**テストが消えていないか**を先に疑う」を 1 項。

### 4. レビュワーの主張を実測したら 1 件が誤りだった

3 周とも「読解による導出」と明記されていたので全件変異を当てた。結果、2 周目の
「世代 id に mtime を混ぜても緑」は誤りで、実際は 3 テストが red だった。
**採用 30 件に対して却下 1 件** — 検閲のコストは十分に見合った。

**切り出し先**: 既存規律どおり (`subagent-model-tiering.md` の検閲)。追記不要。

### 5. 🚨 exit code の罠を 2 形で踏んだ

- `make test ; echo "rc=$?" > file` の**通知**が exit 0 を報告した (ラッパーの rc)。実際は
  lint で rc=2。`verify-execution-not-just-exit-code.md` の「パイプ終端の status」と同族
- `go test` は**`-v` なしだと passing test の stderr を出さない**。診断の warn が
  「0 行」に見えて、レビュワーの「7 回鳴る」を一度は棄却しかけた

**切り出し先の提案**: `verify-execution-not-just-exit-code.md` へ「バックグラウンド実行の
完了通知が返す exit code は**ラッパーのもの**。判定は成果物 (rc ファイル / 失敗ターゲットの
集約行) で行う」を 1 項。2 つ目は Go 固有なので `src/lockman/README.md` か判定スクリプト側へ。

### 6. 診断が「最も危険な操作」へ人を誘導していた

良性の競合 (ミリ秒で解消) のたびに `lockman break` を 7 回勧めていた。`Break` は自分の doc が
「最も現実的に二重取得を作る操作」と書いているもの。**診断は「出す / 出さない」だけでなく
「何を勧めるか」と「誰を名指しするか」まで設計が要る** (名指しも死んだ作成者を出していた)。

**切り出し先の提案**: 新規ルールではなく `adversarial-review-own-safeguards.md` §2
(「沈黙 = 成功」) の隣に「**逆に、鳴らしすぎる診断も害**。良性の状態で危険な操作を勧めると、
機構が防いでいるはずの事故を人手で起こさせる」を 1 行。

### 7. 停止条件を先に決めていなかった

§8 は「gate を書く**前に**脅威モデルと『検出しない形』を書く」と言うが、今回それをやったのは
6 周目の直前。先に書いていれば 5 周目の「診断の質」の指摘を最初から範囲外にできた可能性がある。

**切り出し先の提案**: §8 の発動点を「字句 / 構文 gate」から「**多周回のレビューに入るとき全般**」へ
広げる。ただし発動点の拡張なので、既存節への追記で足りる。

## 残課題

- [ ] 上記 1〜3・5〜7 の切り出し (既存ルールへの追記 6 件。新規ルールは 0 件)。**実行はユーザーの判断待ち**
- [ ] 366 の横展開: [380](380-bug-lockman-renew-and-release-act-on-name-after-check.md) /
      [381](381-bug-lockman-with-renew-latch-stops-renewal-forever.md)
- [ ] 366 が新設した wedge の根治: [362](362-bug-lockman-abandoned-timeout-goroutine-leaves-lock.md) /
      [363](363-bug-lockman-with-signal-handler-installed-too-late.md)
