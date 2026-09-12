# lockman の resource-leaks / performance 監査 (2026-09-11) — 記録と却下理由

🚧 **claim: dotfiles-53 (2026-09-11)**。残タスクのうち 2 件を決着させたが、356 / 357 待ちが残るため **open のまま**。

起票日: 2026-09-11
カテゴリ: research
監査タイプ: `resource-leaks` / `performance` (どちらも direct 実行)
対象スコープ: `src/lockman/` に絞る指定 (ユーザー指示)

この issue は `_claude/rules/move-report-conclusions-to-issues.md` に従って、
**全数勘定・却下した指摘とその理由**を残すためのもの。却下理由を残さないと次の監査が
同じ指摘を再生成する。

## 走査した母集合 (機械で数えた)

`wc -l` の実測:

| 対象 | 行数 |
|---|---|
| production: `cleanup.go` 106 + `lock.go` 441 + `main.go` 348 + `util.go` 89 + `with.go` 94 | **1,078** |
| test: `cleanup_test.go` 133 + `lock_test.go` 306 + `main_test.go` 150 | **589** |
| `src/lockman/*.go` 合計 | **1,667** |
| 併せて読んだ呼び出し側: `bin/lockman` 18 / `zshlib/_av1ify_lock.zsh` 344 | 362 |
| 併せて読んだ doc: `src/lockman/README.md` 64 / `.github/workflows/src_lockman.yml` | — |

**`src/lockman/*.go` は全行読んだ** (サンプリングしていない)。
`issues/done/` 側は `091` (仕様の正本) / `312` / `340` / `093` を読み、既知・却下済みの
指摘と重複しないか照合した。

## 生存した発見 (3 件)

| issue | 内容 | 証拠の強さ |
|---|---|---|
| [356](356-bug-lockman-with-releases-lock-while-grandchildren-run.md) | `with` が孫プロセスの走行中にロックを解放する (排他が破れる) | **再現済み** (孫の生存 + 解放後の保持者との交互書き込み) |
| [357](357-bug-lockman-with-bypasses-io-timeout.md) | `with` だけ I/O タイムアウトの外。詰まると SIGTERM/INT/HUP が全部効かない | 機構は**機械照合済み**、挙動は FIFO ハーネスで**再現済み**、本番条件 (応答しないマウント) は**未再現** |
| [358](done/358-refactor-lockman-cleanup-selftoken-is-production-unreachable.md) | `Cleanup` の `selfToken` ガードが production 到達不能かつ守る対象が存在しない | 機械照合済み |

🚨 **`lockman with` を実行している production コードは 0 件。**
`grep -rn 'lockman with' --include='*.zsh' --include='*.sh' --include='Makefile' --include='*.go'`
の当たりは `zshlib/_av1ify_lock.zsh` のコメント 1 行だけ (フィルタを外すと `bin/lockman` の
ヘッダの用例も当たるので、非 issue ヒットは 2 件)。3 通りで裏を取った: av1ify は
acquire / renew / release しか呼ばない / `tests/zshrc/av1ify/test_helper.sh` の mock は
`with` を未知の subcommand として弾く / repo 全走査。

重要度はこの事実を踏まえて **357 = high / 356 = medium** に分けた:

- **357 は high**。反証レビューで穴が `with` 限定でないことが判明し (deferred `Cleanup` は
  live な `acquire` / `break` / `cleanup` にも効く)、かつ [issue 091](done/091-feat-lockman-directory-lease-lock.md):418 が
  終了コードまで指定した明文の要求 (`--io-timeout` 超過は `with` で 125) に対する違反で、
  受け入れ条件 091:496 のテストも 0 件
- **356 は medium**。091 は孫の封じ込めを一度も約束しておらず (約束しているのは :282 の
  「子プロセスを止められる」まで)、しかも推奨案 A / B が**両方 `setsid` した子孫に破られる**
  ので、high が含意する「直せば閉じる」が成立しない。**high へ上げる trigger**:
  `lockman with` を実行する production コードが入ったとき

`with` を誰も使っていないことが、この 2 件が今まで見つからなかった理由でもある
(091:381 は `with` を「主用途として推す」と明言しているので、最初の利用者がそのまま踏む)。

