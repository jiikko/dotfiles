# 445 (risk): viewer が読み取りだけであることの担保と、見せる範囲

起票日: 2026-09-25

親: [441](../441-design-pro-con-viewer.md)

## 概要

441 の viewer (442〜444) が、人間の pro-con の状態を変えないこと、見せてよいものだけを見せることを、設計とテストで担保する。

## 確かめること

- [x] viewer のコマンド (`card list / show / wait`・`screen`・`log`) が、受付の箱・記録・socket の「起こす / 知らせる」を 1 つも書かない
  (テストで、実行の前後で状態の置き場の中身が変わらないことを見る。socket に `wake` / `notify` を送らないことも)
  — `TestViewCommandsDoNotWrite` (cardview_test.go) / `TestLogDoesNotWrite` (logcmd_test.go) / `TestScreenDoesNotWrite` (screencmd_test.go)。
  状態の置き場の外 (socket の逃がし先 `/tmp/pro-con-<uid>/`) の権限も直さない: `TestViewCommandsDoNotFixFallbackDir` / `TestLogDoesNotFixFallbackDir` /
  `TestScreenDoesNotWrite` の `stillLoose` / `TestViewDoesNotFixFallbackDir` (live_test.go。`--view` の画面) / `TestFallbackDirMustBeOwn` と
  `TestSubscriberRefusesLooseFallbackDir` (wake_test.go。直すのは Listen だけ)。`--view` の画面が受付の箱に書かない: `TestViewOnlyBackend`
- [x] 覗いている Claude が PG に入力できない (attach・SendMessage の経路を viewer に作らない)
  — 読む口のコマンドに attach・入力の経路は無い。`--view` の backend は止める口・書く口を型の上で持たず、依頼・回答・attach を断る:
  `TestViewOnlyBackend` / `TestWireLiveViewStopsAndStartsNothing` (main_test.go)。案内の行にも断る操作を出さない: `TestHintsOmitActionsOnViewOnly` (ui/hints_test.go)
- [x] 見える範囲: 依頼の原文・PG の出力の末尾・入力欄に打ちかけの文。同じユーザーの別の Claude が読むことを前提にしてよいか、
  打ちかけの文は出さない方がよいかを決める (ユーザーに聞く) — 2026-09-25 にユーザーが決めた (打ちかけの文も見せてよい。下の進捗)
- [x] 画面の中継 (443) のファイルと出来事の記録 (444) の置き場の権限 (0600 / 0700) と、大きさの上限
  — 中継: `TestFrameFileAndClose` (relay_test.go。0700 / 0600。上限は持たず 1 枚 = 画面の大きさ) / 記録: `TestAppendPermissions` / `TestAppendRotates`
  (eventlog_test.go。0600 / 0700・1 MiB で回す)

## 進捗

- 2026-09-25: 442 の読む口 (`card list / show / wait`) には、`src/pro-con/cardview_test.go` の `TestViewCommandsDoNotWrite` を入れた
  (実行の前後で状態の置き場と transcript の置き場のファイルの中身・権限・mtime・数を比べ、偽の dispatcher で socket に届いた行が購読の `sub` だけかを見る)。
  443 / 444 の口にも同じ形を当てられる (444 はカード C-006 の依頼に入れた)
- 2026-09-25: **見せる範囲はユーザーが決めた: 入力欄に打ちかけの文も外の Claude に見せてよい** (「打ちかけの文は見せていいよ」)。
  同じユーザーの別の Claude が読む前提で、依頼の原文・PG の出力の末尾と同じ扱い。443 (画面の中継) は入力欄を伏せずに中継してよい (7d に伝えた)

- [x] 444 から移した: 画面の側の出来事 (開いた・閉じた・quit で止めた / 止めなかった) を events.jsonl に入れる。書き手を dispatcher 1 つに保つなら、
  画面は受付の箱へ「出来事」を置き dispatcher が書く (444 の「決めたこと」) — 2026-09-25 (カード C-009) に済んだ (下)

## `--view` (最小の形) の敵対的レビューで分かったこと (2026-09-25)

- 直した: main で `--view` を選ぶ分岐がテストで守られていなかった (外しても緑。外すと quit で PG まで止まる) → つなぎ方を wireLive に切り出して固定 /
  読み直しを待つテストの待ちが空振りしていた → 知らせ (Changed) を待つ
- 記録のみ (「何も書かない」は厳密には成り立っていない):
  - ctrl+r (ライブアップグレード) は、`--view` でも状態の置き場に引き継ぎのファイル (resume-*.json) を書く
  - 3 秒ごとに画面の数を数えるとき、落ちた画面の印を消す (screens/ を書き換える。普通の画面と同じ後始末)
  - 案内の行の「a attach」が `--view` でも使える表示のまま (押すと断るので実害は無い)
- 2026-09-25: 443 の `pro-con screen` にも同じ形の `TestScreenDoesNotWrite` を入れた。中継の置き場 relay/ の権限 (0700 / 0600) と書きかけを読ませない形は 443 で済み。大きさの上限は持っていない (1 枚 = 画面の大きさ)

