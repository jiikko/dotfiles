# 032 fix: bubbletea の使い方の穴 2 件 (tick を落とす経路) を塞ぐ

glogx の bubbletea の使い方を監査して見つかった 2 件 (2026-07-31)。どちらも「Cmd を落として
アニメの tick が止まる」型で、症状は「トーストが shown=0 のまま見えない / ビルド失敗が通知され
ない」という**静かな縮退**になる。

着手が保留中の理由: `tui.go` を並行セッションが編集中 (`issues_state.go` 抽出のリファクタ)。
両方とも `tui.go` の 1 箇所ずつなので、あちらのコミットが入ってから当てる。

## 1. 複数 rune が 1 イベントで届く経路だけ maybeTick を束ねていない (P2)

`tui.go` の `case tea.KeyPressMsg` は 2 経路ある。

```go
if runes := []rune(msg.Text); len(runes) > 1 {
	...
	return m, tea.Batch(cmds...)        // ← maybeTick が無い
}
model, cmd := m.handleKey(msg.String())
return model, tea.Batch(cmd, m.maybeTick())   // ← 単キー経路は束ねている
```

単キー側のコメントは「ハンドラ内部で出したトーストが `return m, nil` されても tick が確実に回る」
と不変条件を宣言しているのに、複数 rune 側はその保証から外れている。分解ループが回った打鍵で
トースト (コピー結果・バリデーション失敗) や glide が始まると、他に tick を回す理由がなければ
アニメが 1 フレームも進まない。

この経路は v1 で実際に起きた回帰の保険として残っているもの (v2 のデコーダは grapheme 単位)。
つまり「普段は通らないが、通ったときに壊れる」形で、気づくのが最も難しい。

修正: `return m, tea.Batch(append(cmds, m.maybeTick())...)`。`maybeTick` は single-flight なので
ループ内で毎回呼んでも二重には走らない。

回帰テスト: `Text` に 2 文字 (例 `"yy"`) を入れた `tea.KeyPressMsg` を Update に流し、
`m.ticking` が true になることを固定する (「cmd != nil」で書くと single-flight のせいで
別経路が先に tick を張っていても通ってしまうため、不変条件は `m.ticking` で見る)。

## 2. autobuildMsg で notify と keepWatching が同時に立つと監視チェーンが切れる (P3・潜在)

```go
res, notify, keep := m.autobuild.handle(msg.result, timeNow())
if notify {
	...
	return m, m.maybeTick()   // ← keep を見ていない = tickCmd() を張り直さない
}
if keep {
	return m, m.autobuild.tickCmd()
}
```

`handle` が `(autobuildStarted, true, true)` を返す形になっているのに、その組み合わせで
`tickCmd()` が捨てられる。捨てると監視が止まり、**その後のビルド失敗が通知されなくなる**
(監視は失敗を検出する唯一の経路)。

今は到達しない: 「ビルド中」は `newAutobuildWatch` が seed した `pending` を `Init` が消費するので、
Update 側で `notify=Started` になる経路が無い。ただし `handle` の契約 (戻り値 3 つ) は同時成立を
許しており、`Init` の消費をやめるリファクタで静かに発火する。

修正: `return m, tea.Batch(m.maybeTick(), keepCmd)` の形にして keep を落とさない
(`keep` が false なら nil を束ねるだけ)。

## 3. 同じ箇所のコメントが実装と乖離している (ついでに直す)

`case autobuildMsg` のコメントが「他のトーストが出ている間は autobuild 側が結果を保持して次の
tick で出し直す」と書いているが、その調停はトーストがスタック化した時点 (同日) に消えており、
`handle` から `busy` 引数も落ちている。読者が存在しない機構を探すことになるので削る。

## 監査で問題が無かった点 (再監査の重複を避けるため記録)

- **Cmd クロージャがモデルを触っていない**: `func() tea.Msg` / `tea.Tick` の本文をブレース深さ
  追跡で機械的に走査して、レシーバ (`m.` / `v.` / `o.`) 参照が 0 件。goroutine から読む値は
  UI スレッドで束縛してある (`usage_overlay.go` の `prev := o.snap` に理由コメントあり)
- `go test -race ./...` green
- **`View()` が純粋**: View 系関数の本文に I/O (`exec.Command` / `os.*` / `http.` / `time.Sleep`) が 0 件
- **ctx のライフタイム**: `cancel` は必ずクロージャ内で `defer` されている
  (呼び出し側で defer すると Cmd 実行前に cancel される古典的バグ) — `action_modal.go` / `usage_overlay.go` とも正しい
- **tick の起動条件が二重管理されていない**: `spinnerActive()` と `tickInterval()` が見る
  アニメ状態の集合が一致 (glide / diff glide / toast / issues viewer)。`issuesView.animating()` は
  listGlide / bodyGlide / drawer / 流し込みの 4 つを漏れなく含む
- **v2 API**: キーは `tea.KeyPressMsg`、外部エディタは `tea.ExecProcess` (端末状態の復帰を
  bubbletea に任せる正しい形)

## 完了条件

- 複数 rune 経路でも `m.ticking` が立つ (テストで固定)
- `autobuildMsg` で keep を落とさない
- 乖離コメントを削除
- `make lint` 0 issues / `go test -race ./...` green
