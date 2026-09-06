# perf: doctor のフレームが「プロセス不変の値」を行ごとに再計算している（同型 4 件）

起票日: 2026-09-07
カテゴリ: perf
優先度: 中（🚨 severity を過大にしないこと。doctor の再描画は spinner tick と打鍵に律速され
**常時 60fps ではない**。20 KB/frame は Go の GC には軽い。修正が安価なので着手順は前の方でよい、
という位置づけ）
出典: /audit performance 2026-09-06（forge Minimum+）。クロスレビューが改善後の値まで実測

## ① `doctorMaxMarkWidth()` を行ごとに呼んでいる（本命）

`src/glogx/doctor_view.go:diskSection` の行ループが `doctorMaxMarkWidth()` を毎行呼ぶ。
この値は `src/doctor/disk/report.go:MarkVocabulary` から導出される**プロセス不変の定数**で、
`labelW` は `o.width` にしか依存しない**ループ不変式**。

### 実測（darwin/arm64・`-race`）

| | allocs | bytes |
|---|---|---|
| before | 600 | 83,122 |
| hoist 単独（call site で 1 回） | 577 | 65,641 |
| **memo 単独（`sync.OnceValue`）** | 579 | **63,174** |

**memo だけで -19,948 B/frame**（フレーム確保バイトの約 26%）。hoist と memo の両方を入れる。

### 🚨 memo 化の理由づけを間違えないこと

一次報告は「x/ansi の初期化順に依存しないよう初回使用時に確定させる」と書いていたが、
これは**不要な警戒**。Go は import 先の `init` を先に完了させ、`termwidth.go` の `init` 自身が
その旨を明記しており、`widthenv` は `RUNEWIDTH_EASTASIAN` 下でテストを走らせない。
**`sync.OnceValue` を使う判断自体は妥当なので、理由だけ差し替える**（「プロセス不変の値を
毎フレーム再計算しないため」）。

### 🚨 `issues/done/238` の制約を壊さないこと

一次報告は「`issues/` を `MarkVocabulary` 等で grep して 0 件」と書いていたが**誤り**で、
`issues/done/238-bug-doctor-disk-row-width-budget-ignores-gutter.md` が正面から扱っている
（9 箇所で言及）。238 の制約は
**「以前 5 語をハードコードして `🔎 未検証` が抜けたので、語彙から導出する形へ直した」**。

- **memo 化は導出性を保つので安全**
- **定数へ焼き直す形は 238 の回帰**

## ② `dockerMarkWidth` も同型

`src/glogx/doctor_docker.go` が同じ形（固定語彙からプロセス不変の幅を、群の行ごとに再計算）。
①と同じ commit で。

## ③ 同一フレームで `SumDeletable` が 2 回走る

`doctor_view.go:tabSummary`（tabDisk 分岐）と `diskSection` の両方から呼ばれる。

## ④ `diskSection` が毎フレーム `results` の完全コピーを作って `sort.SliceStable` し直す

`sorted := append(...)` + `sort.SliceStable`。結果は入力が変わらない限り同じ。

## ⑤（別モジュール）`excludedRootFor` が path 1 本ごとに 10 root すべてを `EvalSymlinks` し直す

`src/doctor/disk/guard.go`。🚨 **これは据え置きの判断もありうる**（未決着。research issue 324 参照）:
`EvalSymlinks` は**破壊的操作のガード**の一部で、
[`sandbox-real-destructive-test-apis.md`](../_claude/rules/sandbox-real-destructive-test-apis.md) の
「実行の直前に取り直した値で判定する」に触れる。**正規化結果のキャッシュは TOCTOU の窓を広げる**
ので、走査（読み取り）経路と削除（破壊）経路で扱いを分けること。

## 受け入れ条件

- [ ] ①②を入れ、**`TestFrameAllocBudget` の doctor-disk を実測へ締め直す**（緩めるのではなく**下げる**）
- [ ] **変異検証**: 「memo を外して毎フレーム再計算に戻す」変異で**バイト予算が red** になる
- [ ] 238 の「語彙から導出する」制約が保たれている（定数へ焼き直していない）
- [ ] ⑤は走査経路と削除経路を分けて判断し、据え置くなら理由をコード直近に残す

## 関連

- `issues/done/238`（語彙から導出する形へ直した経緯。**着手前に読むこと**）
- `issues/done/270`（doctor フレームの遅延化。予算を締め直した先例）
- issue 323（予算が退行を観測できていない件。①の締め直しはそちらと同じ commit でもよい）
