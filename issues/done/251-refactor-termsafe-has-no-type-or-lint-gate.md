# termsafe の「通し忘れ」を型でも lint でも止められない (関門が string -> string)

起票日: 2026-09-04
種別: refactor (security hardening)
優先度: P3
出典: issue 228 の §4 (「termsafe が string -> string で型の関門を持たないことは別の弱さで、
本件を直しても残る」) の切り出し。
🚨 **外部レビュー未通過** (codex を使わない環境。反証されていない仕様として扱うこと)。

## 症状 (まだ壊れていないが、同じ穴をもう一度掘れる)

`termsafe.PlainLine(s string) string` は素の string を返すので、**通した値と通していない値が
型で区別できない**。issue 228 で doctor の live 経路を関門へ通したが、次に viewer や CLI を
足す人が同じ漏れを作れることは変わらない (228 が「3 度目」だったのはこの構造のため)。

現状の防御は 2 つで、どちらも射程が狭い:

- `src/glogx/untrusted_display_test.go` — sink ごとの回帰テスト。**新しい sink は自動では
  対象にならない** (テストを足し忘れれば無検査)
- `.golangci.yml` — termsafe への言及は 0 件 (lint 強制なし)

## 案

1. **型で持つ**: `termsafe.Safe` (新しい string 型) を返し、表示層が `Safe` しか受けない形。
   影響範囲が広い (rows / box / hint / copy の全経路) ので、まず 1 パッケージで試す
2. **lint で止める**: ruleguard (`src/glogx/gorules/rules.go` に前例あり) で
   「表示に出る構造体のフィールドへ、無害化を通っていない外部由来の識別子を渡すな」を書く。
   識別子ベースの近似になるので、**脅威モデルと「検出しない形」を先に書くこと**
   (`adversarial-review-own-safeguards.md` の節 8。書かないと迂回指摘が収束しない)
3. **受け入れる**: sink ごとのテストを足す運用のままにする。その場合はこの issue を却下として
   閉じ、理由を `src/termsafe/README.md` にも残す

## 同じ穴の親戚 (ここに記録しておく。単独では issue にしない)

`replace` 先が **dependent の workflow paths に入っているか**を機械が見ていない。
`scripts/check_go_project_lanes.sh` は「`src/<name>/` が `src_<name>.yml` にあるか」しか
見ないので、`src/glogx/go.mod` に新しい `replace` を足した人が `src_glogx.yml` の paths を
忘れても誰も止めない (2026-09-04 の termsafe 追加時は手で入れた)。
`go.mod` の replace 先を読んで dependent の paths と突き合わせる検査を、上の案 2 と
同じタイミングで足すのが安い。

## 未確認

- 案 1 の影響範囲 (何箇所が `Safe` を受け取る形に変わるか) は数えていない
- 案 2 が既存の ruleguard で表現できるか (型情報が要る判定なので `gorules` の vet 経路が要る)

## 対応（2026-09-04）

**案 2 + 親戚**をユーザー判断で採用。ただし**案 2 の当初案（読み手側の lint）は実測で否定した**ので、
関門の網羅検査へ形を変えた。

### 案 2 の当初案を捨てた根拠（実測）

「表示に出る構造体のフィールドへ、無害化を通っていない外部由来の識別子を渡すな」を
ruleguard で書こうとしたが、**この codebase では成立しない**:

- `doctorRow.text` への代入 **35 件のうち、右辺が `termsafe.` なのは 0 件**
  （無害化は値の生成側 = `doctor/disk/display.go` の `Sanitize*ForDisplay` で済ませており、
  読み手の `doctor_view.go` には `termsafe` の呼び出しが 1 つも現れない）
- つまり識別子ベースのルールを入れると **35 件全部が誤検出**になる
- ruleguard は構文マッチで、`Where` / `Type.Is` の前例も repo に無い（既存 2 ルールは純粋な構文）

### 実装したもの

| 検査 | 何を止めるか | 場所 |
|---|---|---|
| **A** 関門の網羅検査 | `doctor/disk` の表示用構造体に新しい文字列フィールドを足したのに `Sanitize*ForDisplay` へ通し忘れる | `src/doctor/disk/display_coverage_test.go`（新規） |
| **B** replace と CI レーンの突合 | `go.mod` の `replace` 先が dependent の workflow paths（push / pull_request 両方）に入っていない | `scripts/check_go_project_lanes.sh` の 4 つ目の不変条件 |

### 🚨 検査 A が本物の素通し経路を 1 件見つけた

`DeleteReport.HistoryError`（= `err.Error()` で**ファイルパスが入る**）が
`SanitizeDeleteReportForDisplay` を通っておらず、`glogx/doctor_delete.go` が
「記録を書けませんでした: 」に続けて**画面へ生で出していた**。隣の `HistoryPath` は
無害化しているのに、これだけ漏れていた形。塞いだうえで、sink テストの fixture にも足した
（`display.go` の無害化を revert すると `untrusted_display_test.go` が
OSC52 の残留で red になることを確認）。

### 変異検証で実装の穴が 2 つ出た（どちらも修正済み）

- **検査 B の当初版は push と pull_request を区別していなかった** — 片方に残っていれば通る。
  既存の不変条件 3 も同じ穴を持っていた。トリガー別に見るよう直した
- **検査 A の射程が縮んでも緑だった** — `stringish` が named type を見なくなる変異は
  件数が 23 → 22 に減るだけ。`sanitizeExempt` の未使用検出と `wantChecked` の錨で塞いだ

### 敵対的レビュー（opus, read-only）で P1 が 3 件

**すべて実験で再現を確認してから直した**:

1. **`touched` がフィールド名だけのキー** — 同じ関門の中で同名フィールドが互いをマスクする。
   実測: `it.Reason`（ItemOutcome）の無害化を丸ごと消しても `e.Reason`（EntryOutcome）が
   集合に残るため**緑のまま通った**。受け手の識別子（`it` / `e` / `r` / `out` / `c`）まで
   キーにして修正。さらに `r.Failures = sanitizeLines(...)` を消しても
   `r.Failures = append(...)` が残って緑だった形も出たので、**右辺が無害化を通っているか**まで見るようにした
2. **関門がコピーを無害化して親へ書き戻さない形**（`r.Items = kept` の一文だけを消す）は
   代入の有無を見る作りでは原理的に捕まえられない → **「検出しない形」に明記**し、
   sink テストが end-to-end で見る担当だと書いた
3. **`paths_has` がトリガーを取り違える** — `pull_request:   # コメント` や
   `pull_request_target:` で `intrig` が落ちず、push の判定に pull_request の paths が漏れる。
   実測で再現。トリガー名の列挙をやめ、`on:` 直下の深さで見る **fail-closed** に直した

P2 / P3 も対応: `paths_has` の canary（負例つき）/ `wantChecked` の錨 / `stringish` の fail-closed 化 /
`sanitizeExempt` の理由を「呼び出し側の不変条件に依存」へ書き換え / 引用付き replace / 多段 `../` /
`deps > 0` の assert / README の射程を `doctor/disk` に限定。

### 残したもの

- **`doctor/svc` 側の関門には検査が無い** → [252](../252-refactor-svc-display-has-no-coverage-check.md) として起票
- **案 1（型で持つ）は却下**。理由は `src/termsafe/README.md` に記録した
  （パッケージ境界で剥がれるので、効果を出すには 50 呼び出し + 受け側全経路が要る）
