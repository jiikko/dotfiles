# 460 (research): pro-con のもろい作りの監査 (2026-09-25)

起票日: 2026-09-25

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-25): 「pro-con でもろい作りの箇所を探して issue として書き出して」。audit skill の姿勢 (壊れている前提で証拠を探す) で、
観点を 2 つに分け、読むだけの係 (Opus) を 1 体ずつ走らせ、PM が指摘をコードで裏取りしてから issue にした。

- 観点 1: 状態の書き込みと、並行・中断・再起動 (master 6f9182c9)
- 観点 2: 外部の CLI と環境への前提 (下の「観点 2」に追記する)
- 既知 (439 / 445 / 427 の「記録のみ」「残り」、447) と同じ指摘は除いた

## 観点 1 の結果: 生存 8 (issue 化 3 / 記録 5)

- P1 → [457](../457-bug-pro-con-unregistered-pg-not-stopped-reported-stopped.md): 記録に載らなかった PG を終了・閉じるで止めず「既に止まっていた」と書く (プローブで再現)
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

## 観点 2 の結果 (master 51aac3c8・Claude Code 2.1.282): 生存 11 (issue 化 5 / 431 へ 1 / 記録 5)

実行したのは読むだけのコマンド (`claude --version` / `claude agents --json [--all]` / `ps eww`) と、隔離した cwd での `claude -p --no-session-persistence` 3 回。
PM がコードで確かめたもの: `card run` の `strings.Join` / `runClaude` の素の `claude` / 再開の本文を位置引数で渡す / projects の置き場の決め打ち (3 か所) / `Stopped()` の定義。

- P2 → [462](462-bug-pro-con-resume-text-starting-with-dash-loops-forever.md): 回答が「-」で始まると再開が必ず失敗し、上限なく繰り返して枠を占める (実測)
- P2 → [463](463-bug-pro-con-card-run-joins-argv-and-evals.md): `card run` が argv を空白で繋いで eval し、引用が壊れて別のコマンドになる (再現)
- P2 → [464](464-bug-pro-con-claude-resolved-from-path-per-repo.md): claude を PATH の素の名前で引き、nodenv の shim が repo ごとに別の版を選ぶ (実測)
- P2 → [465](465-bug-pro-con-worktree-name-reused-across-state-dirs.md): `-w` が同名の worktree を再利用し、置き場を作り直すと前の世代の上で作業する (実測)
- P1 (潜在) → [466](466-bug-pro-con-session-stopped-reads-unknown-state-as-stopped.md): `Stopped()` が知らない state を止まったと読む (読んだだけ。457 の後に着手)
- P1 (潜在) → 431 へ: transcript の置き場を `~/.claude/projects` に決め打ち (`main.go` 2 か所・`live.New`)。`CLAUDE_CONFIG_DIR` を渡すと (431 / 433 の次の手) 見張り・落ちた回数・
  カードの表示が黙って止まる。`usage.go` には合わせる注意があるが transcript の側には無い
  - 2026-09-26: 431 は `CLAUDE_CONFIG_DIR` を採らず (433 は解消)、この P1 は今は起きない。使うようになったら再び当たる
- P2 (記録・未確認): 2.1.282 の help は `--bg --resume` を「同じ ID で続ける」と書くが、実測 (記録の 20 本) では再開ごとに新しい短い id。help どおりの版になると
  `register` が pid を書き直さず、次の再開が「pid が記録と違う」で止まる。trigger: claude の版を上げたとき、再開で id が変わるかを 1 回見る
- P3 (記録・実測): `HaikuSummarize` に `--no-session-persistence` が無く、`~/.claude/projects/-Users-koji--local-state-pro-con-live/` に約 200KB の transcript が残る。tools も絞っていない
- P3 (記録・実測): `runs/` (テストの係の出力) は消されない (11 本・800K)
- P3 (記録): テストの偽物と本物のずれ — 本物で pid の無い起動の session の cwd は repo root (6/6) だが偽物の Stop は worktree のまま / 偽物は done の state を作らない /
  偽物の Resume はどんな文でも受ける (462・463 が見えない理由) / 偽物の Start は既存の worktree を再現しない (465)。直すときは各 issue の偽物に足す
- P3 (記録): `launcher.go` 冒頭の「まだ一度も本物の claude で走らせていない」は古い (dogfooding 済み)。次に launcher.go を触る commit で直す

攻めたが指摘が出なかった範囲 (観点 2): `exec.Command` を使う非テストの Go 11 ファイル (うち claude を呼ぶのは agents / launcher / usage / runner / live の attach の 5)。
`/usage` の正規表現は 2.1.282 の実出力と一致 / pid や startedAt の型が変わると Unmarshal のエラーで Tick が止まる (黙らない) / `parseBackgrounded` が読めなくても adopt が拾う /
shutdown は `--all` で確かめている / locale に依存しない (JSON と英語の文面だけ)。読んでいない: TMPDIR と socket のパス長 / 空の `[]` が一瞬返ることがあるか

## 反証レビュー (2026-09-25。sonnet・読むだけ。457・458・459・462〜466 の 8 本)

- 8 本とも反証できなかった (主張どおり)。463 は `eval` の別物になる形を実行で、462 は実機の `claude -p … "-hello"` が rc=1 (`unknown option`) で確かめた。
  **462 の直し方の候補の `--` は効く** (`claude -p … -- "-hello"` が rc=0)。465 の `-w` の再利用は `--bg` を打たない決まりのため再検証していない (issue 側は実測済み)
- 重複なし (457 と 466 は判定の領域が近いが、原因が別: 記録に載らない / state の名前の読み方)

## 未確認リスク (2026-09-25 21:50)

- テストの係の実行 (C-016 の `make test`) が `rc=-1` で終わった (実行の bash そのものがシグナルで死んだ形。`runner.go` の runScript のコメントどおり、頼まれたコマンドが
  シグナルで死んだなら 128+n になる)。ログは `tests/tmux/test_version_gte.sh` の途中で切れていた。時間切れ (runTimeout = 1 時間) ではなく、同じ時刻に dispatcher の側で
  この実行を止める出来事も無い。`killStale` は runID の完全一致でしか撃たないので、並行して PM が回していた pro-con の `go test` とも取り違えない。今夜 1 回だけで、原因は未確認
- 観測の案: テストの係が実行を終えるとき、止めた理由 (時間切れ / dispatcher の取り消し (CancelRun・カードが列を離れた) / 外からのシグナル (ProcessState の Signal)) を出来事に出す。
  次に `rc=-1` が出たら、どれかが分かる

