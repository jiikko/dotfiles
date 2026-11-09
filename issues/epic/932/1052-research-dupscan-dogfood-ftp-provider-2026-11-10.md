# 1052 (research): obaket の FTP provider の実装で dupscan を使った所見

起票日: 2026-11-10

所属: [932](932-epic-dupscan-accuracy.md)

既存の issue との重複は照合していない。所見の記録なので反証レビューは省略した。

## 使った回数 (会話の記録から数えた概数)

gate 約 10 回、sweep 3 回、index 約 10 回、callers 2 回。grep に戻したのは文字列・コメント・設定の検索と、`git grep` で origin の木を読むとき。

役に立った場面:
- `index -file` と `callers` で、本番から呼ばれない `FTPClient.machineStatus` と `currentDirectory` を見つけて消した (テストだけが呼んでいた)。
- gate が `hasValidPort` の写しを止め、`ConnectionPortField` に寄せた。

## 摩擦

### F1 関数の `dupscan:allow` が gate では中のクロージャに効かない (直した)

- 場面: `FTPAdapter.write` の直前に `// dupscan:allow intentional …` を書いて `dupscan gate`。
- 期待: sweep と同じく中のクロージャも静かになる (README の sweep の節 2 はそう書いている)。
- 実際: rc=1。`FTPAdapter.write.{closure}` が止まる。クロージャの直前には印を書けないので、関数の印だけが手段だった。
- 対応: 関数に付けた印を `exclude.Set.Allowed` で囲む単位までたどるようにした (gate・sweep・report が同じ判定になる)。`dropAllowedGroups` の別実装は消した。
- 確かめ方: `TestGateAllowOnFunctionCoversItsClosures` (印なしは赤、印ありは緑)。`e = s.byKey[...]` を消す変異で赤。実物の FTP の `write` と SFTP の組で gate が rc=1 から rc=0 になった。
- 種類: 文書とずれ / 使い方が分かりにくい。

### F2 PATH の dupscan が origin より古くて、README の `sweep -focus` が無い

- 場面: `dupscan sweep -focus shared/... shared/...`。
- 実際: `flag provided but not defined: -focus` で rc=2。共有 checkout の dotfiles が origin から 62 commit 遅れていて、`bin/dupscan` の実体は 10-27 のもの (`ls -la`)。
- その後: `-focus` を外した。
- 提案: 未定義の flag のときに「この実体は origin より N commit 古い」を添える。見送るなら README に更新の手順を書く。未対応。
- 種類: 使い方が分かりにくい。

### F3 SwiftUI の form の欄は closure の L2 として毎回止まる

- 場面: `FTPConnectionFormView` の `ConnectionFormField { ObaketTextField(...) }` が SFTP / WebDAV と同じ形で 5 組止まった。
- その後: 欄を組み立てる private の関数に寄せて解消した。寄せること自体は改善だが、provider ごとの form は入力例と AX 識別子だけが違う、`.dupscan.json` に大量に除外がある形。
- 提案: closure の L2 の組で、メンバーの親が別々の `var body` で本体が 1 つの呼び出しだけのとき、`報告だけ` に落とす案。雑音の量は未計測。
- 種類: 雑音。

## 提案

### P1 `dead` 相当: 本番から呼ばれない公開メソッドの一覧

- 場面: 新しい module の中の未使用を探した。
- 今の回避: `index -file` の呼び出し数を見て、`callers` で個別に確かめた (呼び出し数はテストも数えるので、テストだけが呼ぶものを別に見分ける手間がある)。
- 提案: `index` に `-prod-callers 0` の絞り込み。呼び出し元が全部テストの単位を一覧にする。
- 効く理由と確かめ方: 新しい module を仕上げるときに 1 回で済む。obaket の FTP なら 2 件が出ることで確かめられる。
- 失うもの: 名前だけの照合なので同名の別定義で漏れる (0 件は不存在の証拠にならない注意書きが要る)。未対応。