## 却下した指摘とその理由 (5 件却下 + 1 件は却下を取り消し)

### 1. `~/.lockman/av1c/` の per-key ディレクトリが回収されない — 却下 (記録に留める)

実測 2026-09-11 (`~/.lockman/av1c` の実データ):

| 指標 | 実測 |
|---|---|
| key ディレクトリ数 | **2,845** |
| 総 inode 数 | **5,846** |
| 容量 | 2.3 MB |
| `.lockman` を持つ key | **600** |
| `.lockman` すら無い空の key | **2,245** (全体の 79%) |
| 残骸ファイル (`lock` / `probe/*` / `tmp/*` / `graveyard/*`) | **すべて 0 件** |
| mtime の分布 | 2026-09-08: 1,630 / 2026-09-09: 1,215 / **それ以降 0** |

内訳の突合: `600 × 6` (key + `.lockman` + `tmp` + `probe` + `graveyard` + `.cleanup_at`)
`+ 2,245` (空 key) `+ 1` (root) = **5,846** — 総 inode 数と完全に一致する。

**却下の理由 (3 つとも私の当初の主張が崩れた)**:

- 「無制限に増え続ける」→ **成り立たない**。全 mtime が 2026-09-08〜09 に収まり、
  それ以降 1 件も増えていない。av1ify を 2 日使ったぶんの履歴で、進行中の蓄積ではない
- 「手動 `rm -rf` は lease 保持中に危険」→ `zshlib/_av1ify_lock.zsh` 冒頭が既に
  **「アイドル時に `rm -rf ~/.lockman/av1c` の 1 回で片付く」**と前提つきで書いており、
  手動回収は既に下されている設計判断。前提を言い換えただけで反証になっていない
- 「空 key が 79%」→ これだけは新しい事実。原因は `__av1ify_lock_acquire` が
  `mkdir -p -- "$dir"` を `lockman acquire` より**先に**実行し、どの失敗経路でも
  ディレクトリを消さないこと。ただし **由来は未確認** (2026-09-08〜09 は av1ify の
  排他機構を開発していた期間で、ビルド失敗や `AV1IFY_ALLOW_NO_LOCK=1` 経由の可能性)。
  そして**対象は `zshlib/_av1ify_lock.zsh` = lockman の外**で、今回のスコープ外

**再開の trigger**: `~/.lockman/av1c` の key ディレクトリの mtime が **2026-09-09 より
後**になったら (= 蓄積が再開したら) 起票する。確認は

```sh
find ~/.lockman/av1c -mindepth 1 -maxdepth 1 -type d -newermt 2026-09-10 | wc -l
```

### 2. `cleanupDue()` が既知の `now` を捨てて `serverNow` をもう 1 回呼ぶ (perf) — 却下

`Cleanup` は `!force && !cleanupDue()` で早期 return するが、`cleanupDue()` は
`serverNow()` を呼ぶ = **probe ファイルの create + stat + unlink (3 metadata op)**。
`Acquire` は直前に `serverNow()` の結果を持っているので、渡せば省ける。

**実測 (`serverNow` の `defer os.Remove` を外した計測用ビルドで probe を数えた)**:

| コマンド | probe 生成数 |
|---|---|
| `acquire` (初回。`.cleanup_at` が無く cleanup が実行される) | 2 |
| `acquire` (2 回目以降。cleanup は 10 分の rate limit で skip) | 2 (**うち 1 が無駄**) |
| `release` | 1 |
| `renew` | 2 (期限検査 + 打刻の検算。issue 312 の設計どおり) |
| `status` | 1 |
| `check` (保持中) | 1 |
| `check` (空。`readLock` が nil を返して早期 return) | **0** |
| `break` | 1 |

**壁時計の A/B (ローカル APFS、同一 dir で acquire+release を 200 周 × 3 回)**:

| 版 | r1 | r2 | r3 |
|---|---|---|---|
| 現行 | 11.573 | 11.595 | 11.482 ms/cycle |
| `cleanupDue` の追加 `serverNow` を省いた版 | 12.121 | 11.818 | 11.590 ms/cycle |

**差は測定ノイズ以下** (省いた版のほうが遅く出た回すらある)。1 cycle が
プロセス起動 2 回に支配されているため。

