# 952 (bug): dupscan が Swift のバッククォート付き識別子の定義と呼出しを別名として扱う

> 🚨 **担当中: Mac (glogx)**（2026-10-28〜）

起票日: 2026-10-28
工数: M（Swiftの照合用識別子の統一と、既存key/出力互換の回帰確認）

## 概要

Swiftでバッククォート付きで宣言し、バッククォート無しでメンバー呼出した同じ関数を、dupscanが別の名前として扱う。コンパイル可能で構文解析失敗0のfixtureでも、裸名のdefは0件、引用付き名のcontextは定義1件/参照0件/呼出0件になる。indexにも実在の呼出がcalls_approx=0と表示される。

優先度: P2（特定の有効な識別子で探索結果を誤る。クラッシュや全般的な機能停止ではない）。型解決の不足ではなく、Swift識別子のエスケープ表記が照合時に統一されていない問題。0件は未使用/不存在の証明ではないという注意があっても、候補確認・変更影響の調査で取りこぼす。

## 再現

対象source: 82d0fdd8と、別セッションの改善を取り込んだb94e2872の両方で再現。Go 1.26.0で固定binaryをbuildし、Apple Swift 6.4 / arm64-apple-macosx26.0で下記sourceのswiftc -typecheckがrc0になることを確認した。外部AIやサービスへのsource送信は行っていない。

dotfiles rootから、使い捨てgit repoで実行する:

```sh
mkdir -p tmp
case_dir=$(mktemp -d "$PWD/tmp/dupscan-952.XXXXXX")
git -C "$case_dir" init -q
cat > "$case_dir/Sample.swift" <<'SWIFT'
struct A {
    var `default`: Int { 1 }
    func `repeat`(_ value: Int) -> Int { value + `default` }
    func caller() -> Int { self.repeat(1) }
}
SWIFT
swiftc -typecheck -module-cache-path "$case_dir/module-cache" "$case_dir/Sample.swift"
bin/dupscan def 'A.repeat' "$case_dir"
bin/dupscan def 'A.`repeat`' "$case_dir"
bin/dupscan context 'A.repeat' "$case_dir"
bin/dupscan context 'A.`repeat`' "$case_dir"
bin/dupscan index -format json -name repeat "$case_dir"
bin/dupscan callers repeat "$case_dir"
bin/dupscan callers '`repeat`' "$case_dir"
```

| 問い合わせ | 実際 | 期待 |
|---|---|---|
| `def A.repeat` | rc1、定義なし | Aのrepeat定義1件 |
| ``def A.`repeat` `` | rc0、定義1件 | 同じ定義を取得できる |
| `context A.repeat` | rc0、定義0/参照1/呼出1 | 定義も同じ識別子として当たる |
| ``context A.`repeat` `` | rc0、定義1/参照0/呼出0 | このfixtureでは定義1/参照1/呼出1 |
| index -name repeat（JSON） | nameが `` `repeat` ``（バッククォート付き）、calls_approx=0 | 同じ関数の呼出を近似数に含む |
| `callers repeat` | self.repeat(1)を1件取得 | 1件 |
| ``callers '`repeat`'`` | rc0、呼出しなし | 同じ呼出を1件取得 |

全問い合わせのstderrは空。contextは構文解析失敗0/未対応言語なし。宣言名・indexのnameに合わせてバッククォート付きqueryへ直すと、定義は見つかる一方で呼出しが落ちるため、queryを書き換えるだけでは定義と影響箇所の両方を取得できない。computed propertyもdef A.defaultでは0件になることを確認した（主再現は上記関数）。

## 原因の裏取り

b94e2872で確認:

- `src/dupscan/internal/lang/swift/swift.go`のdeclUnit（888–890行付近）は宣言名にw.text(ch)を使い、バッククォート込みで保存する。calleeName（621–630行付近）とreferencesも、各出現の元の字面を使うため、self.repeatの側は裸名になる。
- `src/dupscan/internal/report/symbols.go`のBuildDefinitions（117行付近）はd.Nameとq.Nameを文字列完全一致で比較する。
- `src/dupscan/internal/report/callers.go`のCallQuery.matches（46行付近）も同じ完全一致。index.goのcallCountsも名前でbucket化し、同じ照合を使うため、宣言側の名前に対応するcallが0件になる。
- `src/dupscan/context_cmd.go`はqueryのNameをrefs/callersへそのまま渡すので、引用付きqueryではその後段が0件になる。

## 対応方針（未実装）

Swiftの**照合用識別子**について、宣言・参照・呼出・queryを同じ正規形で比較する。裸名/バッククォート付き名の両方から同じ対象へ到達させる。型解決・別receiverの識別を追加する課題にはしない。

表示用source/署名、unit key、既存の除外設定、重複検出hashを安易に変更しない。名前の正規化をadapter全体へ入れる場合も、それらへ意図せず波及しないことを先に確認する。Goやoperatorを含めた全文字列から無条件にバッククォートを削る修正はしない。

