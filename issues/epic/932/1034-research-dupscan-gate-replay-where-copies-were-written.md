# 1034 (research): gate は写しを止められるか — 写しが書かれた commit で gate を回し直して数える

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-05〜）

起票日: 2026-11-05

## 概要

gate は 2026-10-29〜11-05 の手元の 106 回で、重複を 1 回も止めていない。「写しを書いていない」のか「書いた写しを拾えていない」のかを、
946 で寄せた写しが**書かれた commit** の木で gate と `near -similar` を回し直して切り分ける (1009 の前提 2・3)。測った結果で gate の下限を変えるのは、ユーザーの指示 (2026-11-05「issueにして今やって」。gate の検出力を上げる案への返事) による。

## 測り方 (測る前に決めた。2026-11-05)

- 母集合: 946 で寄せた 21 件のうち、関数の名前で指せる 12 組 (起票時に「13 組」と数え違えた。基準の「7 組以上」は 12 組の過半として据え置く) (lockman `fmtSize`/`fmtAge`・`captureStderr`/`captureStdout`、mac-lan-manager `or` ×2・`DefaultPath` ×2、
  ratelimit `cachePath`/`stampPath`・`paceAdviceUnit`/`paceCellUnit`・`padRight`/`padLeft`・`readStatuslineRaw`/`readWriters`、restartable `envDuration` ×2・`shellQuote`/`ptyShellQuote`・`shortTempDir`/`shortSocketDir`・`outputText`/`text`)。
  クロージャの 3 組 (「Device / Host / 経路の IP が同じ」) と、どの手段も出さなかった 3 件は名前で指せないので外す
- 各組で、後から書かれた側が入った commit C を `git log -S 'func <名前>('` で特定し、今の dupscan で `gate -head C -base C^` を回す (C の木を checkout せずに読む)。
  既定 (L1,L2・下限 20)・`-min-nodes 12 -min-closure-nodes 12`・`-levels L1,L2,L1m` の 3 通り。あわせて C の木で、書かれた側を起点に `near -similar` を当て、相手が上位 3 件に入るかを見る
- 採否の基準: 既定の gate が書かれた時点で止めた数を「今の検出力」とする。変える候補 (下限 12・L1m・near) は、**13 組中 7 組以上を書かれた時点で出し、かつ**
  直近 30 commit (src の Go を触ったもの) で寄せるべきでない組による止まりが 3 commit 以下のときだけ、gate の変更を提案する (988・1009 の保留を解くかの判断材料。解くのはユーザー)

## 結果 (2026-11-05)

### 書かれた時点で何が出せたか (12 組)

書かれた commit は `git log -S 'func <名前>('` で引き、移動だけの commit (ratelimit の `e3d03fb5f` は glogx からの移動) は移動の前まで遡った。
近似の `near -similar` は、相手が 1 位に出たかを「ファイル:行」で照合した (出力は名前でなく行を返す)。

| # | 書かれた側 (commit) | 相手 | 既定の gate | 下限 12 | L1m | near 1 位 (Dice) |
|---|---|---|---|---|---|---|
| 1 | lockman `fmtAge` (67d7167f4。2 つとも同じ commit) | `fmtSize` | — | ✓ | — | — (候補 0) |
| 2 | lockman `captureStdout` (304094f03) | `captureStderr` | — | ✓ (部分のクロージャの一致) | — (L1m は報告だけの段で、組は出たが止めない) | ✓ 0.64 |
| 3 | mac-lan-manager `dump.go:or` (8febc13d8) | `lanmgr/model.go:or` | — | ✓ | — | ✓ 1.00 |
| 4 | mac-lan-manager `prefs.DefaultPath` (228c12ca6) | `ledger.DefaultPath` | ✓ | ✓ | ✓ | — (候補 0) |
| 5 | ratelimit `stampPath` (893be88c6。glogx の時代) | `cachePath` | — | ✓ | — | — (候補 0) |
| 6 | ratelimit `paceCellUnit` (c0346f346) | `paceAdviceUnit` | — | ✓ | — | ✓ 0.67 |
| 7 | ratelimit `padLeft` (007f6c1ed) | `padRight` | — | — | — | ✓ 0.51 |
| 8 | ratelimit `readWriters` (294aff57e) | `readStatuslineRaw` | — | — | — | ✓ 0.69 |
| 9 | restartable ui `envDuration` (3c880af92) | runner `envDuration` | ✓ | ✓ | ✓ | ✓ 1.00 |
| 10 | restartable `ptyShellQuote` (3c880af92) | `shellQuote` | — | ✓ | — (同じ commit の 9 で止まっただけ) | ✓ 1.00 |
| 11 | restartable `shortTempDir` (a256e72f3) | `shortSocketDir` | — | — | — | ✓ 0.67 |
| 12 | restartable `outputText` (32dfd5983) | `text` | ✓ | ✓ | ✓ | ✓ 1.00 |
| | **計** | | **3** | **9** | **3** (止めたもの) | **9** (0.6 以上なら 8) |

