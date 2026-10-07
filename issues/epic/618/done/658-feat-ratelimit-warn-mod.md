# 658 (feat): 5h 枠の警告 (`ratelimit-warn.sh`) を mod へ移し、子プロセス無しで枠を読んで status line にも出す

起票日: 2026-10-07

> 起票時は 657 で採番したが、別セッションの 657 (retro) と衝突したので push 前に 658 へ改番した (参照の少ない側を寄せる。issues/README.md)。

> 🚨 **担当中: obaket の session (peer-inbox を作った Fable)**（2026-10-07〜）

epic 618 の子。618 の表で「今は移さない」とした `ratelimit-warn.sh` を、2026-10-07 の測り直し (`classic.*` が user の mod に届く) を受けて移す。

## 概要

今の `_claude/hooks/ratelimit-warn.sh` (UserPromptSubmit) は、毎プロンプトで bash → `bin/ratelimit -source claude -check -cached` (zsh → Go バイナリ) を起こし、
5h 枠が 80% 以上なら 1 行を注入する。mod にすると:

- **プロンプトごとの処理を減らす**: 枠は `$.session.usage().rateLimits` (この session の直近の応答ヘッダの値) と、statusline が書く `claude-rate-limits.json` から読み、
  zsh → Go → 裏の更新 (`bin/ratelimit`) を起こさない。settings の hook は残すので bash の起動 1 回は残る (下の「読まれなかったとき」)
- **人にも見せる**: 超過中は `$.ui.status` に `🚨 5h NN%` を固定で出す (CLI の statusline にも 5h% と 80% 以上の赤はあるが、流れる表示。hook は model にしか言えなかった)
- 注入の文面と閾値は hook と同じにする (`_claude/rules/subagent-model-tiering.md` が文面を名指ししている)

## 設計

### 枠の読み方 (優先順)

2 つの出所を読み、**使える観測のうち観測時刻が新しい方**を採る (codex 反証 P2: live を優先すると、別 session が 1 分前に書いた 90% より自分の 20 分前の 70% を選んで黙る):

1. **live**: `session.measure` (メインのターンの後に、計測の単位が動いたとき発火) で `rateLimits` の `five_hour` (`percentUsed` / `resetsAt`) を `$.state` に発火時刻つきで控える。
   `rateLimits` は「直近の応答が報告した値」で観測時刻を持たないので、measure の発火時刻を観測時刻の近似にする (メインのターンの後なら直近の応答はそのターンのもの)。
   🚨 近似であることは書いておく: context / cost だけが動いた発火で、subagent の応答の値に古い時刻を付ける可能性は否定できていない (codex P2-3。実機未確認)。
   発火時刻から 30 分以上経った控えと、未来の時刻の控えは使わない (`src/ratelimit/main.go` の `maxStale` と同じ理由: 古い % で判定し続けると、実際は超過していても黙る)
2. **file**: `$XDG_CACHE_HOME/glog/claude-rate-limits.json` (無ければ `~/.cache/glog/`) を `$.fs.read` で読む。
   書き手は `_claude/statusline-command.sh` (issue 627。対話の session が 60 秒ごとに書き、値が新しいときだけ上書き)。
   `observedAt` が 15 分以上前か未来なら使わない (`src/ratelimit/usage/statusline.go` の `statuslineMaxAge` と同じ境界 `>=`)。
   `five_hour` が null / `used_percentage` が null = 窓がリセットを過ぎて落とされた = 超過なし。`used_percentage` があって `resets_at` が無い形は使わない (reader と同じ)。
   既知の限界 (codex P2-5、既存 reader と同じ): `observedAt` はファイル全体に 1 つで、weekly だけ更新されても進む。窓別の鮮度は持たない
3. どちらも無い → **印を打たず hook に任せる** (下節)。hook は `ratelimit-claude.json` と裏の更新で今までどおり判定する (codex P2-4: 「無言 = hook と同じ」ではない。hook には出所がもう 1 つある)

`resets_at` が今以前なら窓は空から始まっているので超過なし (`overLimit` はその窓を判定対象から外し、reader は 0% Unused にする。mod は後者)。
閾値は 80% で `>=` (`-warn-5h` の既定値)。weekly は見ない (hook と同じ)。リセット時刻の表示は hook と同じ `formatReset` (今日なら `HH:MM`、それ以外は `M/D HH:MM`)。

### 注入と表示

- `classic.UserPromptSubmit` で `additionalContext` に hook と同じ 3 行を足す:
  `🚨 Claude の 5h 枠が閾値を超えている:` / `claude 5h NN% (HH:MM にリセット)` / `大きな作業に入る前に、…提案すること (基準は subagent-model-tiering.md の「枠の残量」)。`
