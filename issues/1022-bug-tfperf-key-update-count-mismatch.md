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
- 最後の打鍵の窓: Update と View は閉じる最初の q の Update が始まるまで (Update と View は順に処理される)。書き込みは描画の goroutine が
  自分の周期で行うので q の Update の後に来うる。q の後の最初の View が終わる前に書き始めたものは最後の打鍵のフレームとして数え、q のフレームの書き込みは数えない。
  遅い最後の打鍵は q が待っていると自分のフレームを単独では書かれず q のフレーム (か閉じるときの書き出し) にまとめて書かれるので、自分の書き込みが 1 つも無いときは
  q の後の最初の書き込みを 1 回だけ draw に数える (量には q の差分も入る)
- 台本の最後が q のときは、閉じる q を送る前に session が終わるのを待つ (1.5 秒まで。閉じかけの session へ送った q は成功するが読まれず、
  届けた本数に数えて判定不能になる)。待ちが切れたら送る (判定不能の側に倒れる)
- `sends.tsv` / `quits.tsv` の書き込みの誤りを捨てない。空の `quits.tsv` (q を 1 本も届けずに session が終わった) は 0 本として読む
- `report` は `quits.tsv` が無いと誤りを返す (この変更より前の run の dir を `tfperf report` で集計し直すと落ちる。その run の summary.txt は作ったときに出ている)

## 敵対的レビュー (opus 1 本、2026-11-04)

最初の実装 (閉じる最初の q を送った時刻より後に始まった key の Update を外す) に当てた。採った:
- P2: 遅い最後の打鍵 (前の Update で詰まり、q を送った後に Update が始まる。perf で探したいもの) を外して判定不能にする → 時刻ではなく本数で分ける形に変えた (`TestAnalyzeKeepsSlowLastKey`)
- P2: `report` が quits.tsv を読む配線にテストが無い → `TestReportUsesQuitsFile`
- P3: 最後の打鍵の View / 書き込みが q の送信の後に来ると黙って数字が落ちる → 窓の終わりを q の Update の始まりにした (本数で分けたので時計に頼らない)
- P3: 空の quits.tsv が「読めない行」になる / 書き込みの誤りを捨てる → 直した
- P3 (記録だけ): session が生きているのに send-keys が失敗すると run は summary の前で止まる。届けた q までは quits.tsv にあるので、手で `tfperf report` できる

## 敵対的レビュー 2 周目 (opus 1 本、2026-11-04。本数で分けた後の差分)

すり抜ける誤った組は見つからなかった (数の食い違いは全部判定不能に倒れる)。採った:
- P2: 台本の最後の q が閉じた後、閉じかけの session へ quit が q を送ると、届けた本数に数えて判定不能 (前の実装では通っていた) → 上の「台本の最後が q」の待ち
- P2: 「最後の打鍵の書き込みは q の Update の前に終わる」は誤り (書き込みは描画の goroutine の周期)。遅い最後の打鍵が draw=-1 になり「最も遅く描かれた打鍵」から外れる
  → 書き込みだけ q の後の最初の View まで拾う (`TestAnalyzeLastKeyWriteAfterQuitUpdate`)
採らなかった:
- P2 の案「後ろの q の本数は 1〜届けた本数でよい」: 台本の最後の q を取りこぼし、閉じる q が 2 本とも読まれたときに、閉じる q を台本の q と組にして黙って通る。
  残る形: 最後の打鍵 (または閉じる操作) が約 1.75 秒 (打鍵の間隔 + quit の待ち 1.5 秒) より遅いと、2 本目の q が送られ、読まれるかの競合で判定不能になることがある。
  安全側 (判定不能) で、前の実装でも同じ形は数が合わなかった。再提起するなら、2 本目の q が読まれずに判定不能になった run を添える
- P3: `TestAnalyzeKeepsSlowLastKey` は今の形 (本数で分ける) を守るだけで、時刻で外す形は今の signature では書けない (記録だけ)

## 敵対的レビュー 3 周目 (opus 1 本。2 周目の直しの差分)

採った:
- P2: bubbletea v2 の描画の goroutine は周期ごとに今の View を書くので、遅い最後の打鍵のフレームは多くの場合 q のフレームにまとめて書かれ、2 周目の直しでは拾えない
  (それより前の実装では、記録の終わりまでの窓で q の書き込みごと拾っていた) → 自分の書き込みが無ければ q の後の最初の書き込みを 1 回だけ数える (`TestAnalyzeLastKeyMergedIntoQuitFrame`)
- P2: `gone` は has-session の失敗をすべて「終わった」と読むので、2 周目に足した「各 q の前に session を見る」は tmux の誤りで q を 1 本も送らずに戻りうる
  → その確認は外した (台本の最後が q のときの待ちは、待つだけで送るのを止めない)
- P3: q の後に View が無いと、それより後の書き込みを全部数えた → 上と同じ「1 回だけ」で閉じた
記録だけ: `run` / `quit` の変更 (台本の最後が q のときの待ち) は tmux が要るので単体のテストが無い。実物の `make debug` だけが証拠

## 進捗

- [x] 2026-11-04: 実装・テスト。変異で red を確認 (mutate-verify): 届けた q の本数を見ず末尾 1 本だけ外す → TestAnalyzeRejectsMismatchedKeys /
  後ろが q かを見ない → TestAnalyzeRejectsMismatchedKeys / 最後の窓を記録の終わりへ戻す → TestAnalyzeLastWindowEndsAtQuit /
  report が quits の本数を渡さない → TestReportUsesQuitsFile / 空の quits.tsv を 0 本として読まない → TestReportUsesQuitsFile
- 2 周目の直し: 最後の打鍵の書き込みを拾わない / q の Update まで最後の打鍵に数える → どちらも TestAnalyzeLastKeyWriteAfterQuitUpdate が red (mutate-verify)
- 3 周目の直し: まとめて書かれた最後の打鍵の draw を取らない → TestAnalyzeLastKeyMergedIntoQuitFrame が red。「まとめて書かれた 1 回で打ち切る」の変異は緑
  (draw が付いた後の書き込みは同じ条件で外れるので、打ち切りは余分だった。消した)
- 実物: `make debug KEYS='?'` (閉じる q 2 本)・`KEYS='? j'` (1 本)・`KEYS='j j q' ARGS=-interval 40ms` (台本の q で閉じる。閉じる q 0 本) がどれも rc=0 で集計まで通る
  (修正前は 1 つ目が判定不能)
