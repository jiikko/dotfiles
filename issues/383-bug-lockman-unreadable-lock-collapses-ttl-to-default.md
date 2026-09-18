**着手中 (2026-09-18 / Claude セッション)**: 残タスクの §7 5 周目の敵対レビューを実施中。

# 中身を読めない lock は実効 TTL が宣言値から 30m へ縮む — 生きている lease を正規手順で奪える

起票日: 2026-09-16
カテゴリ: bug / priority: **high**
対象: `src/lockman/lock.go` の `holderTTL` / `tryTakeover` / `takeoverGeneration` のコメント
出典: [381](done/381-bug-lockman-with-renew-latch-stops-renewal-forever.md) の敵対的レビュー (観点① 壊す)
反証レビュー: 4 周実施済み (2026-09-16。下記)

## 問題

`readLock` は中身を parse できないとき `(nil, mtime, errBusy)` を返す (`lock.go:185-188`)。
`tryTakeover` は **`errBusy` を明示的に握り潰して `m == nil` のまま先へ進む**
(`lock.go:377-379` と 2 段目の `lock.go:417-419`)。その先の生死判定は
`holderTTL(m)` を使うが、`holderTTL(nil)` は **`defaultTTL` = 30m** へ倒れる
(`lock.go:204-209`)。

つまり **中身を読めなくなった lock は、保持者が `--ttl 2h` と宣言していても 30 分で
期限切れ扱いになる**。保持者はまだ生きていて子も走っているのに、別マシンが**正規手順で**
引き継げる = 二重実行。宣言 TTL − 30m がそのまま fail-open の幅になる
(`--ttl` に上限の検証は無い。`main.go:200` が見るのは下限 `minTTL` だけ)。

`README.md` が「生死の判定に保持者の宣言でない TTL を使うと他人の lease を早期に奪える」を
不具合として記録しているのと**同じクラス**。経路が「奪う側の `--ttl`」ではなく
「`m == nil` の fallback」なだけ。

## 366 の 2 段構えが効かない

0 バイト状態は**安定している**ので、2 段目の再照合 (`lock.go:417-424`) は 1 段目と
**同じ** `(nil, 同じ mtime, 同じ gen)` を見て同意する。競合の窓ではないので、
窓を狭める対策はどれも当たらない。

## 誰も修復しない

- 掃除は lock 本体を対象にしない (`cleanup.go`)
- 保持者自身の次の `Renew` も `readLock` が `errBusy` を返して即 return する (`lock.go:523-526`)
- `Release` も同じく `errBusy` で返るので lock を消せない (`lock.go:488` 近辺)
- **保持者は奪われたことを構造的に知れない**: 奪取後に読むのは*自分が壊した* lock なので、
  返るのは `errNotOwner` ではなく `errBusy` → `classifyRenewErr` は判定不能 →
  `--on-lost=warn` では子が最後まで走り切る

## 「中身を読めない lock」はどう生まれるか

1. **`Renew` の `O_TRUNC` の後で詰まる** (`lock.go:541`)。truncate だけ済んで `f.Write` へ
   進めないと、lock は **0 バイト + truncate 時刻の mtime** で安定する。
   [380](380-bug-lockman-renew-and-release-act-on-name-after-check.md) が提案している
   「temp へ書いて rename」に直せばこの窓は消える
2. `readLock` の **`os.Stat` と `os.ReadFile` は別の syscall** (`lock.go:169` / `lock.go:180`)。
   あいだに truncate が入ると `(truncate 前の mtime, 壊れた本文)` を観測する。
   これは **SMB 無しで、ローカルでも成立する**

🚨 2 は `takeoverGeneration` のコメント (`lock.go` の「到達可能な非決定性は潰れている …
SMB の属性キャッシュが『古い mtime + 新しい中身』を返す環境は未確認リスク」) を**反証している**。
`O_TRUNC` が mtime を現在へ進めても、`holderTTL(nil)` が 30m へ縮むので
「gen の計算に届く前に expired で弾かれる」は宣言 TTL > 30m では成立しない。