暫定回避: このfixtureでは定義は引用付きdef、呼出/参照は裸名で別々に確認し、原文と照合する。単独のcontextやindexの0だけで未使用と判断しない。これを恒久仕様にはしない。

## 受け入れ条件

- [x] 上記fixtureで、裸名/引用付き名のdef・refs・callers・contextが同じ識別子を追える。
- [x] indexのcalls_approxがこのfixtureの実在する呼出を含む。
- [x] Swiftとして有効な範囲で、引用あり/なしの宣言・使用・queryの組合せと、property・通常識別子を回帰検査する（裸の予約語宣言のような無効入力は含めない）。
- [x] unit key/除外設定/重複検出hashとGoの通常探索に意図しない変更がないことを確認する。

## 既存issueとの関係・検証記録

[922](../890/done/922-bug-dupscan-def-refs-known-limits.md)はproperty束縛・optional binding等、[926](done/926-feat-dupscan-query-diagnostics.md)のbacktick記述は入力を一律拒否しない/旧rcの維持を扱う。この宣言と使用の照合不一致は未記録だった。READMEの型未解決・同名混在・default引数省略等の限界とは別であり、LSPの導入を前提としない。

rawはdotfiles/tmp/audit-dupscan-20261028/のkeyword-results.json、keyword-followup.json、latest-results.json。latest-resultsにはsource/binary SHA256とsource commitを保存した。本体とread-only subagentで再現・source照合・既存記録との重複を反証し、実在する未記録不具合と確認した。今回はissueの記録のみで、修正は行っていない。

改善の評価: 有効な入力で定義/呼出の片側が消える反例、限定した修正範囲と回帰条件を固定した。一般的な精度低下や致命的障害へ誇張せず、既知の近似制約とは分けて対応できる状態にした。製品コードや探索精度を今回改善したとは扱わない。

- 2026-10-28: `docs(dupscan): record escaped identifier lookup mismatch`で最新sourceでも再現する不具合の詳細を記録。

導入commitは未特定であり、最近入った退行と断定しない。独立した反証レビューで追加P1/P2なし。issue標準テストはBash 5で全件成功。gateはrc0・変更単位0、既存不正fixtureはbase/head各1件でpartial、既存の効いていない除外7件を通知した。今回のsource変更・除外追加はない。

## 対応の結果 (c4、2026-10-28)

**照合用の名前を `tree.MatchName` で正規形にして、比べる所の両辺に通した**。全体を囲む 1 組のバッククォートだけを外す (Go の名前・演算子・途中に ` を含む文字列は変えない)。保存する名前・単位の鍵・署名・重複検出の hash・除外の釘は元の綴りのまま、表示も元の綴り (``A.`repeat` ``)。

比べる所 (11 か所): `def`・`refs`・`callers` の完全一致、`index` の名前一致と呼び出し数の集計、型スコープ (`` `A`.f ``)、引数ラベル (`labelsMatch`・`CallQuery.matches`)、`evidence` の定義集合、`near` / `similar` / `compare` の単位の選択 (`pick`)、`search` の完全一致優先、読めないファイルの警告の関連性判定 (`relevantParseErrors`)。

- **issue の再現の全問い合わせが期待どおりになった**: ``def A.repeat`` と ``def A.`repeat` `` が同じ定義 1 件、`context` が定義 1・参照 1・呼出 1、``callers `repeat` `` が `self.repeat(1)` を 1 件、`index` の `calls_approx` が 0 → 実在の件数。
- **変えていないことの実測**: バッククォート付きの名前を含む Swift で、重複検出の hash (`@c1e5d9a32fbe`) と除外の釘が前後で一致。dotfiles の `src` (Go 25 module) の `report` / `sweep` / `index` の出力が前後で byte 同一。
- **codex の敵対的レビュー 1 周**: 6 件を採用 (1 件は**この変更が持ち込んだ退行**: 引用付きの query が、読めないファイルの解析不完全の警告を落とす)。残り 5 件は比べる所の取りこぼし (型名の囲み・引数ラベルの囲み・`evidence`・`near` の単位選択・`search` の完全一致優先)。どれも発火条件を再現テストにした。
- **変異**: 11 か所それぞれを元に戻すと、対応するテストが赤になる。最初に緑のまま残った 3 本は、テストの穴だった (`refs` の query・宣言名の保存・`evidence` の文言が実際と違い常に偽になる assert)。直して赤を確認した。

### 残る限界 (記録)

- `self` / `Type` / `Protocol` は、裸のキーワードと囲み付きのメンバーで意味が異なりうる (Swift の識別子の仕様)。一律に同一視しているが、adapter から誤照合へ到達する具体例は確認できていない。
- `-format xcode` など、名前を表示するだけの経路は元の綴りのまま。
- `gate` の出どころの照合 (元の綴りで比べる) は意図して変えていない。