**却下の理由**: 効果が測れない。しかも `zshlib/_av1ify_lock.zsh` の既定 lock root は
`~/.lockman/av1c` = ローカル HOME なので、**現在の唯一の利用者に対する効果は 0**。
SMB 越しの実時間は**未実測**。
**再開の trigger**: `AV1IFY_LOCK_ROOT` を SMB 共有へ向けた運用が入り、acquire が遅いと
観測されたとき。そのとき上の probe 数の表がそのまま見積もりに使える
(op 数は静的にも計数と一致している)。

### 3. `ensureDirs` が既存ディレクトリにも毎回 4 回 `chmod` する (perf) — 却下

`ensureDirs` は 4 ディレクトリすべてに `Mkdir` (既存なら EEXIST) + `os.Chmod` を
無条件に打つ (+ sticky 検査の `Stat` 1 回) = acquire / break ごとに 9 metadata op。

**却下の理由**: 削減にならない。

- 「`Stat` してモードが違うときだけ `chmod`」→ common case の op 数は 1 → 1 で不変
- 「`Mkdir` が成功したときだけ `chmod`」→ **「以前の run が厳しい umask で作った
  ディレクトリを後から緩める」自己修復を失う** (`cleanup.go` の意図ではなく
  `ensureDirs` のコメント「umask に削られたモードを明示的に戻す」がその目的を明記)。
  `_claude/rules/list-masked-failure-modes-before-removing-guard.md` に照らして、
  失うもののほうが大きい
- `.lockman` の `Stat` 1 回で全体を skip する案は、`probe/` だけが消された状態の
  自己修復を失う (av1ify の doc が手動 `rm -rf` を前提にしているので現実的な状態)

### 4. `withTimeout` の goroutine リーク — 却下 (既知・記録済み)

`util.go` の `withTimeout` のコメントが「🚨 固まった goroutine は回収できない …
プロセスの終了で解放される前提の使い捨て」と既に明記している。lockman は `with` 以外
すべて短命プロセスで、`with` は `withTimeout` を通らない (→ [issue 357](357-bug-lockman-with-bypasses-io-timeout.md))。
**goroutine が積む経路が存在しない**ので、指摘として成立しない。

🚨 **ただし [issue 357](357-bug-lockman-with-bypasses-io-timeout.md) の推奨対応 1 は
まさにその経路を作る。** `with` を `withTimeout` で包むと、`with` は唯一の長寿命モード
(TTL 30 分なら 10 分ごとに renew) なので、応答しないマウントでは **tick ごとに
1 goroutine + 1 ブロック中 syscall が積む**。`util.go` の「プロセスの終了で解放される
前提の使い捨て」という前提はそこで初めて破れる。
**再開の trigger: issue 357 を実装したとき** (包み方を決める段で、tick ごとの
goroutine 蓄積に上限を置くか、renew を専用の goroutine 1 本に固定するかを決める)。
この trigger を書いていなかったため、反証レビューが「却下文が『経路は存在しない』と
主張したまま 357 がその経路を作る」と指摘した。

### 5. `runWith` の lease 喪失後の SIGTERM — **却下を取り消した (間違った問いを却下していた)**

**当初の却下文**: 「ティックごとに SIGTERM を撃ち続けるが、子は 1 回目の TERM で既に
終了要求を受けており追加の TERM は無害。stderr の警告が増えるだけなので低価値」。

**その判定は問いを間違えていた。** 「撃ち続けるのは無害か」(→ 無害。ここは正しい) を
答えたが、答えるべきは **「TERM を 1 回撃つだけで子は止まるか」** だった。反証レビューが
指摘したとおり:

- `--on-lost kill` は `syscall.Kill(-pgid, SIGTERM)` を撃つだけで、**SIGKILL への昇格も
  待ちも上限も無い**。TERM を trap / 無視する子 (`ffmpeg` を含め普通にある) は生き続ける
- lease を失った時点で**他者が既に引き継いでいる**ので、その状態は
  [issue 356](356-bug-lockman-with-releases-lock-while-grandchildren-run.md) と同一の
  二重書き手条件。`with` は子が終わるまで返らないので無期限に続く
- [issue 091](done/091-feat-lockman-directory-lease-lock.md):282 は
  「`--on-lost=kill|warn` (既定 `kill`) で**子プロセスを止められる**ようにする」と
  要求しており、これを満たしていない