## 381 との関係

381 の修正前は、`with` の更新は**同時 1 本**しか存在しなかった (期限切れのあとも `renewCh` を
握っていた)。381 は期限で見捨てて張り直す形にしたので、`Renew` が `O_TRUNC` まで到達する
機会が最大 `maxInFlightRenews` (既定 8) 倍になった。しかも 381 が救おうとしている形
(「パスは通るが古い fd は死んでいる」) が、**新しい `Renew` が `readLock` / `serverNow` を
通過して `O_TRUNC` まで届く条件そのもの**。381 は上限を下げれば緩和できるが、根治は本 issue 側。

## 直し方の候補

- **(a) `holderTTL(nil)` を fail-open にしない**。中身を読めない lock は「期限切れ」と
  判定しない (人が `break` で剥がす)。永久 wedge のリスクと引き換え。
  `holderTTL` のコメントが「0 にすると即座に奪える (危険)、無限にすると永久 wedge」と
  書いており、**30m が安全な中庸だという前提自体が誤り**だったことになる
- **(b) 380 を直して 0 バイト窓を消す** (temp + rename)。1 は消えるが 2 (torn read) は残る
- **(c) `readLock` を 1 つの open で済ませる** (`os.Open` → `Fstat` → `ReadAll`)。2 が消える
- おそらく (a) + (b) + (c) は独立に効くので、全部やるのが正しい

## 進捗 (2026-09-16)

**採用したのは (a) + (c)。(b) は 380 の領域なので触っていない** (ユーザー判断: fail-closed)。

| 候補 | 判定 | 実装 |
|---|---|---|
| (a) `holderTTL(nil)` を fail-open にしない | **採用** | `tryTakeover` の 1 段目・2 段目で **`m == nil` を先に弾く** (`lock.go`)。`holderTTL` 自身は変えない (`TTLMillis <= 0` の既定は、有効な Meta の古い形式のために要る。Release / Renew / Status からは `m != nil` しか来ないことを全数確認した) |
| (b) 0 バイト窓を消す (temp + rename) | **やらない** | [380](380-bug-lockman-renew-and-release-act-on-name-after-check.md) の領域。(a) が入ったので、窓が残っていても**引き継がれない** |
| (c) `readLock` を 1 つの open で済ませる | **採用 (ただし射程は本文の記述より狭い)** | `os.Open` → `Fstat` → `io.ReadAll` に変えた |

### 🚨 (c) の射程は「2 が消える」ではなかった (実測 2026-09-16)

本 issue は (c) で「torn read (2) が消える」と書いていたが、**truncate には効かない**。
同じ fd でも、`Fstat` の後に外から `O_TRUNC` されれば `io.ReadAll` は 0 バイトを返し、
**「古い mtime + 0 バイト」は同じように観測できる** (probe で実測: 読めたバイト数 0 / mtime は
truncate 前のもの)。

(c) が実際に消すのは**差し替え (temp + rename) のときの食い違い**だけ:
2 syscall 版は「古い mtime + 新しい中身」を返せたが、1 つの open なら**同じ inode** の組で返る
(同 probe: rename 後も開いた実体の中身が返る)。

したがって **truncate 由来の torn read を無害化しているのは (a) の fail-closed** であって
(c) ではない。コード側のコメントもこの形に直した (誤った理由をコードに残さない)。

## 結果

- 実装: `lock.go` — `tryTakeover` の 2 段に fail-closed、`readLock` を単一 open 化、
  `readLockAfterStatHook` (テスト用 seam) を新設
