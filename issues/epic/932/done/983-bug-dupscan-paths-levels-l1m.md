# 983 (bug): paths の除外の levels が L1m を粗い側として扱わない — 起票を取り下げ

起票日: 2026-10-30

## 概要

984 の監査で「`levels` に L1 を書いて L1m を書かない paths の除外が L1m の組に効かず、読み込みのエラーにもならない」を bug として起票したが、
反証レビューで **973 で決めて README に書いた仕様**だと分かったので取り下げる (番号は採番済みなので、却下の理由を残すためにこの issue を done に置く)。

- README の「除外の設定」: 「`levels` を書いたときは書いた段だけ (L1m は粗い側の検査に入らないので、要るなら書き足す)」([973](973-bug-dupscan-exclusion-level-and-narrow-allow-group.md) の P2-a)
- 新しく足された除外の要素 (`Rule.elements`) が L1m を出さない件は、973 に「P3 (記録・未対応)」として記録済み (gate を `-levels` に L1m を入れて回したときだけ効く。CI は L1・L2)
- 起票した対応方針 (L1 を含むなら L1m も必須) は、`exclude_cmd_test.go` の `levels: ["L1","L2","L3"]` を使うテスト (`Mirror/**` の除外) を読み込みのエラーにする

## 再提起の条件

L1m を既定の段 (report / gate の `-levels` の既定) に入れるとき。そのときは levels を書いた除外が L1m に効かないことが既定の出力に出るので、
`check` の粗い側に L1m を入れるかと `elements` の L1m を同じ変更で決め直す。
