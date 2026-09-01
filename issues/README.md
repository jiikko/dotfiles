# issues/ — issue 管理

## ファイル命名規約（2026-07-16 導入）

新規 issue は次の形式で命名する:

```
issues/NNN-<カテゴリ>-<スラッグ>.md
```

- **NNN**: 3 桁ゼロ埋めの連番。**issues/ 直下・pending/・done/ の全体**で最大番号 + 1 を採番する（番号は再利用しない）。pending/ や done/ へ移動してもファイル名は変えないため、コードコメント・commit message から「issue 012」で安定して参照できる
- **カテゴリ**: 下表の prefix のいずれか
- **スラッグ**: kebab-case の短い説明。日付を残したい場合は末尾に `-YYYY-MM-DD`

| prefix | 用途 |
|---|---|
| `feat` | 新機能・機能拡張 |
| `bug` | 不具合修正 |
| `refactor` | 挙動を変えない構造改善・複雑性削減 |
| `perf` | 速度・メモリ・リソースの改善（実測の裏付けを本文に置く） |
| `docs` | ドキュメント・ルール・コメント整備 |
| `chore` | 雑務（依存更新・メッセージ/表示の手直し・テストの前提整理など、機能でも不具合でもないもの） |
| `research` | 調査・設計検討（成果物がコードでないもの） |
| `human` | **人間しかできない作業**（動作確認・目視レビュー・外部サービスの操作・判断待ち。人がやるまで open。`期限:` 必須）。何をしてほしいかはスラッグに書く（例 `068-human-verify-ci-poll.md`） |
| `retro` | **セッションの振り返り**（Claude が実質的な作業をやり切ったら自発的に起票。反省・気づき・改善案を書き、切り出し先を提案する。本文の残課題が空になったら done。詳細は下節） |

例: `issues/001-refactor-makefile-test-autodiscovery.md` / `issues/002-bug-nvim-cterm-drift-2026-07-16.md`

次番号の確認 (先に `git fetch` して origin 側も数える — 別セッションが push 済みの採番は
working tree の `ls` には見えない。148 が 2 セッションで衝突した起点):

```sh
git fetch origin
{ ls issues issues/pending issues/done; git ls-tree -r --name-only origin/master -- issues | sed 's|.*/||'; } |
  grep -E '^[0-9]{3}-' | sort | tail -1
```

**番号の一意性は機械が守る**: `tests/issues/test_issue_numbers_unique.sh`（`make test` に自動発見で
含まれる）が `issues/` 配下を掘って NNN の重複を検出し、重複していたら参照の数え方まで出す。
2026-08-28 に 127 と 133 が同時に衝突していたのを人手で見つけたのが起点で、**衝突を先に踏むのは
番号を取る次の人**（最大番号 + 1 が既に使われている / `issues/127-*` の glob が 2 件返る）。

**並行セッションと同時に採番するときは、番号を取る前に一声かける**（衝突は commit してから
気づくと、参照の張り替えか改番のどちらかを必ず払うことになる）。衝突してしまったら
**参照の少ない側を空き番号へ寄せる**。`grep -rn '<番号>'` の tracked 参照と
`git log --grep='issue <番号>'` の commit message 参照を両方数え、**commit message は履歴なので
直せない**ため、そちらから参照されている側は動かさない。改番したファイルの冒頭には
「過去の会話やメモの『旧番号』がこの話ならこの issue」と注記を残す（番号だけ覚えている人の
動線が切れるため。実例: 135 と 136）。

## `期限:` — 人が読む期限を本文に書く

本文冒頭のメタ行（`起票日:` の隣）に `期限: YYYY-MM-DD` を書ける。

- **`human` は必須**（人間待ちの作業は放置すると価値が腐るため）。他カテゴリは任意
- 書式は**行頭 `期限:` + 半角コロン + `YYYY-MM-DD`**。全角コロンでも `issue-sync` は拾うが、
  表記は半角に揃える（拾えなかった期限は「期限なし」と区別できず黙って埋もれる）
- 期限は「読んで確認する期限」であって「直す期限」ではない
- **既読の唯一の出典はファイルの位置**（`issues/` にある = 未読、`issues/done/` にある = 確認済み）。
  既読ヘッダー・チェックボックスは使わない（本文を書き換え忘れると嘘が残るため、移動で表す）