- テスト 3 本を新設 (`unreadable_lock_test.go`。**最終的には 13 本**。周回で足した分は各節に記載)。
  変異検証はケースごとの PASS/FAIL で判定:

  | 変異 | 結果 |
  |---|---|
  | fail-closed の弾きを外す (旧挙動) | `TestUnreadableLockIsNeverTakenOver` red |
  | `readLock` を 2 syscall へ戻す | `TestReadLockReturnsMtimeAndBodyFromSameFile` red |

  🚨 **最初に書いた torn read のテストは vacuous だった** (静止状態で mtime と中身が
  一致することしか見ておらず、2 syscall へ戻す変異が全緑で通った)。seam で
  「読んでいる最中に差し替える」状態を作る形へ書き直して red になった。
- 対照: 中身が**読める**期限切れ lock は従来どおり引き継げる (fail-closed が「常に引き継げない」へ
  倒れていないこと)。`TestBreakRemovesUnreadableLock` で復旧路 (`break`) も固定した
- `go test -race ./...` 緑 / `make test` は EXIT=0 / 83 件報告 / 失敗 0

## 敵対的レビュー (2026-09-16。全数勘定)

指摘 9 件、**採用 5 / 記録 3 / 却下 1**。🚨 **P1 は「私の修正が作った新しい失敗モード」**だった。

| # | 指摘 | 判定 |
|---|---|---|
| P1 | fail-closed は「自動復旧する詰まり」を**無言の恒久 wedge**に変えた。しかも代償として設計した導線 (`lockman break`) が **production のどの出口にも出ない** (`cmdAcquire` は errBusy を**無出力**で `exitBusy` にし、`with` は定型文)。既定 `--ttl 30m` では旧挙動が**正しく**自動復旧していた | **採用**。①`acquire` / `with` が busy の**理由を stderr に出す**ようにした (待ち直す枝では出さない) ②`tryPlace` の O_EXCL fallback が**自分で作った 0 バイト lock を消す**ようにした (この枝は `link(2)` が使えない smbfs = 本番でしか通らず、テストから 1 行も走っていなかったので seam を足した) |
| P2-2 | `TestUnreadableLockIsNeverTakenOver` の red を担っていたのは `strings.Contains(err.Error(),"break")` で、**その文字列は production に 1 バイトも出ていなかった**。安全性 assert は 2 段目が止めるので全部通る | **採用**。CLI 境界 (`run([]string{"acquire", ...})` の stderr) で案内が出ることを固定するテストを足した |
| P2-3 | 2 段目の `m2 == nil` は**等価変異** (削除しても全緑)。局所 FS では直後の世代照合が先に止め、効くのは SMB 属性キャッシュ + 非 hex token のときだけ | **記録**。到達不能な枝のテストは足さない。「2 段目は backstop」と書き分ける (下記) |
| P3-3 | 「`Break` は必ず効く」は言い過ぎ。`graveyard` が通常ファイルだと `break` も `acquire` も rc=1/3 で**どのコマンドでも復旧できない** | **記録**。射程を「中身を読まない」までに限定して書く |
| P3-2 | seam のテスト間干渉 (`once` を別の `readLock` が先に消費すると自明に緑) | **記録・未確認**。現状 `t.Parallel` は 0 件で到達経路が見当たらない |
| — | SMB 属性キャッシュ / pid 再利用の再現 | **却下 (再現手段が無い)**。推測で防御を足さない |

**壊せなかった経路** (レビューが実験で確認): `errBusy` をラップしたことによる呼び出し元の破綻は無い
(`== errBusy` の同一性比較は package 内 0 件、`errors.Is` が全経路で生きている) /
正常系の引き継ぎ (中身が読める期限切れ lock) は壊れていない /
`IsDir` の枝は fail-closed を迂回するが、続く `tryPlace` が EEXIST で errBusy に落ちるので安全側。

### 2 段目の位置づけ (P2-3 の書き分け)