- **既定の gate は、書かれた時点で 12 組中 3 組しか止めていなかった**。ただし 12 組は 946 で後から見つけた写しで、どれも gate の記録 (10-29〜) より前に書かれた。書かれた写しの母集団ではない
- gate の 0 回 (948) と同じ時期の直接の証拠: 10-29〜10-30 の 3 commit (20c67825c・e23b45849・ef6c9fa62) は下限 12 で止まり、既定では 3 本とも `failed: false` だった。**少なくともこの 3 commit では、既定の gate が写しを素通りした**。0 回のうちどれだけが素通りかは数えていない
- 下限 12 と near の和集合で 12 組すべてが出る (互いに拾えない組を補い合う: 1・4・5 は near が 0 件、7・8・11 は下限 12 でも小さすぎる)
- 回し直しは今の dupscan で `gate -head C -base C^ <module>`。canary: 1 の commit で scope を `src` にしても下限 12 で rc=1

### 雑音 (止めるべきでない組による止まり)

- 直近 30 commit (src の Go を触ったもの。変わった単位 115): 既定・下限 12 とも止まり 0
- 基準の外で、さらに 100 commit (変わった単位 1081) を下限 12 で回した: 止まり 3 commit・4 組。読んで仕分けた:
  - 20c67825c treefiler `statusLine` ~ `lastRow` (同じ package のテストで本文が同一): **寄せるべき**
  - 20c67825c treefiler `load_test.go` ~ mac-lan-manager `setup_test.go` の `once.Do(func() { close(gate) })` のクロージャ (module をまたぐ 1 行): **雑音**
  - e23b45849 restartable runner ~ ui のテストの待ちの `Diag` クロージャ (外側の `signalAndWait` / `waitPTYRunner` ごとほぼ同じ。寄せ先の `internal/runnertest` がある): **寄せるべき**
  - ef6c9fa62 mac-lan-manager `joinKeys` ~ `dropEmpty` の空を落とす 1 行の述語のクロージャ (同じ package。`joinKeys` は `dropEmpty` で書ける): **寄せられる (小さい)**。雑音に数える読み方もある
- 雑音だけで止まった commit は 0 (ef6c9fa62 を雑音に数えると 1)。100 commit は基準の外の追加の標本。所要は 1 commit あたり 0〜1 秒で、既定と変わらない

### 判定と対応

- 基準 (12 組中 7 組以上・雑音の止まり 3 commit 以下) を下限 12 は満たした (9 組・直近 30 commit で 0)。L1m (3 組) は満たさない。near は gate の段ではないので、この issue では入れない (988・1009 の扱い)
- **make と CI の gate の下限を 12 節にした** (`Makefile` の `DUPSCAN_GATE_MIN_NODES`)。dupscan 自身の既定 (20) は Swift (DualNoteApp・obaket) の較正なので変えない (この計測は dotfiles の Go だけ)
- 素の `dupscan gate` と make の判定が食い違うので、`src/` の Go を書くときの hook の案内を `make test-dupscan-gate` に変え、CLAUDE.md と `docs/dupscan-overview.md` に下限を書いた
- 確認: 同じ package に `lastRow` の写しを植えると、新しい target は止め (L1、make は Error 1)、既定の下限では素通りした (rc=0)。hook のテストは、案内を素の `dupscan gate` に戻す変異で 5 件赤
- 反証レビュー (2026-11-05、sonnet、読み取りのみ): P2 4 件・P3 3 件を採用 (L1m の数え違い 4→3・雑音の仕分けの読み方・「主」の言い過ぎを直接の証拠に置き換え・gate を変えた根拠の記録・入口の二重の案内・記録の条件の混在・skill の記述)。反証できなかった: 書かれた commit の特定・既定 3 / 下限 12 9 / near 9 の数・30 commit の止まり 0・CI での挙動 (flag は後勝ちで -base と衝突しない)・記録の読み手
- 敵対的レビューは省略した (設定値の変更だけで、効き目は上の実測で確かめた)
- root の `make test` rc=0

### 残り

- [ ] 100 commit の仕分けで見つけた寄せるべき写し 3 組が HEAD に残っているかを見て、残っていれば寄せる (gate は増えた分しか見ないので、下げても既存の写しは止めない)
- [ ] 7・8・11 (下限 12 でも小さい) と 1・4・5 (near が 0 件) は、和集合でしか拾えない。near を gate に入れるかは 988・1009 の再開の条件 ② が今回満たされたので、ユーザーの判断で再開する
- [ ] Swift (obaket) で同じ回し直しをするか: このマシンに checkout が無い
- [ ] 948 の ① の指標に、gate の止まりの数 (E-2) を 11-25 の再測定で足す。`~/.cache/dupscan-gate.jsonl` は 2026-11-05 の push までが下限 20、以後が 12 (`conditions.min_nodes` で分ける)。前後を同じ分母で比べない
- [ ] skill dupscan の 3 節は「make / CI と同じ判定」と書き、素の `dupscan gate` を示す。repo を問わない skill なので変えず、dotfiles の入口 (CLAUDE.md・hook) で `make test-dupscan-gate` を案内した。他の repo でも make が flag を足すなら skill に一般形を書く
