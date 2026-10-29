# 912 (design): audit の forge 経路 (監査タイプ × 専門家) を直接実行に寄せる

起票日: 2026-10-25

> **一部を済ませて保留** (2026-10-29。ユーザー決定: 911〜913 は保留)。eval の要らない 2 つ (audit-log の実績を数える・直接実行を「main が読んで判定する」形に直す) は済ませた。
> 残る eval の比較・Step 3 の推奨の変更・forge 経路の Phase 2〜5 の見直しは、911 の eval が要るので保留。**再開の条件**: 911 と同じ

epic [910](../910-design-fold-subagents-into-main-loop.md)。前提: [911](911-test-task-completion-evals-for-forge-audit.md) の eval。

## 概要

audit の Step 3 は「forge / 直接実行」を同格の選択肢として出す。forge を選ぶと、監査タイプごとに `/forge` を
「〜を探して issues ディレクトリに書き出して」というタスク説明で起動する。forge はこれを**実装モード**と判定するので
(forge SKILL.md「タスクタイプ判定」: パス以外は実装。判定は表を読んだモデルが行うので決定論ではない)、1 タイプにつき Phase 1 の専門家 fan-out
(Standard で Swift / generic は 6+1 体、Go は 5 体) + 1.1 のクロスレビュー + Phase 4 のレビュー fan-out が走る。
記事のいう「問題の種類で分けた分割」を、監査タイプと専門領域の 2 軸で掛け合わせた形になっている。

関連する記述が SKILL.md にある:

- Step 5 の注記: 2026-08-26 に監査タイプの数だけ体を並べて並行起動し (18 タイプ × 9 体)、session limit で 8 体が途中で死に、30 万トークン以上を使って結論に届いたのは 1 体・発見 1 件。
  **これは forge 経由とは書かれておらず、並行起動の体数過多の記録**。forge 経路そのもののコストはまだ測っていない (911 で測る)
- 設計系は「直接実行なら 1 体で全タイプを見てよい」と既に書いてある (2026-10-29 に「1 回の走査で」に直した。直接実行を main が読む形にしたため)

一方で、直接実行も「Explore エージェントでコードベースを調査し」と書かれていて、調査を委譲している。
Explore は抜粋を返す道具で (agent の説明: "It reads excerpts rather than whole files, so it locates code; it doesn't review or audit it")、
監査の読み込みそのものを任せる用途とは合っていない。

## タスク

- [x] eval の前に、既にあるデータを数える: audit は `issues/audit-log` に `forge-<モード>` / `direct` の実施記録を残す (Step 5)。
      各 repo の audit-log (`find ~ -path '*/issues/audit-log' -not -path '*/node_modules/*' 2>/dev/null` などで探す) から、forge で回した回と直接実行の回について、書かれた issue の数とその後の扱い (done になった / 却下された) を数える
- [ ] 911 の eval で、同じ監査タイプ (2〜3 本) を ①直接実行 (main が自分で読む) ②forge Minimum+ ③forge Standard で runs 回ずつ回し、検出数・偽陽性・費用・所要を表にする
- [ ] 結果で決める (判定規則は 911 に先に書いたものを使う。**判別できなければ「未決」として今のまま残す**):
  - ①が②③に勝つ、または費用・所要で勝って検出数が判別できる差で劣らない → Step 3 の推奨を「直接実行」にし、forge は「単独で回して固有の発見が出たと eval で示したタイプ」だけに残す
    (SKILL.md の記述では responsibility・leaky-abstraction が候補。ただしこれは「タイプを分けて回すと固有の発見が出る」という記述で、
    「forge の多体で回すと良い」の証拠ではない。eval で確かめる)
  - ②③が勝つタイプがあれば、そのタイプだけ forge に残し、理由 (eval の数字) を SKILL.md のそのタイプの行に書く
- [x] 直接実行の「Explore エージェントでコードベースを調査し」を見直す: 対象の場所を探すのに Explore を使うのはよいが、読んで判定するのは main にする形に書き換える
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

- 2026-10-29: 本文を今のコードと突き合わせた (forge のタスクタイプ判定・08-26 の並行起動の記録・ロスターの体数は本文どおり)。
- 2026-10-29: audit-log の実績を数えた (10 repo。他の repo の写し・SwiftPM の checkouts・worktree の重複は除いた)。issue 番号を repo の issues/ の今の位置で引いた結果:

  | 経路 | issue | done | open | pending / waiting | 見つからない |
  |---|---|---|---|---|---|
  | forge | 466 | 421 (90%) | 34 | 1 | 10 |
  | 直接実行 | 274 | 249 (91%) | 12 | 7 | 6 |

  done には「直した」と「却下した」が両方入るので、この数では経路の質の差は分からない (どちらも 9 割が片付く、までしか言えない)。
  使われ方: forge は 6 月以前に多く (promiseApp 133 行・dotfiles-gui 170 行など)、08-22 以降は 13 回 (09-03〜09-16、ほぼ Minimum / Minimum+)、09-16 より後は 0 回 (911 の「0 の結果」)
- 2026-10-29: audit の直接実行を「対象は自分で読んで判定する。Explore は読む場所を探すところまで」に直した (`_claude/skills/audit/SKILL.md`)。
  skill の整合を 3 つの見方で点検し、設計系の「1 体で全タイプ」を「1 回の走査で」に直した。`bin/skill-eval audit` PASS (cases 1/1)
