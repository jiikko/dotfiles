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