- 加えて `lost` は `Renew` の**あらゆる**失敗 (I/O エラー / `serverNow` 失敗 /
  `clockSkewTolerance = 5s` 超の打刻ずれ) で立ち、一度立つと戻らない。
  **一過性のヒカップ 1 回で子が殺され rc=122 が返る** — lease は失われていないのに
  (091 の表ではそれは 125)
- **この経路のテストは 0 件**。`exitWithLost` の出現は定数定義と `with.go` の return、
  `main_test.go` の `TestExitCodesDoNotCollide` (定数の重複検査だけ) のみ

**行き先**: 「TERM だけでは止まらない」は [issue 356](356-bug-lockman-with-releases-lock-while-grandchildren-run.md)
の経路 2 へ、「判定不能と lease 喪失の混同 / 終了コード」は
[issue 357](357-bug-lockman-with-bypasses-io-timeout.md) の該当節へ移した。

🚨 **却下を取り消した記録をここに残すのが要点。** 誤った却下文が `done/` に残ると、
次の監査は「ここは見た・無害と決着済み」と読んで同じ箇所を再訪しない。

### 6. `parseFlags` の再解析ループが O(n²) — 却下 (低価値)

位置引数を 1 つずつ剥がして `fs.Parse` を呼び直すので、引数 n 個で n 回パースする。
**却下の理由**: n は argv の長さで実務上 2〜6。`flag` が最初の非フラグで止まる仕様への
対処としてコメントで理由が明記されており、代替 (手書きのパーサ) は複雑性を上げる。

## 反証レビュー 1 周の結果 — 自分の主張が 6 件崩れた

`_claude/rules/issue-creation-codex-review.md` の代替節 (codex を使わない環境では観点を
分けた read-only サブエージェントの反証レビュー) に従って 1 周通した。**追認ではなく
実際に崩れた**ので記録する:

| 崩れた主張 | 正しい内容 |
|---|---|
| 357「`with` **だけ**が timeout の外」 | **誤り**。`main.go` の `dispatch` の deferred `Cleanup` も包まれておらず、穴は `acquire` / `with` / `break` / `cleanup` の 4 コマンド共通。`with` の未包み I/O は 3 → **4** |
| 357 の根拠は「README の言い回し」 | 091:418 が**終了コードまで指定した明文の要求**。README より強い根拠が仕様の正本にあった |
| 357 の推奨対応 2「タイムアウトは lease 喪失と同じ扱い (=122)」 | **仕様違反**。091:418 はタイムアウト = 判定不能 = `with` では **125**。357 自身の推奨対応 3 とも矛盾していた |
| 357「SIGKILL しか残らない」 | Notify しているのは INT / TERM / HUP だけなので **SIGQUIT 等でも終了する**。結論 (`defer Release` が走らない) は不変 |
| 358「1 時間より古い自分の残骸は**原理的に**存在しない」 | **誤り**。`os.Remove` のエラーを `_ =` で捨てており、`with` と `acquire --wait`(上限なし) は長寿命。正しい根拠は (i) probe 名が独立乱数で照合形と一致しえない (ii) `os.Link` 後の tmp は holder に不要 (iii) 過去の試行は別トークンで元から対象外 |
| 356 の推奨案 A / B「排他の破れに効く」 | **両方 `setsid()` した子孫に破られる** (レビュワーが Darwin 24.6.0 で実測)。案 B は「グループが空」と即判定して静かに解放するので A より悪い |
| 却下 5「lease 喪失後の SIGTERM は低価値」 | **却下を取り消した** (上記) |

数え直しは 27 項目のうち **20 項目が一致**し、不一致 7 項目はすべて上表と下の「軽微」節に
反映した。行数 (1,078 / 589 / 1,667)・`expired(` の 1+4・`Cleanup` の呼び出し 1+8・
`~/.lockman/av1c` の 6 指標と内訳計算・probe 生成数の 8 行はいずれも独立に再測されて一致した。

## 軽微だが実在する (issue を立てず記録。直すときは 356 / 357 と同じ commit で)

