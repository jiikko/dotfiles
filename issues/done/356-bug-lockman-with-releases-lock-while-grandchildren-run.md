# lockman with が「子孫がまだ走っている最中に」ロックを手放す

起票日: 2026-09-11
カテゴリ: bug / priority: **medium**（`with` の最初の利用者が現れたら high。理由は下の「重要度」節）
対象: `src/lockman/with.go` の `runWith`
出典: resource-leaks 監査 2026-09-11（[issue 359](../359-research-lockman-resource-leaks-perf-audit-2026-09-11.md)）
反証レビュー: 1 周実施（指摘を反映済み。詳細は 359）

## 問題

🚨 **[363](../363-bug-lockman-with-signal-handler-installed-too-late.md) は trigger が違う** (2026-09-12 追記)。本 issue は**子が正常終了した経路**、あちらは
**シグナルが `signal.Notify` の導入より早く届いた経路** (実測 20/120 で lock 残存、うち 9 件は孤児の子つき)。まとめて直すなら両方の経路を見る。


`runWith` は子を `Setpgid` で独立したプロセスグループに置くが、**グループを止めるのは
(a) シグナルを受けたとき (b) lease を失ったとき の 2 経路だけで、子が正常終了した経路には
無い**。結果、資源に書き手が残っているのにロックを手放す形が 2 通りある。

### 経路 1: 子が孫を残して `exit 0`（再現済み）

1. `cmd.Wait()` が返る → `case err := <-done:` → 即 return
2. `defer l.Release(meta.Token)` が走り **ロックが解放される**
3. 孫はプロセスグループに残って走り続ける
4. 直後に別のプロセスが同じディレクトリの acquire に成功する

### 経路 2: `--on-lost kill` が SIGTERM しか撃たず、昇格も上限も無い（未再現）

```go
case <-ticker.C:
    if err := l.Renew(meta.Token); err != nil {
        lost = true
        if onLostKill {
            _ = syscall.Kill(-pgid, syscall.SIGTERM)   // SIGKILL への昇格が無い / 待たない / 上限も無い
        }
    }
```

lease を失った = **他者が既に引き継いでいる**状態。ここで子が SIGTERM を trap / 無視する
プログラム（`ffmpeg` を含め普通にある）だと、子は生き続け、`with` は子が終わるまで
返らないので**その状態が無期限に続く**。issue 091 は

> `--on-lost=kill|warn` (既定 `kill`) で**子プロセスを止められる**ようにする (091:282)

と書いており、TERM を 1 回撃つだけではこれを満たしていない。

**この経路のテストは 1 本も無い。** `exitWithLost` の出現は定数定義（`main.go`）と
`with.go` の return、`main_test.go` の `TestExitCodesDoNotCollide`（定数の重複検査だけ）
のみで、`lost` 分岐・ticker・`sigCh` のどれも実行されていない。091 の受け入れ条件
「`with` の自動 renew が効いている（renew を止める変異を当てて赤になることを確認する）」も
未達。

## 現時点の影響範囲（誇張しないための注記）

`grep -rn 'lockman with' --include='*.zsh' --include='*.sh' --include='Makefile' --include='*.go'`
の当たりは `zshlib/_av1ify_lock.zsh` のコメント 1 行だけで（フィルタを外すと
`bin/lockman` のヘッダの用例も当たる）、**`with` を実行している production コードは無い**。
av1ify は acquire / renew / release を直に叩いている。

**この危険は `with` 固有ではない。** `acquire; job; release` 型も同型で、実際に
`zshlib/_av1ify_lock.zsh` の `__av1ify_lock_release` は隣接する失敗モード（孤児 renewer が
lease を持ち続ける）のために **`pgrep -P` で子孫を kill してから release している**。
つまり「解放の前に子孫を止める」は既に repo 内に前例がある。

