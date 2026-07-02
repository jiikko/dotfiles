# バグ修正で 1-2 回の自前試行が失敗したら、迷わず forge skill Maximum に escalate

## ルール

- **自分の hypothesis で 1-2 回試行して効果がなかったバグは、3 回目を試す前に必ず `/forge` を Maximum モード指定で起動する**（forge v2 はモードを明示すれば Phase -1 のモード選択確認を省略する）
- 「もう少しで分かりそう」と粘って試行を重ねるのは禁止
- 「治っていない」という症状報告を受けたら、まず [`instrument-before-second-fix.md`](instrument-before-second-fix.md) に従い観測を増やし、観測しても真因が不明なとき forge へ (初回試行直後に blind で forge 直行ではなく、観測データを手土産にする)

## なぜ (起源: DualNote iOS #030 IME バグ, 2026-05-23)

「delegate cycle」という hypothesis に固執して 3 回試行 (フラグ抑制 → delegate=nil → 未検証の overclaim docstring) して全て外した。forge Maximum を起動したら複数の専門家エージェントが全員一致で**別の真因**を特定し 1 セッションで構造的解決。損失は 3-4 時間の無駄な試行と「治っていない」報告 2 回。forge 1 回 ~30 分のコストの方が明らかに安い。

## 「2 回」が境界の理由

1 回目 = 主仮説の検証 (妥当)。2 回目 = 補正/別角度 (まだ自前で OK)。3 回目を考え始めた = 「もう少しで分かる」という認知バイアスのサインで客観性が落ちている。

## 即 escalate するシグナル (1 つでも該当したら forge へ)

- 同じ hypothesis で 2 回連続「効かない」
- ユーザーが「治っていない」と明示 (build キャッシュ問題でない限り)
- 自分の仮説と症状の説明が部分的に矛盾している
- framework 内部挙動が絡む (AttributeGraph cycle / actor isolation / KVO race 等)
- platform-specific bug で他 platform の参照実装を見ていない ([`check-other-platform-reference.md`](check-other-platform-reference.md))
- 3 回目の hypothesis を考え始めた瞬間

## forge 起動時のテンプレ

```
バグ症状: [事実をそのまま、私の解釈を入れない]
試行履歴: [何を試して、何が効かなかったか]
私の現在の hypothesis: [間違っている可能性大、と明示]
関連ファイル: [絶対パスで列挙]
制約: [モード Maximum 推奨、緊急度、revert 可能か]
```

## 例外 (forge 不要)

- typo / build error のような明らかな単純ミス
- 以前直したパターンの完全な再発
- ユーザーが特定アクションだけを依頼している場合

## やること / やらないこと

- ✓ 「治っていない」を聞いた瞬間に forge 起動を検討する
- ✓ forge の結論が自分の仮説と異なる場合、forge を優先する (固執しない)
- ✗ 「あと 1 回試したい」で時間を溶かす
- ✗ docstring に「構造的に発生不可能」のような断定を未検証で書く

## 関連

- [`instrument-before-second-fix.md`](instrument-before-second-fix.md) — **forge より先に発動**: 1 回目が外れたらまず観測を増やし、観測しても不明なら forge へ (観測データを手土産に)
- [`check-other-platform-reference.md`](check-other-platform-reference.md) — platform 特有バグ調査の前段ルール
