# バグ修正で 1-2 回の自前試行が失敗したら、迷わず forge skill Maximum に escalate

## ルール

- **自分の hypothesis で 1-2 回試行して効果がなかったバグは、3 回目を試す前に必ず `/forge` skill Maximum モードを起動する**
- 「もう少しで分かりそう」と粘って試行を重ねるのは禁止
- 「治っていない」という症状報告を受けた時点で、即 forge へ

## なぜこのルールが必要か

過去事例 (2026-05-23 DualNote iOS #030 IME バグ):

私が「delegate cycle」という hypothesis に固執して 3 回試行した:

1. `isApplyingExternalUpdate` フラグで delegate を抑制 → ユーザー指摘「patchwork」で撤退
2. `delegate = nil` で構造的 suppress → ユーザー testing で「治っていない」
3. docstring を「構造的に発生不可能」と overclaim → 治っていないのに自信過剰

3 回試行しても治らない時点で `/forge` Maximum を起動すべきだった。実際に発動したら 5 専門家が **全員一致で別の真因** (`becomeFirstResponder` 同期呼び出し + `@FocusState ↔ Binding<Bool>` bridge) を特定し、1 セッションで構造的解決に至った。

私の損失:
- 約 3-4 時間の無駄な試行
- 「治っていない」を 2 回ユーザーに報告する psychological cost
- 誤った docstring を commit に残した (後で訂正コミットが必要に)

forge 1 回起動のコスト: ~30 分。経済的に明らかに割に合う。

## 採用パターン

### 即 escalate するシグナル (1 つでも当てはまれば forge へ)

| シグナル | 例 |
|---|---|
| 同じ hypothesis で 2 回連続「効かない」 | 「fix A → 治らない」「fix B → 治らない」 |
| ユーザーが「治っていない」と明示 | 単純な build キャッシュ問題でない限り即 forge |
| 自分の仮説と症状の説明が部分的に矛盾している | 「入力時の cycle」仮説なのに「入力前から cycle 発生」が観察される、等 |
| Apple Forum / WWDC level の framework 内部挙動が絡む | AttributeGraph cycle / actor isolation / KVO race 等 |
| platform-specific bug で他 platform の参照実装を見ていない | iOS 限定バグなのに macOS 動作実装を確認していない |
| 3 回目の hypothesis を考え始めた瞬間 | 不確実な仮説の追加は確度を下げる、forge を呼ぶべきタイミング |

### forge 起動時のテンプレ

```
バグ症状: [事実をそのまま、私の解釈を入れない]
試行履歴: [何を試して、何が効かなかったか]
私の現在の hypothesis: [間違っている可能性大、明示]
関連ファイル: [絶対パスで列挙]
制約: [モード Maximum 推奨、緊急度、revert 可能か]
```

`Maximum` モードは複数専門家並行 + クロスレビューで真因に収束する。確度の低い hypothesis に固執するより、5 視点の合意を取る方が早い。

### 例外 (forge 不要)

- typo / build error のような明らかな単純ミス
- 自分が以前に直したパターンの完全な再発
- ユーザーが「これだけ確認して」と特定アクションだけを依頼

## なぜ「2 回」が境界か

- 1 回目の試行: 主仮説の検証 (=妥当な workflow)
- 2 回目の試行: 補正/別角度 (= まだ自前で OK)
- 3 回目の試行: 「もう少しで分かる」という認知バイアスのサイン。客観性が落ちている

## やること / やらないこと

- ✓ 「治っていない」を聞いた瞬間に forge を起動する選択肢を考える
- ✓ 自分の hypothesis を 1 回 forge に投げて、別視点で叩いてもらう
- ✓ forge の結論が自分の仮説と異なる場合、forge を優先する (固執しない)
- ✗ 「あと 1 回試したい」で時間を溶かす
- ✗ 「自分で解決した方が学びになる」と自己満足で粘る (ユーザーへの time cost を無視している)
- ✗ docstring に「構造的に発生不可能」のような断定を、未検証で書く (今回の overclaim 反省)

## 関連

- `~/dotfiles/_claude/rules/instrument-before-second-fix.md` — **forge より先に発動する**: 1 回目の
  仮説 fix が外れたら、まず観測を増やす。観測しても不明なら本ルール (forge) へ。観測データを
  手土産にすると専門家の収束が速い
- `~/dotfiles/_claude/rules/check-other-platform-reference.md` — platform 特有バグ調査の前段ルール
- `~/dotfiles/_claude/rules/issue-creation-codex-review.md` — issue 作成時の codex review ルール (forge とは別経路の検証)
- DualNote iOS issue #030 の経緯 (本ルールの直接の起源)