- 2026-09-25 PM の決定 (カード C-009 に渡した。PG が実装中): 画面の側の出来事は画面が受付の箱に置き dispatcher が書く (`--view` は置かない) /
  `--view` の案内から断る操作を外す / 「読むだけ」の例外の線引き — ctrl+r の resume-*.json はその画面自身の表示の状態なので書いてよい・落ちた画面の印の後始末は残す・
  読む口が socket の逃がし先の権限を直すのはやめる (直すのは listen する dispatcher だけ)

## 2026-09-25 (カード C-009) — 残りを片付けた

やったこと:

- **画面の出来事**: 画面 (`--view` 以外) が受付の箱に種類 `event` の依頼 (`Note` = 文) を置き、dispatcher が Apply で拾って出来事の種類 `screen`
  として、**画面が置いた時刻のまま** events.jsonl に書く (`store.KindEvent` / `dispatcher/events.go` の `applied`。書き手は dispatcher 1 つのまま)。
  置くのは「開いた (開いている画面 N / 印を置けない理由)」「quit で閉じた: ほかに N 画面が開いているので止めなかった」
  「quit で閉じた: 最後の画面なので止める」(止める前に置く → 止める dispatcher が Shutdown の前の Apply で書く)「quit で止めきれなかった」
  (止めた後に置く → 次に起動した dispatcher が書く)。文の頭に `画面 (pid N):`。`--view` の画面は置かない
  - 取りこぼし: 画面が quit を通らずに消えた (落ちた・端末を閉じた・kill) ときは何も残らない (dispatcher の `screens` の出来事が代わり)。
    ctrl+r の入れ替えでは、新版が開き直すので「開いた」がもう 1 度出る。dispatcher が居ない間に置いた出来事は後から書かれるので、
    events.jsonl の中で時刻の順が前後しうる (`pro-con log --follow --since` は、読み始めた後に足された分を時刻で絞らない)
  - 画面の出来事はヘッダーの「受付の箱に適用待ち N 件」に数えない (`store.Pending`。数えると、開くたびに dispatcher が止まっているように見えた)
- **`--view` の案内の行**: 押すと断る操作 (a / r / + / w / d / n / i / x) を暗くせず出さない。e / y / Y・詳細・PG 一覧・終了は残す
  (README の表と docs/glogx-ui-guide.md §8)
- **読むだけの例外の線引き**: (a) ctrl+r の resume-*.json は `--view` でも書く (その画面自身の表示の状態だけで、カード・箱・dispatcher・PG を変えない。
  `main.go` の `switchToNew` の直近に理由) / (b) 落ちた画面の印の後始末は残す (`live.go` の `presence.Count` の直近) /
  (c) 繋ぐ側 (`wake` の dialPath = Poke・Notify・購読) は socket の逃がし先の緩い権限を**直さず**、つながずに `wake.ErrUnsafeDir` を返す。
  直すのは listen する dispatcher (`Listen` → `ensureFallbackDir`) だけ。購読は `OnRefused` で 1 度だけ知らせ、`card wait` / `log --follow` は stderr に 1 行、
  画面は違反の行に 1 行出して読み直し (ポーリング) で待つ。`pro-con screen` はもとから socket を使わない

確かめたこと:

- 上の「確かめること」の各テスト。`go test -race ./...` 緑 (origin/master = 451 の削除・437 の上に rebase 後)
- `bin/mutate-verify` で変異 32 本がすべて red (画面の出来事 14・`--view` の案内 3・逃がし先と購読 11・適用待ち 3・`--since` 1。1 周目 18 + 2 周目 11 + rebase 後 3。
  うち 3 本は当て方が構文エラー / テストの待ちに上限が無く 600 秒で落ちた、ので当て方とテストを直して当て直した)
- 敵対的レビュー (Opus・読むだけ) で直したもの: P1 `watchDir` が購読の goroutine を待たずに戻り、`-race` で再現した (戻る前に待つ) /
  P2 もとからあった `TestSubmitPokesDispatcher` 等が本物の `/tmp/pro-con-<uid>` に socket を作っていた (`wake.RunIsolated` を 4 つの package の TestMain から呼ぶ。
  テストの後に本物の逃がし先の mtime が変わらないことを確かめた) / P3 適用待ちの数・`--follow --since` の取りこぼし・テストの穴 3 つ
- 記録のみ: 画面の開閉 (1 回 2〜3 件) も適用済みの控え (1000 件) を使うが、1 回の Apply が 500 件までなので二重適用の守りは弱まらない

- `make test`: 1 回目 rc=2 (pro-con の lint 1 件: `ui/view.go` の `!(ro && h.acts)` に staticcheck QF1001。Go のテストは全部緑) →
  `!ro || !h.acts` に直して `make lint` 0 件・変異を当て直して red → 2 回目 rc=0 (テストの係。4m18s)。その後 origin/master (472 の枠など) に
  rebase し、README の競合を解いて `go test -race ./...` 緑・`make lint` 0 件

残り: 無し