- `$.ui.status('🚨 5h NN%')` は、プロンプト時だけでなく `session.measure` のたびと 60 秒ごとの timer でも判定し直す
  (codex P2-6: 送信時 79% → 応答で 85% を次の送信まで出さない / リセットや鮮度切れの後も放置中は残る、を防ぐ)。超過でなければ `$.ui.status(undefined)` で消す

### 読まれなかったとき・届かなかったときの扱い (618「mod が黙って止まったときの扱い」)

**settings の hook は消さず、mod がこのプロンプトを判定できたときだけ黙る形にする** (検出ではなく、構造で fallback を残す):

- mod は `classic.UserPromptSubmit` の中で、**使える観測があって判定できたときだけ** `$.env.set('DOTFILES_MOD_RATELIMIT_WARN', '<session_id>:<epoch ms>')` を `await` してから `next(e)` へ進む
  (env は「このプロセスと、その後に起こすもの」に効く。順序は型定義 (ClassicEventOf の doc) のとおり managed の hook → modules → その他の settings の hook なので、同じプロンプトの hook が印を見る)
- `ratelimit-warn.sh` は、印の `session_id` が stdin の JSON の `session_id` と一致し、かつ時刻が今から 10 秒以内のときだけ `exit 0`。それ以外は今までどおり判定する
- 印を**永続の `1` にしない**理由 (codex P1): env は解除するまで残るので、一度打った後に mod の hook が落ちる・hot reload で壊れる・managed settings が戻って `classic.*` が素通しになる、のどれでも
  hook まで黙る。子の `claude -p` も印を継承する。`session_id` + 時刻で「この session の、今のプロンプト」に限れば、mod が止まれば次のプロンプトから hook が出る。
  10 秒の窓は、同じプロンプトの中で mod → hook が走る間隔 (ms) に対して十分広く、人が次のプロンプトを打つ間隔より短い。残る穴: 10 秒以内に 2 回打ち、2 回目で mod が落ちた 1 回だけ黙る (受容)
- 印を `session.start` ではなく `classic.UserPromptSubmit` で打つ理由: managed settings が戻ると `session.start` は届くが `classic.*` は素通しになる (619 の表)。
  注入の口そのものが動いたときだけ印を打てば、素通しのときは hook が今までどおり出る

### 外すことで無くなる副作用 (list-masked-failure-modes)

- hook の `bin/ratelimit -cached` は、キャッシュ `ratelimit-claude.json` が古いと裏で更新を起こしていた。mod が判定できる間はこれが起きなくなる。
  読む側を数えた (codex P3-9 で訂正): `-cached` 無しの `ratelimit` (人が打つ / codex 系 skill の `-source codex -check`) も同じキャッシュを先に読むが、
  5 分より古ければ同期で取り直す (`src/ratelimit/main.go` の `cacheTTL`)。glogx (`src/glogx/usage_cache.go`) と pro-con (`src/pro-con/dispatcher/usage.go`) は自分で取る。
  古いキャッシュを取り直さずに読み続ける読み手は無い。また mod が判定できないプロンプトでは hook が今までどおり走るので、裏の更新も止まりきらない
- `claude -p` の中で hook が走って `ratelimit` → `claude -p /usage` を再帰的に起こす形 (`RATELIMIT_REFRESHING` の印で止めていた) は、mod では起きない (何も起こさない)

## 受け入れ条件

- [x] `_claude/mods/ratelimit-warn/` (manifest / hooks / types / tests)。`claude plugin validate` と `claude plugin test` が緑で、
      テストは 超過 / 未満 / リセット済み / file が超過 / file が古い / live と file の新しい方を採る / 判定できたときだけ印 (`env.set`) を打つ / 判定できないときは打たない、を持つ。
      閾値の比較を外す変異で red を確認
- [x] `ratelimit-warn.sh` に印の分岐 (session_id 一致 + 10 秒以内) を足し、`tests/claude/test_ratelimit_warn.sh` で 印なし → 注入 / 一致する新しい印 → 無言 / 別 session の印 → 注入 / 古い印 → 注入 を固定。分岐を外す変異で red
- [x] A-B (headless。`XDG_CACHE_HOME` を一時 dir にして 90% の `claude-rate-limits.json` と `ratelimit-claude.json` の両方を置く。codex P2-7):
      mod あり → 注入が 1 回 / mod なし → hook が注入する。「mod なし」の腕は `env -u CLAUDE_CODE_PLUGIN_DIRS` では足りない (user の settings の `env` から再び載る) ので、
      user の settings を外し (`--setting-sources`) hook だけの settings を `--settings` で渡す形にし、印の env も落とす。順序 (mod → hook) も debug log で確かめて書き戻す
- [x] 入口の文書: `docs/claude-mods-guide.md` の表 / `docs/claude-mods.md` の分担 / 618 の表の行 / `subagent-model-tiering.md` の hook の名指し (mod + hook と書く)
- [x] 敵対的レビュー (codex) を通し、指摘の採否を進捗に書く (2 周)