1 段目 (`m == nil`) が本体で、**2 段目 (`m2 == nil`) は backstop**。局所 FS では 2 段目を消しても
直後の `takeoverGeneration(m2, mtime2) != gen` が止めるので**挙動が変わらない** (= 等価変異)。
効くのは「SMB の属性キャッシュが古い mtime を返す」かつ「1 段目の token が非 hex」のときだけで、
これは本 issue が未確認リスクとして挙げている経路そのもの。変異表に 2 段目の行が無いのはこのため。

## 敵対的レビュー 2 周目 (2026-09-16)

1 周目の指摘への修正が**新しい判定ロジック** (`cleanupOwn` / 理由の出し分け) だったので §7 の
打ち切り条件に当たらず、2 周目を通した。**また P1 が出た**。

| # | 指摘 | 判定 |
|---|---|---|
| P1 | `cleanupOwn` の前提「O_EXCL が通った = この lock は自分のもの」は**偽**。`os.Remove(l.lockPath())` は**名前**に対する操作で、Write が失敗して戻るまでのあいだに `Break` が入れば名前は**別ホストの生きた lock** を指す。消すと二重実行。**同じファイルの `tryTakeover` が「rename が原子なのは操作であって、名前の指す先が入れ替わらないことは保証しない」と明文で否定している前提そのもの** (issue 366) | **採用**。開いた実体を `f.Stat()` で控え、`os.SameFile` で照合したときだけ消す。🚨 **1 周目の修正 (stderr の案内) が「`break` を打て」と人に言うので、窓の発火条件を自分で作っていた** |
| P1-b | その後始末のテストは「消えたこと」しか見ておらず、**消す範囲を 1 mm も固定していなかった** (他人の lock を消しても緑 / `RemoveAll` でも緑)。変異表が「消さない」方向しか無く、**正解が『消さないこと』の fixture が 0 件**だった | **採用**。否定ケース (`TestTryPlaceDoesNotRemoveSomeoneElsesLock`) を足した |
| P2 | 「理由を出す」が**正常系まで騒がしくし、自分が作ったシグナルを埋める**。`lockman acquire \|\| exit 0` の cron が skip のたびにメールを飛ばし、operator が `2>/dev/null` を足す → **wedge の案内まで黙る** | **採用**。`errUnreadableLock` (errBusy を包む sentinel) を作り、**機械で見分けられる差**にした。鳴らすのはそちらだけ |
| P3 | 対照テストの余裕が**自分の SIGKILL との競走**で決まっていた (`grace=0` で 20/20 取りこぼし)。落ちると「冒頭 guard の退行」に見える | **採用**。対照の grace を仕様値から切り離した (この対照は「TERM が飛ぶか」しか見ない) |
| P3 | 「待ち直す枝では出さない」が**コメントだけで無検査** (再試行枝にも warnf を足す変異が全緑) | **採用**。`TestWaitLoopDoesNotWarnPerRetry` を足した |
| — | 秘密の漏洩 / stdout の汚染 / seam のテスト間干渉 / fixture の残骸 | **壊せなかった** (レビューが全数確認) |

### 変異検証 (2 周目の修正分)

| 変異 | 結果 |
|---|---|
| 実体照合 (`os.SameFile`) を外す | `TestTryPlaceDoesNotRemoveSomeoneElsesLock` red |
| 正常 busy でも理由を鳴らす | `TestNormalBusyStaysQuiet` red |
| 再試行枝でも鳴らす | `TestWaitLoopDoesNotWarnPerRetry` red |

🚨 **テストを書く側でも 2 回固まった**: hook の中で `other.Acquire` を呼ぶと同じ枝へ**再入して
無限再帰**し、`sync.Once` で塞ごうとすると**再入した `Do` が 1 回目の完了を待ってデッドロック**した
(どちらも 600 秒 timeout で観測)。素のフラグにした。

## 敵対的レビュー 3 周目 (2026-09-16)

**また P1 が 2 件**。どちらも **2 周目の修正が作った / 悪化させた**もの。

