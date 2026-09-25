# 464 (bug): claude を PATH の素の名前で引くので、nodenv の shim が repo ごとに別の版の claude を選ぶ

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

もろい作りの監査 (460) の観点 2。PG の起動・再開・一覧・停止が、repo によって違う版の claude で行われうる。

## 詳細

- 該当: `src/pro-con/dispatcher/launcher.go` の `runClaude` (`exec.CommandContext(ctx, "claude", …)`、起動は `cmd.Dir = repoPath`、再開は worktree) / agents・usage・runner も同じく素の名前
- 発火条件 (実測): dispatcher の env の PATH に nodenv の shim が入っていて `NODENV_VERSION` が無い (人が画面から起こした dispatcher はこの形)。
  shim は cwd の `.node-version` で node を選ぶので、`~/src/gx-navi` (node 19.3.0) では claude 2.1.17 (`agents --json` は `unknown option '--json'`)、
  `~/src/ubiregi-server` (22.11.0) では 2.1.273 が選ばれた (監査の係が `NODENV_VERSION` を外した shim の PATH で実行)
- 壊れ方: 古い版では起動・一覧が失敗して 462 のループに入る。少し古い版では、起動・再開はその版、一覧と stop は別の版で行われ、stop の意味 (done / stopped。01dbb3b0 の実測) が合うかは未確認
- 今の dogfooding の dispatcher は PM の Claude から起こしたので node 24.2.0 に固定されていて、この形が見えていない

## 対応方針 (候補)

- dispatcher の起動時に `claude` の絶対パスを 1 回解決して固定し、版をログと出来事 (444) に出す (`path-shim-must-resolve-real-binary.md` の形。解決は PATH を見た 1 回だけ)
- あわせて、dispatcher を起こした側の env が PG まで流れる件 (PG に `CLAUDE_PID` / `CLAUDE_EFFORT` など PM の値が入っていた。`card run` には CLAUDECODE・CLAUDE_CODE_SESSION_ID・
  MESSAGING_SOCKET まで渡る。挙動への影響は未確認。460 の P3) も、PG に渡す env を決めて絞る

## 対応 (2026-09-25 / カード C-024)

- 実体: `dispatcher.ResolveClaude` が dispatcher の起動時に 1 回だけ `claude` を PATH から引き、`<root>/shims` の下 (版を cwd で選ぶ版管理。
  nodenv / rbenv / pyenv / asdf / mise の置き場。symlink は 1 段ずつ辿って各段を見る) なら `<root の名前> which claude` で実体まで辿る。
  起動・再開・停止 (launcher)・一覧 (agents)・枠 (usage)・要約と btw (haiku) はすべてこの絶対パスで呼ぶ。
  解決は **家 ($HOME) の cwd** で行う (画面・人が dispatcher を起こした repo の `.node-version` で版を決めない)。
  shim でない symlink (native の `~/.local/bin/claude` → `versions/<版>`) は版つきの先に固定しない (自動更新が古い版を消しても動き続ける)
- 解決できない (PATH に無い / 版管理が実体を返さない / 返したものがまた shim) なら、dispatcher は rc=1 で起動しない (素の名前に倒さない)。
  `--stop` は動いている dispatcher に頼むだけなら解決しない (claude が壊れていても止める依頼は出せる)。止める役を自分で取ったときだけ解決する
- 版: 起動時に `<実体> --version` の 1 行目を取り、「claude は <パス> (<版>) を使う」を出来事 (444) と dispatcher.log に残す
- PM の回数 (462 の残り): 確かめた結果、PM には「生きていない PM を起こし直す」上限 (`pmReviveLimit`) しか無く、1 度も起動できていない PM (記録も session も無い)
  と終了で止めた PM (`Stopped`) の起動・再開の拒否は launchGrace ごとに上限なく繰り返していた (偽の launcher で 40 Tick に 20 回)。
  462 と同じく `ErrRejected` を `launchRejectLimit` (3) 回続けて受けたら印を外して起こさず、理由を hold の出来事に 1 回書く。
  PM には回答で戻す人の番が無いので、回数は dispatcher のメモリにだけ持ち、**直してから dispatcher を起動し直すと戻る** (claude の実体を引き直すのも起動時)。
  起動・取り込み・拒否でない失敗で 0 に戻す
