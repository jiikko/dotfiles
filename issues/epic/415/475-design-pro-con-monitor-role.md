# 475 (design): 見張りの係 — dispatcher とは別のプロセス (pro-con monitor) で見張り、判断が要るときだけ使い捨ての haiku

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの提案 (2026-09-25): 利用枠が 80% に達して dispatcher が同時に動かす PG を 1 本に絞ったのを見て、「ratelimit に合わせて消費を抑えるムーブはいいね。
dispatcher なのか、monitor ロール (haiku) あたりを常駐して定期的にチェックさせてもいいかもね」。
2026-09-25 の夜、PM (人間の代わりの Claude) は使い捨ての見張りのスクリプト (カードの状態の変化・`git merge-tree` で master と PG どうしの取り込みの衝突・
PG ごとの最新の commit を 60 秒ごとに見て、変わったら PM を起こす) を手で回していた (440 の 5 回目)。

## 分け方 (叩き台)

1. **決まった手順で判定できるものは dispatcher** (トークンを使わない。今の利用枠の絞り (stage 5) と同じ形):
   カードの状態の変化 / 取り込みの衝突 (commit 済みの分を `git merge-tree` で。master と、作業中どうし) / 停滞 (watchdog) / テストの順番の長さ / 枠の枯れ。
   結果は出来事 (444 の events.jsonl) に出し、画面・PM・外の Claude (441) が読む
2. **判断・要約が要るものは monitor 役 (haiku)**:
   - 進み具合の要約 (「C-009 は変異の検証の 21 本目・あと 5 分ほど」。467 / 469 / 473 の中身を人向けに 1 行へ)
   - テストの失敗が、その変更のせいか・負荷で落ちただけか・前から落ちるかの一次判定 (2026-09-25 の C-010 の tmux のテスト = 単独では緑。471)
   - 人が見るべきことの知らせ (人の番 (452)・止められない PG・枠の枯れ・同じ判断を変えるカードが並んだ (468))
   - 今のテストの係の要約役 (`dispatcher/runner.go` の `HaikuSummarize`。426 の決定 5) と同じ系列の役として置く
- 🚨 常駐と起動のコスト: 1 回の起動で約 5 万 token の固定の文脈 (449 の実測)。決まった間隔で起こすより、**dispatcher の出来事をきっかけに起こす** (1 の結果が変わったときだけ) 方がよい。
  利用枠が高いときは monitor 役を起こさない (stage 5 の絞りに含める)
- monitor 役は読むだけ (カードの記録・受付の箱・PG に書かない。知らせは出来事として dispatcher が書く)。`--setting-sources` と `--no-session-persistence` で軽く起こす (460 の P3: 要約の係の transcript が残る件も合わせて直す)

## 決めたこと (2026-09-26)

人間の決定: **見張りを dispatcher とは別のプロセスにする**。残りは「あとはいい感じに」と任され、取り込みの係の session が次の形に決めた
(1〜3 は前の版で推していた案のとおり)。

1. **形**: 別プロセスの `pro-con monitor`。dispatcher の Tick の中では判定しない
   - 重い操作 (`git merge-tree`) を dispatcher の Tick から外す。見張りが落ちても dispatcher と PG は止まらない
   - 見張りは読むだけ (`cards.json`・`events.jsonl`・git)。カードの記録には書かない (書き手は dispatcher だけ = 426 の決定 1)。
     見つけたことは受付の箱に置き、dispatcher が新しい種類の出来事として書く
   - 起こすのも止めるのも dispatcher (dispatcher と一緒に動く)。二重に起動しないよう lock を取る
   - LLM は常駐の claude session にしない。要るときだけ使い捨ての `claude -p --model haiku` を起こし、`--no-session-persistence` を付ける
2. **最初の範囲**: 段 1 の 2 つ (取り込みの衝突の検出・テストの順番の長さ) を monitor で作る。テストの失敗の一次判定は、dispatcher のテストの係の要約役
   (`HaikuSummarize`) を広げて行う (起動の回数が増えない)。468 の知らせと進み具合の要約は後にする
3. **枠**: 80% で進み具合の要約を止め、失敗の一次判定は 95% まで続ける。monitor の haiku も dispatcher の利用枠の判定を読んで止める

前の版で「role.go の役には載せない」とした理由 (鍵がカード / モデルを渡す口が無い / 常駐すると会話が伸びて毎回読み直す (449 の 2) /
worktree が要らない) は、別プロセスにする決定でもそのまま当てはまる。

## 設計

### プロセスの形

| | dispatcher | `pro-con monitor` |
|---|---|---|
| 書く物 | `cards.json`・`events.jsonl`・様子のファイル | 受付の箱だけ (`store.Submit`。依頼の種類 `monitor`) |
| 読む物 | すべて | `cards.json` (`store.Load`)・設定の repo・各 PG の worktree の git |
| lock | `dispatcher.lock` | `monitor.lock` (取れなければ何もせず抜ける = 二重に起動しない) |
| 間隔 | 3 秒 (起こされたらすぐ) | 60 秒 (git の結果は HEAD の組が変わったときだけ出し直す) |