🚨 **issue 091 は孫の封じ込めを約束していない。** 091 が約束しているのは
`--on-lost=kill|warn` で子プロセスを止められること（:282）と、`with` の子が異常終了・
シグナル死しても解放されること。ロック自体は実際に解放されているので、usage の
「取得 → 実行 → 確実に解放」は文字どおりには満たされている。**欠陥は実在するが、
「`with` の主張が成立していない」は言い過ぎ**（反証レビューの指摘 P3-3 を反映）。

## 発火条件

### 経路 1 の再現（実測済み）

子が孫を background に置いて自分だけ終わる形。実務で普通に起きる:
`sh -c 'cmd & exit 0'` / 子スクリプトが末尾で daemon を起こす / `make -j` が子を残す。

```
lockman with $D --ttl 30s -- /bin/sh -c "( sleep 20; echo alive > $MARK ) & exit 0"
```

| 観測 | 結果 |
|---|---|
| `with` の rc | 0 |
| `.lockman/lock` | **解放済み**（`lock` が無い） |
| 孫の生存 | `sleep 20` が生存 |
| 直後の `lockman acquire $D` | **rc=0（取得成功）** |

**解放後の保持者と孫が同じ資源へ交互に書いた実測**（`with` が返った直後に別プロセス B が
acquire し、両者が同じログへ追記）:

```
A tick 1     ← 解放済みのはずの保持者の孫
B write 1    ← 新しい保持者 (lease を持っている)
A tick 2
B write 2
A tick 3
B write 3
A tick 4
A tick 5
A tick 6
```

環境: macOS 15（Darwin 24.6.0）/ ローカル APFS / `src/lockman` を scratchpad へ複製して
`go build` した実バイナリ。

### 経路 2 の発火条件（未再現）

`--on-lost kill`（既定）で、子が SIGTERM を trap / 無視するプログラムであること。
**再開の trigger**: SIGTERM を無視する子（`trap '' TERM` した sh でよい）で lease を
失わせて、子が生き残ることを再現できたとき。

## 推奨対応（どれも trade-off があるので選択が要る）

🚨 **前提: `Setpgid` ベースの回収が届くのはプロセスグループに残った子孫だけ。
`setsid()` した子孫は下の A も B も取りこぼす**（反証レビューが Darwin 24.6.0 で実測:
グループへ SIGTERM を撃つと非 setsid の孫は死ぬが、`setsid()` した孫は新しい pgid へ
移っており生存する）。「入れたから閉じた」にならないので、どの案を採ってもこの限界を
明記すること。

| 案 | 効く範囲 | 失うもの・限界 |
|---|---|---|
| A. 正常終了時もグループへ SIGTERM → 猶予後 SIGKILL してから return | グループに残った子孫の孤児と二重書き手 | **`setsid()` した daemon には届かない**。グループに残ったまま意図的に起こす子（`start-server &`）は殺してしまう。opt-out（`--no-reap` 等）が要る |
| B. グループが空になるまで待ってから Release | 同上、ただし | **`setsid()` した孫はグループから消えているので即「空」と判定して Release へ進む** = hang しない代わりに静かに排他を破る。A より悪い |
| C. 「`with` が守るのは直接の子だけ」と明文化し、`Setpgid` コメントの過大申告を訂正 | ドキュメントの嘘だけ | 二重書き手は残る。ただし 091 が孫の封じ込めを約束していない点では、既存の設計意図に最も近い |
| D. 経路 2 だけを直す（`--on-lost kill` を TERM → 猶予 → KILL に昇格させ、上限を置く） | 経路 2（091:282 の未達を埋める） | 経路 1 は残る。ただし 091 の明文の要求を満たすので**単独でも価値がある** |

`with.go` の `Setpgid` の直上コメントは

> 子を独立したプロセスグループに置き、まとめて止められるようにする
> (**子が孫を作ったまま残るのを防ぐ**)。

