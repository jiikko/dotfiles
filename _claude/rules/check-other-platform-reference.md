# Platform 特有のバグ調査では、必ず他 platform に動いている参照実装がないか確認する

## ルール

- **platform-specific (iOS / macOS / Windows / Linux 等) なバグを調査するときは、調査開始時に「同じ機能を別 platform でも提供しているか」「動いている参照実装があるか」を必ず確認する**
- ある場合は **構造を比較**して「動いている方の構造」「動いていない方の構造」の差分を抽出する
- 差分が見つかったら、それが原因仮説の最有力候補になる

## なぜこのルールが必要か

過去事例 (2026-05-23 DualNote iOS #030 IME バグ):

iOS の `CompositionAwareTextEditor` (UIViewRepresentable + UITextView) が AttributeGraph cycle + カーソル消失で壊れていた。

私の初動: **iOS だけ見て** delegate cycle 仮説に固執 → 3 回試行も治らず。

forge skill 起動 → 5 専門家中 4 人が **macOS の `ImeAwareTextEditor`** (同じ機能の動いている wrapper) を参照し、構造比較を一発で実施:

| 観点 | macOS (動く) | iOS (壊れる) |
|---|---|---|
| Focus 制御 | `onFocusChange: (Bool) -> Void` callback 一方向 | `@Binding var isFocused: Bool` 双方向 ← 真因 |
| `updateUIView/NSView` 内の responder chain | なし | `becomeFirstResponder()` 同期呼出 ← 真因 |
| Coordinator が保持するもの | 個別 closure / Binding | `parent: View struct` snapshot ← 副次原因 |

**真因がコード比較で 5 分で特定できた**。私が iOS だけ見て 3 時間溶かしたのが愚かだった。

## 採用パターン

### バグ調査開始時にやること (順序)

1. **症状を確認**: ユーザー報告 / ログ / 再現条件
2. **影響範囲を確認**: どの platform / どの module / どの機能
3. **「同じ機能を別 platform でも提供しているか」を grep / find で確認**
   - DualNote のように macOS / iOS で同じ app なら、対応する Sources を探す
   - 他プロジェクトでも同様 (Windows / macOS / Linux のクライアント 等)
4. **動いているか確認**:
   - 他 platform で同じバグが出ているなら共通根本原因
   - 他 platform は動いているなら **構造的差分** が原因仮説の最有力候補
5. **構造を比較**:
   - public API / 型シグネチャ
   - 内部実装の主要部分
   - lifecycle (init / update / dispose) の扱い
   - state management
   - delegate / callback パターン
6. 差分があれば、それを優先して仮説に組み込む

### grep / find のテンプレ

```bash
# 同名 file / 類似名 file を探す
find . -name "*EditorView*" -o -name "*TextEditor*" -o -name "*ImeAware*"

# 同じ機能を扱う class 名
grep -rn "UIViewRepresentable\|NSViewRepresentable" --include="*.swift"

# 同じ機能の implementation file を grep
grep -l "becomeFirstResponder\|makeFirstResponder" -r --include="*.swift"
```

### 構造比較の観点

- public API (引数 / return / 型) は同じか
- 内部 state は同じ種類のものを持っているか
- 同じイベント (focus 変化 / text 変化 / IME composition) をどう受け取っているか
- 同じ side effect (delegate からの parent 書き戻し / responder chain 操作) をどう扱っているか

差分が見つかったら、**動いている方の構造に揃える** ことを最初に検討する。

### 例外 (本ルールが効きにくいケース)

- platform に **同じ機能の参照実装が存在しない** (例: macOS にだけある機能)
- 動いている参照実装も **実は壊れている** (両方が壊れているケース)
- 構造的差分が **意図的なもの** (= docstring に理由が明記されている場合)

これらは別の調査手法 (forge skill / Apple Forum 検索 / 公式 sample) に切り替える。

## 関連事例

- **DualNote iOS #030** (2026-05-23): iOS UIViewRepresentable が macOS NSViewRepresentable と構造的に違うことが真因 (本ルールの直接の起源)
- 同種の事例は今後 DualNote 以外でも起きうる: macOS / iOS / watchOS で同じデータモデルや UI を持つ multi-platform app は要注意

## やること / やらないこと

- ✓ 調査開始時に「他 platform に参照実装あるか」を最初の 5 分で確認する
- ✓ 動いている参照実装と「壊れているコード」の構造比較を、仮説立てる前に実施
- ✓ 「動いている方の構造に揃える」を第 1 の修正方針として検討
- ✓ 意図的な構造的差分は docstring に理由を残す (将来の調査者のヒント)
- ✗ 自分が触っている platform だけ見て仮説を立てる
- ✗ 「macOS は別物だから関係ない」と切り捨てる (構造的には同根の場合が多い)
- ✗ 「動いている参照実装を見るのは時間の無駄」と省略する

## 関連

- `~/dotfiles/_claude/rules/escalate-to-forge-after-failed-tries.md` — 自前試行が失敗したら forge を呼ぶルール
- `~/dotfiles/_claude/rules/issue-creation-codex-review.md` — issue 作成時の codex review ルール
- DualNote iOS/Sources/Views/Components/CLAUDE.md の「UIViewRepresentable + Focus 設計原則 — 原則 5」 (本ルールのプロジェクト寄りの具体化)
