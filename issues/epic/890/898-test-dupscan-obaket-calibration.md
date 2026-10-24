# 898 (test): dupscan を obaket に当てて既定の閾値を決め、肯定と否定の期待値と変異で効いていることを確かめる

起票日: 2026-10-24

## 概要

[891 (要件の正本)](891-design-dupscan-requirements.md) の既定値 (下限・閾値) は実測で決める。obaket に当てて決め、PR 24 の前の木で肯定と否定の期待値を置き、変異で効いていることを確かめる。

## 受け入れ条件

- [ ] obaket の全体に当てたときの組の数・誤検知の割合・ノード数の分布・所要を実測して、この issue に書く (下限と閾値の既定の根拠)
- [ ] 全体の期待値: PR 24 の前の木の全体に **既定の設定 (既定の下限と閾値)** で当て、6 つが出ること、**同じ組に入ってよいもの / 入ってはいけないもの** を期待値にする。
      反証レビューが文法から推した候補: L2 で同じ形になるもの (`BrowserSelectionStateTests.makeLocation` の `"c"` / `.bucket("b")`、`S3StubTestSupport.leaf` の `"connection"`)、
      L3 で入りうるもの (`.container(Self.identifier)` / `.root` / `context.connection.id` を使う版・`StorageResumableMoveUseCaseTests.location(connectionID:path:)`)。どれを入れるかを決めて書く
- [ ] 変異で効いていることを確かめる。肯定と否定を対で置く: 6 つのうち 1 つに文を 1 つ足すと Type-2 から外れて Type-3 に残る / 1 つ消すと組が 5 になる /
      段を外すと期待どおりに組が割れる / 「入ってはいけないもの」が入るように閾値を緩める・畳みすぎる変異で否定の期待値が赤になる (`mutate-verify` で当てる)

## 進捗

- 2026-10-24 起票 (epic 890 の分解)
