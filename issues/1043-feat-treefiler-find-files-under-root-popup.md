# 1043 (feat): treefiler の / で root 以下の全ファイルを名前で探すポップアップを出す (今の木の検索は t へ)

起票日: 2026-11-07

## 概要

ユーザー依頼 (2026-11-07):「treefiler を使っていて current dir 以下のファイル名を探索して、カーソルの移動候補として出したい。今だと open している node のみが対象になっていると思う」。
今の `/` は開いている木の中だけを探す (spec §6.2 で決めた形)。閉じたフォルダの中を名前で探す手段は無かった (`:` はパスを打てば飛べるが、名前では探せない)。

## 決めたこと (ユーザー回答 2026-11-07)

- 出し方: 中央のポップアップに、root 以下の全部のファイルとフォルダから打った語に合うパスを一覧にする。Enter で途中のフォルダを開いてカーソルを移す (`:` と同じ)
- キー: `/`。今の検索 (開いている木の中・打つたびにカーソルが飛ぶ・一致が光る・`n` `N`) は別のキーへ移す → `t` (空いていたキー。例として挙げたものを採った)
- 探す範囲: 隠したフォルダの中も探す。`.git` の中と、git が無視するフォルダ (`>` と同じ判定) には降りない

## 状態 × 出来事の表 (実装の前に決めた)

| 出来事 \ 状態 | 閉じている | 開いている・走査中 | 開いている・走査済み (上限で停止を含む) |
|---|---|---|---|
| `/` | 開く。root の走査を始める | (キーはポップアップが取る) | 同左 |
| 文字の入力・↑↓ (検索の履歴) | – | 語を変え、取り込んだ全部から引き直す | 同左 |
| Tab / Shift-Tab | – | 選ぶ候補を移す | 同左 |
| Enter | – | 選んだ候補へ移り (途中を開く)、閉じる。候補が無ければ開いたまま | 同左 |
| Esc / Ctrl-C / 空欄の Backspace | – | 閉じて走査を止める。カーソルは動かさない | 閉じる |
| `Advance` | – | 見つかった分を取り込み、今の語で照らして足す | – |
| `Close` (glogx で F を閉じる) | – | 走査を止めて閉じる | 閉じる |
| `Busy` | – | 真 (呼び出し側が遅い周期で Advance を呼ぶ) | 偽 |
| 候補が消えていた | – | Enter で「見つかりません」を知らせる (動かない) | 同左 |

## 実装

- `src/treefiler/filer/finder.go` (新規): 裏の goroutine で root から幅優先に辿る (`os.ReadDir`。symlink は辿らない)。10 万件 (`finderMax`) で止めて「上限で止めた」と出す。
  見つかった分を Advance で取り込み、今の語で照らして段に足す。並びは「名前 (最後の要素) に一致 → パスの途中にしか一致しない」の 2 層 × fuzzy の段 (検索と同じ `fuzzy`)、同じ段は幅優先の順 (浅い方が先)
- `filer/search.go`: 検索の履歴を辿る位置を `histCursor` に切り出した (ポップアップも同じ履歴を ↑↓ で辿る)
- `filer/model.go`: `/` → ポップアップ・`t` → 今の検索。層 (`layer:"finder"`)・OwnsKeys・LayerHints・CaretPos・Busy・Advance・Close・描画・Paste・debugLayer に配線
- 文書: spec §0.1 の表に 1 行・§6 のキー表・README のキー表・`?` の一覧・treefiler の CLAUDE.md・glogx の UI ガイドの `n` の行
- テスト: `filer/finder_test.go` (9 本) と glogx の `TestFilerFinderOwnsKeysWithHintAndCaret`。`/` で今の検索を開いていた既存のテストは `t` に置き換えた (treefiler 23 か所 + 一覧 5・glogx 5・tfprobe の段)。tfprobe に `find` の段を足した

## 進捗

- 2026-11-07: 実装・テスト。変異 12 本で red を確かめた (降りないフォルダの判定・`.git`・名前の層・Busy・Close・上限・キャレット・一致の地の色・reveal・貼り付けなど。Busy と Close の 2 本は最初テストか置換の当て方が弱く、直してから red を確かめた)
- 検査 (2026-11-07): treefiler の `go test -race` 全 package ok・`make lint` 0 issues・`TREEFILER_VIRT_SEEDS=300` の仮想化の等価性 ok / glogx の `make test` ok・`make lint` 0 issues
- 計測: tfprobe (root=`src/`、1,922 項目、2 サイクル) の `find` の段で 1 打の p95 3.2ms・フレームの最大 5.9ms、goroutine・fd・子は一定。
  上限いっぱいの照らし直しは使い捨てのベンチマーク (10 万件の作ったパス、M3 Max) で 1 打あたり `m` 10.0ms・`mo` 11.6ms・`model` (一致なし) 29.0ms・`fn` 11.9ms。
  **10 万件の root で一致の少ない語を打つと 1 打が 1 フレーム (16ms) を超える**。速くするなら、語を伸ばしたときは前の候補だけを照らす (幅優先の順を保つ印が要る)
- 実物 (`make debug`、root=src): `/` → `model` で閉じた木の中の `*/model.go` が浅い順に並び、Tab で選択が移る。`treefiler/filer/find` → Enter で `filer/finder.go` へ途中を開いて移る