と書いているが、正常終了経路に kill が無いので「防ぐ」は成立していない。**どの案を
採ってもこのコメントは訂正が要る**（`_claude/rules/comment-no-restate-enforced.md`:
成立していない不変条件を残さない）。

🚨 **どの案でも、入れる前に上の「交互書き込み」をテストに落として変異で red を
見ること。** 「孫を kill する」実装だけ入れて交互書き込みを固定しないと、次に猶予秒数を
いじった人が同じ穴を戻せる。

### 実装時の注意（反証レビューが repo 内で確認した分）

- `main_test.go` の `with` テストは 3 本（`TestWithPassesThroughChildExitCode` /
  `TestWithReportsBusyWithoutCollidingWithChild` / `TestWithRequiresCommand`）で
  **どれも孫を作らない**。`Wait` 後のグループ kill は空グループ（ESRCH）に当たるので
  1 本も壊れない
- ただしこれらは `run()` を**テストプロセス内**で呼ぶので、猶予を固定 `sleep` で
  実装すると 3 本すべてが毎回その秒数を払う（条件のポーリングにする。
  `_claude/rules/avoid-wall-clock-assertions.md`）
- 🚨 **pgid の計算を誤って 0 にすると `kill(0, …)` は「呼び出し側のプロセスグループ」を
  撃つ**（テストでは `go test` 自身、本番では呼び出し元シェル）。`pgid > 1` を assert する
- 案 A は `cmd.Wait()` で**リーダーを回収した後**にグループへ撃つので、その時点で pgid は
  再利用可能になっている。[issue 340](340-risk-av1ify-lock-unverified-residuals.md)
  項目 2（av1ify の pgrep → kill の PID 再利用。「直さないと決めた」）と同クラスなので、
  A を採るなら同じ扱い（記録 + 再開 trigger）で明記する

## 重要度

`with` の production 呼び出し元は 0 件だが、**issue 091 が `with` を「主用途として推す」と
明言している**（:381「`acquire` + `trap` は `set -e`・サブシェル・`kill -9` で簡単に漏れ」/
:293「長い処理は `with` を使う。これを『推奨』ではなく `--help` の最初に書く」）。
最初の利用者がそのまま踏む。

それでも **high ではなく medium** に置く理由:

- 091 は孫の封じ込めを一度も約束していない（上記）
- 同じ危険は release-when-done の全パターンに内在し、production の av1ify は自分で対策を持っている
- **推奨案 A / B が両方 `setsid` に破られる**ので、high が含意する「直せば閉じる」が成立しない

**high へ上げる trigger**: `lockman with` を実行する production コードが入ったとき。

## todolist

- [x] 案の選択 → **D + C**（2026-09-15、ユーザー判断）
- [x] 経路 2 を SIGTERM を無視する子で再現し、テストに落とす（091:282 の未達を埋めた）
- [x] `with.go` の `Setpgid` コメントの過大申告を訂正する
- [x] `setsid` した子孫を取りこぼす限界を、コードコメントと本 issue の両方に残す
- [x] 変異検証: 採った対処を外す変異で、新テストの**そのケースが** red になることを確認
- [ ] ~~「解放後の保持者と孫の交互書き込み」を `with_test.go` に落とす~~
      → **やらない**。C を採った＝経路 1 は直さないと決めたので、固定すべき不変条件が無い
      （直していない挙動をテストに焼くと、将来 A を採るときに「正しい挙動」を先に消す作業が要る）

## 採った方針と、あえて残した穴（2026-09-15）

**D（経路 2）**: `--on-lost kill` を **TERM → 猶予 5s → KILL** に昇格させ、**昇格は 1 回だけ**に
した。tick ごとに撃ち直すと猶予が毎回振り出しに戻り、SIGKILL へ永久に到達しない
（上限の無い再試行は上限が無いのと同じ）。091:282「子プロセスを止められるようにする」を満たす。