- **`--on-lost` の値が検証されていない**。`main.go` は `o.onLost != "warn"` で判定するので、
  `--on-lost warm` のような typo は**黙って kill** になる (091:282 は `kill|warn`)。
  1 行の入力検証で直る。`with` の呼び出し元が 0 件なので単独では起票しない
- **`--io-timeout` にも値の検証が無い** (2026-09-11 追記。358 の敵対レビューが実測)。
  `--ttl` は `main.go` が `minTTL` を強制しているのに `--io-timeout` は素通しで、
  `--io-timeout 5h` / `0` / `-5s` がすべて rc=0 で受理される。実害は 2 つ:
  - `0` / 負値は `check` が恒久的に rc=3 (busy) を返す (「I/O が 0s 以内に返らない」)
  - `scratchRetention` (1h) を超える値を渡すと、`cleanup.go` の `minRetention` が根拠に
    している「保持期間 ≫ 1 回の I/O の上限」が成立しない (コメントに明記済み)

  `--on-lost` と**同じ族なので同じ commit で直す**
- **091 の受け入れ条件のうち 2 つが未達**:
  - 「`--io-timeout` で exit 1 になること。**「空いている」に倒れないこと**が本体」(091:496)
    → `grep -n 'io-timeout\|ioTimeout\|timed(\|withTimeout(' *_test.go` = **0 件**。
    包んである 5 箇所も一度も検証されていない (→ [issue 357](357-bug-lockman-with-bypasses-io-timeout.md) の todolist に入れた)
  - 「`with` の自動 renew が効いている (renew を止める変異を当てて赤になることを確認する)」
    → `lost` 分岐 / ticker / `sigCh` を実行するテストが **0 件** (→ [issue 356](356-bug-lockman-with-releases-lock-while-grandchildren-run.md) の todolist に入れた)

## 攻めたが見つからなかった範囲 (次の監査の起点)

🚨 **2026-09-12 追記**: 358 の敵対レビュー 5 周目が、ここに挙がっていない 3 経路で実害を
実測した (362 / 363 / 364)。**この節は「攻めて見つからなかった」ではなく「攻めていなかった」
範囲を含んでいた** — 特に `withTimeout` の goroutine 回収と `with` のシグナル導入順序。
次の監査はこの節を「済み」として読まないこと。

- **`Acquire` の勝敗判定**: `tryPlace` の `link(2)` → `O_CREAT|O_EXCL` fallback →
  write-then-verify (`readLock` で token 一致を確認) の 3 段は穴を見つけられなかった。
  `tryTakeover` の rename 引き継ぎも、`os.IsNotExist` を「作りにいってよい」へ倒す
  分岐まで含めて勝者が 2 人になる経路を作れなかった。`README.md` が「この 1 行を
  `unlink` に変える変異でテストが「勝者が 2 人」で赤になることを確認済み」と記録しており、
  該当テストは `TestAcquireHasExactlyOneWinner` / `TestStaleTakeoverHasExactlyOneWinner`
- **`holderTTL` / `expired`**: 奪う側の `--ttl` が判定に混入する経路 (issue 093 で
  塞いだもの) の再発は無い。判定の出典は `tryTakeover` / `Release` / `Renew` / `Inspect`
  の 4 箇所**すべて**が `expired(now, mtime, holderTTL(m))` を呼んでいる (`grep -n 'expired('`
  の production ヒットは定義 1 + 呼び出し 4 でちょうど尽きる)。2 実装は無い
- **`tmp` / `probe` / `graveyard` の残骸**: 実データ (上記 1) で残骸 0 件を確認。
  `serverNow` / `tryPlace` の `defer` が両方とも早期 return より外に置かれている
- **`cmdAcquire` の backoff**: `rand.Int63n` に 0 以下が渡る経路 (panic) を作れなかった
  (`backoff` は 1s 始まりで `*3/5` が常に正)
- **fd リーク**: `os.OpenFile` / `os.Create` の全経路で `Close` が呼ばれている
  (`tryPlace` / `Renew` は error 時にも `f.Close()` を打っている)。`ReadDir` / `ReadFile`
  は fd を持ち越さない

## 付随して見つけた乖離 (スコープ外。切り出し先はユーザー判断)

`src/lockman/README.md`「対象環境」節が

