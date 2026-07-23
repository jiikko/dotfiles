# 020 feat(glogx): 失敗コンテキストの一括コピー (`Y`)

## 背景

`y` は URL のみのコピー。しかし job 詳細ポップアップは既に「step 一覧 + annotations (無ければログ末尾)」という LLM に渡す最良の素材 (README 自身が明記) を保持している。これを Markdown 整形してクリップボードへ入れる `Y` を足すと、「✗ を見る → Claude Code に貼って修正依頼」が 2 操作になる。glogx の Claude Code 連携 (`U`/`C`) の方向性に合う (2026-07-23 ユーザー承認)。

## やること

- job 詳細ポップアップ表示中 (`handleDetailKey`) に `Y` を割り当てる。コピー内容 (Markdown):
  - ヘッダ: repo / commit (SHA + subject) / job 名 / job URL
  - 本文: `detailOv` がキャッシュ済みの表示行 (step 一覧 + annotations or ログ末尾)。ANSI は `stripANSI` で除去 (`jobLogText` と同じ処理。共用する)
- job パネル (`handlePanelKey`) の `Y` は、フォーカス中 job の詳細が未取得なら取得してからコピー (openJobDetail の取得経路を再利用し、jobDetailMsg 到着時に「コピー待ち」フラグで続きを実行)。実装が膨らむようなら第 1 段は詳細ポップアップ内限定でよい (詳細を見てからコピーする動線が自然)
- コピーは既存の `copyToClipboard` (tmux load-buffer + pbcopy/xclip) を再利用
- 完了 notice: 「失敗コンテキストをコピーしました (N 行)」

## 出力例

```markdown
## CI failure: lint (owner/repo@6a59f1c "feat(glogx): ...")
https://github.com/owner/repo/actions/runs/.../job/...

### steps
✓ Set up job (2s)
✗ Run golangci-lint (13s)

### annotations
[failure] src/glogx/tui.go:42
  ineffectual assignment to err
```

## 関連

- [019](019-feat-glogx-rerun-failed-ci.md) — 同じ「✗ 対応を TUI 内で閉じる」系
- hint 行への `Y` 追記を忘れない (detailOv 表示中の hint)
