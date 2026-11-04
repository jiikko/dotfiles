# 1022 (bug): tfperf (make debug / perf) が「送った打鍵 1 件と key の Update 3 件が合わない」で判定不能になる

起票日: 2026-11-04

## 概要

`make -C src/treefiler debug KEYS='?'` が、画面は撮れているのに `tfperf: 送った打鍵 1 件と、ログの key の Update 3 件が合わない (判定不能)` で rc=2 を返した
(1019 の監査の後、キー一覧の 2 段組 (aa82a0f44) を実物で確かめたときに見つけた)。台本の最後でキー一覧・タイルなどの層が開いたままだと、毎回こうなる。

## 詳細

- 3 件の中身 (`tmp/tfdebug-20261104-213510/perf.log` の key の update): `?` (台本) / `q` / `q`
- `tools/tfperf/main.go` の `tmuxServer.quit` は、層が開いていると q 1 本ではそれを閉じるだけなので、session が終わるまで q を最大 8 本送る
- 一方 `analyze` は「最後の q (閉じる) は送った記録に無い」として、末尾の q を **1 本だけ** 外していた。閉じる q が 2 本以上だと数が合わず判定不能になる
- 同じ仕組みの別の穴: 本数で外す形だと、台本の最後の `q` を取りこぼしたときに、閉じる q がその穴を埋めて数が合い、閉じる q の数字を台本の q の数字として出す。
  また最後の打鍵の集計の窓は「記録の終わり」までで、閉じる操作の描画・書き込みまで最後の打鍵に数えていた

## 対応

- `quit` が送った q の時刻を `quits.tsv` に書く (`sends.tsv` と同じ形)
- `analyze` は、閉じるための最初の q を送り始めた時刻 (`quitAt`) より後に始まった key の Update を照合から外す (本数ではなく時刻で分ける)。
  最後の打鍵の窓も `quitAt` で切る
- `report` は `quits.tsv` が無いと誤りを返す (この変更より前の run の dir を `tfperf report` で集計し直すと落ちる。その run の summary.txt は作ったときに出ている)

## 進捗

- [x] 2026-11-04: 実装・テスト (`TestAnalyzeRejectsMismatchedKeys` に閉じる q 2 本の形・`TestAnalyzeDoesNotPairQuitKeyWithLostScriptKey`・`TestAnalyzeLastWindowEndsAtQuit`)
- 変異で red を確認 (mutate-verify): 時刻で外すのをやめる → TestAnalyzeRejectsMismatchedKeys / 最後の窓を記録の終わりへ戻す → TestAnalyzeLastWindowEndsAtQuit /
  旧来の「末尾の q を 1 本だけ外す」に戻す → TestAnalyzeDoesNotPairQuitKeyWithLostScriptKey
- 実物: `make debug KEYS='?'` (閉じる q 2 本) が rc=0 で集計まで通る (`tmp/tfdebug-20261104-215929/`。修正前は同じ形で判定不能)。`KEYS='? j'` (閉じる q 1 本) も rc=0