> ただし **CI は ubuntu で回る**ため、コード自体は linux でもビルド・テストできる状態に保つこと。

と書いているが、**runner は macOS に移っている** (issue 133)。
`.github/workflows/src_lockman.yml` のヘッダは正しく「runner は macOS。issue 133 で
ubuntu から移した」と書いており、README だけが取り残されている。しかも README 側は
「linux でも動く状態に保て」と**能動的に誤誘導する**内容で、root の `CLAUDE.md`
「Linux はサポート対象外。GNU でも動くようにという理由だけで分岐を足さない」と衝突する。
`_claude/rules/claude-md-maintenance.md` の「意味的乖離」に該当。

## 進捗

- 2026-09-12: **358 の敵対レビュー 5 周目が、この監査が攻めていなかった範囲を 3 件出した**。
  掃除機構の内側は 358 で解消 (`sub` 軸の fail-closed / 打刻の失敗の伝播 / `serverNow` の
  良性判定)。外側は **[362](362-bug-lockman-abandoned-timeout-goroutine-leaves-lock.md)**
  (見捨てた goroutine が失敗報告後に lock を置く。35/450) /
  **[363](363-bug-lockman-with-signal-handler-installed-too-late.md)**
  (`signal.Notify` が遅く、中断で lock + 孤児。20/120) /
  **[364](364-bug-lockman-with-release-failure-and-graveyard-retention.md)** (小粒 4 件) /
  **[366](366-bug-lockman-stale-takeover-sometimes-has-two-winners.md)**
  (引き継ぎの勝者が低頻度で 2 人。原因未特定) として起票。
  下の「軽微だが実在する」の `--io-timeout` 無検証は 362 と同じ族なので、356 / 357 と
  まとめて直すときに 362 も見る
- 2026-09-11: resource-leaks / performance の 2 タイプを直列で実行。生存 3 件を起票、
  却下 5 件 + 却下を取り消した 1 件を本 issue に記録
- 2026-09-11: 反証レビュー 1 周を通し、**自分の主張 6 件が崩れた**ので 356 / 357 / 358 を
  書き直し、却下 5 を取り消し、却下 4 に再開 trigger を足した (上記の節)
- 2026-09-11: `make test` を worktree で通した (rc=0。stdout / stderr を分けて確認し
  `✗` / FAIL / 失敗ターゲットの集約行はいずれも 0 件。`ok lockman 3.170s`)
- 2026-09-11: **[358](done/358-refactor-lockman-cleanup-selftoken-is-production-unreachable.md)
  を実装完了** (推奨対応 A)。359 の残タスクが保留していた「下限検査をコンパイル時に
  置けるか」は**置けた**ことが実測で決着 (358 の「実施結果」1・2)。敵対レビューは 358 側の
  残タスクへ引き継いだ
- 2026-09-11: 下の「付随して見つけた乖離」(README) を修正。同時に**同じ節に書き足すべき
  だった事実**を 1 つ見つけたので README へ足した — 下の「乖離の是正」節
- 2026-09-11: 残タスク 1 (却下 1 の再開 trigger を定期確認する仕組み) を
  **「作らない」で決着**。下の「決着: 再開 trigger は本文のみに留める」節
- 2026-09-11: 358 の敵対レビュー 1 周目で `--io-timeout` の無検証が出たので、
  「軽微だが実在する」節へ追加した (`--on-lost` と同じ族。356 / 357 の実装時に一緒に直す)

## 決着: 再開 trigger は本文のみに留める (仕組みを作らない)

却下 1 の再開 trigger (`find ~/.lockman/av1c -mindepth 1 -maxdepth 1 -type d -newermt 2026-09-10 | wc -l`)
を定期実行する仕組みは**作らない**。理由:

- **却下した指摘のために監視機構を新設するのはスコープ拡大**。却下 1 は「無制限に増え続ける」が
  実測で崩れた (全 mtime が 2 日に収まる) ものであり、**再発を待つ価値のほうが監視のコストより
  小さい**
- 仕組みを足すと、それ自体が `adversarial-review-own-safeguards.md` の対象 (自作の検査) になり、
  異常系の実験・false green の確認・敵対レビューが要る。**却下済みの指摘に対して払う額ではない**
