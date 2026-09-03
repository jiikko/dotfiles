# awaitCI に commits 外の SHA が入ると諦めに到達せず、80ms の再描画がセッション中回り続ける

出典: audit (responsibility) 2026-09-03 / forge-Minimum。**発火経路を実コードで裏取り済み** (下記)。

## 何が起きるか

`m.awaitCI` (push 直後で CI がまだ 1 つも見えない SHA の集合) に **`m.commits` に存在しない SHA** が
入ると、その要素は**どの経路でも取り除かれない**。結果:

- `spinnerActive()` は `len(m.awaitCI) > 0` を含む → `maybeTick` が恒常的に再アームする
- `tickMsg` の `if m.fetching || len(m.awaitCI) > 0 { m.invalidateLines() }` が
  **80ms ごとに全行を組み直し続ける** (コード注記の実測: `-p` 併用時 332µs / 733KB per frame)
- 画面は静止しているのに、アプリが**アイドルに戻らない**

## なぜ諦めに到達しないか (構造)

`ciPollTargets` は `m.commits` を走査して targets を組む。**commits に無い awaitCI の SHA は
targets に入らない**。そして `ciPollMsg` の case は打ち切りより**前**に早期 return を置いている:

```go
// src/glogx/tui.go: case ciPollMsg
targets := m.ciPollTargets()
if len(targets) == 0 {
	m.ciPolling = false   // ← ここで返る
	return m, nil
}
...
if len(m.awaitCI) > 0 {
	m.awaitAttempts++                       // ← 到達しない
	if m.awaitAttempts >= ciAwaitMaxAttempts {
		m.awaitCI = nil                     // ← 到達しない (2 分の打ち切りが効かない)
	}
}
```

`awaitCI` を空にする経路は 3 つしかなく、どれも閉じている:

| 経路 | なぜ効かないか |
|---|---|
| `settleAwaitCI` | `statuses[sha]` が現れたら消す。commits に無い SHA の status は永久に来ない |
| `ciPollMsg` の打ち切り | 上記のとおり早期 return で到達しない |
| `applyLogData` の `awaitCI = nil` | 次の pull まで起きない (= それまで回り続ける) |

## 入口 (commits 外の SHA が入る経路)

`refetchAfterPush` が `pushAnimTip` を **`m.commits` 所属を確かめずに**入れている:

```go
// src/glogx/tui.go: refetchAfterPush
m.awaitCI = map[string]bool{}
m.awaitAttempts = 0
if m.pushAnimTip != "" {
	m.awaitCI[m.pushAnimTip] = true   // ← membership の検査なし
	m.pushAnimTip = ""
}
for _, c := range m.commits { ... }   // ← こちらは commits 由来なので安全
```

そして **`applyLogData` は `awaitCI` を nil にするが `pushAnimTip` を消さない**
(`m.awaitCI, m.awaitAttempts = nil, 0` だけ)。

### 発火条件

1. push する → `startPushAnim` が `pushAnimTip = <push 時点の tip SHA>` を捕捉 (演出は最大 4.8 秒)
2. **その演出中に** `u` (pull) を押す → `applyLogData` が `awaitCI` を nil にするが `pushAnimTip` は残る
3. その pull で**履歴が書き換わって** tip の SHA が消える (rebase / amend を伴う pull)
4. 演出の着地で `advancePushAnim` → `refetchAfterPush` が、**もう commits に無い SHA** を `awaitCI` に入れる

**silent に壊れる**。build もテストも通り、画面は正常。CPU と再描画だけが止まらない。

## 確認したこと (2026-09-03 実測)

- `refetchAfterPush` の `awaitCI[m.pushAnimTip] = true` に membership 検査が無い (上に引用)
- `applyLogData` は `pushAnimTip` をリセットしない (`awaitCI` / `awaitAttempts` のみ)
- `ciPollMsg` の打ち切りが `len(targets) == 0` の早期 return より**後ろ**にある
- `spinnerActive()` の式に `len(m.awaitCI) > 0` が含まれる
- `settleAwaitCI` は `statuses` に現れた SHA しか消さない

⚠️ **未確認**: 手順 3 の「pull で tip の SHA が消える」を実機で再現していない
(通常の pull なら SHA は残るので発火しない)。**構造としては、commits 外の SHA が入れば
どの経路からでも同じ結果になる**ので、入口の網羅よりガードの方を直す。

## 対応方針

### 局所修正 (これだけでも閉じる)

`awaitCI` の不変条件を **「awaitCI ⊆ commits の SHA」**にする。最小は 2 箇所:

- `refetchAfterPush`: `pushAnimTip` を入れる前に commits 所属を確認する
- `settleAwaitCI`: commits に無い SHA を必ず落とす (毎周期の掃除)

打ち切りの計上を早期 return の**前**へ出すのも併せて行う (targets の有無に関わらず 2 分で諦める)。

### テストと変異

「`awaitCI` に commits 外の SHA を入れて `ciPollMsg` を `ciAwaitMaxAttempts` 回流すと
`awaitCI` が空になり `spinnerActive()` が false へ落ちる」を書く。

**変異**: 加算と上限判定を早期 return の後ろへ戻す (= 今の形に戻す) → red を確認する。
これは「機構を戻す」形の変異なので、実際に起こりうる退行を再現している。

### 構造 (issue 227 と同根)

`awaitCI` のライフサイクルに単一の所有者が無い — 張る (`refetchAfterPush`) / 卒業 (`settleAwaitCI`) /
諦め (`ciPollMsg`) / 破棄 (`applyLogData`) が 4 箇所に分かれているのが根。
CI 取得の会計を型へ閉じる話は issue 224 に分けた。

## 先行事例: 同じ不変条件の「逆向き」だけが守られている

**issue [032](done/032-fix-glogx-bubbletea-tick-gaps.md) (done)** は、まったく同じ tick チェーンの
**落ちる側**を塞いだ issue:

> どちらも「Cmd を落として**アニメの tick が止まる**」型で、症状は「トーストが shown=0 のまま
> 見えない / ビルド失敗が通知されない」という静かな縮退になる。

本件はその**反対向き** — tick が**止まらない**。`maybeTick` / `spinnerActive` の単一チェーン契約は
「必要なときに回る」を守る手当てだけがあり、「**不要になったら止まる**」側は
`spinnerActive()` が参照する述語 (ここでは `len(m.awaitCI) > 0`) が下りることに委ねられている。
述語の集合を空にする責任が誰にも無いと、契約は片側だけ成立する。

→ テストを書くときは**両向き**を 1 つのテーブルに入れると良い
(「この述語が立つと回る」/「この述語が下りると止まる」)。

## 探して見つからなかった範囲 (2026-09-03)

`awaitCI` / `pendingFetches` / `refetchAfterPush` / `ciPoll` を issues 全体 (open + next + pending + done)
で grep したが、**本件と重なる既存 issue は無い** (032 は上記のとおり逆向きで、統合はしない)。

## 関連

- issue 224 (CI 取得の会計に所有者が無い。本件はその帰結の 1 つ)
- issue 227 (同じ「単一の概念が手書きで散る」形)
- issue [032](done/032-fix-glogx-bubbletea-tick-gaps.md) — tick チェーンの落ちる側 (対になる欠陥)