- 未完了の一覧は glogx の issues viewer（`i` キー）の **`human` タブ**が見せる。このタブは
  **件数 0 でも All の右に固定**で出る（少数のうちに `other` へ沈んで見落とすのを防ぐため）。
  ただし viewer は期限を表示しないので、期限は下の hook / skill が受け持つ
- **未完了と期限切れはセッション開始時に自動で出る**: `_claude/hooks/human-tasks-due.sh`（SessionStart
  hook。配線は `_claude/settings.json`）が未完了の `human` issue と期限切れ／期限間近を Claude の
  コンテキストへ注入する。読み取れなかったもの（期限なし・書式不正・読み取り不可・抽出失敗）も
  黙って捨てず列挙する。`issue-sync` skill でも同じ点検を最初に行う

## `retro` — セッションの振り返りを流さずに残す

Claude が**実質的な作業をやり切った時点**（機能追加・バグ修正・ルール整備・構造変更など）で
`NNN-retro-<スラッグ>-YYYY-MM-DD.md` を自発的に起票する。typo 修正・数行の chore・調査だけで
終わったセッションは対象外（薄い retro を量産すると形骸化する）。

- 中身は「どこで踏んだか / 何が回りくどかったか / 次に効きそうな改善」。うまくいった話は書かない
- **各項目に切り出し先の提案を添える**（新規 issue にする / `_claude/rules/` に落とす / 却下）。
  切り出しの実行はユーザーの判断を待つ（勝手に issue を量産しない）
- **done の条件は「本文の残課題が空になったこと」**（全項目が issue 化・rule 化・却下のいずれかで決着）。
  実装の有無では判定できないため、`issue-sync` の自動 done 判定の対象外（`human` と同じ扱い）
- ルールに落ちた項目は `_claude/rules/` を正本とし、retro 側には要約を残さない（二重管理は乖離を生む）
- 却下した項目は消さずに「却下: 理由」を 1 行残す（同じ気づきが次の retro で再生産されるのを防ぐ）
- **未決着の retro はセッション開始時に自動で出る**: `_claude/hooks/retro-open.sh`（SessionStart
  hook。配線は `_claude/settings.json`）が open な retro を古い順に列挙する。`human` の期限催促
  （`human-tasks-due.sh`）と同じ「読む契機を起動に依存させない」ための仕掛け。
  `pending/` の retro は一覧には出るが「N 件」には数えない（`human` の保留と同じ規律）
- **経過日数はファイル名末尾の `-YYYY-MM-DD`**（無ければ本文の `起票日:`）から取るので、retro は
  日付サフィックスを付けて命名する。読めなかったものは hook が理由つきで列挙する
  （`日付不明` / `読み取り不可` / `起票日が未来`）。黙って落とさないのが契約

## ディレクトリ構成

- `issues/*.md` — open な issue
- `issues/pending/` — 着手を保留している issue の置き場（着手条件・trigger を本文冒頭に書いておく）
- `issues/done/` — 完了した issue の移動先（ファイル名は変えずに移動）
- `audit-log` — audit 実行の記録（TSV）。issue ではない。**issue ファイルをパスで参照しているため、既存ファイルを rename するとここの参照が切れる**
- この `README.md` も issue ではない

## 運用ルール（詳細は `~/.claude/CLAUDE.md`「Issue管理」と `_claude/rules/`）

- 対応が完了したら `issues/done/` へ移動する
- issue の新規作成・大幅改訂は commit 前に codex レビューへ通す（[`issue-creation-codex-review.md`](../_claude/rules/issue-creation-codex-review.md)）
- issue の記述を鵜呑みにしない。着手前に実コードと git 履歴で検証する（既に修正済み・false positive を弾く）

## 既存ファイルの番号付け（2026-07-16 実施済み）

規約導入以前のファイルは 2026-07-16 に一括 rename 済み（作成日順に 001〜017 を採番。audit-log・コード内コメント・docs・issue 間クロスリンクのパスも同時更新済み。commit message 内の旧パスは immutable なため対象外）。
