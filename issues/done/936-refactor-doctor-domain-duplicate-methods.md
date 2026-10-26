# 936 (refactor): doctorの同一ドメイン内の重複処理を共通化する

起票日: 2026-10-26

## 概要・裏どり

[935](../epic/932/done/935-test-doctor-duplicate-discovery-effect.md)の検出結果から、次の2組を共通化する。起票時のsourceは212ddbbd。dupscan index/report/callers/nearと本体読取で再確認した。共通化は独立した2箇所で行い、別packageを横断する汎用utilityは追加しない。

| 対象 | 現状 | 寄せ先 |
|---|---|---|
| internal/displaycheck/displaycheck.go:317 sortedGateKeys / :326 sortedKeys | mapの値型だけが違い、キー取得・ソートの本体は同じ。reportでL1の1組、2定義 | 同packageのprivate sortedKeys[V any] |
| disk/scan_test.go:31 fakeRunner.run / disk/delete_test.go:32 deleteRunner.run | 最長のcommand prefixをトークン境界まで照合するloopが同じ。nearは「近いものはない」で部分重複を検出しない | diskの_test.goにprivate longestCommandPrefix[V any] |

表示検査は製品runtimeから呼ばれない検査ライブラリ。diskのrunnerもテスト専用。削除/走査の製品処理を共通化する課題ではない。

## 対応方針・維持する振る舞い

1. 表示検査は既存sortedKeysをgeneric化し、Gateとstringのmapの両方から呼ぶ。nil/空mapでも従来どおり空の非nil sliceを返し、キーを昇順にする。
2. diskのprefix選択だけを抽出する。map値を返さずkeyを返し、runner側のresponse型、stdout/stderr/rc/error、callsのmutex保護、onCallの順序と引数コピーを維持する。
3. prefix一致は行末またはliteralな空白で区切る。部分的なパス一致やtabを空白とみなす変更は加えない。最長keyを選び、空key/一致なしは従来どおり未一致として扱う。
4. 二つの変更は同じdoctor内でも別の規則。表示検査とdiskテストのhelperを共有packageへまとめない。

## 検証・受け入れ条件

- [x] 表示検査のソート本体2個を1個へ集約し、2利用箇所が同じ定義を使う。
- [x] diskのprefix選択loop2個をテスト専用の1定義へ集約し、両runnerを実際に通すテストで境界・最長一致・未一致・応答/副作用を確認する。
- [x] 表示検査で両map値型、nil/空、昇順・値非依存を確認する。
- [x] make -C src/doctor lint / test（raceとderived-fieldsの強制実行を含む）が成功。
- [x] dupscan report / gate -v -strict-analysisで完全重複の解消と新規違反なしを確認。部分重複は機械結果だけで解消と扱わず、差分と両runnerの呼出しを照合する。
- [x] 読み取り専用の反証レビューを通し、作業で得たdupscan改善点を専用issueまたは既存課題へ記録する。

## 進捗・改善評価

- 2026-10-26: `issues: reserve doctor duplicate consolidation`で番号予約。実装前のreportはL1の1組。index -name prefixはbrewPrefixだけで、今回の部分重複は既存loopの読取で確認した。着手時点では未修正。

目的は同じ規則を修正する箇所を各2個から各1個へ減らすこと。テスト基盤だけの変更で、実行速度やmodel context削減の効果は未測定。genericを2個追加する複雑さと、runnerの意図的な違いを保つことをレビューする。

- 2026-10-26: `refactor(doctor): share display keys and test command matching`で両処理を共通化。doctorのreportは1組から0組、strict gateは解析エラー/新規違反なし、消えた組1。callersでsortedKeysの本体利用2箇所、longestCommandPrefixの両runner利用2箇所を照合した。部分loopはソース差分でも2→1を確認した。
- 2026-10-26: make -C src/doctor lint testが成功（race・derived-fieldsのキャッシュ無効実行を確認）。読み取り専用Codex subagentがソース/テスト/issueを反証レビューし、不具合の指摘なし。src全体の通常gateは新規違反なしだが、既知の意図的な壊れたfixture src/subproc/boundarycheck/testdata/broken/broken.goにより解析不完全。doctorに絞ったstrict gateは成功しており、src全体の完全解析成功とは扱わない。
- 2026-10-26: 実作業で得たtest owner表示の課題を[937](../epic/932/done/937-feat-dupscan-go-test-call-owner.md)へ切り出した。部分重複の検出自体は895で継続。規則の実装箇所は各2→1となり修正漏れの可能性を減らした。追加コストはprivate generic helper2個と回帰テスト。性能/context効果は未測定。

- 2026-10-26: `docs(doctor): complete consolidation and specify dupscan test owners`で完了記録と改善課題への参照を保存。gateの除外は追加していない。
