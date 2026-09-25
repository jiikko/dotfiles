# 448 (retro): pro-con の複数の画面・終了の保証・dogfooding を入れた日 (427 / 440 / 441)

起票日: 2026-09-25

## 概要

435 の後、同じ日に即時の割り振り (socket)・複数の画面・終了で claude を確実に止める (敵対的レビュー 8 周)・dogfooding (Claude が PM になり
本物の PG で 428 / 436 を実装)・`--view` を入れた。うまくいった話は書かない。踏んだ所と、他の作業にも効く形の改善だけ。

## どこで踏んだか

1. **テストの偽物が実物の状態を持たず、本物の不具合が緑のまま通った**。「止めたら state: stopped」を 1 回の実測から偽物に写し、判定も
   state の名前で書いた。dogfooding で、終えた session は stop 後も state: done のまま (pid 無し) と分かり、止め直しが続いた (01dbb3b0 で直した)
2. **push の再試行のループが、失敗した rebase を隠した**。`for …; do git push …; [ ok ] && break; git pull --rebase; done` で、rebase が衝突で
   止まったまま次の push が「Everything up-to-date」(rc=0) を返し、push できていないのに成功と読みかけた
3. **速くするための経路 (socket) に正しさを載せた**。「画面の数を dispatcher (socket) が数える」設計は、dispatcher が居ないと最初に閉じた画面が
   全部止める (2 周目のレビュー)。自分で「socket は速くするための口で、正しさは頼らない」と書いた直後だった。flock の印に作り直した
4. 帰属を推測で伝えた: 見覚えの無い commit (f82819a1) を 7d の lint の作業と書いて、ユーザーと 7d の両方に伝えた (7d が否定して分かった)
5. 確かめずに説明した: 「左右は跳ね返りも見える」とユーザーに 2 回説明したが、丸めで左右も 0 になる (PG の記録と計算で分かった)

## 次に効きそうな改善 (切り出し先の提案。実行はユーザーの判断を待つ)

1. **偽物 (fake / stub) の状態は、実物の状態を列挙した実測の表から作る。判定は状態の名前の列挙でなく、名前が増えても崩れない性質で書く**
   (例: 「止まった」= プロセスが無い)。実物を 1 回だけ測って偽物に写すと、偽物に無い状態が本番で出たとき、テストは緑のまま判定が外れる
   → 切り出し先: `_claude/rules/mutation-verify-new-tests.md` の「fake / stub」の項へ追記
2. **push を再試行するなら、合間の `pull --rebase` の rc を見る。rebase が止まったまま次の push を打たない**
   (途中の rebase の上で打った push は「Everything up-to-date」rc=0 を返し、空振りが成功に見える)
   → 切り出し先: `_claude/rules/commit-with-pathspec.md` の「push の空振り」の項へ追記
3. **正しさの判定を、落ちうる補助の経路 (速くするためのキャッシュ・通知・常駐プロセス) に載せない**。載せるなら「その経路が無いとき」の答えを
   先に決める (安全側に倒れるか)。守りを新設する前の問いとして
   → 切り出し先: `_claude/rules/adversarial-review-own-safeguards.md` の §0 (作る前に問う) へ 0-C として追記

## 却下 (既存のルールで足りる。守れなかっただけ)

- 4 (帰属の推測): `commit-with-pathspec.md` の「推測した帰属を第三者へ伝えない」がそのまま当たる
- 5 (確かめずに説明): CLAUDE.md の「主張は証拠ではない」が当たる
- 見張りが一時的な状態 (再開の途中の分解済み) で早く抜けた: `avoid-wall-clock-assertions.md` の「ポーリングする条件は待ちたい事象そのものにする」が当たる

## 決着 (2026-09-25, pro-con C-016)

3 件とも既存に同じ規範が無いことを grep で確かめてから、ユーザーの決定どおり追記した (本文は規範だけ、起源は同名の `rules-rationale/`)。

1. `_claude/rules/mutation-verify-new-tests.md` の「fake / stub」へ「fake の状態は実測で列挙した表から作り、判定は名前が増えても崩れない性質で書く」
2. `_claude/rules/commit-with-pathspec.md` の push の空振りの項 (`-q` の次) へ「再試行の合間の `pull --rebase` の rc を見る」
3. `_claude/rules/adversarial-review-own-safeguards.md` の §0 へ 0-C「正しさの判定を、落ちうる補助の経路に載せていないか」

残課題なし。