| # | 指摘 | 判定 |
|---|---|---|
| P2-1 | `errUnreadableLock` が**正反対の 2 状態を同じバケツ**に入れていた。健全な保持者の `Renew` が `O_TRUNC` している**一瞬**でも同じ分類になり、CLI が「保持者が居ないと分かっているなら `lockman break` で剥がす」と出す。**助言どおり break すると生きた保持者を剥がす = 二重実行** | **採用 (最重要)**。実測で確認 (期限内の保持者が居る状態で同じ文が出た)。**破壊的操作を促すのをやめ**、読み取り専用の `status` へ誘導する文へ変えた。一過性と恒久を 1 回の観測で区別するのは不可能なので、**区別しようとせず危険な助言を消す**方を採った |
| P1-1 | 「人が動くまで解けない busy」が分類から漏れ、**2 周目の commit で完全に無音になった**。`lock` がディレクトリのとき、`tryTakeover` が **busy エラーを握り潰したうえで `mtime.IsZero()` を根拠に `took=true`** を返し、その先の `tryPlace` が EEXIST → 素の errBusy。commit 前は全 busy で理由が出ていたので、**分類を導入した側が分類の外に何が落ちるかを数えていなかった** | **採用**。`if err == nil && mtime.IsZero()` にした。🚨 **この「全部に効く」という記述は誤りだった** —
直したのは 1 段目だけで、2 段目 (`mtime2.IsZero()`) は素のまま残っていた (4 周目 P1-1 で発覚)。実測: ディレクトリの lock で stderr が空だった → 分類されるようになった |
| P1-2 | **9 つ目の fixture の嘘**: `own` を **fd で控えていること自体**が無検査。`f.Stat()` を `os.Lstat(l.lockPath())` に変えても全緑 = 「照合を**する**こと」は守られているが「**何を**照合するか」は無検査。同じ論点に対して `takeoverRefreshHook` は既に seam を新設している (80 行上) のに、こちらには無い | **未対応** (4 周目へ) |
| P2-2 | `with.go` の分類が 1 本も pin されていない (「常に理由を出す」に潰しても全緑)。commit message の変異表は **main.go では真、with.go では偽**だった | **未対応** (4 周目へ) |
| P3-1 | Lstat エラー枝の fail-closed 方向が無検査 (「確認できなくても名前で消す」に反転しても全緑) | **未対応** (4 周目へ) |
| P3-2 | `os.SameFile` は dev+ino 一致だけで **ino==0 / ino 再利用を返す FS では inert**。APFS では 3000 回の create/delete で再利用 0 件 = ローカルでは実演不能 | **記録・未確認**。防御コードは足さない。実 SMB での測定手順だけ残す |

### 変異検証 (3 周目の修正分)

| 変異 | 結果 |
|---|---|
| zero-mtime の判別を戻す | `TestDirectoryLockIsClassifiedAsUnreadable` red |
| `break` を促す文へ戻す | `TestUnreadableLockIsNeverTakenOver` + `TestUnreadableLockGuidanceReachesCLI` red |

## 敵対的レビュー 4 周目 (2026-09-16)

**4 周連続で P1**。今回は 2 件とも「3 周目の修正が片手落ち / 案内先が空振り」だった。

