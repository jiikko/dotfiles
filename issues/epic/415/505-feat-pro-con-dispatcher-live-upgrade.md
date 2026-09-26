# 505 (feat): dispatcher が新しいビルドへ自分で切り替わる (master に入った変更が、手で起動し直すまで効かない)

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの指摘 (2026-09-26): 見張りの係 (475) が「止まっている」と出ていた理由を聞かれ、答えると「復旧処理も修正したら？」。

見張りの係は 08:02〜08:13 に master へ入ったが、動いていた dispatcher (pid 18123) は 02:20 に手で起動したもので、見張りを起こす処理を持っていなかった。
同じ理由で、483 (起動時の自動の復旧)・C-054 (権限の確認で止まった PG を人の番に) なども、16:51 に手で起動し直すまで効いていなかった。
dispatcher は長く走り続けるのに、新しいビルドへ切り替わる口が無い。取り込みの係 (487) が 1 日に何本も master へ入れるので、差は広がる一方になる。

## 今の形 (2026-09-26)

- 画面には入れ替えの口がある: `src/pro-con/upgrade` (ソースが変わったら shim `bin/lib/go_autobuild.zsh` に裏でビルドさせ、`ctrl+r` で `syscall.Exec` して自分を新しいバイナリへ入れ替える。PID と端末はそのまま、UI の状態は Save / Load で引き継ぐ)
- dispatcher には無い。手で `pro-con dispatcher --stop` → 起動し直すしかなく、`--stop` は PG も止める (再開は続きから)
- PG・PM・取り込みの係は Claude Code の bg の session で、dispatcher の子ではない (入れ替えで止める必要は無い)。
  子なのは テストの係の実行 (`make test` 等)・btw の haiku・見張り (475。`monitorsup.go`) だけ

## 期待する動作

- dispatcher が、自分のバイナリより新しいビルドができたことに自分で気づき、**安全な区切り**で新しいバイナリへ切り替わる。人の合図は要らない (dispatcher には画面が無い)。
  PG・PM・取り込みの係は止めない
- 安全な区切り: テストの係の実行・btw の答えを作っている最中・起動 / 再開の結果が分からない印 (Launching) が残っている間は待つ。待ちすぎたら (上限) 出来事にする
- 切り替えたことは出来事 (`pro-con log`) と画面のゲージに出す (旧版 → 新版)
- ビルドの判定とビルドそのものは画面と同じく shim に任せる (`upgrade` パッケージを使う。Go 側に写経しない)

## 考えること

- 🚨 **dispatcher の lock (`dispatcher.lock` の flock)**: `syscall.Exec` で入れ替えると、CLOEXEC の fd は閉じて lock が外れ、その隙に画面の keeper が
  2 つ目の dispatcher を起こしうる。lock の fd を exec に引き継ぐか、入れ替え前後で取り直す形を決めて、2 つ立たないことをテストで固定する
- 子のプロセス (見張り・テストの係・haiku) は、入れ替えの前に止めるか待つ。見張りは入れ替えた後に起こし直す (`superviseMonitor`)
- 入れ替えに失敗したとき (新しいバイナリが起動しない・壊れている) に、旧版のまま動き続けて知らせる (止まらない)
- 画面の `ctrl+r` と同じく、新版の中身の変わり目 (記録の形の変更) を旧版の記録で読めることが前提。読めない変更が入ったときの扱い (待つ / 知らせて止める) を決める
- 破壊的ではないが、状態遷移と外部 I/O (exec・lock) を動かすので、敵対的レビューを最終ゲートにする

## 進捗 (2026-09-26 C-060)

決めたこと (「考えること」への答え。仕様の正本は `src/pro-con/README.md` の「dispatcher の入れ替え」):

- [x] lock は**外さずに exec へ引き継ぐ** (`dispatcher.HeldLock.Exec`)。`syscall.ForkLock` を書き手で持って CLOEXEC を外し
  (その間の fork を止める)、fd の番号を `PRO_CON_DISPATCHER_LOCK_FD` で渡す。新版は `main` の頭で CLOEXEC に戻し
  (`GuardInheritedLock`。claude --version などの子へ渡さない)、`AdoptLock` で受け取る (lock のファイルと inode が違えば受け取らず取り直す)
- [x] 安全な区切り (`dispatcher.Busy`): テストの係・btw・進捗の収集 (裏の子)・閉じた / 削除のカードを止めている途中・Launching (カードと役)。
  30 分待っても来なければ出来事にして待ち続ける
