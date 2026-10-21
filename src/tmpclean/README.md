# tmpclean

一時領域 (Claude Code の scratchpad = `/private/tmp/claude-<uid>/<project>/<session-id>/`、`$TMPDIR` の直下) を数え、
**ユーザーが承認した plan の範囲だけ**、**生きているもの・使用中のものを避けて**、**ゴミ箱へ移す** CLI `bin/tmpclean`。
手順 (数える → 候補を見せる → 承認 → 移す) は skill `cleanup-tmp-dir`。dotfiles の `tmp/` は扱わない (`make clean-tmp`)。

```
tmpclean count [-json]
tmpclean plan  -kinds session,tmpdir -out <dir> [-session-days 7] [-tmpdir-days 3] [-json]
tmpclean apply -plan <dir>/plan-<ID>.json -approve <ID> [-json]
```

- `plan` は何も動かさず、候補を `<dir>/plan-<ID>.json` に書く。ID は中身 (作成時刻・選ぶ条件・候補) の hash
- `apply` は plan の ID を計算し直して `-approve` と照合し (1 時間を超えた plan は断る)、**plan に載っていて今も候補であるもの**だけを、
  1 件ずつ判定し直してから `~/.Trash/tmpclean-<日時>-<ID>/<kind>/[<project>/]<名前>` へ移す。何をどこへ移したかは `<dir>/moved-<ID>.jsonl` (1 件ごとに fsync)
- 終了コード (doctor の exitcode とは別の語彙): 0 = 走り切った / 1 = 移せなかったものがある / 2 = 前提の検査で止めた (1 件も移していない) /
  3 = 実行環境の失敗 / 4 = 途中で止めた (一部は移した)

## 守り (層を重ねる。どれも回帰テストと変異で固定してある)

生きているセッションは外から完全には見分けられない (claude 本体は `CLAUDE_CODE_SESSION_ID` を環境変数に持たず、Desktop から起動したものは
`PWD` も持たず、会話の記録も scratchpad も開いたままにしない。2026-10-21 実測)。なので層を重ね、最後の層をゴミ箱 (戻せる) にしている。

| 層 | 中身 |
|---|---|
| プロジェクト単位 | 生きている claude (`pgrep -a -x claude` ∪ UCOMM が版番号 ∪ command に `claude-code` / `/claude/versions/` / `/claude`) の cwd から本体と同じ規則で project 名を導き、その project のセッションは移さない。200 字を超える名前は先頭 200 字の前方一致 |
| ID | 子孫のシェルの環境変数の `CLAUDE_CODE_SESSION_ID`・argv の `--resume` / `-r` / `--session-id`・自分の ID |
| 判定不能で止める | cwd を読めない・ASCII 以外や `\` (lsof のエスケープ) を含む・導いた project が無い・起動時の `PWD` と違う・自分の claude の導出が自分の scratchpad と合わない → `session` を止める (2) |
| 使用中 | lsof の全体の写し (100 件未満なら失敗とみなす) と、移す直前の 1 件の取り直し (stdout・stderr・rc の 3 つで判定。判定不能は残す) |
| 古さ | 自身と中の全部 (ファイルも dir も) が N 日より前。走査に失敗したら残す。`$TMPDIR` では socket / FIFO を含むもの・`com.apple.*`・rename を拒む属性 (immutable・append・SIP の restricted / nounlink) が付いたもの・symlink も残す |
| plan を信用しない | apply は候補を作り直して plan との交わりだけを移す。dev/ino が変わったものは残す |
| 検査点 | `safefs.TrashMove` に渡す Guard が、根の配下の決まった深さ以外と今回の移し先の外を拒否する。ゴミ箱の経路の symlink は何も作る前に断る |

## 検出しないもの

- 移す直前の確認から移動までの窓 (1 件ぶん)
- パスだけ覚えて、まだ何も開いておらず socket も置いていないプロセス (N 日の閾値で守る)
- Desktop から起動し、起動後に cwd が移り、子のプロセスも走っていない生きたセッション (session の既定を 7 日にしてある。移されてもゴミ箱から戻せる)
- 承認の結び付けは暗号的ではない (書き換えた plan の ID を計算し直せば通る。ただし今の候補との交わりしか移さないので、移すものは増やせない)
- 見落とした生きたセッションは、移した後も開いた fd でゴミ箱の中へ書き続ける
- まだ知らない起動の形の claude (拾い方に足す)。本体の「200 字 + -hash」は実装を読んだだけで、実物の長い名前では未確認

## 構成

公開は `Run` と、数えるだけの `Count` (glogx の doctor の「一時領域」タブが使う。消す部分は公開しない)。`cmd/tmpclean/main.go` は `Run` を呼ぶ 1 行。外部コマンドは `internal/runner` だけが起こす (depguard)。
テストは `deps` に fake の ps / pgrep / lsof と使い捨ての根を入れ、`safefs/sandbox` で根の外への操作を実行前に拒否する (TestMain で違反を見る)。

- `make -C src/tmpclean lint` / `test`
