# 252 refactor: `doctor/svc` の無害化の関門に網羅検査が無い (disk 側だけ入った)

起票日: 2026-09-04
種別: refactor (security hardening)
優先度: **P2**（まだ壊れていない。ただし `~/Library/LaunchAgents` には誰でも plist を置けるので、
`disk` 側と脅威は同じ）
出典: issue 251 の敵対的レビュー（opus, 2026-09-04）の P3-14。**実コードで裏を取った**

## 症状

issue 251 で `doctor/disk` に「表示用の構造体へ新しい文字列フィールドを足したのに
`Sanitize*ForDisplay` へ通し忘れる」を止める検査を入れた
（`src/doctor/disk/display_coverage_test.go`）。**双子の関門である `doctor/svc` には同じ検査が無い。**

`svc/display.go` 自身が「`~/Library/LaunchAgents` には誰でも plist を置ける」と書いているとおり
脅威は同じで、次の構造体に文字列フィールドを 1 つ足しても誰も止めない:

- `svc.Finding` — `Label` / `PlistPath` / `Domain` / `Reasons` / `MissingExec` / `RestartKeys` / `BrewFormula` / `Commands`
- `svc.Report` — `StatusErr` / `BrewErr` / `DirErrs`

## なぜ 251 で一緒にやらなかったか

検査は `package disk` の中で `parser.ParseDir(".")` を使っており、**そのまま svc へ持って行けない**
（別 package なので、共有するには `doctor/internal/...` にヘルパーを置く必要がある）。
251 のスコープ（termsafe の関門に型 / lint が無い）から広がるので分けた。

## 案

1. **テストをパラメタ化して共有する** — `(pkgDir, sanitizeGate, sanitizeExempt)` を引数に取る
   ヘルパーを `src/doctor/internal/displaycheck/` に置き、`disk` / `svc` の両方から呼ぶ。
   検査の本体が 1 つになるので、片方だけ直る形を避けられる
2. **svc に写経する** — 安いが 2 実装になり、`mutation-verify-new-tests.md` の
   「同じ判定を 2 箇所で別実装していないか」に抵触する
3. **却下** — svc の表示経路は disk より狭いと判断するなら、理由を `svc/display.go` に残して閉じる

案 1 を推す。ただし 251 の検査は敵対的レビューを 1 周通した直後で、
**まだ形が固まっていない可能性がある**（受け手キー / 右辺の無害化判定 / fail-closed な型判定を
レビュー後に足した）。**1〜2 週間は disk 側で運用してから共通化する**方が、
共通化した後に両方を直す手間を避けられる。

## 参考: 251 で入れた検査が実際に見つけたもの

- `DeleteReport.HistoryError`（`err.Error()` = ファイルパスが入る）が関門を通っておらず、
  `glogx/doctor_delete.go` が画面に生で出していた（**本物の素通し経路**）

## 関連

- [251](next/251-refactor-termsafe-has-no-type-or-lint-gate.md) — 出典。`disk` 側の実装と脅威モデル
- `src/termsafe/README.md` の「通し忘れを機械で止める」節 — 検査の入口と「検出しない形」
