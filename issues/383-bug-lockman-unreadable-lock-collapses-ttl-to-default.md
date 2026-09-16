# 中身を読めない lock は実効 TTL が宣言値から 30m へ縮む — 生きている lease を正規手順で奪える

起票日: 2026-09-16
カテゴリ: bug / priority: **high**
対象: `src/lockman/lock.go` の `holderTTL` / `tryTakeover` / `takeoverGeneration` のコメント
出典: [381](done/381-bug-lockman-with-renew-latch-stops-renewal-forever.md) の敵対的レビュー (観点① 壊す)
反証レビュー: 未実施

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
- テスト 3 本を新設 (`unreadable_lock_test.go`)。変異検証はケースごとの PASS/FAIL で判定:

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
- [ ] **未実施**: 3 周目 (§7)。2 周目の修正が `os.SameFile` による**新しい判定ロジック**なので
      打ち切り条件 (a) を満たさない。攻め口は「`f.Stat()` が失敗する枝」「Lstat と Remove の
      あいだに残る窓」「Close 失敗枝で fd が無い状態」
