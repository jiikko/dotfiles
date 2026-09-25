# 460 (research): pro-con のもろい作りの監査 (2026-09-25)

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-25): 「pro-con でもろい作りの箇所を探して issue として書き出して」。audit skill の姿勢 (壊れている前提で証拠を探す) で、
観点を 2 つに分け、読むだけの係 (Opus) を 1 体ずつ走らせ、PM が指摘をコードで裏取りしてから issue にした。

- 観点 1: 状態の書き込みと、並行・中断・再起動 (master 6f9182c9)
- 観点 2: 外部の CLI と環境への前提 (下の「観点 2」に追記する)
- 既知 (439 / 445 / 427 の「記録のみ」「残り」、447) と同じ指摘は除いた

## 観点 1 の結果: 生存 8 (issue 化 3 / 記録 5)

- P1 → [457](457-bug-pro-con-unregistered-pg-not-stopped-reported-stopped.md): 記録に載らなかった PG を終了・閉じるで止めず「既に止まっていた」と書く (プローブで再現)
- P2 → [458](458-bug-pro-con-running-card-whose-pg-vanished-holds-slot.md): 一覧から消えた PG の作業中のカードが枠を占め続ける (プローブで再現)
- P2 → [459](459-design-pro-con-manual-stop-undone-by-open-screen.md): 画面が開いていると手の `--stop` を keeper が取り消す (コードを読んだだけ)
- P2 (記録): **記録の JSON が壊れると、終了で PG を 1 本も止めない / dispatcher が落ち続ける**。`shutdown.go` の `stopCards` / `ensureStopped` は
  記録を読めないと即エラー、`store.writeAtomic` と `live.writeRows` は rename だが fsync が無い。壊れうるのは電源断か手での編集 (壊れ方そのものは未確認。
  壊した記録を注入したプローブで stops=[] と Tick のエラーは確認)。直す方向: 読めないときもカードの Session と worktree の cwd で PG を止める / 世代を 1 つ残すか fsync。
  発火が稀なので記録に留める。trigger: 記録が壊れた実例が 1 度でも出たら issue にする
- P3 (記録): dispatcher が作業中から外したカードに、PG が後から置いた review / ask / run は除けられる (`store.transition`。Shutdown の Apply から実際に止めるまでの間・
  起動の印が残っている間。再現。PG がやり直せば回復する)
- P3 (記録): 動き続けている古い dispatcher と新しい `pro-con card` の版がずれると、新しい依頼の種類は「未知の依頼」で除けられ、新しい欄は黙って捨てられる
  (2026-09-25 の dogfooding では dispatcher を入れ替えて回避した。427 の「旧版の RunID」と同じ形)
- P3 (記録): lock の有無を確かめるのに lock を取るので (`startDispatcherIfIdle` / `RequestStop` → `dispatcher.Lock`)、dispatcher.lock の pid が画面や --stop の pid に
  書き変わり、その瞬間に手で起動した dispatcher は「既に動いている」で抜ける
- P3 (記録): 待ちの判定を JSON に書いた壁時計どうしで比べるので、Mac のスリープから戻った最初の Tick で停滞の印・restartWait・launchGrace・closeStopWait が一斉に過ぎる。
  sessions-retired.json と inbox/rejected/ は消されずに増え続ける

攻めたが指摘が出なかった範囲 (観点 1): 受付の箱の Submit の rename と二重適用の防止 (Applied / Rejected の控え・perApply / keepApplied) / events.jsonl の回し /
socket の奪い合い (Listen は lock の後、Close は unlock の前) / presence の数え方 / テストの係の書き手 (goroutine は記録に書かず Tick だけ) /
stop-request と stop-result の往復。「書き手は dispatcher だけ」は cards.json・sessions*.json・events.jsonl・dispatcher-state.json で守られていた
(fake を除く非テストの Go 49 ファイルの書き込みの呼び出しを grep、ヒット 18 ファイル。dispatcher 以外が書くのは箱への Submit・screens/・relay/・resume-*.json (445 に既知)・dispatcher.lock の pid (上の P3))

## 観点 2 の結果

(続きに書く)
