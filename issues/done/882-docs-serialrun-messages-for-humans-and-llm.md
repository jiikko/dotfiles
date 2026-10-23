# 882 (docs): serialrun が人と LLM に見せる表示と文書

起票日: 2026-10-23

## 概要

serialrun の監査で見つかった、グループ E の 10 項目。

- 監査: 2026-10-22〜23 に audit skill の 22 観点を 1 体ずつ直列で回した (sonnet・読み取りのみ。直接実行)。全数勘定と、直さないと決めたものの理由は [886](../886-research-serialrun-audit-2026-10-23.md)
- 設計の正本: [869](../869-feat-serial-run-machine-wide-heavy-command-queue.md)。下の `869:NNN` は 869 の行番号 (2026-10-23 時点)
- 各項目の「確認済み / 推測」は監査の報告の申告。直す前に実コードで裏を取る (`~/.claude/CLAUDE.md`「主張は証拠ではない」)
- 出典の「観点名 P2-1」は監査の報告の番号 (報告そのものは tmp に置いた使い捨てで残らない。要点は各項目に写した)

## 項目

### E1 待ちの上限 (既定 1 時間 / rc=125) が、Bash tool の timeout (既定 2 分・最大 10 分) の前に負ける。シグナルで抜けるときは無言
- 重要度: P2 / 一部確認済み (待ち中の INT で出力は無く rc=130 のみ。`waitSignal` 経路に stderr の出力が無い)。Bash tool の timeout の見え方は推測
- 根拠: main.go `defaultMaxWait` と `waitSignal` / hook.go `queueNote` (「run_in_background で起こすか timeout を延ばし」と言うだけで、foreground は最大 10 分で頭打ちなことも、再試行すると札が最後尾に戻ることも書かない)。869:307 も「待ちの上限は前に並ぶ全員の合計で尽きる」と記録済み。再試行が最後尾に戻り、他 session が次々割り込むと LLM の再試行が餓死する形は未実測 (ux U-1)
- 直し方の案: note に「foreground の timeout は最大 10 分。前の本が長いなら run_in_background 一択」「同じ command を再試行すると最後尾に戻る」を書く。signal で抜けるときも 1 行 (`serial-run: 待ちをやめた (実行していない)`)。869:239 の未実測 (中断のシグナル) とセット
- 出典: ux P2-1・U-1 / design U-2

### E2 先頭が詰まったとき、待たされている LLM / 人が取れる手が書かれていない (止め方・`--max-wait`・待ちの上限の文言)
- 重要度: P2 / 文言の欠落は確認済み (コードと実出力)。実際に詰まった日の挙動は推測
- 根拠: main.go `describe` / `printStatus` の `🚨 長く持っている` (その後の指示が無い) / README の止め方は `SERIAL_RUN_HOOK_DISABLE=1` だけで usage・待ちの行・status・hook note のどこにも出ない / `waitTimeout` の Fprintf が `opt.maxWait` を `fmtDur` を通さず出す (実出力 `待ちの上限 (3s)`、1 時間なら `1h0m0s`) / LLM が `serial-run --max-wait 60 -- make test` と書けば短くなる (command に `serial-run` の文字列が入ると hook は包まないので実際に効く未文書の抜け道。B1 を直すと変わる)
- 直し方の案: 🚨 の行と hook note に「持ち主の session が詰まっているならユーザーに知らせる・自分は `--max-wait N` で切り上げられる」と、止め方 (人間向け: SERIAL_RUN_HOOK_DISABLE の置き場) を足す。上限切れの文言を `fmtDur` に通し、末尾に「コマンドは実行していない。もう一度打つか `--max-wait 0` で待ち続けられる」。最初の待ち行 (`(状況は serial-run status)` と同じ所) か status の末尾に止め方を 1 行
- 出典: ux P2-2・P3-4・P3-5

### E3 定期の待ち行が毎回 cwd とコマンドを全文繰り返し、1 時間待つと tool の結果が数十 KB になる
- 重要度: P3 / 確認済み (report=3s で毎回全文を出すのは実験。1 時間 = 約 120 行 × 約 150〜250 字 = 20〜30KB は見積もり)。出力の切り詰めで make test の本体出力の頭が削られるかは未確認
- 根拠: main.go `reportPos` (30 秒ごとに `待ち N 番目 (..) 前: <cwd> の <cmd 最大 120 字> (札を出してから ..)`)
- 直し方の案: 最初と順番が変わったときだけ全文、周期の行は `待ち N 番目 (経過)` に縮める (または間隔を 5 分に伸ばす)
- 出典: ux P3-2