- [x] 見張りは入れ替えの前に止め、新版が起こし直す。失敗したら旧版のまま起こし直す
- [x] 壊れた新版 / 記録を読めない新版: exec の前に新版へ `dispatcher <同じ引数> --preflight` を走らせ、通らなければ旧版のまま続けて
  出来事にする (その新版は試し直さない。次のビルドで試す)。記録の形の変わり目は「待つ (旧版のまま続けて知らせる)」を選んだ
- [x] 出来事 (`upgrade`) とゲージ (`dispatcher 新版 旧版 → 新版` を 10 分・待っている理由・切り替えられない)
- [x] 入れ替えで起きた dispatcher は止める印を捨てず、人の止めた印を外さない・それで抜けない (前のプロセス像の続き)

テスト (commit「pro-con: dispatcher が新版のビルドへ自分で切り替わる (505。lock を exec に引き継ぐ)」):

- `dispatcher/lock_test.go` `TestLockSurvivesExec`: 本物の syscall.Exec の前から後まで (隙を 300ms に広げる) 2 つ目が lock を取れない・PID 不変・
  入れ替わった後の子が lock を握らない。mutation 2 本 (CLOEXEC を外さない / 戻さない) でそれぞれ落ちることを確認
- `dispupgrade_e2e_test.go`: 本物の pro-con のバイナリ + 偽の shim / claude / tmux で、dispatcher が自分で入れ替わる (4 秒)。
  入れ替えの間ずっと 2 つ目が取れない・引数を引き継ぐ・抜けた後に lock が外れる。CLOEXEC を外さない mutation で 3/3 落ちる
- `dispupgrade_test.go` (区切りを待つ・待ちすぎの出来事は 1 回・失敗で試し直さない・確かめた後の差し替えで確かめ直す・shim は 30 秒ごと)、
  `dispatcher/upgrade_test.go` (Busy の各理由・壊れた記録)、`dispupgradecmd_test.go` (止める印・人の止めた印・--preflight)、`ui/gauge_test.go`

敵対的レビュー (最終ゲート。read-only のサブエージェント): 不変条件 1 (2 つ立てない)・2 (子に lock を渡さない) は壊せず、P0 / P1 無し。P2 7 件の扱い
(commit「pro-con: 505 の敵対的レビューを受けて、入れ替えの直前の信号・通知の出し直し・lock 待ちを直す」):

- 直した: ①見張りを止めている間に来た止める信号を捨てて exec する → exec の直前に ctx を見て入れ替えない
  ②新版が signal の受け口を作るのが遅い → lock を受け取った直後 (claude を引く前) へ移した
  ③人の番の macOS 通知が切り替えのたびに全件出直す → 知らせ済みの鍵を `PRO_CON_DISPATCHER_NOTIFIED` で渡す
  ④repo の lock 待ち (runBlock) を区切りで見ていない → Busy に足した
  ⑥止める途中に切られた確かめ・shim の問い合わせを「新版の失敗」と記録する → ctx が取り消されていれば記録しない
  ⑦exec の失敗で RLIMIT_NOFILE が下がったまま (syscall.Exec が戻して上げ直さない) → 失敗したら戻す
- 受けた (記録のみ): ⑤役の起動を受け付けられなかった回数・起動時の確かめ・起動の出来事が切り替えのたびに初めからになる (手で起動し直すのと同じ。上限あり)
- 未確認リスク: 切り替えた後に新版が Tick で落ちると旧版へ戻らない (現状の手の起動と同じ) / 起動から 10 分以内に切り替わると起動時の確かめの警告がゲージから消えうる /
  新版の確かめ (最大 30 秒) の間は Tick が止まり、keeper が 2 つ目を起こしうる (ErrRunning で抜けるので不変条件は保たれる)

残り:

- [x] 本番の dispatcher が実際に入れ替わるところ (2026-09-26 に確認): `pro-con log` の kind `upgrade` に 2 回。
  20:22:23 に 445f809 → e14c42d、20:57:58 に e14c42d → c98ccef (`go build` の直後) で、どちらも「PID 72811 のまま」。
  入れ替わった後の dispatcher の実行ファイルの inode (`lsof -p 72811` の txt) は、その時点の `src/pro-con/pro-con` と一致した
- 既知の制約 (README): 確かめてから exec までの一瞬にもう一度差し替わると確かめていない版が走る / exec から新版が signal の受け口を作るまでの一瞬の SIGTERM は PG を止める処理を通らない

## 関連

- 475 (見張りが動いていなかった実例) / 483 (起動時の復旧) / 487 (取り込みの係。master へ入る頻度が上がった) / `src/pro-con/upgrade` (画面の入れ替え)
