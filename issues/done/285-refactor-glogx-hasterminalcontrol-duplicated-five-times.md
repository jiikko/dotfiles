# refactor: セキュリティ判定 `hasTerminalControl` が 5 コピー（同じ判定の 5 実装）

起票日: 2026-09-06
カテゴリ: refactor
優先度: 低（今は矛盾していない。予防的だが、対象がセキュリティ境界の判定基準）

## 何が起きているか

制御文字オラクル `hasTerminalControl` が 5 箇所にある:

- `src/glogx/status_view_test.go:hasTerminalControl`
- `src/glogx/issues/untrusted_test.go:hasTerminalControl`
- `src/termsafe/termsafe_test.go`
- `src/doctor/disk/report_test.go`
- `src/doctor/svc/scan_test.go`

5 箇所とも `r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)` で**バイト一致**。
glogx 内の 2 本は本体 7 行 + 6 行の 🚨 コメントまで含めてバイト同一。

そのコメントは **2026-08-05 の敵対的レビューが突いた盲点**（ESC と BEL だけ見る判定は
8bit CSI U+009B / OSC U+009D を原理的に見逃す）を記録した load-bearing なもの。

## 何の複雑性が下がるか（行数ではなく）

- **変更時の touch 箇所数 5 → 1**。無害化の定義を広げるとき（例: U+2028/2029、CSI の別形）、
  今は 5 箇所を直す必要があり、**4 箇所を直し忘れても全パッケージ green のまま**
  その関門だけ旧い狭いオラクルで守り続ける
- `~/.claude/rules/mutation-verify-new-tests.md` の「同じ判定・同じ結論を 2 箇所で別実装して
  いないか」に真正面から該当する。しかも対象が**セキュリティ境界の判定基準**

## 対応 (2026-09-06)

**`src/termsafe/ctlprobe`（テスト専用の leaf パッケージ）へ 1 本化した。**
`termsafe` は glogx と doctor の両方が `replace` で取り込んでいるので、4 module 全部から使える。

- `glogx/status_view_test.go` / `glogx/issues/untrusted_test.go`：薄い別名へ
- `glogx/untrusted_display_test.go` の `hasControlExceptNewline` も `ctlprobe` へ
- `termsafe/termsafe_test.go` の `hasControl`：薄い別名へ
- `doctor/disk/report_test.go` / `doctor/svc/scan_test.go`：**インラインのループ**だったので
  `ctlprobe.HasControl(line)` の 1 行へ

🚨 **`termsafe` の production から導出していない**（自己言及になる）。`ctlprobe` は
「C0 / DEL / C1 のどれかが残っていたら真」を素朴に書いた**独立した言い換え**で、
その旨を package doc に書いた。

### 「touch 箇所 5 → 1」の実証

共有オラクルを **1 箇所だけ**広げる変異（空白も制御扱いにする）を当てると、
**glogx 18 本 + doctor 2 本**のテストが落ちた。5 コピーのままなら 1 パッケージにしか
波及しなかった。

オラクルが生きていることも確認: `termsafe` の sanitizer の C1 範囲を狭める変異で 1 本 red。
（逆にオラクルを常に false にしても 0 本しか落ちないが、これは正常 — sanitizer が
正しい間は検出すべきものが無い。）

## 🚨 直し方の制約

- **共有先はテスト専用にすること。** `termsafe.isC1` を呼ぶ形に「単純化」すると、
  オラクルが実装の自己言及になり**独立した言い換え**でなくなる
  （期待値を production と同じ式から作る形。`adversarial-review-own-safeguards.md` の 0-B）
- 4 module に跨るので、`src/termsafe` 配下の**テスト専用サブパッケージ**（全員が既に
  `replace` で取り込んでいる）が置き場の候補
- 実行は 4 module に跨るため、`issues/done/252` の案 1（`doctor/internal` に共有ヘルパー）と
  **同じタイミングでやる**のが自然。glogx 内 2 コピーだけを寄せても半端

## 反証の試み

`.golangci.yml` は `_test.go` を `dupl` から外し「テーブル/フローの相似は仕様」と書いているが、
本件は**相似ではなく同一判定の複製**なので射程外。
`src/glogx/CLAUDE.md` の termsafe 節 / `issues/done/081` / `252` に、
オラクル複製を許容する記述は無い（**251 は逆に**「同じ判定を 3 実装持たない」として
docker の写経を退けている）。

`issues/done/081` が**採用**した形（バイト同一 + load-bearing コメントの touch 箇所が 2→1）と
一致し、081 が**却下**した形（`assert_contains` の 8 コピー = 失敗時の契約が 3 通りに分岐）
とは違う（ここは契約が完全に一致している）。

## 関連

- `issues/done/081`（共通化の採用 / 却下の基準）/ `issues/done/252`（`doctor/internal` の案 1）