### E4 `--help` / `-h` を受けず、help の要求を「未知の引数」として rc=125 で返し、「コマンドの前に -- が要る」と誤案内する
- 重要度: P3 / 読みのみ (ui-components は実行していない。runtimeout も同形)
- 根拠: main.go `parseArgs` の `default:` 節 / `run`。rc=125 は「道具の失敗」の意味。pro-con は `--help` を持つ。「未知の引数」(serialrun) と「未知のフラグ」(runtimeout) の 2 語の揺れも
- 直し方の案: `-h` / `--help` を先に見て usage を stdout に出し rc=0。runtimeout にも同じ commit で入れる (G1 で共有化するなら 1 か所)
- 出典: ui-components P3-3

### E5 `status` の表が、見出しも無く、全角と半角が混ざる state 列をタブで区切るので桁が揃わない
- 重要度: P3 / 推測 (コードを読んだだけ。端末で目で見ていない。`no-mixed-width-columns-in-terminal-ui.md` の形に当たる)
- 根拠: main.go `printStatus` の `Sprintf("%s\t#%d\t%s\tpid %d\t%s\t%s", ...)` (state は `先頭` / `待ち` / `(持ち主なし)`)
- 直し方の案: 見出し行を足す。state 列を全部半角の語 (`head` / `wait` / `dead`) にするか最後の列へ。列を詰める処理を `row(cols ...string)` 1 つに。実際に端末へ出して目で確かめる。D1 の表示を直す commit と同時
- 出典: ui-components P3-2

### E6 先頭の札の説明が 3 通りに書かれ、同じ警告 (長く持っている) の文言も 2 通りある
- 重要度: P3 / 確認済み (読んで。3 つとも `info.Cwd` / `info.Cmd` / `time.Since(info.Start)` を同じ順で埋める)
- 根拠: main.go `describe` (`前: %s の %s (札を出してから %s)` と `🚨 長く持っている: pid %d %s`)・`printStatus` (`🚨 長く持っている ` + procStat、コロン無し)・hook.go `queueNote` (`先頭は %s の %s (%s 前から)`)
- 直し方の案: `holderSummary(info)` と `longHoldNote(info)` を 1 つずつ作り、3 か所から呼ぶ。D8 の修正 (🚨 の判定を実行開始時刻に) を入れる場所と同じなので同時にやる。テストは 1 関数の出力に対して書く
- 出典: ui-components P3-1

### E7 既定値 (lock 名 `default`・待ちの上限 1 時間・rc=125) が hook.go の文面と usage に文字で複写され、定数と食い違うおそれがある
- 重要度: P3 / 確認済み (今は一致。置き場の名前がずれると queueNote は「置き場が無い」に落ちて案内が黙って消える。テストも「無いとき空」を通る)
- 根拠: hook.go `queueNote` (`filepath.Join(base, "default")`・文面の「既定 1 時間」「rc=125」) / main.go `usage` (`既定 3600`・`既定 default`) と `parseArgs` の `lock: "default"` / `defaultMaxWait` / `rcTool`
- 直し方の案: `defaultLock` を const にして parseArgs / queueNote / usage が共有し、文面は `defaultMaxWait` / `rcTool` から Sprintf で作る (`usageText()` に)。hook_test.go に「文面が定数と一致する」を足す
- 出典: leaky-abstraction P3-3 / ui-components P3-4

### E8 README と入口の文書が、実装済みの範囲・効かない経路・前提を書いていない
- 重要度: P3 / 確認済み (README と 869 の突き合わせ、grep の範囲内)
- 根拠: src/serialrun/README.md に、(a) 効かない経路 (端末・git hook・スクリプトの中・codex・pro-con の PG。869 にはあるが README に無い)、(b) 未完了の前提 (新しい session での観測・Makefile 組み込み・中断のシグナルが未実測)、(c) 環境が同じ session 同士だけ (A7)・他ユーザーとの共有は対象外・共有 dir の置き場は安全でない (security U-1)、(d) 孫は対象外 (I2)、(e) 重い 5 系統の deny / ask は `serial-run -- …` の字面でも書く (I1) が無い。`docs/README.md` の索引・`_claude/CLAUDE.md` の hook 一覧にも無く、rc=125 の意味は README のみ。新しい session の LLM が rc=125 を「テストの失敗」と読む
- 直し方の案: README に「効かない経路」と「未実測の前提」の節。dotfiles の CLAUDE.md か docs/README.md の索引に 2〜3 行 (hook が重いコマンドを包む・rc=125 は道具の失敗・止め方)。`new-tool-requires-entrypoint-docs.md` の対象
- 出典: general P3-1・P3-5 (+ concurrency P3-2 / resource-leaks P3-1 / security P2-1・U-1 の「README に書く」案)