- **起こし方**: dispatcher (本物のモード。`--once`・e2e では起こさない) が、lock を取って d の欄を書き終えた後 (serve の直前) に、
  自分と同じ実行ファイルで `pro-con monitor --until-stdin-closes` を子として起こす (`monitorsup.go`)。
  子が抜けたら 10 秒後に起こし直すが、30 分の間に 3 回を超えて落ちたら起こし直さず、出来事 (`monitor`) にする (見張りが落ち続けても dispatcher は回り続ける)
- **止め方**: dispatcher が抜けるとき (ctx の取り消し・serve が返った) に、見張りの stdin のパイプを閉じて SIGTERM を送り、終わるのを待つ (5 秒で kill)。
  止める途中に抜けた見張りは落ちたと数えず、起こし直さない
- **dispatcher が kill -9 で死んだとき**: 見張りの stdin は dispatcher が握るパイプの読む側なので、OS が書く側を閉じ、見張りは EOF ですぐ抜ける (macOS には PDEATHSIG が無い)。
  前の見張りが抜ける前に次の dispatcher が起こした見張りは、`monitor.lock` が取れずに**終了コード 3** で抜ける。これは落ちたと数えず、30 秒ごとに起こし直す (出来事は 1 度だけ)
  - 🚨 親の pid を見る案は採らない (60 秒ごとの確かめの間に次の dispatcher の見張りが lock で抜け、落ちたと数えて 3 回で止まる。設計レビューの P1)
  - 🚨 dispatcher の lock を取って生死を確かめない (lock を取っている一瞬に起動した dispatcher が ErrRunning で落ちる。460 の P3 と同じ罠)
  - 🚨 `dispatcher.lock` を子へ渡さない (Go の開くファイルは CLOEXEC。`ExtraFiles` に入れると、死んだ dispatcher の lock を見張りが握り続ける)
- **知らせ方**: 箱に置く依頼 `monitor` は、カードの記録を変えない (画面の出来事 `event` と同じ扱い)。dispatcher の Apply が受け取り、出来事の種類 `monitor` として
  `events.jsonl` に書く (カードの ID が付いていれば、出来事にもその ID を付ける)。`store.Pending` の「適用待ち」には数えない (`event` と同じ理由)
- **変わったときだけ置く**: 見張りは前に知らせた結果をメモリに持ち、「現れた」「消えた」の 2 つだけを箱に置く。見張りを起こし直すと、残っている衝突をもう 1 度だけ知らせる (記録を持たない分の割り切り)

### 段 1-a: 取り込みの衝突

- 対象: 片付けていない、分解済み (再開待ち)・作業中・質問待ち・レビューの列のカードで、repo が設定にあり、PG の worktree (`<repo>/.claude/worktrees/pc-<id>`) が在るもの
- **commit 済みの分だけ**を見る: worktree の `HEAD` を取り、`origin/HEAD` (無ければ `origin/master` → `origin/main`) の祖先なら (まだ commit が無い) 見ない
- master との衝突: `git -C <repo> merge-tree --write-tree --name-only --no-messages <基点> <HEAD>`。rc 1 が衝突で、衝突したファイルの名前を出来事に書く
- PG どうしの衝突: 同じ repo の対象のカードの組ごとに、HEAD どうしを同じく `merge-tree` にかける (共通の祖先は git が選ぶ)。
  どちらかが master と衝突している組は見ない (master を取り込めば組の結果も変わる。同じ衝突を二重に知らせない)
- git が rc 0 / 1 以外で失敗した組 (unborn・object が無い) は飛ばし、見張りのログに 1 度だけ出す。取り込む先が読めない repo は、前に知らせた衝突を「消えた」にしない
- 🚨 `git fetch` はしない (見張りは読むだけ)。`origin/master` は、取り込みの係が worktree から `git push origin HEAD:master` するたびに手元で更新されるので、見張りが知りたい鮮度は保たれる
- `merge-tree --write-tree` は object database に tree を書く (ref は動かさない。後の gc で消える)。作業ツリー・index・ref には触らない
- 同じ (基点, HEAD) の組の結果は覚えておき、HEAD が動くまで git を呼ばない

### 段 1-b: テストの順番の長さ

- dispatcher (テストの係) が列に並べて順番を書いたカード (`Wait` が WaitResource で、リソースが `テスト`) の本数と、先頭 (Position 1) の頼んでからの時間 (`RunAt` から)。
  列の組み方を見張りで真似ない (削除中のカードを外す・pro-con の外が repo の lock を持っている 1 本を別の名前で待たせる、は runner.go だけが知る)
