# 985 (ux): 書く前の案内の hook が、別の package のテストの非公開の helper を「近い既存の定義」に出す

起票日: 2026-10-30

概要: Go で新しいテストの helper を書くと、hook (`inject-dupscan-candidates-on-new-func.sh`) が別の module・package の `*_test.go` にある同名の非公開の関数を候補に出す。別の package のテストの関数は呼べないので、再利用の候補にならない。
