---
name: review-loop
version: 1.0.0
description: make review で Codex レビュー PR を作成し、指摘がなくなるまで修正→再レビューを繰り返し、最後に make review-close する。
---

# Review Loop

`make review` → Codex レビュー → 修正 → 再レビュー → `make review-close` の自動ループ。

## 前提条件

- プロジェクトに `make review` / `make review-close` が定義されていること
- GitHub に Codex（chatgpt-codex-connector）が設定されていること
- origin/master に push していない差分コミットがあること

## 手順

### 1. レビュー PR を作成

```
make review
```

PR URL を控える。

### 2. Codex のレビューを待つ

10分間隔の定期ジョブ（CronCreate）で PR のコメントを監視する。

**確認方法:**
```bash
# レビューコメント（コード上の指摘）
gh api repos/{owner}/{repo}/pulls/{number}/comments --jq '.[] | select(.created_at > "{前回確認時刻}") | {id, path, line, body}'

# issue コメント（全体レビュー結果）
gh api repos/{owner}/{repo}/issues/{number}/comments --jq '.[] | select(.user.login != "{owner}") | {id, body}'
```

### 3. 指摘への対応（コメントがある場合）

各コメントについて以下を判断:

- **P1/P2（要修正）**: コードを修正 → テスト実行 → コミット → レビューブランチに push → PR に返信
- **P3（認識のみ）**: issue として起票するか、PR に「認識済み」と返信
- **スコープ外**: 「別 issue で対応」と返信

**修正後の push:**
```bash
git push origin master:{review-branch-name}
```

**返信:**
```bash
gh api repos/{owner}/{repo}/pulls/{number}/comments -X POST -f body="..." -F in_reply_to={comment_id}
```

### 4. 再レビュー依頼

```bash
gh pr comment {number} --body "@codex review for 前回指摘への修正が正しく反映されているか確認してください。"
```

### 5. ループ判定

- 新しいコメントがある → 手順3に戻る
- 「Didn't find any major issues」 or コメントなし → 手順6へ

### 6. クローズ

```bash
git push origin master
make review-close
```

レビューブランチが自動マージされてクローズ失敗する場合は、手動でブランチを掃除:
```bash
git remote prune origin
git branch --list 'review/*' | grep -v 'review/base' | xargs -r git branch -D
```

定期ジョブがあればキャンセルする（CronDelete）。

## ルール

- **レビュー中は master に push しない。** 修正コミットはレビューブランチにのみ push する（`git push origin HEAD:review/{branch-name}`）。master に push するとPRの差分が消え、未レビューの変更が本番に入ってしまう。master への push はレビュー完了後（手順6）のみ。
- 修正コミットには対応する issue 番号を含める
- `make lint` と `npm test`（または該当プロジェクトのテストコマンド）が通ることを確認してから push
- Codex の P1/P2 指摘は必ず修正する
- P3 以下で issue 化する場合は issues/ ディレクトリにファイルを作成する
- セルフレビューコメントも積極的に投稿する（Codex との相互レビュー）