**C（経路 1）**: 正常終了時の孫は**直さない**。`Setpgid` の直上コメントの過大申告
（「子が孫を作ったまま残るのを防ぐ」）を訂正し、実際に撃つ 2 経路と、
**`setsid()` した子孫にはどの経路でも届かない**限界を書いた。

🚨 **残る穴（承知のうえで残した）**: 子が孫を残して `exit 0` すると、孫が走ったまま
ロックが解放される。issue 冒頭の「解放後の保持者と孫が交互に書く」は**今も起こりうる**。
A を採らなかったのは、正常終了時にグループを薙ぐと**意図的に起こす background の子**
（`start-server &`）まで殺すことになり、`--no-reap` の opt-out を新設する必要があるため。
**A へ倒す trigger**: `lockman with` を実行する production コードが入り、その子が
孫を残す形だったとき。

## 併せて入れた安全弁

`killGroup(pgid, sig)` を新設し、**`pgid <= 1` には撃たない**。`kill(0, sig)` は
「呼び出し側のプロセスグループ」を撃つので、pgid の計算を誤ると本番では呼び出し元シェル、
テストでは `go test` 自身を殺す（実装時の注意として本 issue が挙げていた項目）。

## 結果（実測）

経路 2 のテストは着手時 **0 本**だった。変異検証:

| 変異 | 結果 |
|---|---|
| 昇格の本体を TERM 1 回に戻す（修正前の挙動） | `TestOnLostKillEscalatesToSigkill` **RED**（安全網 20s で赤）。`TestOnLostWarnDoesNotKillChild` は PASS のまま = ケースが分かれている |
| `killGroup` の pgid ガードを無効化 | **隔離セッション（`setsid`）が SIGTERM で自死**。assert に到達する前に「撃ってはいけない相手を撃つ」ことが観測された |

🚨 変異 1 本は**ビルド不能**（`escalate` が未使用になる）で第 3 の結果として扱い、当て直した。
🚨 pgid ガードの変異は、失敗すると**自分のプロセスグループを撃つ**ので、
`os.setsid()` した子プロセスに隔離して実行した（そうしないと測定側が死ぬ）。

`go test ./...` rc=0 / `go vet` OK。

## 進捗

- 2026-09-11: 起票。経路 1 を実測、経路 2 は機構を確認（未再現）。反証レビュー 1 周を
  通し、framing の過大申告・案 A/B の限界・pgid 再利用の見落としを反映
- 2026-09-15: **D + C** を実装（ユーザー判断）。経路 2 の昇格・`killGroup` のガード・
  コメントの訂正。経路 1 は残す判断を trigger 付きで記録
  → commit `fix(lockman,356): --on-lost kill を TERM → 猶予 → KILL へ昇格させ、Setpgid の過大申告を訂正`

## 残タスク

- ~~未再現: 経路 2~~ → **再現してテストに落とした**（2026-09-15）。
  `sh -c "trap '' TERM; sleep 60"` を子にすると、昇格が無い版では `with` が返らない
- 未検証: `setsid` した子孫まで回収する手段（プロセスグループでは届かない）。
  取りこぼしを許容するなら記録で閉じる
- スコープ外: [issue 301](301-bug-parallel-runner-leaves-orphan-grandchildren-on-term.md)
  （parallel runner の孫残留）自体の対処

## 関連

- [issue 301](301-bug-parallel-runner-leaves-orphan-grandchildren-on-term.md) — 同族（孫の残留）
- [issue 340](340-risk-av1ify-lock-unverified-residuals.md) 項目 2 — pgid 再利用（案 A が同クラスの risk を持ち込む）
- [issue 357](357-bug-lockman-with-bypasses-io-timeout.md) — 同じ `runWith` の別の欠陥
- [issue 091](091-feat-lockman-directory-lease-lock.md) — 仕様の正本（:282 / :381 / :293）
- [issue 359](../359-research-lockman-resource-leaks-perf-audit-2026-09-11.md) — この issue の出典（監査記録）
