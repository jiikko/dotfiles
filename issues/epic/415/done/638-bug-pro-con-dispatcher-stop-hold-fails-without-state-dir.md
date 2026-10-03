# 638 (bug): pro-con dispatcher --stop が、状態の置き場が無いと止めた印を置けず、それでも rc 0 で返る

起票日: 2026-10-04

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

状態の置き場 (`state/`) がまだ無いところで `pro-con dispatcher --stop` を打つと、「止めた印を置けない」と stderr に出るのに rc 0 で返る。
止めた印 (`state/dispatcher-held`。`store/held.go` の `HeldFile`。issue 459) が置かれないので、開いている画面の keeper が dispatcher を起こし直しうる (印の目的が果たされない)。
548 の敵対的レビュー (2 周目 1/3) が範囲外として見つけた。

## 再現 (2026-10-04 実測)

使い捨ての e2e の置き場で、`src/pro-con` を build したバイナリを使った (本番の置き場には触れていない):

```sh
root=<空のディレクトリ>
pro-con dispatcher --stop --e2e "$root" >out 2>err; echo rc=$?
```

- rc=0、stdout は空
- stderr: `pro-con dispatcher --stop: 止めた印を置けない (開いている画面が dispatcher を起こし直しうる): open <root>/state/.dispatcher-held.tmp-…: no such file or directory`
- 終わった後には `<root>/state/` があり、中は `dispatcher.lock` と `stop-result` だけ (止めた印は無い)。置き場は印を書いた後の停止の処理で作られている

## 原因 (コードを読んだ範囲)

- `dispatchercmd.go` の `--stop` の分岐は、`store.Hold(dir, …)` を停止の処理 (`stopDispatcher`) より先に呼ぶ (「止め終えた直後の keeper に先を越されない」ための順序)
- `store.Hold` → `writeAtomic` (`store/store.go`) は `os.CreateTemp(filepath.Dir(path), …)` で、親のディレクトリを作らない
- 失敗しても stderr に出すだけで停止を続け、停止が成功すれば rc 0 で返る (印を置けなかったことが終了コードに出ない)

## 影響の範囲

- 本物のモード (`--e2e` 無し) も同じ経路を通る見込み: `main.go` の `liveDir(home)` から `runDispatcher` までに置き場を作る処理が無い
  (反証レビューが grep で確認。実行はしていない)。初めて使うマシンや、置き場を消した後に `--stop` を打つと当たる
- `supervise.go` の `ReasonGaveUp` の分岐の `store.Hold` は当たらない: そこへ来る前に `dispatcher.LockSupervisor` → `lockAs` (`dispatcher/lock.go`) が
  `MkdirAll(dir)` で置き場を作っている (コードで確認)

## 対応方針 (案。決めるのは着手時)

1. 印を書く前に置き場を作る。作る場所は `store.Hold` / `writeAtomic` / `--stop` の分岐のどれかで、`--stop` の分岐で作るなら本物のモードも同じ分岐を通す
   (e2e だけで作る実装にしない)。`writeAtomic` の呼び口 (`store/` の archive.go / dispatcher_state.go / doing.go / pm_state.go / purge.go /
   settings.go / store.go / held.go) ごとに、親を誰が作る前提かを洗い、`writeAtomic` 自身に入れるかを決める
2. 印を置けなかったときの終了コードを決める。停止はできたが印が無い (keeper が起こし直しうる) のは部分的な失敗なので、非 0 で返して呼び手 (人・スクリプト) に気づかせる案。
   止める処理そのものは今どおり続ける (印を置けないことを理由に止めないと、止めたい人が止められない)。
   1 を入れると、置き場の無いケースは印を置けるようになり、2 の対象は「lock は取れるが印だけ置けない」形に狭まる

## 受け入れ条件

- [x] 置き場の無い e2e の置き場で `pro-con dispatcher --stop` を打つと、止めた印が置かれる。本物のモードも同じ分岐を通る (e2e だけで作る実装にしない)
- [x] lock は取れるが印だけ置けない状態 (例: `state/dispatcher-held` がディレクトリで rename が失敗する) では、停止は続けたうえで rc が非 0 になる (決めた方針どおり)。
  置き場そのものに書けない (chmod 500) 形は lock が開けずに今でも rc 1 になるので、印の失敗の確かめには使えない

## 関連

- 459 (人が止めた印) / 548 (見つけた経緯)

## 進捗

- 2026-10-04: 起票。反証レビュー (sonnet 1 本、読み取りのみ) を通した。再現・原因・番号の参照は反証されなかった。指摘を反映した:
  印のファイル名 (`dispatcher-held`。`.dispatcher-held.tmp-*` は一時ファイル)、受け入れ条件 2 の作り方 (chmod 500 では lock の失敗と区別できない)、
  本物のモードも同じ分岐を通すこと、`writeAtomic` の呼び口の洗い出し。未確認だった 2 点はコードで確かめて「影響の範囲」に書いた
- 2026-10-04: 実装 (commit「dispatcher --stop が置き場の無いときも止めた印を置き、置けなければ rc 1 で知らせる (638)」)
  - 置き場は `store.Hold` で作る (`MkdirAll`)。`writeAtomic` には入れない: ほかの呼び口 (archive / dispatcher_state / doing / pm_state / purge / settings /
    store) は dispatcher か monitor が lock を取った後 (`lockAs` が置き場を作る) にしか走らず、そこで黙って作ると誤ったパスへの書き込みを隠す
  - 印を置けなくても止める処理は続け、止め終えたら rc 1 と「止めたが、止めた印は置けていない」を出す。止める処理も失敗したら今までどおりその理由で rc 1
  - テストの準備 (`heldRoot`) は置き場を先に作っていた (本番の初期状態と違う) ので、作らない置き場のテストを足した
- 実機 (build したバイナリ + 使い捨ての e2e の置き場): 空の置き場で `--stop` → rc 0・印 `dispatcher-held` あり。`state/dispatcher-held` をディレクトリにして `--stop` →
  rc 1・2 行の知らせ・stop-result は `ok` (止める処理は済んだ)。一時ファイルは rename の失敗で消える (`writeAtomic`)。
  本物のモード (`--e2e` 無し) は手元の PG を巻き込まない隔離を取りきれないので実行していない (印の書き方と rc は同じ分岐を通る。下の Hold のテストが守る)
- 変異 6 本 red: Hold の MkdirAll を外す (main のテストと store のテストの両方) / 印の失敗を rc に出さない / 印を置けない時点で抜ける (2 つの書き方で、assert を 1 つずつ確かめた)
- 敵対的レビュー (opus 1 本): P1 / P2 なし
  - rc 1 で壊れる呼び手はいない: コードから --stop を起こすのは `stopCmd` だけで必ず `--from-screen` (印を置かない)。tests / bin / scripts / _claude に --stop を打つものは無く、README と help は rc に触れていない
  - P3 (採用): 本物のモードで印を置くことを守るテストが無かった (置き場を作る処理を e2e の分岐へ移す変異が緑) → `store/held_test.go` を足した
  - P3 (記録・今回より前から): 「止める前に印を置く」(459) の順序を守るテストが無い (--stop の頭で止める処理を先に呼ぶ変異で、全テストが緑)。
    順序を観測する口が無く、印の時刻 (RFC3339) と stop-result の時刻は同じ 1 秒の中で区別できないので、今回は足していない。
    trigger: 止め終えた直後に画面の keeper が起こし直す事象が出たら、順序を観測できる seam を足す