### E9 他 session の cwd とコマンド行 (制御文字・ESC シーケンスを含みうる) が、無加工で別 session の LLM の `additionalContext` と、人の端末 (`status`・待ちの行) に出る
- 重要度: P3 / 経路は確認済み (cwd に `\x1b]0;PWNED\x07` を持つ dir で待たせ、`status | cat -v` に `^[]0;PWNED^G` が生のまま出た)。端末ごとの実害 (タイトル書き換え・OSC 52) とモデルが従うかは未確認。単一ユーザー・自分の session 同士なので脅威は弱い
- 根拠: main.go `displayCmd` → `oneLine` (`strings.Fields` で空白を畳むだけ)・`describe` / `printStatus` が `info.Cwd` / `info.Cmd` を無加工で Fprintf / hook.go `queueNote` の head
- 直し方の案: 表示の入口 (displayCmd と cwd の表示の 1 か所) で制御文字を `?` に落とす。history.log への書き込みも同じ関数を通す。札の JSON そのものは書き換えない (identity に使っていない)。`additionalContext` には件数と所要だけを入れて詳細は `serial-run status` に任せる案 (design の案) もある
- 出典: design P3-2 / security P3-2

### E10 表示用の整形の責務が散っている (`oneLine` の二重適用・`displayCmd` が消えた hook の形 `zsh -c '<元>'` を知っている)
- 重要度: P3 / 確認済み (読んで・grep。害は表示だけ)
- 根拠: main.go `appendHistory` の `oneLine(cmd)` (呼び出し元が `displayCmd` 済みを渡す。冪等で無害) / `displayCmd` の `len(argv)==3 && argv[1]=="-c" && base(argv[0]) in {zsh,bash,sh}` (hook.go の `wrapPrefix` は `serial-run -- ` だけで丸ごと包まない設計に変わり、hook はこの形を作らない。`serial-run -- bash -c 'make test'` を人が打つと表示から `bash -c` が消える)
- 直し方の案: 整形は `displayCmd` に寄せ、`appendHistory` は受け取った文字列をそのまま書く (改行を含まないことをコメントに 1 行)。zsh -c の剥がしは分岐ごと消して `strings.Join(argv, " ")` にする (テストと 869 の見え方の記述を直す)。残すならコメントを「人が打った sh -c を剥がす」に直す。E9 の制御文字の落とし先もここ
- 出典: dead-code P3-2 / duplication P3-2 / polymorphism P3-2

---

## 進捗

- 2026-10-23 (commit「fix(serialrun): 待ち・上限・status・hook の案内の文言と文書を直す (issue 882)」):
  - [x] E1 待ちの間のシグナルで抜けるときも「待ちをやめた (シグナル … を受けた。コマンドは実行していない)」を 1 行 (採番の再試行の中も)。
    hook の案内に「foreground の Bash の timeout は最大 10 分。前が長いなら run_in_background。打ち直すと最後尾に並び直す」を足した。`TestSignalWhileWaitingSaysNotRun`
  - [x] E2 上限切れは「待ちの上限 (1 時間 00 分)」の形 (fmtDur。時間の表記を足した) で、「コマンドは実行していない。もう一度打つと最後尾。--max-wait 0 なら上限なし」を添える。
    🚨 長く持っているには「持ち主の session が止まっているならユーザーに知らせる。待つ側は --max-wait で切り上げられる」を添える (待ちの行・status・hook の案内)。
    最初の待ちの行に「待ちをやめるなら Ctrl-C」。hook の止め方は usage と README・docs/README.md の索引に。`TestMaxWaitMessageIsReadable` / `TestFmtDurHours`
  - [x] E3 周期の待ちの行は、順番・先頭の札・その状態 (走っている / 孤児 / 開けない)・長く持っているかが前の説明つきの行と同じなら、先頭の説明を省く (`待ち N 番目 (経過)` だけ。長く持っているときは短い 🚨 を残す)。`TestFirstWaitLineShowsStatusHint` / `TestWaitLineReportsHeadBecomingOrphan`
  - [x] E4 `-h` / `--help` は usage を stdout に出して rc=0 (serial-run と runtimeout の両方)。未知の引数の語は runtimeout に合わせて「未知のフラグ」。`TestHelpPrintsUsage` (2 module)
  - [x] E5 status に見出し (`状態 番号 pid 持ち主`) を足し、全角 2 字の状態・番号・pid の固定の列を先に置いて、長さの揃わない説明を最後にまとめた。
    2026-10-23 に一時の置き場で 2 本並べて端末へ出し、見出しと 2 行の桁が揃うのを目で見た。状態の語は全角のまま (同じ列に半角を混ぜない)
  - [x] E6 `holderSummary` / `longHoldNote` (display.go) を待ちの行・status・hook の案内の 3 か所から呼ぶ
  - [x] E7 `defaultLock` を const にし、usage と hook の案内の既定値・rc は定数から Sprintf で作る。`TestUsageMatchesConstants`・hook の案内のテストも定数で照合
  - [x] E8 README に「効かない経路・前提」の節 (包まれない経路・孫・同じユーザーどうし・foreground の timeout・未実測)。docs/README.md の道具の表に 1 行 (rc=125 の意味と止め方)。
    I1 (許可ルールは `serial-run -- …` の字面でも書く) は README に既にある。**_claude/CLAUDE.md (全 session が読む) には足していない**: 待ちに入ったときと上限で抜けたときの文言が
    rc=125 の意味を言うので、常時読み込みを増やさない
  - [x] E9 表示の入口 (`displayCmd` と `holderSummary`) で制御文字 (C0・DEL・C1) を ? に落とす。history.log の cwd も同じ。札の JSON は書き換えない。`TestStatusDropsControlCharacters` / `TestDisplayCmdIsOneSafeLine`
  - [x] E10 整形を display.go に寄せた。`displayCmd` の `zsh -c` の剥がしは消した (hook はこの形を作らない。人が打った `sh -c` が表示から消えていた)。`appendHistory` は受け取ったコマンドをそのまま書く
