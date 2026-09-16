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

## 残タスク

- [x] 3 候補の採否を決めた ((a) + (c) を採用、(b) は 380 へ)
- [x] `takeoverGeneration` のコメントの「到達可能な非決定性は潰れている」を訂正した
      (truncate 経由の torn read は残るが、fail-closed で引き継ぎには到達しないことを明記)
- [x] 0 バイト lock が保持者にも回収できないことの再現 (レビュワー環境。本セッションでは未追試)
- [x] `--ttl 2h` の lock を 0 バイトにして既定 TTL 超過後に奪えることを、**本セッションで**
      再現した (`TestUnreadableLockIsNeverTakenOver` の変異 G1 = fail-closed を外すと奪える)
- [ ] **スコープ外**: (b) = `Renew` / `Release` を temp + rename にして 0 バイト窓自体を消す
      → [380](380-bug-lockman-renew-and-release-act-on-name-after-check.md)
- [ ] **未実施**: 反証レビュー (本 issue の実装に対する敵対的レビュー)

## 関連

- [380](380-bug-lockman-renew-and-release-act-on-name-after-check.md) — 0 バイト窓を作る側
- [381](done/381-bug-lockman-with-renew-latch-stops-renewal-forever.md) — その窓に到達する機会を増やした側
- [366](done/366-bug-lockman-stale-takeover-sometimes-has-two-winners.md) — 2 段構えの出典 (本件には効かない)
