# atomicfile

途中の状態を残さないファイル書き込み (temp + rename) を 1 箇所に置く module。glogx (状態キャッシュ・
issue の claim バナー)・ratelimit (利用枠キャッシュ)・pro-con (状態の置き場)・treefiler (設定・履歴・前回の場所)・mac-lan-manager (台帳・設定・操作のログ) が replace で取り込む。残骸の掃除は `IsTemp` で名前を判定する。制約は atomicfile.go の doc。

- 振る舞いのテストは消費者側にある (glogx の `cache_test.go` 等が temp の残骸と失敗経路を見る)