## 関連ファイル

- `_claude/hooks/ratelimit-warn.sh` / `_claude/settings.json` (UserPromptSubmit の配線)
- `src/ratelimit/main.go` (`overLimit` / `maxStale` / `-warn-5h`) / `src/ratelimit/usage/statusline.go` (`claude-rate-limits.json` の形と `statuslineMaxAge`)
- `_claude/statusline-command.sh` (`write_rate_limits`)
- `_claude/rules/subagent-model-tiering.md`「枠の残量」

## 進捗

- 2026-10-07: 起票
- 2026-10-07: codex 反証 (gpt-6.1-sol high、read-only) 10 件。採用: P1 永続の印 → `session_id:時刻` の印 + 判定できたときだけ打つ / P2 live 優先 → 新しい方 /
  P2 判定できないときの無言 → 印を打たず hook に任せる / P2 status の更新契機 → measure と 60 秒 timer / P2 A-B の腕の作り方と順序 (型定義: managed → modules → settings) /
  P3 「子プロセス無し」「読み手は無い」「overLimit と同じ」の文面を訂正。記録して採らない: P2-3 measure の発火時刻を観測時刻にする近似 (限界を本文に書いた) /
  P2-5 file の observedAt が窓別でない (既存 reader と同じ限界)。反証されなかった: 閾値 80 `>=`・maxStale 30 分・statuslineMaxAge 15 分・`-cached` の明示呼び出しが hook だけ
- 2026-10-07: 実装 — mod (`hooks/limit.ts` 判定 / `hooks/register.ts` 配線)、hook の印の分岐、テスト (plugin test 21 件 / `tests/claude/test_ratelimit_warn.sh` 11 件)、入口の文書 4 箇所。
  **A-B (headless、haiku)**: 偽の `XDG_CACHE_HOME` に 90% の `claude-rate-limits.json` + `ratelimit-claude.json`、user の settings を外し (`--setting-sources ""`) hook だけの settings を `--settings` で渡し、
  mod は `--plugin-dir`。mod なし → hook が注入 (debug log に hook の出力)。mod あり → model の答えは「1」(注入 1 回)。hook を wrapper で包んで実走を記録すると
  `ran rc=0 mark=[<session_id>:<epoch ms>] out_len=0` = hook は走り、印を見て黙った。**順序は mod → hook** (同じプロンプトの hook が mod の印を見た)。
  `$.clock.now()` は epoch ms (hook の `date +%s` と比べられた)。mod ありの腕では応答後の `session.measure` が本物の 29% (live) を新しい方として採り status を消した (file の 90% より新しい = 設計どおり)
- 2026-10-07: 敵対的レビュー 1 周目 (codex gpt-6.1-sol high、read-only)。P1 なし / P2 6 / P3 2。採用 7: 丸めを切り捨てに (statusline と同じ。79.6 を 80 にしない) /
  空の `rateLimits` で live の控えを消す / timer の `refreshStatus` を try-catch (未処理 reject) / 印は `<sid>:<ms>` の 2 要素だけ (3 要素は印でない) /
  テストの false green 3 件 (底の hook の呼び出し回数・status の配列・11 秒 / 20 秒 / 3 要素の印・formatReset の固定文字列)。
  受容 1: 同じプロンプト内で hook 到達が印の 10 秒後になる二重注入 (沈黙側でなく二重側。同一プロンプトで 10 秒空く経路は実機に無い。起きても害は重複 1 行)。
  変異 7 本 (null で `{}` を返す / status を出さない / live を消さない / formatReset を 00:00 / round に戻す / 窓を 30 秒 / 3 要素の検査を外す) でそれぞれ狙ったテストだけ red、復元で green。
  修正後に A-B を再実行 (A3): 注入 1 回・hook は印を見て無言
- 2026-10-07: 敵対的レビュー 2 周目 (修正差分に限定)。**本番の修正は壊せなかった** (P1 / P2 なし)。切り捨ての境界 (80.0 → 超過 / 79.99 → 未満)・context だけの measure で控えが残ること・
  3 要素の印の扱いは codex 側の harness で確認済み。P3 のテストの穴 2 件を採用: 空の measure の直後に status も消えることを assert / 60 秒 timer の経路を走らせ、
  `$.ui.status` が拒否された周の次も動くことを assert。変異: status を消さない形は red。**try-catch の撤去は green のまま** (`claude plugin test` は timer の callback の未処理 reject を失敗にしないので、
  この修正の撤去はテストでは検出できない。timer の経路を実行することまでが射程)。
  未確認 (記録): try-catch が実機で診断情報を失わせる影響 (拒否を無言で吸収する。警告が欠ける経路は見つかっていない)

