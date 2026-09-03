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

🚨 **実害は disk セクションの中に留まらない** (反証レビューの補正)。`buildRows` は
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

🚨 `expanded` は既にキー方式で、並べ替えでずれない (red team が確認)。カーソルだけが index。

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
- 🚨 fixture は「退行したら見えるようになる場所」に置く
  (`_claude/rules/mutation-verify-new-tests.md`)。**カーソルより下に挿入する fixture では
  index が偶然一致して素通りする**

## レビュー状態

red team (opus) が実測で再現 → **反証レビュー (opus) も反証できなかった** (引用・発火条件は
現コードと一致、probe で `before=disk:small after=disk:mid` を再現)。P2 の重要度も妥当と判定。

## 適用ログ (2026-09-03)

commit `d6c674d2` (本体) / `9f21c470` (敵対レビューの P1 対応)。

### 入れたもの

- `cursorKey` を足し、`lines()` が rows を組み直した後に **key で index を復元**する
  (`restoreCursor`)
- key が消えていたら近傍の selectable 行へ寄せ、**寄せた事実を知らせる**
- `moveCursor` は動いた先の key を覚える。`start` では key も落とす

### 敵対的レビュー (opus) が出した P1 2 件 — どちらも私が入れた回帰

1. **`G` (末尾へ) が次の描画で巻き戻る**。`defer v.rememberCursorKey()` を dir==0 の
   ブロックより**後**に置いたため、その経路 (G = 末尾へ / 寄せ直し) が key を覚えず、
   `restoreCursor` が古い key の行へ cursor を戻していた。実測:
   `after G cursor=6 / cursorKey="disk:b"` → `after repaint cursor=4`。
   **親 commit では index clamp なので G は効いていた = 私が壊した**。defer を関数先頭へ移した
2. **トーストが本番で一度も出ない**。表示するのは `tui.go` の `case doctorToast:` =
   `handleKey` の戻り値経路だけで、`restoreCursor` は View (lines) から呼ばれる。
   つまり「無言の寄せは同じ問題を再生産する」という補正が**実装として死んでいた**。
   フラグ `cursorFellBack` に残し、**次のキー操作で**知らせる形にした。
   🚨 旧テストは `pendingToast != ""` しか見ておらず、**配線の穴を 1 mm も守っていなかった**。
   `browseModel` 経由でトーストが出ることまで見る形に書き直した

P2 も 1 件: 選べる行が 0 件のフレームで `rememberCursorKey` が key を捨て、index 保持へ
退行していた。空フレームでは保持する。

### 壊せなかったと報告された観点 (次の監査で再生成しないための記録)

- **key の一意性**: `disk/catalog.go` の ID は全て一意 / `brew:%d:%s` は index を含み一意 /
  `svc:` と `svcundiagnosed:` は prefix で分離。人工的に同 ID を 2 つ与えると先頭に付くが、
  **本番でその入力を作る経路は見つからなかった**
- **key が空の selectable 行**: 5 種すべて非空 key を持つ
- **削除導線との相互作用**: `selectedResults()` は `v.selected[Entry.ID]` で引くので cursor 追従と
  独立。`del.active()` の間は `lines()` が `deletePanel` で早期 return して `buildRows` /
  `restoreCursor` を通らない
- **restore のトーストが削除の通知を潰す**逆向きは再現しない (Update で `toast.show` が
  即座に文字列をコピーし、View はその後に走る)

### 変異検証 (7 本すべて red)

key 復元をやめて index の clamp に戻す / `moveCursor` が key を覚えない /
トーストを出さない / defer を dir==0 の後ろへ戻す (= 私が入れていた回帰) /
空フレームで key を捨てる / 寄せたフラグを立てない / `handleKey` で寄せを通知しない。

🚨 「空フレームで key を捨てる」は初回 green で、**fixture が弱かった** (同じ 2 件で戻すと
index が偶然一致する)。戻すときに**行が上へ増える**形にして red にした。

### 残り (この issue では直さない)

- `brew:<i>:<summary>` は index 込みなので、警告が 1 件消えると以降の全行の key が変わり
  「消えた」判定で寄せが起きる (レビューの P3)。`brew:<summary>` にすれば安定するが、
  同じ summary が 2 度出る入力での一意性を確かめる必要があり別 issue の範囲
- レビュワーのぼやき: `restoreCursor` が View から状態を変える構造そのものは残っている
  (key 復元を Update 側へ移すとトーストの配線も自然になる)

### 閉じる前の最終ゲート (opus、2026-09-03)

「閉じてよいか」を red team に通したら **P2 1 件 + P3 2 件**が出た (いずれも私の実装の穴)。

- **寄せた直後の 1 打鍵が飲まれていた**。通知を `handleKey` の先頭で `doctorToast` を返す形に
  していたため、その打鍵が空振りする。実運用の主経路は「削除完了で選択行が rows から落ちた
  直後」で、`q` / `esc` / `d` / `r` / Enter が等しく 1 回死ぬ。
  `takeCursorFellBack()` を seam にし、**tui.go 側で handleKey を呼ぶ前に**出す形へ直した
- 選べる行が 0 件のフレームでは cursor が動いていないのに「寄せた」と言っていた
  (判定を「cursor が実際に動いたか」へ)
- `start()` が `cursorFellBack` をリセットしていなかった

🚨 テスト側の穴も 1 つ直した: `v := m.doctorOv` は **値フィールドなのでコピー**になり、
m 側に何も伝わらない。配線 (tui.go) のテストが緑にならず気づいた。

🚨 「handleKey で飲む形に戻す」の初回変異は**緑だった**。tui.go の配線を残したまま当てたため
そちらが先にフラグを消費していた = **変異が旧回帰を再現していなかった**。旧コードと同じ形
(配線も消す) に当て直して red を確認した。

### レビューが壊せなかった観点 (記録)

`moveCursor` の defer を先頭へ移した副作用は無し (dir != 0 / pgup / pgdown / g / G /
rows 0 件のいずれも)。「古い key が永久に残る」経路も無し (再オープンは必ず start を通り、
restoreCursor は rows 0 件で key を捨てる)。
