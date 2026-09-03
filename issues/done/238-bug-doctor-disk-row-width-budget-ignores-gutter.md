# 238 bug: ディスク行の幅予算がカーソル欄 2 桁を数えておらず、狭い端末で「❓ 走査できず」が切れる

起票日: 2026-09-04
出典: audit (ux / ui-components) 2026-09-04 / doctor スコープ。**指摘は実コードと termwidth の実測で裏を取った**
重要度: **P2**（フレーム最小構成では常時発火。issue 182 が入れた「マークを優先して残す」規律が達成できていない）
対象: `src/glogx/doctor_view.go` の `diskSection`（`labelW` の算出）/ `doctorDiskRowFixedW` / `doctorMaxMarkWidth` / `lines`

## 症状

`lines` は行頭に**カーソル欄 2 桁**（`"  "` / `"▶ "`）を足してから `truncateDisp` で幅に切る。
しかし `diskSection` の予算は `room := o.width - doctorDiskRowFixedW - doctorMaxMarkWidth()` で、
その 2 桁を引いていない。**同じファイルの `sectionHeader` は `inner := o.width - 2 // 行頭のカーソル欄 2 桁` と
正しく引いており、ファイル内で非対称**（`doctor_view.go:587` と `:670`）。

## 発火条件（実測）

`contentWidth <= 66`（フレーム有効なら端末幅 <= 73。`frameMinWidth = 60` なので**フレーム最小構成の
contentWidth 53 では常時**）かつ `StatusFailed` の行があるとき。

| contentWidth | labelW | 行幅(gutter 込) | 超過 |
|---|---|---|---|
| 53（フレーム最小） | 28 | 55 | **+2** |
| 60 | 35 | 62 | **+2** |
| 65 | 40 | 67 | **+2** |
| 66 | 40 | 67 | +1 |
| 67 以上 | 40 | 67 | 0 |

マークの実測幅は `✅ 安全`=7 / `🚨 注意`=7 / `⛔ 要確認`=9 / `🚫 対象外`=9 / `❓ 走査できず`=**13**。
超過が噛むのは最長の 13 だけなので、**切れるのは「走査できず」の行のマーク**（`❓ 走査で…`）。
issue 182 が `doctorMinLabelWidth` / `doctorMaxMarkWidth` を入れた目的（「マークより先にラベルを削る」
「マークを優先して残す」）が gutter ぶんだけ未達。

## なぜ既存テストで出ないか

`doctor_view_test.go` の `doctorTestOpts` は**引数が `page` だけで width は常に 100 固定**。
`doctor_cleanup_test.go` の 4 箇所も `width: 100`。**doctor の描画テストは全部 width 100** で、
182 が新設した縮退経路が一度も実行されていない（`doctorLabelWidth` / `doctorMinLabelWidth` /
`doctorMaxMarkWidth` / `doctorDiskRowFixedW` を参照するテストは 0 件）。

## 直し方

予算から `cursorGutterWidth`（`box.go` の共有定数 = 2）を引く。あわせて **doctor だけが
`"  "` / `"▶ "` のリテラルを持っている**ので共有定数へ寄せる（`issues_view` は
`cursorGutterWidth` を `fixed` に含め、`status_view` も `cursorGutterMark` / `cursorGutterBlank` を使う）。
寄せれば予算の出典が 1 つになり、この形のずれが構造的に起きなくなる。
テストは width 40 / 53 / 60 / 66 / 67 を掃く。

## 副次（同じ修正で見るもの）

`doctorMaxMarkWidth` の一覧は 5 語彙だが `disk.Mark` は 6 語彙を返す（`🔎 未検証`=9 が一覧に無い）。
今日は max が 13 のままなので無害だが、予備の出典と実際の語彙が同期していない（突合テストも無い）。
🚨 語彙の出典は 2026-09-04 に `disk.Mark` へ一元化された（issue 222）ので、
`doctorMaxMarkWidth` もそこから導ける。

## 既存 issue との関係

issue 182 は done で「対応済み」。issue 053（issues viewer の幅）は**同族だが別の形**
（あちらの失敗モードは `clipToWidth(width<=0)` の素通し、こちらは予算が gutter を数えないこと）。
053 が残した「同型の穴が status viewer 側にもある可能性（未確認）」の名指し先も status viewer で、
doctor ではない。237 / 236 には無い。

🚨 **共有できるのは幅だけ**: `box.go` の `cursorGutterMark` は `"→ "` で doctor は `"▶ "` を使う。
記号まで寄せると見た目が変わるので、寄せるのは `cursorGutterWidth`（= 2）に限る。

## 決着 (2026-09-04)

- `diskSection` の予算から `cursorGutterWidth`（`box.go` の共有定数 = 2）を引いた。
  `sectionHeader` は元から `o.width - 2` と引いており、ファイル内の非対称を解消した
- 副次で挙げていた「`doctorMaxMarkWidth` の一覧が 5 語で `🔎 未検証` が抜けている」も直した。
  **手で並べるのをやめ**、`disk.Mark` に代表の Result を通して 6 語を導く `doctorMarkVocabulary` にした
  （語の出典は issue 222 で `disk.Mark` へ一元化済みなので、そこから自動で追従する）

### 検証

- `TestDoctorDiskRowFitsWidthIncludingGutter`: width 40 / 53 / 60 / 65 / 66 / 67 / 100 を掃き、
  **行が幅を超えないこと**と**最長マーク「❓ 走査できず」が切れないこと**を固定。
  🚨 doctor の描画テストは全部 width 100 固定で、182 が入れた縮退経路が一度も走っていなかった
- `TestDoctorMarkVocabularyCoversMark`: 語彙が 6 語で重複が無く、`🔎 未検証` を含み、
  最大幅が `❓ 走査できず` と一致すること
- 変異 2 本（`go build` 通過を確認してから判定）:
  | 変異 | 結果 |
  |---|---|
  | 予算から `cursorGutterWidth` を引かない（本 issue 以前） | 幅のテストが red |
  | 語彙から `🔎 未検証` を落とす（以前のハードコード相当） | 語彙のテストが red |
- 🚨 fixture の作り方で 1 度落ちた: 走査中の状態のままだとヘッダが経過時間
  （`2562047h47m…`）を出して**測りたい行ではなくヘッダで幅超過**する。走査完了の状態へ直した
