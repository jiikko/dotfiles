# 627 (bug): Claude の利用枠の取得が `/api/oauth/usage` を 1 分ごとに叩いて 429 になり、枠が取れなくなる

起票日: 2026-10-02

## 概要

`ratelimit -source claude` が rc=3 `ratelimit: claude: /usage 出力から利用枠を検出できず` を返し、glogx の U / R も Claude の枠を
出さなくなった (2026-10-02 12:10 以降)。

## 詳細 (実測 2026-10-02、Claude Code 2.1.287)

- `claude -p /usage --output-format json` は rc=0・`is_error: false` だが、`result` に `Current session: N% used · resets …` の行が無い
  (「What's contributing to your limits usage?」の内訳だけ)。2.1.286 でも同じ
- `--output-format stream-json --verbose` の assistant 行の `usage_report.rate_limits` が `null`
- `--debug-file` のログ:
  `fetchUtilization: GET /api/oauth/usage (attempt 1)` → `fetchUtilization: 429 remembered for this bearer; not asking again for 2692s` →
  `Failed to load usage data: Request failed with status code 429`
- CLI の `/usage` (local command) は `/api/oauth/usage` の結果から枠の行を組み、取れなければ**その行を黙って省く** (エラーにしない)
- 誰が叩いたか: `claude -p` は一時ディレクトリの project にセッション記録を残す (`usage.Fetch` の cmd.Dir のコメント)。
  `/usage` を含む記録の mtime を数えると、普段は 1 時間に 2〜8 回、10-02 は 11 時 12 回 / 12 時 46 回 / 13 時 34 回。
  11:53:41 から **ちょうど 60 秒ごと**に 17 回成功し、12:10:41 から枠の無い応答になった
- 60 秒周期の出所は glogx: U の箱 / R のダッシュボードの表示中、`usageRefreshInterval = time.Minute` (`src/glogx/tui.go`) ごとに
  `usage.FetchAll` → `claude -p /usage` を起こす (起票時点。同日の 626 で `FetchClaudePart` に置き換わった)。ratelimit (hook) はキャッシュ 5 分 (`cacheTTL`) で、別に起こす

## 対応方針 (ユーザー判断 2026-10-02: 頻度を落とし、429 で止める)

- `claude -p /usage` を起こす全ての呼び出し元 (glogx / bin/ratelimit / pro-con の dispatcher) を、全プロセス共有のゲート
  (`usage.FetchShared`、`src/ratelimit/usage/shared.go`) に通す。最後の結果を `~/.cache/glog/claude-usage-shared.json` に書き出し、
  全ての取得元がまずそこを読む (ユーザー提案 2026-10-02)。claude の起動は flock で 1 本に絞る
  - 成否によらず、5 分以内に誰かが claude を起こしていれば起こし直さない (成功ならその結果、失敗ならその理由を返す)
  - サーバが枠を返さなかった (`usage_report.rate_limits == null`。429 か通信の失敗) ら、10 分は起こさず、理由と再開時刻をエラーにする
  - glogx の R の `r` (人の操作) だけは 5 分の間引きを飛ばす。止めている間は飛ばさない
- 失敗の理由を「検出できず」ではなく「サーバから利用枠を受け取れない (429 か通信の失敗)。HH:MM まで取得を止める」にする
- glogx / ratelimit のキャッシュの契約 (main.go 冒頭の分離理由) は変えない

## 関連ファイル

- `src/ratelimit/usage/usage.go` (`Fetch`) / `src/ratelimit/usage/codex.go` (`FetchClaudePart` / `FetchClaudePartNow`)
- `src/glogx/tui.go` (`usageRefreshInterval`) / `src/glogx/usage_overlay.go`
- `src/ratelimit/main.go` (`cacheTTL` / `refreshInterval`)

## 進捗

- 2026-10-02: 起票。原因を実測で特定
- 2026-10-02: 実装 (commit「fix(ratelimit): Claude の利用枠の取得を全プロセス共有のゲートに通し、429 のあいだは止める」
  「fix(ratelimit,glogx,pro-con): 共有ゲートの敵対的レビュー指摘を直す」「fix(ratelimit): 共有ゲートの 2 周目の指摘を直す」
  「docs(ratelimit,glogx,pro-con): claude -p /usage の共有ゲートを入口の文書に書く」)
  - [x] 共有ゲート (5 分の共有・失敗の共有・10 分の停止・flock・未来の時刻を信用しない)
  - [x] glogx / bin/ratelimit / pro-con の dispatcher をゲートに通す (pro-con は自前の引数のまま `FetchShared` + `ParseStream`)
  - [x] glogx の R の `r` は間引きを飛ばす (`FetchClaudePartNow`。626 の出所ごとの取得の上に載せ直した)。フッターは `usage.SharedFresh` から「5分ごとに更新」
  - [x] ratelimit / glogx / pro-con の lint と全テストが緑。変異 23 本 (ゲートの各判定・stream-json の判定・glogx の `r` の配線・
        pro-con の素通り) がすべて想定どおり red
  - [x] 実環境 (429 中): worktree の build で `ratelimit -source claude` が 1 回目 3.2 秒で「…14:14 まで取得を止める」、2 回目は 0.00 秒
        (claude を起こさない)。この時点の止める期間は 30 分 (後で 10 分に変えた)
  - [x] 実環境の成功経路 (サーバが枠を返す状態で stream-json から枠が読めること) の観測。2026-10-02 15:27:22 に共有ゲート越しの
        `claude -p /usage` が成功: `claude-usage-shared.json` に 5h 37% / 7d 27% / 7d(Fable) 0% / 版 2.1.287、blockedUntil は空
    - 2026-10-02 14:29 時点ではまだ観測できない: push 後の本番 `bin/ratelimit -source claude` は rc=3
      「サーバから利用枠を受け取れない (429 か通信の失敗)。14:36 まで取得を止める」(ゲートは期待どおり)。
      `claude -p /usage --debug-file` は `429 remembered for this bearer; not asking again for 2849s`。13:32 には
      残り 2692 秒 (14:17 ごろ切れる) だったので、切れた後の最初の問い合わせにサーバが再び 429 を返している
      (サーバ側の窓は 45 分より長い。数え方は未確認)
    - **再観測の条件** (14:55 に改訂。下の進捗を参照): 15:45 以降に `ratelimit -source claude` を 1 回。成功なら 5h / 7d が出て、
      `~/.cache/glog/claude-usage-shared.json` に `fetchedAt` と枠が入る。まだ 429 なら `--debug-file` で残り秒数を見て、その後にもう 1 回
    - 成功を見たらこの項目を埋めて done へ (`scripts/issue_done.sh 627`)
- 2026-10-02 14:55: 止める期間を 10 分 → 50 分に直した (commit「fix(ratelimit): 429 のあとはサーバの Retry-After より長く止める」)
  - 🚨 前提の誤り: 「CLI は 429 を覚えている間はサーバへ行かない」と書いていたが、CLI のコード (`fetchUtilization`) を読むと
    その記憶は `claude -p` のプロセスをまたいで残らない。debug ログの「GET /api/oauth/usage (attempt 1)」→「429 remembered …
    not asking again for Ns」は、その場でサーバから受け取った 429 の記録 (覚えた値の再生なら「… replayed —」の行になる)。
    Ns が 2692 → 2849 と伸びたのも、新しい 429 を受け取った証拠
  - つまり 10 分の停止では 10 分ごとに 429 を叩き直していた (14:16 / 14:26 / 14:51 の試行がすべて 429)。50 分は実測の
    Retry-After の最大 (2849 秒 ≒ 47.5 分) を上回る値
  - 再観測は、最後の 429 (14:51) から 50 分以上空けた 15:45 以降に 1 回

### 敵対的レビュー (opus、観点を分けて 3 本 + 2 周目 1 本)

採用して直した: pro-con の dispatcher がゲートを素通りしていた / 読めない応答のたびに起こし直す / blockedUntil に上限が無い
(→ 丸める) / 未来の fetchedAt を信用する / R の `r` が取り直さない / フッターの「1分ごと」/ rate_limits=null は通信断でも出うる
(→ 文言と 10 分) / type が result でない行の result キー / 共有する失敗の理由の長さ

記録だけ (今より悪くはしない。直していない):
- 他の呼び出し元 (起こし方が違う pro-con 等) の失敗が、自分の取得を 5 分止める。止まりきりにはならない
- claude が呼び出し側の持ち時間 (glogx は 10 秒。ロック待ちを含む) より遅いと、時間切れは共有しないので毎回起こしては kill する
- 止めている間でも、5 分以内に取れていた枠は force 無しの呼び出しに成功として返る (glogx の staleErr の注記が消える)
- Snapshot に取得時刻が無く、受け取った側が自分の時刻で記録する (各キャッシュの古さが最大 5 分若く見える)。
  claude の版の表示も最大 5 分古い
- ロック待ちで打ち切ると「他のプロセスが利用枠を取得中で、待ちきれなかった」を出す (以前は自分で起こしていた)
- `cachedir.Base()` が失敗する環境 (HOME も XDG も無い) ではゲートを通らない

却下: pro-con の旧 ParseUsage が受けていた小数・「<1%」を usage.Parse が受けない — `-p` の /usage は Math.floor の整数で出す
(2.1.287 のバイナリで確認)。resets の無い行は 0% の枠。理由は `src/pro-con/dispatcher/usage.go` の `usageOf` のコメント
- 2026-10-02 15:00: ユーザー指摘「このセッションの /usage は普通に見えている」。CLI のコード (`/usage` の取得 `R` / 画面 `$s`) を読んで判明:
  - 対話セッションの `/usage` は、推論の応答ヘッダで受け取った利用枠 (statusline の `rate_limits` と同じ出所) や前回の値を使い、
    「Usage read answered from a snapshot …; endpoint not asked」で `/api/oauth/usage` を叩かずに答える。429 のときもヘッダの値か
    前回の値で表示を続け、「(rate limited — try again in a moment)」「Per-model breakdown unavailable」等の注記を付ける
  - `claude -p /usage` は推論をしない新しいプロセスなのでヘッダの値を持たず、前回の値 (`~/.claude.json` の
    `cachedUsageUtilization`。この時点で 11:10 のもの) も古すぎて使われない。必ず `/api/oauth/usage` を叩き、429 なら枠が出ない
  - つまり `/api/oauth/usage` の 429 は本物だが、壊れるのは `claude -p` 経由で取る側 (glogx / ratelimit / pro-con) だけ
  - 残る設計の選択肢 (ユーザー判断待ち): statusline が受け取る `rate_limits` (推論の応答ヘッダ由来。サーバを余計に叩かない) を
    ファイルへ書き出して主な出所にする。欠点: 7d(Fable) が出ない・セッションが動いていない間は更新されない
- 2026-10-02 15:40: ユーザー判断「statusline の値を主にする」で実装 (commit「feat(ratelimit,statusline): Claude の利用枠の主な出所を
  statusline の rate_limits にする」と、レビュー指摘の 3 commit「fix(ratelimit,statusline): 敵対的レビュー指摘 …」
  「fix(statusline): 2 周目の指摘 …」「fix(statusline): 3 周目の指摘 …」)
  - [x] statusline (`write_rate_limits`) が `~/.cache/glog/claude-rate-limits.json` を書き、`usage.FetchShared` が先に読む
        (glogx / bin/ratelimit / pro-con の 3 つとも切り替わる)。7d(Fable) は出ない (受け入れ済み)
  - [x] statusline は送信が無くても `refreshInterval` (60 秒) ごとに描画し、古い値を持ってくる (ユーザー指摘・レビューが再現)。
        書き手は窓ごとに値で新旧を比べ (リセット時刻が後 / 600 秒以内なら同じ窓として使用率が高い方)、新しいときだけ書く。
        値が同じでも、そのセッションの transcript がファイルより新しければ observedAt だけ進める
  - [x] 15 分新しい値が来なければ予備 (`claude -p /usage`、共有ゲート) へ落ちる。pro-con の PG は statusline を走らせないので、
        PG だけが動いている間はサーバの値で見る。R の `r` は statusline を飛ばす
  - [x] キーの無い窓 (リセットを過ぎて落とされた窓) は 0% Unused。使用率があるのにリセット時刻が無い形は予備へ。版は入力から
  - [x] 3 module の lint・全テスト・shellcheck・test_statusline.sh が緑。変異 33 本 (書き手の新旧比較・bump・桁の検査・stderr・
        読み手の各分岐・force) がすべて red。statusline の描画は 22.3 → 24.9 ms (50 回平均)
  - 敵対的レビュー (opus): 1 周目 2 本 + 2 周目 1 本 + 3 周目 1 本。3 周目の修正は検査の順序とテストだけで、変異で直接確かめたので打ち切り
  - 記録だけ (直していない):
    - `-nt` は「transcript が書かれた」で、「ヘッダ付きの応答が届いた」ではない。API を呼ばずに transcript を書く操作が
      あると、ファイルの値が最長 15 分新しく見える (どの操作が書くかは未確認)。bash 3.2 の `-nt` は秒単位
    - 2 つのセッションの同時の書き込み (読む → 比べる → mv の間) で新しい値が負けることがある。相手の次の描画 (60 秒以内) で戻る
    - サーバ側の補正などで使用率が実際に下がると、リセットまで高い方が残る
    - 別のアカウントのセッションも同じファイルに書く (今は同じアカウントだけを使う前提)
    - bin/ratelimit のキャッシュは取得した時刻で古さを測るので、statusline の観測からは最大 20 分古い値で `-check` が答える
  - [x] 実環境: pull 後 (15:30) に statusline が `claude-rate-limits.json` を書き出し (5h 37% / 7d 27% / 版 2.1.287)、
        `ratelimit -source claude` が rc=0 で同じ値を返した。15:27 のサーバの値と一致
- 2026-10-02 15:31: 完了。記録だけの項目 (上の「記録だけ」) は open な残課題にしない (害が限られ、再現したら新しい issue で扱う)
- 2026-10-02 21:00 頃 (別セッションが epic 618 の `make test` の失敗から調べた。直していない): `TestFetchCallerTimeoutIsNotShared` は環境で合否が変わる。
  `countingClaude` が `PATH` を stub のディレクトリだけにするので、stub の `sleep 2` は `command not found` で素通りし、偽の claude はすぐ成功を返す。
  合否は「Fetch が 200ms の持ち時間の内に終わるか」(マシンの速さ) で決まり、速いと落ちる。単体の `-race -count=1` で 3 回中 2 回、5 回中 3 回落ちた。
  statusline の commit より前の 681b5f5e でも 5 回中 3 回で、66e222ac〜7fb015cd が原因ではない。`PATH` を絞って stub と同じ形の script を起こすと、
  `sleep: command not found` を出して rc=0 で続きを書いた (`/bin/sh` は bash)。
  `TestFetchConcurrentCallsRunClaudeOnce` の `sleep 0.3` も同じ形で、狙ったロック待ちの窓は作れていない見込み (未確認。緑のまま通る)。
  直し方は stub の `sleep` を `/bin/sleep` にする (`codex_test.go` の stub は既に絶対パス)。どの open issue でも追っていない
- 2026-10-03: 上の環境依存の失敗を直した (commit「test(ratelimit): 偽の claude の sleep を絶対パスにする …」)。stub の `sleep 2` /
  `sleep 0.3` を `/bin/sleep` にし、countingClaude に「PATH を絞るので外部コマンドは絶対パス」の注記を足した。
  `PATH` を絞った stub で `sleep: command not found` → rc=0・0 秒で続きを書くことを直接再現してから直した。
  直した後: 2 本とも `-race -count=1` で 3 回緑。変異で red を確認: 持ち時間切れを共有する (TestFetchCallerTimeoutIsNotShared) /
  flock を外す (TestFetchConcurrentCallsRunClaudeOnce)。同じ形 (PATH を絞った stub の素の外部コマンド) を ratelimit / glogx の
  テストで grep し、他は絶対パスか組み込み (printf / echo / read / pwd) だけだった
