# 壁時計の経過時間で assert しない — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/avoid-wall-clock-assertions.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## 2026-09-01 obaket — Xcode Cloud build 534 の false red

Xcode Cloud build 534 が
`#expect(clock.now < cancelledAt.advanced(by: .seconds(1)))` で false red になった。
テストの主張は「cancel されたら **deadline (5 秒) まで回り続けない**」だったのに、
判定は「1 秒以内に返る」で書かれており、**主張と判定がずれていた**。実測は 1.656 秒 —
cancel は正しく効いていて、遅かったのは CI の負荷。

書き直した判定は **condition が呼ばれた回数**:

- cancel が効く → 次の周回の先頭で抜けるので呼び出しは少数で止まる
- cancel が効かない → deadline まで回り続けるので桁が変わる

**実測 (同一マシン)**: cancel が効く側 = **1 回** / 効かない側 = **6,710,401 回**。
**6 桁の差**があるので、閾値をどこに置いても判別できる (時間予算の 1 秒 vs 1.656 秒 =
2 倍未満の差とは対照的)。

**負荷と無関係に成立し、しかもマシンが速くなるほど失敗側の回数が増えて判別しやすくなる**
(時間予算とは逆で、性能向上が味方になる)。


## なぜ「回数」が「時間」より強いのか

- **負荷と無関係に成立する**。時間予算は CI の空き具合を測っているだけで、実装の性質を測っていない
- **桁差が判別の余裕になる**。1 秒 vs 1.656 秒 (2 倍未満) では閾値をどこに置いても危ういが、
  1 回 vs 670 万回なら任意の閾値で分かれる
- **性能向上が味方になる**。マシンが速くなると、失敗側の回数は増えて判別しやすくなる
  (時間予算はマシンが速くなるほど閾値が窮屈になり、逆向きに壊れる)

学び: 閾値を広げるのは対症療法。落ちたら「判定軸が主張と一致しているか」を先に問う。

## 「待つために sleep を書かない」の起源と実測 (dotfiles, 2026-09-05)

`make test` の高速化 (issue 259 / commit `59f9e48c`) で出た実測。

**規模**: `tests/` 配下の `sleep <数値>` は 89 箇所。うち 60 秒未満の**ブロックする待ち**が
65 箇所 = **159 秒**。残りは背景プロセスを生かしておくための `sleep 3600` / `sleep 300`
(待ちではないので数えない)。

**単独最大のテスト**: `tests/bin/test_go_autobuild.sh` が **46.4 秒**で、109 本のテスト中 1 位
(2 位の 22.3 秒の倍)。中身は `sleep 3` ×2 / `sleep 4` / `sleep 0.5` ×4 / `sleep 0.3` ×2 /
`sleep 1.1` ×4 など。8 箇所を条件ポーリングへ、4 箇所を `touch -t` (下記) へ変えて
**46.4 秒 → 35 秒**。

**待ちの置き換え方の実例**:

| 元 | 何を待っていたか | 置き換え |
|---|---|---|
| `sleep 3` / `sleep 4` | lock を奪われた builder が終わるの | `kill -0 <pid>` が偽になるまでポーリング |
| `sleep 0.3` / `sleep 0.5` | builder が指紋を確定してビルドに入るの | 作業ファイル `.autobuild.new.*` の出現をポーリング |
| `sleep 0.6` | あとから来た builder の掃除が終わるの | その builder の成果物が入るまでポーリング |
| `sleep 1.1` ×4 | **待ちではない**。mtime 秒精度を跨ぐため | `touch -t` で対象を 60 分前へ置く (待たない) |

**残した 3 箇所**はいずれも「再ビルドが**起きない**」という否定の assert で、待つべき成立条件が
無い。ここは `sleep` が正しい。

**なぜ「待ち」の方が assert より質が悪いか** — 同じ 2026-09-05 に実証された。
`test-discovered` を並列化 (直列 367 秒 → 並列腕 122 秒 + 直列腕 89 秒) した直後の通し実行で、
`tests/claude/test_claude_links_sync.sh` (並行 apply のレースを検査する) が落ちた。
**単独では 3/3 緑**。このテストは `e7678f37 fix(claude): 並行 apply の状態確認を最大 5 回
やり直す (CI で残っていたレース)` で既に一度直されており、**元から負荷に敏感だった**。
壁時計の assert は「遅いと落ちる」で気づけるが、待ちの `sleep` は**速いマシンでは緑のまま
通り、負荷が上がった日にだけ落ちる**。並列化はその日を作る。

## 2026-09-05 obaket — 別セッションの xcodebuild と重なった `make test` で 2 秒待ちが 10 本一括 timeout

`TransferActivityCenterTests` 系の `pollUntil(timeout: .seconds(2))` (`TransferActivityCenterTests` / M2 / M3 の 3 ファイルで 84 箇所) が、別セッションの xcodebuild と
同時刻 (load average ≈ 5) の `make test` で 10 本まとめて超過した。同じコードの単独再実行は 637 tests 全 green。
待っていたのは「injected sleeper に要求が入った」「replay job が enqueue された」という**事象**で、時間を測る必要は無かった。
同じセッションで入れた `waitUntilStopConsumedForTesting` (`.stop` 消費の瞬間に resume する continuation) はこの形の置き換え例。
記録: obaket `issues/724-test-transfer-activity-center-tests-two-second-poll-timeouts.md`。

## 窓を作る sleep が「順序」も担保していた実例 (2026-09-05, dotfiles issue 267 / commit `6467aeea`)

`tests/bin/test_go_autobuild.sh` の「あとから始まったビルドが勝つ」テストは、偽 go に
`FAKE_GO_SLEEP=1` (先行ビルド A) と `=4` (後発ビルド B) を与えていた。数字の意図は
「走行中の窓を作る」ことだと読めるが、実際には **「A が B より先に完走する」順序も決めていた**。

窓だけをイベント同期 (ゲート) へ置き換えたところ、A と B が同時に走り出す形になり、
**12 回に 1 回** A が最後に install して `got: A` で落ちる flake になった。

紛らわしかったのは `wait "$A_PID"` が順序の担保に見えたこと。実際には `--async` のラッパーは
builder を detach して即 exit するので **`$A_PID` はとっくに死んでおり、何も待っていない**。
release の直後に「A の成果物が入ったこと」(`binary_is "$ROOT" A`) を待つ形へ直して 14 回連続 green。

教訓: **長さの違う複数の `sleep` は、窓と順序の 2 つを同時に担保していることがある**。
イベント同期へ移すときは、窓 (走行中であること) と順序 (どちらが先に完走するか) を
**別々に**作り直す。片方だけ置き換えると、成功率の高い flake になって 3 回連続 green を素通りする。
