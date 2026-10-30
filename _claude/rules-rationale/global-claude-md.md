# `_claude/CLAUDE.md` (global の共通ルール) の根拠・出典・実例

起動時には読まれない。`_claude/CLAUDE.md` の項を疑う・改訂するときに読む。常時読み込みの字数を減らすため、本文の括弧書きからここへ移した (2026-10-30)。
見出しは `_claude/CLAUDE.md` の節に揃える。

## 作業開始前の準備

- Bash で編集する前に CLAUDE.md を Read する: obaket retro 1109
- cwd の外の repo の規約を Read する: retro 709 (`~` で起動したセッションが、dotfiles の worktree の規約を知らないまま共有ツリーへ書いた)

## Git 禁止操作

- commit / push の後に実際の git state を見る: push の失敗や heredoc の破損を成功と誤報した実例がある

## 応答・成果物の長さとスコープ

- モデルの傾向: Opus 5.5 は Opus 5 より少ないトークンで終え、すぐ作業に取りかかる側、Fable 5.1 は報告が少なくなる側に寄る。
  出典: [Opus 5.5 ガイド](https://platform.claude.com/docs/ja/build-with-claude/prompt-engineering/prompting-claude-opus-5-5)
  (Opus 5 ガイドのパターンも引き続き有効とされる) / `claude-api` skill の Fable 5.1 節。乖離に気づいたら同じ commit で直す
- 「次はこれをやります」で終えない: 実測 2026-09-09 に 1 セッションで 3 回やり、3 回とも「スタックしていませんか？」と聞かれた
- 確認の選択肢に失われるものを書く: obaket retro 1112 (承認の後で「オフラインのときキャッシュの一覧を見せる」挙動が消えると気づいて諮り直した)
- 消す対象を種類で分ける: retro 778
- 体験する場面を添える: vlc-multi-video-player retro 426 (「後ろが待たされる」と書いて承認された 1 件ずつの通知が、使うと固まったように見えて作り直した)

## 一時ファイルの配置

- macOS の一時領域に溜めない理由: 起動が 20 分止まり、筆者が Claude Code の作業ファイル約 99 万件の削除を原因と推定した事例
  (zenn.dev/singularity/articles/vibe-coding-mac-boot-20min)。再起動で消えない置き場は、空振りか遅れて完了かの証拠も残す (codex-drive の obaket 581)
- `skill-tmpdir` と、直下に散らさない: dotfiles issue 774 (実測 2026-10-15: `<番号>-push3.log` の形のログが直下に 1170 件・136 MiB、参照 0 件)
- `..` で repo の外へ出さない: 実測 2026-09-26 に push のログ 22 個が `~/src` と `my-products/apps` の直下に残った (my-products issue 033)
- `trap` は中断で走らない: `scripts/tmux_reap_orphan_servers.sh` の背景注記 (里子化した tmux サーバで自動復元が 17 日間不発)

## Issue管理

- `issue-rules.d/` へ移したのは常時読み込みの字数を減らすため (issue 414)

## ぼやきポイント推奨

- 不在の主張の裏を取る: 実例 2026-09-02 に「検査対象外」とぼやいた検査が `src/glogx/internal/term/box_test.go` に在った (dotfiles の Go の検査は `tests/` ではなく `src/<proj>/*_test.go` にある)

## 不具合対応の原則

- grep を失敗の仕組みで書く: 実測 2026-09-26 obaket retro 961 (`printf '%s\n'` の形で探して `printf '%s'` 41 箇所と `strings | grep -q` を見落とした)

## レビュー方針

- N を機械で数える: 実測 2026-09-06 に 4 と書いて実際は 3
- 等価を compiler / テストで確かめる: 実測 2026-09-21 に exhaustive switch 化で 2 case を落とし、ビルドを壊したまま「完全に同一」と書いた
- 「A より B が悪い」は現状と比べる: obaket 895

## シンボル検索の使い分け (grep と dupscan)

- 根拠の実測は dotfiles の issue 942 / 943。広い名前では rg の 100 倍以上小さく返る例がある (`Join`: 約 44 万 B 対 2.9 KB)
