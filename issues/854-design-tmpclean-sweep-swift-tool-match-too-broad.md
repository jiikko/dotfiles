# 854 (design): tmpclean sweep の「Swift のツール」の拾い方が広く、Xcode の中の make が動いている間は消えない

起票日: 2026-10-21

## 概要

`swiftToolCommand` が Xcode.app の中の全部 (make・git など) を拾い、長く動く 1 本が sweep の範囲を止める。絞るかを決める。