| # | 指摘 | 判定 |
|---|---|---|
| P1-1 | 3 周目に直した zero-mtime の判別が、**同じ関数の 54 行下では逆向き (fail-open) のまま**。2 段目は busy エラー + zero mtime を「別の誰かが先に退けた」と読んで `took=true` を返す → `tryPlace` が EEXIST → **素の errBusy** で CLI が無音。🚨 commit と issue の「zero-mtime busy 全部に効く」は**偽** (2 箇所中 1 箇所) | **採用**。2 段目も同じ判別にした。**上の 3 周目節の記述も訂正した** (issue は done 後も残るので、誤った一文が次の監査を誤らせる) |
| P1-2 | 3 周目が新しく入れた案内先 `lockman status` が、その状態で**答えられない** (rc=1 = 道具の失敗 / stdout 空 / `--json` でも空)。`check` には「判定不能は busy 側へ倒す」分岐があるのに、案内は**それを持っていない方**を名指ししていた。結果、**wedge の出口を名指しする出力が production から 1 つも無くなった** | **採用**。`Inspect` が「保持中 かつ 中身を読めない」を**状態として**答えるようにし、`status` が rc=3 で `unreadable lock (size=0B, age=Ns)` と判断材料を出す。`--json` にも `"unreadable":true` |
| P1-2 系 | **10 個目の fixture の嘘**: 案内のテストは `strings.Contains(out, "status")` だけで、**名指しされたコマンドがその状態で何をするかを 1 行も検査していなかった**。1 周目 P2-2 (「文字列は production に出ていなかった」) の一段外側の版 | **採用**。案内から**コマンドを抽出して実際に走らせ**、rc と出力を assert する形にした |
| P2-1 | `with` 経路の分類が依然として無検査 | **採用**。正常 busy の stderr を**完全一致**で固定した (部分一致だと「常に理由を足す」変異が緑で通る。実測) |
| P2-2 | `own` を fd で控えていることが無検査な**理由**を特定 — 割り込める seam が照合の**後**にしかなく、fd 版とパス版が単一プロセスから原理的に区別できない | **採用**。`tryPlaceBeforeIdentityHook` を足し、「照合の前に break → 別ホストが取得」を起こすテストで pin した |
| P3-1 | `cleanupOwn` の異常系 3 枝は「変異が緑」ではなく **coverage 0** (どんな変異でも緑にしかならない領域) | **記録**。seam を足せば埋められるが、`ownErr != nil` は本番では I/O エラーでしか起きない。**未検査であることを記録**して残す |
| P3-2 | `lockman break` の案内が production に 2 箇所残っている | **記録**。どちらも**防御可能**: `lock.go` の後始末側は `os.SameFile` を通った後 = 自分のものだという積極的証拠がある地点 / 回収の案内は「mark 自身が猶予超過のときだけ鳴らす」と根拠がある。**全廃したわけではない**ことをここに残す (次の監査が「全廃したはず」を再生成しないため) |
| — | zero-mtime の新分岐が引き継ぎを止めすぎないか / 一過性の誤鳴りが signal drowning を作らないか (実測 0.40%) | **壊せなかった** (レビューが実測で棄却) |

### 変異検証 (4 周目の修正分)

| 変異 | 結果 |
|---|---|
| `own` をパスで控える (2 周目 P1 の再生産) | `TestTryPlaceCapturesIdentityFromFdNotPath` red |
| 2 段目の判別を戻す | `TestStage2ZeroMtimeBusyIsNotTreatedAsEvicted` red |
| `status` を旧挙動 (道具の失敗) へ | `TestStatusAnswersForUnreadableLock` + `GuidanceReachesCLI` red |
| `with` の分類を潰す (常に理由 / 常に定型文の両方向) | `TestWithStaysQuietOnNormalBusy` / `GuidanceReachesCLI` + 既存の `TestCorruptLockIsTreatedAsBusy` red |

🚨 **既存テストを 1 本書き換えた**: `TestCorruptLockIsTreatedAsBusy` は `Inspect` が
**エラーを返すこと**を assert していたが、それは当時の**手段**で、コメントが書いている**意図**は
「空いていると解釈しない」。エラーに倒していたせいで `status` が空振りしていたので、
意図 (Held かつ Unreadable) を pin する形へ直した。`check` の rc も別に pin した。

## 残タスク

- [x] 3 候補の採否を決めた ((a) + (c) を採用、(b) は 380 へ)
- [x] `takeoverGeneration` のコメントの「到達可能な非決定性は潰れている」を訂正した
      (truncate 経由の torn read は残るが、fail-closed で引き継ぎには到達しないことを明記)
