# 新規 issue を作成したら codex に通すこと

## ルール

- **issue を新規作成 / 大幅改訂したら、コミット前に必ず codex review を通す**
- codex 指摘 (P1/P2/P3) は無視せず、根拠の弱い断定 / カウント / 関連 issue 参照を訂正する
- レビュー後に修正したら、その訂正自体を別 commit として残す (issue の事実誤認の修正履歴が後追いできる形)
- 一連の流れ: **「ぼやき / audit 出力 → issue 化 → codex review → 訂正 commit」** までを 1 セットとする

## なぜこのルールが必要か

ぼやき / audit から起こした issue は **自己レビューだけだと事実誤認が紛れ込みやすい**。

- ぼやき: 軽い気持ちで書くので「断定」「カウント」「関連参照」の精度が落ちる
- audit: 自動生成された出力をそのまま貼ると、現状コードと食い違うことがある (LLM 系の hallucination / 古い前提)
- どちらも「次に触る人が前提として読む文書」になるため、誤った断定が混入すると **誤った前提で改修が走る** 二次被害が起きる

過去の実例 (2026-05-23):

> issue #027 (`ProjectViewModel` のエラー処理重複) を自分で書いて codex に通したら、
> 4 件 (P1 / P2 / P2 / P3) すべて妥当な指摘が返ってきた:
>
> - P1: 「出口は確保された」が `setProjectsEnabled` 経路で本当は未確保 (実は dead code だったが、断定は危険)
> - P2: 「7 箇所」「8 箇所」のカウント混在
> - P2: `removeProjectInternal` を enable/disable と「同型」と書いたが構造が違う
> - P3: 関連 issue `#013` 参照が事実と食い違う
>
> どれも単独で見れば軽微だが、合算すると issue を読んだ次の人が誤った前提で
> 改修を始めるリスクがある。codex 1 回 ~30 秒のコストで全部潰せた。

## 採用パターン

### A. ぼやき → issue 化の場合

```bash
# 1. issue ファイルを書く (まだ commit しない)
$EDITOR issues/NNN-{prefix}-{title}.md

# 2. codex に通す
cat > tmp/codex_review/prompt_issue_NNN.md <<EOF
issue 作成の妥当性レビュー依頼。

## 聞きたいこと
- 事実誤認 (file:line / カウント / 関連 issue 参照)
- 対応方針の見落とし
- 「出口は確保された」等の断定が現コードと一致しているか
- 重要度判断の妥当性

問題のあるものだけ重要度順 (P1/P2/P3) に。

---
EOF
cat issues/NNN-*.md >> tmp/codex_review/prompt_issue_NNN.md
# 必要なら関連コードも追加
cat path/to/related.swift >> tmp/codex_review/prompt_issue_NNN.md

cat tmp/codex_review/prompt_issue_NNN.md | codex exec --skip-git-repo-check --color never -

# 3. 指摘を反映して issue を訂正、その後 commit
$EDITOR issues/NNN-*.md
git commit -m "docs(issue): refine #NNN per codex review"
```

### B. audit 出力 → issue 化の場合 (一括レビュー)

audit / forge skill から複数 issue が一度に作成されるケース。1 件ずつ codex に通すと
コストが高いので、**主要 issue (P1 系 / 影響範囲が広いもの)** を選んで通す。

判断目安:
- カテゴリが `bug` / `perf` の高 priority → 通す
- カテゴリが `refactor` / `ux` の low priority → 自己レビューで OK
- 関連 issue を 2 つ以上参照している → 通す (参照の事実誤認リスク)
- ファイル名 / 行番号を断定している → 通す (位置情報の精度確認)

### C. 既存 issue の大幅改訂

「priority 変更」「対応方針の根本見直し」「関連 issue 追加」をした場合は **改訂後**に
codex に通す。typo 修正 / 進捗チェックボックスの更新は不要。

## 対象外 (codex を通さなくてよいケース)

- issue 番号だけ振った skeleton (本文を後で書く場合)
- typo / リンク切れ修正
- 進捗チェックボックスの ON/OFF
- 完了済み issue の `done/` 移動 (中身は変えていない)
- audit-log 等の機械的ログファイル
- 内容が 5 行以下の超軽量 issue (chore 系)

## コスト感

- codex 1 回 ~30 秒、~50K tokens
- 月数十回程度なら課金影響は微小
- 「issue を真面目に書いた → codex で精度を上げる」のセットを習慣化する方が、誤った
  前提で改修して revert する時間より安い

## 関連

- `~/dotfiles/_claude/rules/pending-issue-rationale-in-code.md` — pending issue の理由を
  コード側に残す運用 (こちらと併用すると、issue / コード両側で誤った前提が残らない)
- 一般的な codex-review tool: `~/src/my-products/tools/codex-review/` (各 app の Makefile
  から symlink 経由で `make review` として叩く)
