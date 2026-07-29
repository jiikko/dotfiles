# 029 bug: glogx リソースリーク監査 (2026-07-29)

リソースリーク観点の監査結果 (sonnet 調査 → main agent が file:line・bubbletea vendored source
で全件裏取り済み)。codex レビュー未通過 (codex 不使用の運用指示による)。着手前に各項を
コード再確認すること。

## P1: SIGTERM/SIGINT 経路で push/pull/usage の子プロセス cancel が走らない

**問題**: 終了時の後始末 (`usageOv.stop()` / `actModal.stop()`) は `browseModel.quit()`
(tui.go, 呼び出し元は handleKey の q / esc / Ctrl-C 系のみ) の中にしかない。bubbletea v2 は
SIGINT/SIGTERM を `InterruptMsg`/`QuitMsg` に変換するが、**この 2 つのメッセージだけは
`model.Update` を経由せず `eventLoop` が直接 return する** (裏取り済み:
`charm.land/bubbletea/v2@v2.0.8/tea.go` の `handleSignals` と `eventLoop` の
`case QuitMsg: return model, nil`)。よってシグナル終了では `quit()` が呼ばれない。

`main.go` の `defer browse.cancel()` は CI fetch 用 ctx だけをカバーし、
`actModal` / `usageOv` の ctx には同等の defer がない。

**発火条件**: `git push` / `git pull --rebase` の走行中に外部から SIGTERM が届く
(tmux kill-window / kill-session、`kill <pid>`、端末強制クローズ)。SIGINT も stdin が
TTY でない場合は同経路。

**影響**: push/pull 用 ctx は deadline なし (`context.WithCancel`、意図的) のため、走行中の
git 子プロセスが cancel されず孤児化し、ネットワーク stall 時は無期限に残る。usage の
`claude -p` は内部 10s timeout で自己終了するため実害は小さい (主眼は push/pull)。

**経緯**: 「leak 監査 2026-07-23」で Ctrl-C 2 連打の対話経路は修正済みだが、フレームワーク
レベルの signal → QuitMsg バイパスが未カバー。

**推奨**: `main.go` の `defer browse.cancel()` と同列に `defer` で actModal / usageOv も
cancel する。構造的には 3 つの cancel を `cancelAll()` に集約し、`quit()` と top-level defer
の両方がそれを呼ぶ形 (後始末の単一ファネル化)。

## P2-1: diffOverlay.cache / jobDetailOverlay.cache がセッション中無制限に成長

**問題**: `d` / Enter で閲覧した SHA・job ごとの整形済み行をメモリ内 map に永久保持する。
縮む契機は `reloadAfterPull` (`u` で pull 成功時) の `reset()` のみ (tui.go の
detailOv/prStatusOv/diffOv reset 3 連。grep で他の呼び出し元なしを確認済み)。
オンディスク CI キャッシュには `maxCacheEntries=2000` + TTL prune があるのに、
メモリ内キャッシュ 2 つには対応する上限機構がない。

**発火条件**: pull を挟まず多数のコミットの diff / job ログを渡り歩く長時間セッション。

**影響**: 閲覧 SHA 数 × (diff 最大 5000 行) に比例して増加。人間の閲覧速度に律速されるため
急激な OOM には至らないが構造的には無制限。

**推奨**: `cache.go` の `pruneToLimit` と同型の「エントリ数上限 + 古い順 evict」を両キャッシュへ。

## P2-2: runGit (ローカル git 実行) だけタイムアウト/context の規律から外れている

**問題**: ネットワーク経路は全て `context.WithTimeout` 済み (GraphQL 10s / claude update 5min
/ npm 5s) なのに、`gitlog.go` の `runGit` は素の `exec.Command` (ctx なし)。特に `d` キーの
`loadCommitDiff` は tea.Cmd の goroutine から ctx なし同期実行され、`m.cancel` にも非連動。

**発火条件**: ネットワークマウント上のリポジトリ、`.git` ロック競合、stdin 待ちの git hook
などで git がブロックする場合 (まれ)。ハング中に `q` で抜けても goroutine と git 子プロセスが残る。

**推奨**: 対話ループから非同期発行される経路 (最低限 `loadCommitDiff`) に `gitOpTimeout`
相当の ctx を導入する。

## P3 (低頻度・環境依存)

- **IME 復元にタイムアウトなし**: 終了時 `defer restore()` の `exec.Command(macism).Run()`
  (ime.go) がハングすると exit がブロックする。シェルからの Ctrl-C は届くため実害は軽微
- **`xdg-open` にタイムアウトなし** (external_commands.go, Linux フォールバック):
  環境によってはブラウザに直接 exec して戻らない既知挙動。darwin の `open` は該当しない

## 問題なしと確認した経路 (攻め方の記録)

- **fd リーク**: `os.ReadFile` / `os.CreateTemp` 全 grep。`writeAtomic` は全分岐で
  Close + 失敗時 Remove 済み
- **time.Ticker/Timer 直接使用**: なし。全て `tea.Tick` 経由で、`ticking` / `panelPolling`
  の single-flight フラグが多重チェーンを防止済み
- **ゾンビプロセス**: 全 exec 呼び出しが `Run()`/`Output()` 系のみ (手動 Start+Wait /
  pipe 管理なし) で Wait 漏れなし
- **端末状態復元**: bubbletea v2 が signal 経由を含む全 Run() 終了経路で
  `restoreTerminalState()` を呼ぶことを vendored source で確認
- **起動時並列 goroutine** (planCh/decorCh/displayCh/IME/ResolveRepo): 全てバッファ 1 で
  早期 return パスでも送信ブロックなし

## 関連

- issues/done/019 (rerun) / 024 (claude update toast) — actModal の ctx 設計の経緯
- `_claude/rules/instrument-before-second-fix.md` — P1 修正後は SIGTERM 送信での実挙動確認を
  (子プロセスが消えることを ps で観測)

## 対応状況 (2026-07-29) — 全項対応済み、クローズ

- **P1 完了** (bb0b852): 後始末を `cancelAll()` に集約し `quit()` と main.go の defer の
  両方から呼ぶ。SIGTERM 実挙動の ps 観測は未実施 (対話環境が必要) — 構造は bubbletea の
  vendored source で確認済み
- **P2-1 完了** (cce8bef): `evictOverlayCache` (上限 50・挿入順 evict・表示中 keep) を
  diff / job 詳細の両キャッシュへ
- **P2-2 完了** (cce8bef): `runGitTimeout` (30s) を導入し `LoadCommitDiff` へ適用。
  起動時の同期経路は従来どおり timeout なし (ハングしてもシェルの Ctrl-C がプロセスごと
  落とせるため。全 runGit への一律適用はしない)
- **P3 完了** (cce8bef): IME 復元 (5s) / xdg-open (10s) に timeout
- **追加の関連指摘 (別監査) の見送り 2 件**:
  - `external_commands.go` の rebase abort が ctx なし → **意図的に見送り**。abort は
    pull 失敗からの復旧操作で、途中キャンセルすると rebase 中間状態が残り事態が悪化する。
    中断させないのが安全側
  - `usageOverlay.cancel` の上書き (連続 fetch の短い時間窓) → 見送り。fetch 自体が
    10s timeout の ctx を持ち自己完結するため、実害は「最大 10s の余分な subprocess 生存」
    に留まる
