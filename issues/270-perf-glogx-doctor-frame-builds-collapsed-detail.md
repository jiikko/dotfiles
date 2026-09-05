# perf: doctor の 1 フレームが「畳まれている行の詳細」と「全行のコピー文」を毎回作り直している

起票日: 2026-09-05
カテゴリ: perf
優先度: 高（走査中は 12.5fps で回るため、実機の規模では体感に出る）

## 何が起きているか

`doctor_view.go:diskSection` が組む行リテラルは、`detail:` と `copyText:` を **無条件に評価する**:

```go
row := doctorRow{
    text:       ...,
    selectable: true,
    key:        "disk:" + r.Entry.ID,
    detail:     v.diskDetail(o, r),      // ← 常に構築
    copyPath:   diskCopyPath(r),
    copyText:   diskCopyText(r, mark),   // ← 常に構築
}
```

一方、消費側は条件付きでしか読まない:

- `doctor_view.go:buildRows` の `add()` が `r.detail...` を並べるのは **`v.expanded[r.key]` が真のときだけ**
- `copyText` は **`y` / `Y` を押したときにしか**読まれない

`diskDetail` は `r.Contents` / `r.Items` を全件走査して `doctorRow` を組み、`diskCopyText` は
`strings.Builder` で Items 全件 + Contents 全件 + `diskVerifyCommands` を含む複数行を組み立てる。
つまり **1 フレームのコストがカタログ全エントリの Items / Contents の総数に比例する**。
可視窓 (`o.page` 行) とも、展開しているかどうかとも無関係。

`svcSection` / `brewSection` / `dockerSection` も同じ形（`detail:` / `copyText:` を無条件評価）。

## 実測

環境: darwin/arm64, Apple M3 Max。`go test -run='^$' -bench=... -benchmem -benchtime=200x -count=3`。
使い捨てベンチ（commit していない）で、**展開なし**（`expanded` は空）の定常フレームを測った。
フィクスチャは合成 `disk.Report`（`Status: StatusOK`、Items のみ、Contents なし）。

| フィクスチャ | 現状 | `detail: nil` / `copyText: ""` にした場合 | 差 |
|---|---:|---:|---:|
| 32 エントリ × 1 item | 208 µs / 231 KB / 2,053 allocs | 141 µs / 160 KB / 645 allocs | 1.47x |
| 32 エントリ × 200 items | **7,153 µs / 9.92 MB / 174,352 allocs** | 489 µs / 774 KB / 7,082 allocs | **14.6x / 12.8x / 24.6x** |

- **1 フレーム 7.2ms、9.9MB、17 万 alloc**。しかもこの行は **1 つも展開されていない**
- 走査中・削除中は `spinnerActive()` が真になり `View()` が `spinnerInterval = 80ms`（12.5fps）で
  回るので、**毎秒 89ms の CPU と 124MB の確保**が畳まれた行の詳細のためだけに使われる
- 対照: 同じ repo の他ビューの定常フレームは CI 実測で 0.027〜0.074ms
  （`issues_view_frame` / `issues_view_2000`）。doctor はその **100〜260 倍**
- Items 200 件は保守的な想定。`Contents` は `listContents`（`os.ReadDir` の全名前、**上限なし**）
  由来なので、Inspect エントリでは数千件になりうる

### 再現コード（`tmp/` は gitignore なので本文に残す）

`src/glogx/` に置いて `go test -run='^$' -bench='BenchmarkAuditDoctorFrame' -benchmem -benchtime=200x -count=3 .`。
「外した場合」は `doctor_view.go:diskSection` の行リテラルを
`detail: nil` / `copyText: ""` に置き換えて同じベンチを回した。

```go
package main

import (
	"fmt"
	"testing"

	"doctor/disk"
	"doctor/svc"
)

func benchDoctor(tb testing.TB, entries, itemsPer int) *browseModel {
	tb.Helper()
	m := benchBrowse(tb, 3, 120, 40)
	m.width, m.height = 120, 40
	m.doctorOv = doctorView{shown: true, expanded: map[string]bool{}} // 展開なし
	res := make([]disk.Result, 0, entries)
	for e := range entries {
		items := make([]disk.Item, 0, itemsPer)
		for i := range itemsPer {
			items = append(items, disk.Item{Path: fmt.Sprintf("/home/u/.cache/e%02d/item-%05d", e, i), Size: int64(i + 1)})
		}
		res = append(res, disk.Result{
			Entry:  disk.Entry{ID: fmt.Sprintf("e%02d", e), Label: fmt.Sprintf("Entry %02d", e), Risk: disk.RiskSafe, Recover: "再取得", DeleteVia: "rm"},
			Status: disk.StatusOK, Size: int64(itemsPer), Items: items,
		})
	}
	m.doctorOv.diskRep = &disk.Report{Results: res}
	m.doctorOv.svcRep = &svc.Report{}
	m.doctorOv.brew = &brewDoctorResult{Clean: true}
	return m
}

func BenchmarkAuditDoctorFrameSmall(b *testing.B)     { benchDoctorFrame(b, 32, 1) }
func BenchmarkAuditDoctorFrameManyItems(b *testing.B) { benchDoctorFrame(b, 32, 200) }

func benchDoctorFrame(b *testing.B, entries, itemsPer int) {
	m := benchDoctor(b, entries, itemsPer)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View().Content
	}
}
```

🚨 ヘルパーは `newTestBrowse`（`*testing.T` 専用）ではなく **`benchBrowse`（`testing.TB`）**を使うこと。

### 「外した後」も比例が残る点

