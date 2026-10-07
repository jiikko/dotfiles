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

- [x] 偽物を detached の孫にして red を確認 (`TestMermaidCancelKillsGrandchild` が「孫が 10 秒たっても残る」で落ちた)
- [x] 止め方の修正 — fix(zundamon-kaisetsu) の commit

## 結果 (2026-10-07)

- issue 640 の runtimeout の止め方 (グループ ∪ ppid の木を凍らせて集め、TERM → 猶予 → KILL) を共有 module `src/proctree` へ切り出し、
  runtimeout と zundamon-kaisetsu の両方がそれを使う。`renderMermaid` の `cmd.Cancel` は `proctree.Target{Root: npx, Group: true}.Stop(3s)`
- テスト: 偽の npx の孫を setsid で別のセッションに置く (本物の puppeteer の detached と同じ)。`TestMermaidTimeoutMessage` も時間切れの後に孫が残らないことを見る
- 変異: Cancel を元のグループへの SIGKILL に戻すと、中断と時間切れの 2 本が red。proctree の ppid の木を外すと proctree と runtimeout のテストが red
- 敵対的レビュー (opus 1 周): P1 / P2 なし。採用した P3: Cancel と回収が並行しうることをコメントで正しく書く / 時間切れのテストは孫の起動を待ってから判定 /
  runtimeout の `TestDefaultPutsChildInOwnGroup` が `sleep 300` の孤児を残していた (グループごと撃つ)。記録のみ: 回収後も最長 3 秒同じ番号へ撃つ窓
  (runtimeout と同じ。pid の再利用が要る) / 中断で最長 3 秒待つ / CONT の後に Chrome が新しく起こす子は集めていない (実機は未確認)
- 実機 (本物の npx + Chrome) で中断したときに Chrome が残らないかは未確認 (偽物での確認)
