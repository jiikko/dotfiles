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

- `quit` が届けた q (send-keys が成功したもの) を `quits.tsv` に書く (`sends.tsv` と同じ形)
- `analyze` は key の Update が「送った打鍵 + 届けた q の本数」あることを求め、後ろの q の本数ぶんは全部 `q` でなければ判定不能にする。
  台本の最後の q を取りこぼすと数が 1 つ足りなくなるので、閉じる q と取り違えない
- 最後の打鍵の窓は、閉じる最初の q の Update が始まるまでで切る (Update は順に処理されるので、最後の打鍵の View と書き込みはその前に終わる)
- `sends.tsv` / `quits.tsv` の書き込みの誤りを捨てない。空の `quits.tsv` (q を 1 本も届けずに session が終わった) は 0 本として読む
- `report` は `quits.tsv` が無いと誤りを返す (この変更より前の run の dir を `tfperf report` で集計し直すと落ちる。その run の summary.txt は作ったときに出ている)

## 敵対的レビュー (opus 1 本、2026-11-04)

最初の実装 (閉じる最初の q を送った時刻より後に始まった key の Update を外す) に当てた。採った:
- P2: 遅い最後の打鍵 (前の Update で詰まり、q を送った後に Update が始まる。perf で探したいもの) を外して判定不能にする → 時刻ではなく本数で分ける形に変えた (`TestAnalyzeKeepsSlowLastKey`)
- P2: `report` が quits.tsv を読む配線にテストが無い → `TestReportUsesQuitsFile`
- P3: 最後の打鍵の View / 書き込みが q の送信の後に来ると黙って数字が落ちる → 窓の終わりを q の Update の始まりにした (本数で分けたので時計に頼らない)
- P3: 空の quits.tsv が「読めない行」になる / 書き込みの誤りを捨てる → 直した
- P3 (記録だけ): session が生きているのに send-keys が失敗すると run は summary の前で止まる。届けた q までは quits.tsv にあるので、手で `tfperf report` できる

## 進捗

- [x] 2026-11-04: 実装・テスト。変異で red を確認 (mutate-verify): 届けた q の本数を見ず末尾 1 本だけ外す → TestAnalyzeRejectsMismatchedKeys /
  後ろが q かを見ない → TestAnalyzeRejectsMismatchedKeys / 最後の窓を記録の終わりへ戻す → TestAnalyzeLastWindowEndsAtQuit /
  report が quits の本数を渡さない → TestReportUsesQuitsFile / 空の quits.tsv を 0 本として読まない → TestReportUsesQuitsFile
- 実物: `make debug KEYS='?'` (閉じる q 2 本) と `KEYS='? j'` (1 本) がどちらも rc=0 で集計まで通る (修正前は前者が判定不能)
