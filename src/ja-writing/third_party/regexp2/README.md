# regexp2 v2.5.2 の写し (dotfiles で 1 か所を直したもの)

元: [github.com/dlclark/regexp2](https://github.com/dlclark/regexp2) の `v2` の v2.5.2 (MIT。`LICENSE` と `ATTRIB`)。
`src/ja-writing/go.mod` の `replace` で使う。写したのはビルドに要るパッケージ (`.`・`syntax`・`helpers`) のテストを除いた Go のソースだけ。

## 直した所 (2 か所)

1. `syntax/charclass.go` の `mayOverlapByEnumeration` に、1 字ずつ調べる字の数の上限 (`enumerationBudget` = 4096) を足した。
   超えたら調べ切らずに「重なりうる」と答える。なぜ: `internal/pyre` は否定の集合を補集合の範囲で書く (`internal/pyre/emit.go` の `emitSet`。
   regexp2 の後戻りの誤りを避けるため)。上限が無いと compile のたびに 100 万字を超える集合を回し、ja-writing lint の起動込みの所要の約 3 割を占めていた (issue 1079 に計測)
2. `syntax/tree.go` の `canBeMadeAtomic` の、後ろが 0 回からの集合のループのときの条件を `!subsequent.Set.MayOverlap(n.Set)` にした (上流は否定が無い。
   .NET の元は否定がある)。上流のままだと、後ろのループが前のループと重なるときに次のノードを調べる段へ進み、前のループを atomic にして後方参照の答えを変える
   (`([ad]*)([ab]*)c\2b` を `aacab` に当てると本家の Python の re は当たり、上流は当たらない)。1 の上限で「重なりうる」と答える組もこの段へ進ませていた

2 を直したので、`MayOverlap` の呼び出し元 (`canBeMadeAtomic` の 3 か所) はどれも「重なりうる → atomic にしない」の向きになり、1 の上限は最適化を見送るだけになる。
どちらも `internal/pyre/regexp2_fork_test.go` が見る (2 は本家の re と同じ答えになることで見る)。

## 新しいバージョンを取り込むとき

1. 上流の新しいバージョンで `mayOverlapByEnumeration` が同じ形か (上限が入っていないか) を見る。上流が直していれば、この写しと `replace` を消す
2. 直していなければ、同じパッケージのテストを除く Go のソースを写し直し、上の 1 か所を当て直す
3. `make -C src/ja-writing test` (本家の Python の re と比べる pyre の記録を含む) と、`bench/pybench.sh -g` で前後の所要を比べる