- 変異 (全部 red): help を受けない / usage の既定値を手で書く / 制御文字を落とさない / status の cwd を落とさない / 周期の行で全文を出す / シグナルで黙って抜ける /
  上限を素の Duration で出す / runtimeout が help を受けない
- 敵対的レビュー 1 周目 (Opus): P2 1 / P3 5。help の誤読・札の Cmd を判定に使う箇所・go_autobuild の温めは壊せなかった
  - P2 先頭が孤児・開けない札になっても、順番が変わらないと待ちの行に出ない (直す前は出た。再現済み) → 省略を「順番・先頭の札・状態・🚨 が同じ」で判定する。変異 (順番と 🚨 だけで判定) → red
  - P3 status の「hook の最近の判断」が hook.log を生のまま出す → printable を通す / P3 長く持っている間は毎周期全文 → 説明が変わらなければ短い 🚨 だけ
  - P3 README の待ちの行の説明が実装と食い違う・使い方に -h が無い → 直した / P3 `serial-run /bin/echo -h` を「未知のフラグ」と言う → `-` で始まらない語は「コマンドは -- の後に書く」(変異 → red)
  - P3 双方向の上書き (U+202A〜202E・U+2066〜2069) を通す → ? に落とす (変異 → red)。日本語・ZWJ の絵文字・C1 の CSI は壊さないことを確認済み (レビュー)
- 敵対的レビュー 2 周目 (Opus): P1 1 / P2 1 / P3 2。2 本同時・順番・rc は壊せなかった
  - P1 長く持っているかを待ちの行の文面の 🚨 で読んでいた。先頭のコマンド行に 🚨 があると、走り出して 1 秒で「長く持っている」と出て、本物の注意は一度も出ない (再現済み。直す前より悪い)
    → describe が長く持っているかを返す (longHoldNote が空でないか)。`TestWaitLineLongHoldAndEmojiInCommand` (変異: 文面から読む → red)
  - P2 status の hook.log の行で区切りのタブが ? になる (再現済み) → 欄ごとに落とす。`TestStatusHookLogKeepsTabs` (変異 → red)
  - P3 短い 🚨 の行を検査するテストが無い → 上のテストで固定 (変異: 短い 🚨 を出さない → red)
  - P3 **記録**: 説明つきの行を出す 1 回で札が書き換えの途中 (rewriteInfo) だと説明が空になり、その先頭の説明は以後出ない (推論のみ。直す前も同じ)
- 敵対的レビュー 3 周目 (Opus): P1 / P2 なし (壊せなかった)。直す前より良くなったことを変異 4 本で確かめた (レビュー側のコピーで)
  - P3 **記録**: 待っている途中で先頭が 30 分を越える形 (キーの「長く持っているか」が変わる) をテストが通らない。`long` をキーから外す変異が緑のまま通る (再現済み)。
    longHold を縮める差し込み口が無いので、テストは最初から 30 分を越えた札で始めている。直す前からの穴。テストは周期の行の 2 行目だけを見ている
