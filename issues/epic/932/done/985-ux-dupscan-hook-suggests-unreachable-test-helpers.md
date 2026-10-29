# 985 (ux): 書く前の案内の hook が、別の package のテストの非公開の helper を「近い既存の定義」に出す

起票日: 2026-10-30

概要: Go で新しいテストの helper を書くと、hook (`inject-dupscan-candidates-on-new-func.sh`) が別の module・package の `*_test.go` にある同名の非公開の関数を候補に出す。別の package のテストの関数は呼べないので、再利用の候補にならない。

> 反証レビュー: 実装と合わせて 1 回 (opus、読み取り)。本文は実装の後に書いた。

## 詳細

- 場面: 963・976 の作業中に、Go の新しいテストの helper を書いたら hook が `src/pro-con/card/card_test.go` の `reasons(vs []Violation)` (別の module) と `src/dupscan/internal/group/group_test.go` の `entry` (同じ module の別の package) を出した。前者は別の道具 (module) のテストの helper で、呼ぶことも共通の置き場へ寄せることもできない
- 後者は呼べないが、同じ module の中なら共通のテストの置き場へ寄せる候補になる (obaket 1308・1309 が寄せた写しはこの形)。なので落とすのは**別の Go の module** (一番近い `go.mod` が違う) の `[test,unexported]` の候補だけにした

## 進捗

- 2026-10-30: 実装 (dotfiles-53)。hook の `reachable()` が、Go で `[test,unexported]` の印の付いた候補のうち、書いているファイルと module が違うものを落とす。印は `(呼び出し ~N)` の直前の位置で読む (署名の `map[string]` を印と読まない)
- 直したついで: hook が書くファイルの相対パス `rel` と `git check-ignore` に渡すパスを実パスにした。git の root は symlink を解いた実パス (macOS の `/var` → `/private/var`) なので、`/var` のまま渡すと `rel` が `../` で始まり、型の照らしで自分のファイルを外せず、ignore の判定も外れていた
- 検証: `tests/claude/test_inject_dupscan_candidates_on_new_func.sh` ok=51 fail=0、root の `make test-lint` (下)
- 変異 (mutate-verify-list、4 本。どれも狙った検査が赤): module を比べない / 実パスにしない / 印の正規表現を直す前の `\[([a-z,]+)\]` に戻す。最初の版の 3 本 (同じ dir に絞った版) も赤を確かめた後、レビューで絞り方を変えた
  - 後ろの位置の絞り (`(呼び出し` の直前か doc の前か行末) を外す変異は緑だった: 署名の `map[string]` は先頭の空白の条件だけで外れ、後ろの条件が効くのは、印の無い行の doc の末尾に ` [test,unexported]` と書いた形だけ。テストでは固定していない
- **反証レビュー (opus、読み取り)**: P2 2 件・P3 4 件。採ったもの: ①P2 署名の `map[string]` を印と読む → 位置で絞った ②(絞り方の見直し) 同じ dir に絞った最初の版は、同じ module の中で寄せられる写しまで落としていた → 別の module だけにした ③P3 `git check-ignore` に `/var` のままのパスを渡していた → 実パス
  - 採らなかったもの: 絞り込みが dupscan の `-limit` (20 / 50) の後にあり、同名の候補が多いと残すべき候補が上限の外に落ちる (前と比べて悪くならない: 前も上限の外の候補は出ていなかった)。external test package (`package foo_test`) を区別しない・別の package の prod の非公開の関数を落とさない (どちらも寄せる候補として残す側。雑音は今回の形に限る)