- [x] 0 バイト lock が保持者にも回収できないことの再現 (レビュワー環境。本セッションでは未追試)
- [x] `--ttl 2h` の lock を 0 バイトにして既定 TTL 超過後に奪えることを、**本セッションで**
      再現した (`TestUnreadableLockIsNeverTakenOver` の変異 G1 = fail-closed を外すと奪える)
- [ ] **スコープ外**: (b) = `Renew` / `Release` を temp + rename にして 0 バイト窓自体を消す
      → [380](380-bug-lockman-renew-and-release-act-on-name-after-check.md)
- [x] 反証レビュー (敵対的レビュー) を 1 周通した。P1 の 2 件を修正し、変異で red を確認:
      `acquire` の理由出力を消す / `with` を定型文へ戻す → `TestUnreadableLockGuidanceReachesCLI` red /
      `tryPlace` の後始末を no-op へ → `TestTryPlaceRemovesOwnLockWhenBodyCannotBeWritten` red
- [x] 2 周目 (§7) を通した。P1 / P1-b / P2 / P3 x2 を採用し、変異で red を確認
- [x] 3 周目 (§7) を通した。P2-1 / P1-1 を採用 (変異で red を確認)
- [x] 4 周目 (§7) を通した。P1 2 件 + P2 2 件を採用、P3 2 件を記録 (変異 4 本で red を確認)
- [ ] **未実施**: 5 周目 (§7)。4 周目の修正が新しい判定 (`Inspect` の状態分岐 / 2 段目の fail-closed /
      pre-identity seam) を含むため打ち切り条件 (a) を満たさない。攻め口: ①2 段目の新しい fail-closed が
      over-block しないか ②`status` の新分岐が「中身を読めない」以外のエラーまで飲み込まないか
      ③seam を足した後の `tryPlace` の順序 ④`cleanupOwn` の coverage 0 の 3 枝

## 引き継ぎ (2026-09-16 時点)

**状態: 実装・テスト・レビュー 4 周が完了して push 済み。残るのは §7 の 5 周目と、未検査の 3 点。**

- 直った: 中身を読めない lock は引き継がない (fail-closed) / 読めない理由が CLI の出口に出る
  (ただし**破壊的操作は促さない**) / `status` がその状態に答える (rc=3 + size・age) /
  `tryPlace` の後始末は自分の実体だけを消す
- テスト: `src/lockman/unreadable_lock_test.go` (13 本)。seam は `readLockAfterStatHook` /
  `tryPlaceLinkFn` / `tryPlaceBeforeIdentityHook` / `tryPlaceAfterCreateHook`
- **次の一手**: 5 周目の敵対レビュー。攻め口は 4 周目の報告の 4 点 (①2 段目の新しい fail-closed が
  over-block しないか ②`status` の新分岐が他のエラーまで飲み込まないか ③seam を足した後の順序
  ④`cleanupOwn` の coverage 0 の 3 枝)
- **未検査として残っているもの**: `cleanupOwn` の異常系 3 枝は **coverage 0** (変異検証が原理的に無効。
  `ownErr != nil` を作る seam が要る) / `os.SameFile` は ino 再利用・ino==0 を返す FS では inert
  (**ローカルでは実演不能**。実 SMB 共有で `stat -f '%d %i'` を create/delete で繰り返すのが測定手順)

🚨 **踏んだ罠**: 1 周目〜4 周目まで**毎周 P1 が出た**。核 (fail-closed) は 1 周で閉じており、
2 周目以降は**全部「私が足した案内文と後始末」由来**。機構を足すと、その機構に固有の failure mode が
生まれて §7 が次の周を要求する。**足す前に §0-A (作らずに済む構造はないか) を問うこと。**

🚨 **380 が入ったので前提が変わった**: 0 バイト lock の主要な生成経路 (`Renew` の `O_TRUNC` 窓) は
消えた。本 issue の機構は「外部要因で壊れたとき」の受け皿として残るが、**発火頻度は下がる**。

