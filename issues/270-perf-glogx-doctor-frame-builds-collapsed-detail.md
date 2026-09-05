# perf: doctor の 1 フレームが「畳まれている行の詳細」と「全行のコピー文」を毎回作り直している

起票日: 2026-09-05
カテゴリ: perf
優先度: 中（**構造の問題**として直す価値がある。🚨 起票時に「実機の規模では体感に出る」と
書いたが、著者自身の実機データで反証された — 下の実測を参照。規模を根拠にしないこと）

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

環境: darwin/arm64, Apple M3 Max。`go test -run='^$' -bench=... -benchmem`
（`-benchtime` を 200x / 1000x / 2s、`-count=3〜5` で振ってばらつき 3% 以内）。
いずれも **`expanded` は空**（全行が畳まれている）状態の定常フレーム。

### 🚨 まず規模: 実機データでは 223µs（合成フィクスチャの 1/32）

著者の実機 snapshot（`~/.cache/glog/doctor-snapshot.json`、走査 **2026-09-04 15:10** =
本 issue 起票の前日）を `doctorSnapshot` へ読み込んでそのまま描いた実測:

| フィクスチャ | ns/op | B/op | allocs/op | 行数 | 総 Items |
|---|---:|---:|---:|---:|---:|
| **実機 snapshot** | **223 µs** | 205 KB | 1,802 | 32 | **29** |
| 現実的な重い形（下記） | 785 µs | 1.41 MB | 8,999 | 36 | 1,722 |
| 合成 32×1 | 208 µs | 231 KB | 2,054 | 67 | 32 |
| 合成 32×200（初版の見出し） | 7,153 µs | 9.92 MB | 174,352 | 67 | 6,400 |

実機の内訳は **29 エントリ / 総 Items 29 件 / 総 Contents 38 件**（最大は 1 エントリ 10 items）。
初版が見出しに使った **32×200 = 6,400 items は実機の約 220 倍**で、
**合成ストレス点であって実機の規模ではない**。実機データでは 12.5fps でも CPU 0.28% /
2.6 MB/s にすぎない。

「現実的な重い形」は敵対レビューが組んだもの（`xcode-deriveddata` 100 items /
`simulator-runtimes` 15 / `orphan-container` 30 items × 50 contents、他は実機値）で **785µs**。
**規模の主張をするならこの帯（数百 µs）を使うこと。**

### 構造: 何倍が「捨てられている仕事」か

合成 32×200 での切り分け（変異を当てて実測）:

| 条件 | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| 現状 | 7,153 µs | 9.92 MB | 174,352 |
| `detail: nil` / `copyText: ""` | 489 µs | 774 KB | 7,082 |
| さらに `copyPath: ""` | **378 µs** | **466 KB** | 7,016 |

合成 32×1 では 208 µs → 141 µs。つまり**捨てられている仕事の比率は Items 数に比例して
効いてくる**もので、実機の規模ではこの差は小さい。**これは「速くする」issue ではなく
「消費されないものを毎フレーム作る構造をやめる」issue** として読むこと。

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

### 残余の主犯は `diskItemRows` ではない（初版の誤り）

初版は「`detail`/`copyText` を外しても比例が残る（`diskItemRows` など別経路）。内訳は未実測」と
書いたが、**誤り**。`diskItemRows` の非テスト呼び出し元は `diskDetail` の 2 箇所だけなので、
`detail: nil` にした時点で**到達不能**になる。

`-memprofile` の `alloc_objects` で帰属を取ると **`diskItemKey` が 88.9%** で、経路は:

`tui.go:hintLine` →（毎フレーム）→ `doctor_view.go:hint` → `doctor_delete.go:selectionSummary`
→ `selectedResults` → `diskItemKey(r.Entry.ID, it.Path)`

`hint` は `if n, total := v.selectionSummary(); n > 0 && v.tab == tabDisk` の形で、
**呼び出しは条件の前**。`selectedResults` は全エントリの全 Item を走査し、Item ごとに
`diskItemKey` で文字列を 1 本確保する — **`selectedItems` が空でも毎フレーム**。

これは `detail` の遅延化では 1 mm も減らない（`copyPath` も外した変異でも allocs は
7,082 → 7,016 で件数比例はそのまま）。**独立した問題なので issue 274 に切り出した。**

## 発火条件

- doctor（`d`）を開いている。かつカタログのどれかが Items / Contents を多く持つ
  （DerivedData / simruntime / Inspect 系）
- **走査中・削除中**: `spinnerActive()` が真（`doctorOv.scanning() || deleting()`、
  `spinnerInterval = 80ms`）→ 12.5fps で毎フレーム再構築。
  🚨 ただし走査中は `diskResults` が進捗ごとに追記されるので、**走査中の平均フレームは
  完了時より軽い**。「走査中ずっと満額」は上限であって定常値ではない
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

1. `doctorRow` の `detail` / `copyText` / **`copyPath`** を**値ではなく遅延生成**にする
   （`func() []doctorRow` / `func() string`、または row に必要な参照だけ持たせて
   `lines()` の窓ループと copy 実行時にだけ組む）
   - 🚨 **`copyPath` を落とさないこと**。これも無条件構築で `y` でしか読まれない。
     `copyText` だけ遅延化した変異では 489µs / 774KB だが、`copyPath` も外すと
     **378µs / 466KB**（307KB 減）
   - 🚨 **「畳まれている行のコストが構造的に 0 になる」は誤り**（初版の記述）。
     `hint` → `selectionSummary` の経路（issue 274）はこの変更では減らない
   - `svcSection` / `brewSection` / `dockerSection` も**同じ形**（無条件評価）なので同じ規律が
     要るが、これらの `detail` は findings / warnings / groups の件数で**有界**なので、
     得られる削減は disk 節とは桁が違う。**性能効果を並置しないこと**

