# 912 (design): audit の forge 経路 (監査タイプ × 専門家) を直接実行に寄せる

起票日: 2026-10-25

epic [910](910-design-fold-subagents-into-main-loop.md)。前提: [911](911-test-task-completion-evals-for-forge-audit.md) の eval。

## 概要

audit の Step 3 は「forge / 直接実行」を同格の選択肢として出す。forge を選ぶと、監査タイプごとに `/forge` を
「〜を探して issues ディレクトリに書き出して」というタスク説明で起動する。forge はこれを**実装モード**と判定するので
(forge SKILL.md「タスクタイプ判定」: パス以外は実装。判定は表を読んだモデルが行うので決定論ではない)、1 タイプにつき Phase 1 の専門家 fan-out
(Standard で Swift / generic は 6+1 体、Go は 5 体) + 1.1 のクロスレビュー + Phase 4 のレビュー fan-out が走る。
記事のいう「問題の種類で分けた分割」を、監査タイプと専門領域の 2 軸で掛け合わせた形になっている。

関連する記述が SKILL.md にある:

- Step 5 の注記: 2026-08-26 に監査タイプの数だけ体を並べて並行起動し (18 タイプ × 9 体)、session limit で 8 体が途中で死に、30 万トークン以上を使って結論に届いたのは 1 体・発見 1 件。
  **これは forge 経由とは書かれておらず、並行起動の体数過多の記録**。forge 経路そのもののコストはまだ測っていない (911 で測る)
- 設計系は「直接実行なら 1 体で全タイプを見てよい」と既に書いてある

一方で、直接実行も「Explore エージェントでコードベースを調査し」と書かれていて、調査を委譲している。
Explore は抜粋を返す道具で (agent の説明: "It reads excerpts rather than whole files, so it locates code; it doesn't review or audit it")、
監査の読み込みそのものを任せる用途とは合っていない。

## タスク

- [ ] 911 の eval で、同じ監査タイプ (2〜3 本) を ①直接実行 (main が自分で読む) ②forge Minimum+ ③forge Standard で runs 回ずつ回し、検出数・偽陽性・費用・所要を表にする
- [ ] 結果で決める:
  - ①が②③と同等以上 → Step 3 の推奨を「直接実行」にし、forge は「単独で回して固有の発見が出たと eval で示したタイプ」だけに残す
    (SKILL.md の記述では responsibility・leaky-abstraction が候補。ただしこれは「タイプを分けて回すと固有の発見が出る」という記述で、
    「forge の多体で回すと良い」の証拠ではない。eval で確かめる)
  - ②③が勝つタイプがあれば、そのタイプだけ forge に残し、理由 (eval の数字) を SKILL.md のそのタイプの行に書く
- [ ] 直接実行の「Explore エージェントでコードベースを調査し」を見直す: 対象の場所を探すのに Explore を使うのはよいが、読んで判定するのは main にする形に書き換える
- [ ] forge 経路を残す場合、監査は issue を書く作業なので forge の Phase 2〜5 (実装・セルフレビュー×5・修正方針) が要るかも見直す
      (レビューモード相当の fan-out だけで足りるなら、それを呼ぶ形にする)
- [ ] 変えたら skill を直した規約どおり、整合チェックを観点を変えて 3 回 + `bin/skill-eval audit` を回す (`~/.claude/CLAUDE.md`「レビュー方針」)

## 受け入れ条件

- Step 3 の推奨と、forge に残すタイプの理由が eval の数字で書かれている
- 直接実行の手順が「main が読んで判定する」形になっている

## 関連ファイル

- `_claude/skills/audit/SKILL.md` (Step 3 / Step 4 / Step 5 の並行起動の注記)
- `_claude/skills/forge/SKILL.md` (タスクタイプ判定) / `_claude/skills/forge/_common/modes.md`

## 進捗

起票のみ。
