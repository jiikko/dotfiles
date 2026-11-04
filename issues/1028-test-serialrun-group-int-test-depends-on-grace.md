# 1028 (test): serialrun の TestGroupIntIsNotForwardedTwice が root の make test で 1 回落ちた (仮説: 子の trap が猶予 3 秒のうちに走らなかった)

起票日: 2026-11-04

## 概要

root の `make test` (2026-11-04、issues viewer の `u` の検証の 1 回目) で `src/serialrun` の `TestGroupIntIsNotForwardedTwice` が落ちた。
同じ HEAD で root の `make test` をあと 2 回回すと 2 回とも通った。単独でも 3 回続けて通った。

```
main_test.go:1129: 子が INT を 1 回受ける が 10s 待っても成り立たない (待ち始めから 10.006s)
  --- このテストのプロセスの子孫
  33434 31312 50880 0 S  00:29 .../serialrun.test ...
  38627 33434 38627 0 Z  00:10 <defunct>        ← serial-run (自分のグループの長) は終わっている
```

子の `sh` は一覧に無く、INT を数えるファイルは空だった。

## 原因の仮説 (実測で絞った範囲。再現はしていない)

このテストは、グループ宛ての INT を子の `sh` が trap で 1 回受け、serial-run が伝え直さないこと (2 回目が来ないこと) を見る。
serial-run は端末の外からの INT を受けると、猶予 (`SERIAL_RUN_TEST_INT_GRACE`。このテストは 3 秒) の後に子孫ごと止める。

- 落ちたときの形 (serial-run は猶予の後に終わり、子は消え、INT の行が 0) に合うのは「子の trap が猶予 3 秒のうちに走らず、serial-run が先に子を止めた」だけ
- 退けた仮説:
  - **INT が無視された状態でテストが起こされた**: root の Makefile の `run_go_projects` は `( make -C $dir test … ) &` で起こすので、
    非対話の sh の背景のジョブとして INT / QUIT が無視される。しかし `go test` (cmd/go) が INT の扱いを既定に戻すので、テストのバイナリと
    その子は既定で始まる (`/bin/sh -c '( go test … ) & wait'` の中で `signal.Ignored(SIGINT)=false`・孫の perl も DEFAULT を実測)。
    しかも無視されていたなら serial-run も INT を Notify しない (`runQueued`) ので猶予で終わらず、落ちたときの形 (serial-run が終わっている) と合わない
    (`( sr.test … ) &` で INT を無視させて起こすと、serial-run も子も生きたまま 10 秒で落ちる。形が違う)
  - **INT が子に届いていない**: 子は serial-run と同じグループで起こされる (`queue.go` の Pgid の注記)。グループ宛ての INT は子にも届く
- 再現はしていない: テストのバイナリ 8 本を並べて 48 回 (INT を既定に戻して起こした)、glogx・treefiler・doctor・mac-lan-manager の `go test -race` を
  同時に回して load average 19 の中で 15 回、どちらも全部通った。落ちた回は他の session の `make test` も serial-run の列に並んでいた (負荷は未実測)

trap が走らなかった理由 (スケジュールの遅れか、FIFO の open で止まっている間の trap の扱いか) は決めていない。

## 対応

- テストの猶予を 60 秒にし、終わらせるのは TERM にする (serial-run は TERM で子孫ごと止めて返る)。判定 (1 回受ける・1 秒の窓で 2 回目が来ない) は変えない。
  これで「子の trap が猶予のうちに走るか」がテストの合否から外れる。🚨 効くのは trap の遅れが 3 秒を越えて 10 秒 (`testwait.Until` の上限) に収まる場合だけで、
  10 秒を越えて遅れれば同じ形で落ちる。猶予の後に止める振る舞いは `TestIntToSerialRunPidStopsChildAfterGrace` と
  `TestIntGraceStopsGrandchildren` が別に見ている
- 次に同じテストが落ちたら観測に戻る。猶予を 60 秒にしたので、失敗時の子孫の一覧で区別がつく (前の形では猶予の 3 秒で `sh` も消えていたので区別できなかった):
  `sh` が生きたまま「1 回受ける」が成り立たないなら trap が 10 秒走っていない (スケジュールの遅れだけで 10 秒を越えるかは未実測)、
  `sh` が居ないなら別の経路で子が止められている
- 同じ仕組み (子の trap が猶予のうちに走ること) に頼るテストは `TestIntLetsChildFinishCleanupWithinGrace` (猶予 10 秒・後始末 1 秒) もある。
  こちらは「猶予のうちなら後始末を最後まで走らせる」が主張そのものなので猶予を外せない。余裕は 9 秒あり、今回は変えない

## 進捗

- [x] テストの猶予を 60 秒・TERM で終える形に直す。変異で red を確認 (serial-run が INT を子へ伝え直す `syscall.Kill(tgt.Root, got)` を足すと「2 回届いた」で落ちる)
- [x] 反証レビュー (sonnet、読み取りのみ): P2 (題名と原因の節が未確認の原因を断定していた・修正が効く範囲) と P3 2 件 (未実測の明記・観測が上がった点) を反映。反証できなかった: Notify の条件・同じグループ・TERM で子孫ごと止まる・別のテストの実在・同じ仕組みのテストは他に無い・検知力は落ちていない
