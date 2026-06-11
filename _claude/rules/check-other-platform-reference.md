# Platform 特有のバグ調査では、必ず他 platform に動いている参照実装がないか確認する

## ルール

- **platform-specific (iOS / macOS / Windows / Linux 等) なバグを調査するときは、調査開始時 (最初の 5 分) に「同じ機能を別 platform でも提供しているか」「動いている参照実装があるか」を必ず確認する**
- ある場合は仮説を立てる前に **構造を比較** し、「動いている方」と「動いていない方」の差分を抽出する。差分が原因仮説の最有力候補になる
- 修正方針の第 1 候補は「**動いている方の構造に揃える**」

## なぜ (起源: DualNote iOS #030 IME バグ, 2026-05-23)

iOS の UIViewRepresentable wrapper が壊れていたとき、iOS だけ見て delegate cycle 仮説に固執し 3 回試行して全て外した。forge の専門家は macOS の**同機能の動いている wrapper** と構造比較を一発で実施し、真因 (双方向 Binding と `becomeFirstResponder()` 同期呼出という構造的差分) を 5 分で特定した。

## 調査開始時の手順

1. 症状と影響範囲 (platform / module / 機能) を確認
2. **同じ機能を別 platform でも提供しているか** を grep / find で確認 (同名・類似名ファイル、`UIViewRepresentable|NSViewRepresentable` 等)
3. 他 platform で動いているか確認:
   - 他でも同じバグ → 共通根本原因
   - 他は動いている → **構造的差分が最有力仮説**
4. 構造を比較する。観点:
   - public API / 型シグネチャは同じか
   - 内部 state は同じ種類を持っているか
   - 同じイベント (focus 変化 / text 変化 / IME composition) をどう受け取っているか
   - 同じ side effect (parent への書き戻し / responder chain 操作) をどう扱っているか
   - lifecycle (init / update / dispose) の扱い
5. 差分が見つかったら「動いている方の構造に揃える」を最初に検討

## 例外 (効きにくいケース)

- 別 platform に同じ機能の参照実装が存在しない
- 参照実装も実は壊れている (両方壊れているケース)
- 構造的差分が意図的 (docstring に理由が明記されている)

→ 別の調査手法 (forge skill / 公式 sample / Forum 検索) に切り替える。

## やること / やらないこと

- ✓ 調査開始の最初の 5 分で参照実装の有無を確認する
- ✓ 仮説を立てる前に構造比較を実施する
- ✓ 意図的な構造的差分には docstring に理由を残す
- ✗ 自分が触っている platform だけ見て仮説を立てる
- ✗ 「別 platform は別物だから関係ない」と切り捨てる (構造的には同根の場合が多い)

## 関連

- [`escalate-to-forge-after-failed-tries.md`](escalate-to-forge-after-failed-tries.md) / [`instrument-before-second-fix.md`](instrument-before-second-fix.md) (成功 vs 失敗の A-B 差分観測と同じ思想)
