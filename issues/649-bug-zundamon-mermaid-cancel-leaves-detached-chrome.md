# 649 (bug): zundamon-kaisetsu の mermaid の描画を中断・時間切れで止めると、puppeteer が起こした Chrome が残る

> 🚨 **担当中: dotfiles-4d**（2026-10-07〜）

起票日: 2026-10-07

## 概要

`src/zundamon-kaisetsu/show_mermaid.go:renderMermaid` は npx を専用のプロセスグループで起こし、中断・時間切れ (`mermaidTimeout`、既定 180 秒) では
グループへ SIGKILL を送る (`cmd.Cancel`)。mermaid-cli の中の puppeteer は Chrome を **`detached` (別のプロセスグループ) で起こす**ので、
グループ宛ての SIGKILL は Chrome に届かない。SIGKILL なので node 側の後片付け (puppeteer の handleSIGINT / handleSIGTERM) も走らない。

## 詳細

- 根拠: 手元の npx キャッシュの `@puppeteer/browsers/lib/launch.js` に `opts.detached ??= process.platform !== 'win32'` と
  `handleSIGINT ??= true` / `handleSIGTERM ??= true` (2026-10-07 に確かめた)
- 再現 (監査のサブエージェントが scratch の写しで実施): 偽の npx が孫を `POSIX::setsid` で切り離して起こす形にし、`mermaidTimeout=2s` で走らせると、
  時間切れの後も孫が生きていた
- 既存の `TestMermaidCancelKillsGrandchild` は孫を同じグループに置く偽物なので、この経路を見ていない (偽物が本物の起こし方を模していない)
- 発火条件: mermaid の図を描いている途中の Ctrl-C、または 180 秒の時間切れ

## 対応方針

- 止め方を「グループへ SIGTERM (puppeteer に Chrome を閉じさせる) → WaitDelay の間に終わらなければ SIGKILL」にする。
  それでも親を離れた Chrome を確実に止めるには、ppid の木で子孫を集める (issue 640 の runtimeout の stop と同じ考え方) ことも検討する
- テストの偽物を、setsid で切り離した孫を作る形にして、直す前に red になることを確かめる

## 関連ファイル

- `src/zundamon-kaisetsu/show_mermaid.go` (`renderMermaid`) / `show_mermaid_test.go` (`TestMermaidCancelKillsGrandchild`)
- 監査の記録: issue 656

## 進捗

- [ ] 偽物を detached の孫にして red を確認
- [ ] 止め方の修正