- 「長い」は、**3 本以上か、先頭が 30 分以上待っている**とき (426 のやり方どおり、既定値で始めて動かしながら直す)。長くなったら 1 度、戻ったら 1 度知らせる

### テストの失敗の一次判定 (dispatcher の要約役を広げる)

- `HaikuSummarize` に渡す材料を、ログの末尾だけから 3 つに増やす (`dispatcher/triage.go`):
  - ログの末尾
  - そのカードの変更: 実行した場所 (PG の worktree) の `git diff --stat <取り込む先との分かれ目>` (作業ツリーまで = commit 前の変更も入る) と未追跡のファイルの数
  - 同じコマンドの最近の結果: dispatcher のメモリに、テストの係が実際に走らせて rc が出た分だけを覚える (50 本まで。材料には同じコマンドの新しい 5 本)。
    実行できなかった・途中で止めた (rc -1) と、rc が lockman のものかもしれない (122 / 125) は入れない。履歴の文は拾わない (コマンドが 60 文字で切られ、rc -1 も同じ形で残る)
- 要約の 1 行目に判定を 1 つ書かせる: `この変更のせい` / `負荷・環境で落ちた見込み` / `前から落ちる見込み (ほかのカードでも同じ落ち方)` / `判定できない`。
  材料に無いことは推測させない
- 🚨 **判定は証拠ではない** (「見込み」)。PG の結果の文に要約と並べて渡すだけで、dispatcher は判定を見て動きを変えない
- 枠: テストの係が実行を始めるときに、dispatcher の枠の判定 (`capacity`) が 0 (95% 以上 = 新しく起動・再開しない) なら、その実行の要約を作らない。
  判定は Tick の中で取る (要約は実行の goroutine で走るので、そこから枠の値を読まない)
- 同じコマンドの最近の結果と枠の判定は Tick の中で `runJob` に写してから実行の goroutine に渡す (goroutine から dispatcher の欄を読まない)
- `haiku()` (要約・btw の共通の口) に `--no-session-persistence` を付ける (460 の P3。1 回あたり約 200KB の transcript が残っていた)

### 後回し

- 468 の知らせ (同じ判断を変えるカードが並んだ) / 進み具合の要約。作るときは monitor が使い捨ての haiku を起こし、`dispatcher-state.json` (dispatcher の様子) の
  `max(UsageSession, UsageWeek)` と `UsageAt` の鮮度で 80% 以上かを判定して、進み具合の要約を止める
  (`Cap` では判定しない: 上限がもともと 1 なら、80% の絞りの 1 と見分けられない。usage.go の capacity)
- haiku 1 回あたりの固定の文脈の実測 (進み具合の要約を作るときに要る。一次判定は起動の回数が増えないので、先に作ってよい)

## 進捗

- [x] 設計のレビュー (opus 1 本)。採った指摘: kill -9 の後に見張りが永久に止まる (P1。親の pid → パイプの EOF と終了コード 3) /
  一次判定の材料が commit 前の変更を含まない (P1) / 止める途中の起こし直し (P2) / テストの順番を自前で数えると実際の列とずれる (P2) /
  履歴の文から結果を拾う形が脆い (P2) / `Cap` では 80% を判定できない (P2。後回しの節に書いた) / merge-tree の失敗と組の二重の知らせ (P3) / git diff の時間の上限 (P3)。
  採らなかった: 1 周ぶんの知らせを 1 件にまとめる (衝突が現れる・消えるのは少ないので、1 件ずつのまま)
- [x] 実装: `monitor/` (判定と git の読み取り)・`monitorcmd.go` (`pro-con monitor`)・`monitorsup.go` (dispatcher が起こして止める)・
  受付の箱の `monitor` と出来事の `monitor`・`dispatcher.LockMonitor`・`dispatcher/triage.go` (一次判定の材料と prompt、枠)・`haiku()` の `--no-session-persistence`・README
- [x] テスト 20 本 (monitor 8・起こし直し 5・一次判定 5・store 1・出来事 1。既存の 2 本 = 適用待ちの数・haiku の引数を広げた) と変異 31 本すべて red
- [ ] 本番の dispatcher で、見張りが起き、衝突が出来事 (`pro-con log`) に出ることを確かめる
- 未テスト: dispatcher を kill -9 したときに見張りが抜けること (パイプの EOF。起こし直しの係のテストは「止めるときに stdin を閉じる」まで) /
  `IsAncestor` で commit の無い・取り込み済みのカードを飛ばすこと (飛ばさなくても merge-tree は衝突なしを返すので、結果は変わらない。git の呼び出しを減らすだけ)

## 関連

- 426 (決定 5・6: テストの係と要約役・役割) / 444 (出来事の記録) / 449 (起動のコスト) / 455・467・469・473 (見せる中身) / 468 (順番) / 471 (負荷)
