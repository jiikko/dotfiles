# 新規 issue を作成したら codex に通すこと

## ルール

- **issue を新規作成 / 大幅改訂したら、コミット前に必ず codex review を通す**
- codex 指摘 (P1/P2/P3) は無視せず、根拠の弱い断定 / カウント / 関連 issue 参照を訂正する
- レビュー後の訂正は別 commit として残す (事実誤認の修正履歴を後追いできる形)
- 流れ: **ぼやき / audit 出力 → issue 化 → codex review → 訂正 commit** を 1 セットとする

## なぜ

ぼやき / audit から起こした issue は自己レビューだけだと「断定」「カウント」「関連参照」の事実誤認が紛れ込みやすく、**次に読む人が誤った前提で改修を始める**二次被害を生む。実例 (2026-05-23): 自作 issue #027 を codex に通したら P1〜P3 の 4 件すべて妥当な指摘 (未確保の経路を「確保された」と断定 / カウント混在 / 構造の異なるものを「同型」と記述 / 関連 issue 参照の食い違い) が返ってきた。codex 1 回 ~30 秒・~50K tokens で全部潰せた。誤った前提で改修して revert する時間より圧倒的に安い。

## 採用パターン

### A. ぼやき → issue 化

```bash
# 1. issue を書く (まだ commit しない)
# 2. codex に通す
cat > tmp/codex_review/prompt_issue_NNN.md <<EOF
issue 作成の妥当性レビュー依頼。

## 聞きたいこと
- 事実誤認 (file:line / カウント / 関連 issue 参照)
- 対応方針の見落とし / 断定が現コードと一致しているか / 重要度判断の妥当性

問題のあるものだけ重要度順 (P1/P2/P3) に。
---
EOF
cat issues/NNN-*.md >> tmp/codex_review/prompt_issue_NNN.md   # 必要なら関連コードも
cat tmp/codex_review/prompt_issue_NNN.md | codex exec --skip-git-repo-check --color never -

# 3. 指摘を反映して issue を訂正してから commit
```

### B. audit 出力 → issue 化 (一括作成時)

1 件ずつ通すとコストが高いので、以下に該当する**主要 issue だけ**通す:

- `bug` / `perf` カテゴリの高 priority
- 関連 issue を 2 つ以上参照している (参照の事実誤認リスク)
- ファイル名 / 行番号を断定している

`refactor` / `ux` の low priority は自己レビューで OK。

### C. 既存 issue の大幅改訂

priority 変更 / 対応方針の根本見直し / 関連 issue 追加をしたら改訂後に通す。

## 対象外 (codex 不要)

- skeleton (本文は後で) / typo・リンク切れ修正 / 進捗チェックボックスの更新
- 完了済み issue の `done/` 移動 / 機械的ログファイル / 5 行以下の chore 系 issue

## 関連

- [`pending-issue-rationale-in-code.md`](pending-issue-rationale-in-code.md) — 併用すると issue / コード両側で誤った前提が残らない
- codex-review はスキル経由で起動する (`~/.claude/skills/codex-review/SKILL.md`)。`make review` がある app では review-loop skill (`~/.claude/skills/review-loop/SKILL.md`) 経由で叩く
