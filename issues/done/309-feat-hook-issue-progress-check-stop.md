# 309 feat: 関わった issue の更新漏れを Stop hook で差し戻す (issue-progress-start / issue-progress-check)

起票日: 2026-09-06 (実装後に起票。obaket 730 のセッションで「関連 issue 3 本が未更新のまま完了報告」を踏み、
ユーザーの依頼で hook 化した。この作業自体を issue に起こさず進めたのが、まさにこの hook が止めたい形だった)

## 背景

タスク完了後に「issue を更新したか」とリマインドすると、本当に漏れているケースが多い。書くのは Claude の手順だが、
手順だけでは出口で抜ける。作業前後の差分 (md5 でなく git) と本文の構造で「触っていない / 進捗が無い」を検出し、
Stop hook で差し戻す。

## todolist

- [x] SessionStart で session_id ごとに repo root と開始時 HEAD を記録する (`issue-progress-start.sh`)
- [x] Stop で「関わった issue」(commit subject の `(NNN)` / next/ の claim) を集め、①未変更 ②`[x]` と進捗系見出しが増えていない ③参照する open issue が未変更、を block で差し戻す (`issue-progress-check.sh`)
- [x] 同じ指摘は 1 セッション 1 回 / `stop_hook_active` では黙る (無限ループ防止)
- [x] issue ファイルを触っただけの番号を作業対象に格上げしない (関連 issue に 1 行足した番号への誤報を防ぐ)
- [x] `_claude/settings.json` に配線、`~/.claude/hooks/` の symlink (`scripts/claude_links.sh apply`)
- [x] 手順を `~/.claude/CLAUDE.md`「Issue管理」に 1 項、hook の説明を `issues/README.md` に追記
- [x] テスト `tests/claude/test_issue_progress_check.sh` (10 ケース) と変異検証

## 進捗

- 実装 + テスト + 配線 + docs: commit `feat(hooks): 関わった issue の更新漏れを Stop hook で差し戻す` (2026-09-06)

## 結果

- テスト 10 ケース green。変異 4 本 (未変更の指摘を消す / `stop_hook_active` ガードを消す / 1 回抑制を消す / 構造判定を消す) 全部 red。
  `stop_hook_active` の変異は最初 green で素通りし (対照ケースの session に基準点が無く、ガードの手前で exit していた)、
  対照ケースを足して red にした。
- `make test-lint` green。`tests/claude` の残る FAIL 2 件は既存の dangling symlink (`next-claim-uncommitted.sh` / `coc-settings.json`) で無関係。

## 残タスク

- 未検証: 実セッションでの発火 (settings は次に起動するセッションから読まれる)。最初の数セッションで誤報の量を見る。
  誤報が多ければ「関わった issue」の抽出 (subject の `(NNN)` 限定) を見直す。
- スコープ外 (承知の上で検出しない): 番号を出さずに commit した作業 / 本文の記述が古いだけの漏れ /
  `git pull` で混ざった他セッションの commit の番号 (1 セッション 1 回で黙るので放置)。
- 既存の dangling symlink 2 件は `setup.sh` 再実行で掃除する (別件)。