`detail`/`copyText` を外しても 141 → 489 µs と件数比例が残る（`diskItemRows` など別経路）。
本 issue が主張するのは **支配的な項が畳まれた行の構築であること**までで、残りの比例の
内訳は未実測。

## 発火条件

- doctor（`d`）を開いている。かつカタログのどれかが Items / Contents を多く持つ
  （DerivedData / simruntime / Inspect 系）
- **走査中・削除中**: `spinnerActive()` が真 → 80ms 周期で毎フレーム再構築
- **走査後も**: `j` / `k` / `ctrl+d` のスクロール打鍵ごとに全再構築
- **silent に壊れる**: 機能は正しい。遅くなり、確保が増えるだけ。build も lint もテストも緑

## なぜ既存のゲートが見ていないか

- `frame_alloc_test.go` の予算ケースは list / list-ja / status-40 / diff-overlay / job-panel /
  issues-40 / usage-glance / toast-holding の 8 つで、**doctor は 1 つも無い**
- `tests/glogx/bench_budgets.ci` にも doctor の metric は無い
- doctor は issue 148 で、予算の枠組み（047 / 051 / 062）より**後**に足されたため、
  どのゲートの視界にも入らなかった

🚨 ただし **「予算が無いこと」は原因ではなく、気づかれなかった理由**。予算ケースを足すだけでは
無駄な構築は消えない。

## 推奨対応

構造的な直し方は **status viewer と同じ「可視の窓ぶんだけ作る」へ寄せる**こと。
status viewer は同型の問題を明示的に直した前例がある
（`bench_budgets.ci`: 「以前は全行を整形してから窓で切っており 40 件 0.103 / 2000 件 1.65ms
だった (16 倍)。可視の窓だけ整形する形へ直して 0.128ms」）。doctor はその規律を引き継いでいない。

1. `doctorRow` の `detail` / `copyText` を**値ではなく遅延生成**にする
   （`func() []doctorRow` / `func() string`、または row に必要な参照だけ持たせて
   `lines()` の窓ループと copy 実行時にだけ組む）。畳まれている行のコストが構造的に 0 になり、
   「新しい節を足した人が忘れる」余地も消える（`svcSection` / `brewSection` / `dockerSection`
   にも同時に効く）
2. 退行防止を 2 段で入れる:
   - `frame_alloc_test.go` に doctor ケース（Items / Contents を多めに持つ合成 Report）を足す
   - `tests/glogx/bench_budgets.ci` に `doctor_view_frame` / `doctor_view_2000`
     （件数比例の有無を見る）を足す。`status_view_frame` / `status_view_2000` が前例
   - 🚨 予算値は **269 を先に片付けてから**決める。今の時間予算は CI 実測の 16〜125 倍
     緩いので、同じ緩さで足すと最初から何も観測しないゲートになる
3. 修正後は before/after を実測して commit message に残す
   （`~/.claude/rules/perf-claims-need-measurement.md`）

## 🚨 これは issue 163 が却下した指摘ではない（再生成ではない）

`issues/done/163-audit-doctor-implementation-red-team.md` の却下欄にこうある:

> **却下: `lines()` の毎フレーム再構築が重い** — 実測 172µs/call (実機相当の行構成) /
> 100 行の合成でも 267µs。12.5fps で CPU 0.2%、60fps でも約 1%。`-cpuprofile` の top は
> grapheme 幅計算 (uax29 / displaywidth) で 13% 程度。メモ化 (`status_view.go` の `idxCache`
> 相当) を入れる価値は現時点で無い。**再検討の trigger**: 行数が 200 を超える設計変更が
> 入ったとき

本 issue はこの却下の**射程外**。3 点で違う:

1. **コストの駆動因が違う**。163 が測ったのは**行数**に対するコストで、trigger も
   「行数が 200 を超えたら」。本 issue の実測フィクスチャは**行数 67 のまま**（全行が
   畳まれている）で **7.15ms** に達する。駆動しているのは行数ではなく
   **Items / Contents の総数**なので、**163 の trigger は永久に発火しない**
2. **数字の桁が違う**。163 が受け入れたのは 172µs（12.5fps で CPU 0.2%）。本 issue の実測は
   **7,153µs = その 41 倍**（12.5fps で CPU 約 9%、毎秒 124MB の確保）
3. **提案している直し方が違う**。163 が却下したのは**メモ化**（`idxCache` 相当）。
   本 issue が提案するのは「**消費されないものを作らない**」— 種類が違い、はるかに安い
   （キャッシュの無効化規律を新設しなくてよい）

163 の判断は 163 が測った条件では正しい。本 issue はその条件の外側を測っている。

## 反証の試み

`src/glogx/CLAUDE.md` / `README.md` / `doctor_view.go` のコメント / `issues/` と `issues/done/`
（doctor 関連 40 件超。148 / 163 / 182 / 205 / 237 / 242 / 249 / 254 を確認）を探したが、
「詳細とコピー文を毎フレーム作るのは意図的」と書いた箇所は見つからなかった。
lint の除外にも当たらない（`prealloc` / `perfsprint` / `makezero` はこの形を検出しない）。

## 関連

- 269（ベンチ予算が緩すぎて退行を観測できない件。2 の予算値はこちらの後に決める）
- 268（同型: issues viewer の「見えない行のための毎フレームの仕事」）
- `issues/done/048-perf-glogx-status-displayindex-per-frame.md`（status viewer で同型を直した前例）
- `issues/done/163-audit-doctor-implementation-red-team.md`（`lines()` のメモ化を却下した記録。
  上記のとおり本 issue はその射程外）
- `issues/done/062-*`（「どのゲートの視界にも入っていなかった」ビューへ予算ケースを足した前例）
