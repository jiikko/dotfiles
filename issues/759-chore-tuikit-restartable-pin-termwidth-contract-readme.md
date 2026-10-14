# 759 (chore): restartable の tuikit の固定版が古い / termwidth が不正な入力を受けたときの契約 / README の CI の記述

起票日: 2026-10-14

## 概要

2026-10-14 の tuikit の監査 (記録は 760) の dependency・broken-code・general で出た、今は実害の無いもの。

## 詳細

1. **restartable が tuikit を 2026-09-30 の版 (99b256e39f08) で固定していて、以降の修正を取り込んでいない**
   - restartable が使うのは `confirm` / `layout` / `termwidth` / `widthenv`。固定版以降、`termwidth.StripSGR` / `DropColumns` が x/ansi に寄った
     (issue 700。旧版は「英字まで」の自前の走査で、OSC 8 のように英字で終わらない列を途中で切る)
   - restartable の画面に OSC 8 を含む文字列が流れる経路は未確認
   - `src_restartable.yml` の paths に `src/tuikit/**` が無いのは固定版のため (設計どおり)
   - 直し方: go.mod の版を上げ、restartable 側の CI で確かめる (756 の警告は固定版のままでも足せる)
2. **termwidth は不正な UTF-8・不正な列を受けると、関数どうしの結果が食い違う** (品質の監査体。潜在)
   - `StripSGR` は ESC を含む行に生の C1 バイト (0x9e など。CP1252 の `ž`) が混ざると、後ろの本文を ST / BEL / 行末まで飲む
     (`"\x1b[33mFix \x9e bug in parser\x1b[m and more"` → 旧 `"Fix � bug in parser and more"`、新 `"Fix  and more"`)。ESC の無い行は素通しなので、ESC の有無で結果が変わる
   - 幅の不変条件 (`Of(DropColumns(s,n)) == Of(s)-n`・`Of(StripSGR(s)) == Of(s)`) を新版だけが破る入力が、31 トークン × 長さ 1〜3 の 30,783 文字列のうち
     DropColumns で 118・StripSGR で 42 (全体で壊れる数は旧 4,070 → 新 3,176 と減っている)。どれも CSI の中に `-` などの不正な列
   - 使う側の入口 (termsafe の `DetailLine` / `LineKeepTabs` / `PlainLine`) が先に無害化するので、今は届かない (glogx 4 か所・treefiler・schedkeys・pro-con の経路を監査体が確認)
   - 702 の 10 (不正 UTF-8 で `Of` と他の関数が食い違う) と同じ根
   - 直し方: termwidth の README に「termsafe を通した文字列だけを受ける」と契約を書く。または入口で不正 UTF-8 を U+FFFD へ寄せる
3. **`src/tuikit/README.md` の「tuikit を変えると glogx の CI も走る」が古い**: 実際は glogx・pro-con・treefiler・schedkeys・ratelimit の 5 つの workflow の paths に `src/tuikit/**` がある (main が `.github/workflows/*.yml` を grep。tuikit 自身の src_tuikit.yml を除いて 5)

## 関連ファイル

- `src/restartable/go.mod`・`src/tuikit/termwidth/`・`src/tuikit/README.md`
- 前回: 700・702。監査の記録: 760

## 進捗

- [x] 1 restartable の tuikit を 99b256e39f08 → 5bf65d09 へ上げた (`go mod tidy -diff` 差分 0・`tests/scripts/test_tuikit_consumers_aligned.sh` rc=0)
  — `chore(restartable): tuikit の固定版を 2026-09-30 から 5bf65d09 へ上げる`。副作用の確認の案内の不揃いは 761 へ切り出した
- [x] 2 termwidth に渡す文字列の契約 (termsafe を通したもの) を tuikit の README の「使う側の約束」に書いた
- [x] 3 README の CI の記述を、tuikit の変更で走る 5 つの workflow と、版を固定する restartable は走らないことに直した
  — `docs(tuikit): termwidth に渡す文字列の契約 ...`
- ついで: `claude-md-refresh` で tuikit の CLAUDE.md・README を実体に揃えた (6 件) — `docs(tuikit): CLAUDE.md と README を実体に揃える`
- 敵対レビュー 3 体 (壊す+回帰 / 素通り / 回帰。read-only・1 体ずつ直列、5h 枠に合わせて間を空けた) を通した
- 6 module (tuikit・glogx・pro-con・schedkeys・treefiler・restartable) の `make lint` / `make test` rc=0 (最後の編集の後)