- 蓄積が再開したときの検出は監視でなくても効く: 対象は `~/.lockman/av1c` で、av1ify を使う
  本人が容量・速度で気づく経路がある。加えて `zshlib/_av1ify_lock.zsh` 冒頭が
  「アイドル時に `rm -rf` 1 回で片付く」と前提を明記しているので、気づいた時点の回復も安い

**検出可能性は「確実な検出手段はない」** (`refuse-low-value-coverage.md` の三値)。
「本人が気づくはず」を肯定的な理由として数えていない — 上のコマンドを**人が思い出して
打つ**以外の検出手段は無い、が正確な記述。再評価の trigger は本文の却下 1 のとおり。

## 乖離の是正 (2026-09-11 実施)

`src/lockman/README.md`「対象環境」節を直した。

- **直した誤り**: 「**CI は ubuntu で回る**ため、コード自体は linux でもビルド・テストできる
  状態に保つこと」→ runner は macOS (issue 133)。root `CLAUDE.md` と衝突する能動的な誤誘導
  だったので、事実の訂正だけでなく**その指示ごと落とした**
- 🚨 **同じ節に書き足した事実 (当初は気づいていなかった)**: ubuntu runner を失ったことで
  **実際に消えた検出力がある**。`lock.go` の `holderTTL` / `lock_test.go` の
  `TestShortTTLCannotStealLiveLock` が「macOS では速すぎて出ず、**CI の Linux が
  「勝者が 4 人」で露見させた**」と記録している。runner の移行は
  `list-masked-failure-modes-before-removing-guard.md` の言う「防御を外す」変更に当たるのに、
  マスクしていた failure mode (時間差で顕在化するレース) の行き先が決まっていなかった。
  README にその前提を明記した (「この付近を触るときは競合の窓を人為的に広げて確かめる」)

🚨 **第 1 版の記述は敵対レビューで崩れ、直した**。「`TestShortTTLCannotStealLiveLock` が
記録している不具合 …… 競合の窓を人為的に広げて確かめること」と書いたが、**そのテストは
逐次 2 コールで窓を持たない**。「勝者が 4 人」を出せるのは 16 goroutine で競わせる
`TestStaleTakeoverHasExactlyOneWinner` / `TestAcquireHasExactlyOneWinner` のほうで、
広げるべきはそちらの TTL とワーカー数。**指示が、広げても何も起きないテストを
指していた**ので実害があった (README は修正済み)。

**未確認**: 同型の退行が今の macOS CI で本当に捕まらないかは**実測していない**
(上記 3 本は現在の CI で緑だが、それは「それらが守っている既知の形」についてであって、
時間差で出る**未知の**形については何も言っていない)。

## 残タスク

- 「軽微だが実在する」節の 1 件目 (`--on-lost` の値検証) は 356 / 357 の実装時に一緒に直す
- **反証レビューは 1 周のみ。** `_claude/rules/adversarial-review-own-safeguards.md` §7 は
  「指摘を直したら直した差分にもう 1 周回す」を求めるが、監査時点の修正は**issue 本文の
  書き換えだけでコードを 1 行も変えていない**ため 2 周目は回していない。
  **358 は実装済みなので、その差分への敵対的レビューは
  [358](done/358-refactor-lockman-cleanup-selftoken-is-production-unreachable.md) の残タスクへ移した。**
  356 / 357 の実装に入るときも同様に、その差分に対して改めて敵対的レビューが要る

**この issue を done にできる条件**: 上の 1 件 (`--on-lost` の値検証) が 356 / 357 の実装で
片付いたとき。358 の完了だけでは閉じない。

## 関連

- [issue 356](356-bug-lockman-with-releases-lock-while-grandchildren-run.md) / [issue 357](357-bug-lockman-with-bypasses-io-timeout.md) / [issue 358](done/358-refactor-lockman-cleanup-selftoken-is-production-unreachable.md) — 生存した発見
- [issue 340](done/340-risk-av1ify-lock-unverified-residuals.md) — lockman / av1ify で「直さないと決めた」ものの記録 (今回の却下と重複していないことを照合済み)
- [issue 318](done/318-research-dead-code-and-broken-code-audit-2026-09-06.md) / [issue 324](done/324-research-performance-audit-2026-09-06.md) — 同じ形の監査記録 issue の前例
