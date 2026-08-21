# 087 bug: fsnotify に依存する glogx の watch 経路が CI で一度も検証されていない

起票日: 2026-08-21
種別: bug
優先度: **P3** (production の挙動は正しい。CI の検証範囲の穴)

`223050d` で「消えて戻ったディレクトリを再 watch する」回帰テスト
(`TestIssuesWatchReAddsRecreatedDir`) を入れたが、**CI ではこのテストが skip される**。
commit メッセージにしか書いていなかったので issue として追跡する。

## 事実 (実測 2026-08-21)

- CI (`src/glogx` workflow) で `v.watch.w` が nil になり、`WatchList()` の nil 参照で
  panic した (`fsnotify.go:370`)。つまり **CI 環境では `fsnotify.NewWatcher()` が通らない**
- 既存の watch テスト (`TestIssuesWatchReloadsAfterExternalEdit` 等) は CI で緑だが、それは
  **指紋ポーリング経路だけを通っている**ため。イベント経路は検証されていない
- 現状の回避: watcher を作れない環境は `t.Skipf` で理由つきに落とし、「作れるのに張っていない」
  場合だけ `t.Fatal` にしている (環境不対応と配線の退行を区別する形)

## なぜ追跡が必要か

これは 1 つのテストの問題ではなく、**glogx の issues viewer のイベント駆動経路 (fsnotify) 全体が
CI で未検証**という範囲の話。`_claude/rules/adversarial-review-own-safeguards.md` に今日足した
「新設した検査が CI で実際に走るか、同じ commit で確認する」に照らすと、**明示的に受け入れた
例外**なので、受け入れたことを記録に残しておく必要がある (黙って未カバーにしない)。

## 調べる順 (着手時)

1. **なぜ CI で watcher が作れないのか**を特定する。候補: `ubuntu-slim` コンテナの inotify
   インスタンス上限 / `fs.inotify.max_user_instances` / seccomp。`inotify_init` を直接叩く
   最小 Go プログラムを CI で 1 回走らせれば切り分けられる
2. 直せるなら直す (runner を変える / ulimit を上げる / 別 runner で 1 job だけ走らせる)
3. 直せないなら、**watcher を使わずに不変条件を観測する形**にする。候補: `Add` を通す薄い seam
   (`var watchAdd = func(...)`) を production に置き、テストは呼び出し回数と引数で「消えて戻った
   ディレクトリを再 Add する」を確認する (実 fsnotify を要さない)
4. どちらも採らないと決めたら、その判断を **issue ではなくコード直近のコメント**に移して閉じる
   (`pending-issue-rationale-in-code.md`)

## やってはいけないこと

- **`t.Skip` を消して「CI で走っている」ことにしない**。CI で watcher が作れないのは実測なので、
  消すと CI が赤くなるだけで検証は増えない
- ローカルだけで変異検証して「守られている」と報告しない (今日の時点では手元でのみ red を確認済み:
  ledger 方式に戻すと red)

## 関連

- `_claude/rules/adversarial-review-own-safeguards.md`「その機構が CI で実際に走るか確認する」
- `_claude/rules/mutation-verify-new-tests.md`「本番と同じ制約の下で観測しているか」
- `223050d` (この skip を入れた commit) / `f6c5efd` (再 Add 自体の修正)