2. 退行防止を 2 段で入れる:
   - `frame_alloc_test.go` に doctor ケース（Items / Contents を多めに持つ合成 Report）を足す
   - `tests/glogx/bench_budgets.ci` に `doctor_view_frame` / `doctor_view_2000`
     （件数比例の有無を見る）を足す。`status_view_frame` / `status_view_2000` が前例
   - 🚨 **絶対時間の予算を足さない**。今の時間予算は CI 実測の 16〜125 倍緩く、しかも
     `check_bench_budgets.sh` の `rel` は実効上限を 0.1ms 刻みに丸める（issue 269）。
     足すなら **`doctor_view_scale_x100`（件数を変えた 2 点の比）**にすること
     — `agent_panel_tick_scale_x100` が前例で、比なので混雑が打ち消える
3. 修正後は before/after を実測して commit message に残す
   （`~/.claude/rules/perf-claims-need-measurement.md`）

### 🚨 遅延化は既存の無害化テストと衝突する（工数を過小に見ないこと）

`doctor_view.go:flattenDoctorRows` は `lines()` から毎フレーム呼ばれ、**畳まれた行の `detail` も
再帰的に**走査して改行を潰す。`untrusted_display_test.go:TestFlattenDoctorRowsEnforcesSingleLine`
がその意図を明記している:

```go
if got := rows[0].detail[0].text; got != "子 偽の行" {
    t.Errorf("detail の改行が残った (畳まれた行も Enter で開くと描かれる): %q", got)
}
```

走査自体のコストはゼロ（再帰を消す変異を当てても実測は区別不能）だが、**`detail` を
`func() []doctorRow` にすると `flattenDoctorRows` は thunk を無害化できない**（強制すれば
遅延化が無意味になる）。無害化を**展開時へ移す**設計変更が伴い、そこは security 系の
テスト群が守っている。加えて `expandableRow` が `len(r.detail)` を見ているので `hasDetail`
フラグが要る。

## issue 163 との関係（初版の「射程外」は言い過ぎだった）

`issues/done/163-audit-doctor-implementation-red-team.md` の却下欄:

> **却下: `lines()` の毎フレーム再構築が重い** — 実測 172µs/call (実機相当の行構成) /
> 100 行の合成でも 267µs。…メモ化 (`status_view.go` の `idxCache` 相当) を入れる価値は
> 現時点で無い。**再検討の trigger**: 行数が 200 を超える設計変更が入ったとき

初版は「本 issue は 163 の**射程外**」と書いたが、**言い過ぎ**だった（敵対レビューの指摘）。
正確な関係は:

- ✅ **行数の話は本当に別**: 本 issue のフィクスチャは行数 **67**（実機は **32**）のままで、
  163 の trigger「行数 200 超」は発火しない。駆動因は行数ではなく Items 数
- ❌ **「163 の判断はもう production の実態を記述していない」は誤り**: 163 の 172µs は
  「実機相当の行構成」で detail / copyText の構築込みで測った値。**今日の実機 snapshot で
  測り直すと 223µs** で、163 と同じ桁。**163 の結論は今も正しい**
- したがって 163 と本 issue を分けているのは「コストの駆動因の発見」ではなく、
  **フィクスチャの Items 件数だけ**

**本 issue の新規性は「規模」ではなく「構造」にある** — 「消費されないものを毎フレーム作って
いる」という指摘自体は 163 に無く、直す価値もある。ただし**優先度を規模で正当化しないこと**。

（付随: 163 は「実機で 60〜100 行」と書いているが、実機 snapshot の実測は **32 行**。
163 側の行数見積もりも 2〜3 倍高い。）

## 反証の試み

`src/glogx/CLAUDE.md` / `README.md` / `doctor_view.go` のコメント / `issues/` と `issues/done/`
（doctor 関連 40 件超。148 / 163 / 182 / 205 / 237 / 242 / 249 / 254 を確認）を探した。

🚨 **初版はテストファイルを探していなかった**。「畳まれた行の `detail` も処理するのは意図的」は
まさにそこ（`untrusted_display_test.go:TestFlattenDoctorRowsEnforcesSingleLine` のコメント）に
書かれていた。**不在の主張をするときはテストも探索範囲に入れること**。

「詳細とコピー文を**構築する**のが意図的」と書いた箇所は（テストを含めても）見つからなかったので、
構造の指摘自体は生きている。lint の除外に当たらないという記述は**未検証**。

## 関連

- 269（ベンチ予算が緩すぎて退行を観測できない件。2 の予算値はこちらの後に決める）
- 268（同型: issues viewer の「見えない行のための毎フレームの仕事」）
- `issues/done/048-perf-glogx-status-displayindex-per-frame.md`（status viewer で同型を直した前例）
- `issues/done/163-audit-doctor-implementation-red-team.md`（`lines()` のメモ化を却下した記録。
  上記のとおり本 issue はその射程外）
- `issues/done/062-*`（「どのゲートの視界にも入っていなかった」ビューへ予算ケースを足した前例）
