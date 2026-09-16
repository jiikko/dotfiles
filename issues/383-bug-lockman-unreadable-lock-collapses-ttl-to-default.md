# 中身を読めない lock は実効 TTL が宣言値から 30m へ縮む — 生きている lease を正規手順で奪える

起票日: 2026-09-16
カテゴリ: bug / priority: **high**
対象: `src/lockman/lock.go` の `holderTTL` / `tryTakeover` / `takeoverGeneration` のコメント
出典: [381](381-bug-lockman-with-renew-latch-stops-renewal-forever.md) の敵対的レビュー (観点① 壊す)
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

## 残タスク

- [ ] 上記の 3 候補の採否を決める (それぞれ独立に効く)
- [ ] `takeoverGeneration` のコメントの「到達可能な非決定性は潰れている」を訂正する
- [x] 0 バイト lock の残骸が `with` のプロセスより長生きし、**保持者にも回収手段が無い**ことを
      本物の `runWith` ループで再現した (381 のレビュー観点③。`O_TRUNC` の直後で 1 本目だけ
      止める seam。3/3): `with` の exit=125 / 解放後も lock が size=0 で残存 /
      `readLock`=errBusy / 別マシンの `Acquire`=errBusy / **保持者自身の `Renew` も errBusy**
- [ ] 実測: `--ttl 2h` の保持者の lock を 0 バイトにしてから 31 分後に別 Locker が
      `Acquire` できることを再現する (レビュワーは `Chtimes` で時間を圧縮して再現済み)。
      🚨 **上の 2 件はどちらもレビュワーのコピー環境での実測で、本セッションでは追試していない**
- [ ] 🚨 **`with` 経路で本当に危ないのは「`open(2)` の中で止まる」と「truncate 以降」だけ**
      (381 のレビュー観点③ が実測で絞り込んだ)。`readLock` / `serverNow` の途中で止まっても、
      `now` は stall の後に取り直されるので `lock.go` の期限検査が errNotOwner で弾き、
      他人の lock は無傷だった (3/3)。380 を直すときの範囲の絞り込みに使える

## 関連

- [380](380-bug-lockman-renew-and-release-act-on-name-after-check.md) — 0 バイト窓を作る側
- [381](381-bug-lockman-with-renew-latch-stops-renewal-forever.md) — その窓に到達する機会を増やした側
- [366](done/366-bug-lockman-stale-takeover-sometimes-has-two-winners.md) — 2 段構えの出典 (本件には効かない)
