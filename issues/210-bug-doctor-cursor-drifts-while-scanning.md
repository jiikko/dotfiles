# 210 bug: 走査中に doctor のカーソルが黙って別エントリへ移る (y/Y が別の行をコピーする)

起票日: 2026-09-03
出典: issue 205 の red team (opus、攻め口①)。**再現済み**
重要度: P2 (誤コピー・誤展開。データは壊れないが、ユーザーは別エントリの内容を掴む)
関連: `src/glogx/doctor_view.go` の `lines` / `buildRows` / `diskSection` / `moveCursor` /
issue 180 (Failures 行を selectable にした) / issue 148 (disk 診断)

## 症状

doctor の disk セクションは**結果を Size 降順に並べ替えて**描く
(`doctor_view.go`: `sort.SliceStable(sorted, ...Size > ...)`)。一方 `cursor` は
**`rows` の index** で保持され、`lines()` が `v.rows = v.buildRows(o)` で行を作り直したあとは
`cursor >= len(rows)` の clamp と `moveCursor(0)` しかしない。

走査中は結果が届くたびに行が増え、**大きい結果は既存の行より上へ挿入される**。すると
index は同じまま指す行が変わる = **ユーザーが見て選んだ行と、キーが効く行が食い違う**。

- `y` / `Y`: 別エントリのパス・解説をコピーする
- `Enter`: 別の行を開く

⚠️ **実害は disk セクションの中に留まらない** (反証レビューの補正)。`buildRows` は
disk → svc → brew の順に積むので、走査中の disk 行の挿入は **svc / brew に置いたカーソルも**
ずらす。ただし破壊的操作は無い (削除は未実装) ので P2 据え置き。

## 再現手順 (red team が実測)

`diskResults = [small(100), mid(200)]` → `lines()` → `moveCursor(+1)` で `disk:mid` を選択 →
`receiveDisk(gen 一致, huge(999999))` → `lines()`。

```
before=disk:small  after=disk:mid                    # 1 件挿入で 1 つずれる
before=disk:small  after=disk:huge                   # Failures 行を持つ結果を差した場合
```

発火条件: doctor を開いて**走査中の数秒**に j/k でカーソルを動かしている間。

## 補足: 「View を挟まずキーが来る」形は成立しない (訂正)

issue 205 の攻め口は「bubbletea が Update → View の順なので Msg 2 つが連続すると View を
挟まない」と書いていたが、**bubbletea v2.0.9 は Update ごとに `p.render(model)` を呼ぶ**
(`tea.go:886`)。したがって `rows` は常に最新で、壊れるのは
**「ユーザーが見た行」と「index が今指す行」のずれ**の方。

## 直し方 (案)

カーソルを index ではなく**行キー** (`disk:<ID>` / `brew:<i>:<summary>`) で保持し、
`lines()` で「同じキーの行」を探して index を復元する。キーが消えていたら
近傍の selectable 行へ寄せる。

⚠️ `expanded` は既にキー方式で、並べ替えでずれない (red team が確認)。カーソルだけが index。

キー設計との整合は反証レビューが確認した (`disk:<ID>` / `diskfail:<ID>:<f>` / `svc:<plist>` /
`brew:<i>:<summary>`)。ただし**寄せ先が必ず要る**穴が 2 つある:

- `brew:<i>:<summary>` は**本文が変わればキーごと消える**
- `diskRep` 到着時に `StatusOK && Items==0 && Failures==0` の行が落ちる (`diskSection`)

「近傍の selectable 行へ寄せる」で足りるが、**寄せた事実をユーザーに見せる**こと
(黙って別の行に付くのが元の症状なので、無言の寄せは同じ問題を再生産する)。

## テスト観点

- `receiveDisk` で行が**カーソルより上に**挿入されたとき、選択が同じキーに留まること
  (Msg を直接 `Update` に流す形。実機の TUI は起こさない)
- 変異で red を見る: キー復元を外す / clamp だけに戻す
- ⚠️ fixture は「退行したら見えるようになる場所」に置く
  (`_claude/rules/mutation-verify-new-tests.md`)。**カーソルより下に挿入する fixture では
  index が偶然一致して素通りする**

## レビュー状態

red team (opus) が実測で再現 → **反証レビュー (opus) も反証できなかった** (引用・発火条件は
現コードと一致、probe で `before=disk:small after=disk:mid` を再現)。P2 の重要度も妥当と判定。