- テスト: 偽の nodenv (cwd から上へ `.node-version` を探す shim と `nodenv which|exec`) で、2 つの repo が 2.1.17 / 2.1.273 を選ぶ形を作り、
  直す前に red を見た (`TestLauncherUsesOneClaudeAcrossRepos`)。解決の拒否は `TestResolveClaudeRefusesUnresolvableShim` / `TestRealDispatcherRefusesWithoutClaude`、
  組み立ては `TestNewDispatcherPassesUserSettingsToLauncher`。PM は `TestPMRejectedLaunchLimited` (直す前は 20 回) / `TestPMRejectCountResetsOnOtherOutcomes`。
  変異 9 本 (実体を無視して素の名前 / shim を辿らない / 辿った先が shim でも受ける / 組み立てが渡さない / 解決の失敗で止まらない / PM の上限を外す /
  上限で印を残す / 0 に戻さない / 拒否を分類しない) がすべて red (bin/mutate-verify)。印を残す変異は最初は緑で、上限に達した直後に見るよう直した
- 敵対レビューで直したもの: ①解決を起こした側の cwd で行っていた → claude の入っていない node を指す repo から画面を開くと dispatcher が起動せず、
  古い版を指す repo からだと全 repo をその版に固定した (`nodenv which` の rc=127 を実測) → 家の cwd で解決 ②`--stop` が止める依頼の前に解決して、
  claude が壊れていると止める依頼すら出さなかった → 止める役を取ってからに ③mise の shim (本体への symlink) を EvalSymlinks が通り過ぎる →
  1 段ずつ辿る ④版つきのパスに固定していた → 固定しない ⑤`--version` だけに上限があり `which` には無かった → 解決全体に 30 秒。
  ①③④ はテスト (`TestLauncherUsesOneClaudeAcrossRepos` を古い版の repo の cwd から / `TestResolveClaudeSymlinks`) と変異 3 本で red を確かめた。
  ② の順序はテストで固定していない (本物のモードで dispatcher を動かしたまま `--stop` を撃つ仕掛けが無い)
- `make lint` 0 件 / `make test` rc=0 (063daab8。origin/master 486dcbe3 の上に rebase 済み)
- 分かっていて受けるもの: 解決は dispatcher の cwd で行うので、選ばれる版は dispatcher を起こした場所の `.node-version` (無ければ版管理の既定) で決まる
  (repo ごとには変わらない) / npm の `cli.js` のように `#!/usr/bin/env node` で始まる実体なら node は PATH の shim で repo ごとに選ばれうる
  (今の手元は native の `claude.exe`) / 版管理の判定は「shim の置き場が `shims`」という慣習による (volta の `~/.volta/bin` は当たらない。未実測) /
  PM の拒否の上限で止まった後は、PM を手で生き返らせても dispatcher を起動し直すまで知らせない。画面が起こした dispatcher を起動し直すには画面の quit
  (PG も止まる) が要る
- 範囲外で残るもの: 画面 (live) の一覧と `claude attach` は素の名前のまま (画面の cwd は 1 つなので repo ごとには変わらないが、dispatcher の実体とは版がずれうる)。
  PG に渡す env を絞る件 (CLAUDECODE・CLAUDE_CODE_SESSION_ID・MESSAGING_SOCKET 等。460 の P3) は、`CLAUDE_CODE_USE_BEDROCK` のように渡すべき値と
  混ざるので、何を落とすかを決めてからにする (挙動への影響も未確認)

## 関連

- 460 (監査の記録) / 462 (失敗が上限なく繰り返す) / 461 (PG に渡す設定)
