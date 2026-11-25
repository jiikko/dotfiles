# regexp2 v2.5.2 の写し (dotfiles で 1 か所を直したもの)

元: [github.com/dlclark/regexp2](https://github.com/dlclark/regexp2) の `v2` の v2.5.2 (MIT。`LICENSE` と `ATTRIB`)。
`src/ja-writing/go.mod` の `replace` で使う。写したのはビルドに要るパッケージ (`.`・`syntax`・`helpers`) のテストを除いた Go のソースだけ。

## 直した所

`syntax/charclass.go` の `mayOverlapByEnumeration` に、1 字ずつ調べる字の数の上限 (`enumerationBudget` = 4096) を足した。
超えたら「重なりうる」と答える。呼び出し側 (`RegexNode.findAndMakeLoopsAtomic` の、ループを atomic にできるかの判断) は最適化を 1 つ見送るだけで、当たり方は変わらない。

なぜ: `internal/pyre` は否定の集合を補集合の範囲で書く (`internal/pyre/emit.go` の `emitSet`。regexp2 の後戻りの誤りを避けるため)。
上限が無いと compile のたびに 100 万字を超える集合を回し、ja-writing lint の起動込みの所要の約 3 割を占めていた (issue 1079 に計測)。

## 新しいバージョンを取り込むとき

1. 上流の新しいバージョンで `mayOverlapByEnumeration` が同じ形か (上限が入っていないか) を見る。上流が直していれば、この写しと `replace` を消す
2. 直していなければ、同じパッケージのテストを除く Go のソースを写し直し、上の 1 か所を当て直す
3. `make -C src/ja-writing test` (本家の Python の re と比べる pyre の記録を含む) と、`bench/pybench.sh -g` で前後の所要を比べる
