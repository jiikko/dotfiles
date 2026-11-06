# 1041 (feat): make test の結果に、走っている間に作業ツリーが変わったかを出す

起票日: 2026-11-06

## 概要

背景の `make test` が走っている同じ worktree を編集しても、結果はそのまま緑 / 赤として読まれる。retro 1040 の提案 C。
同じ日に 2 つのセッションで、背景の `make test` が走っている同じ worktree を編集した (retro 1038 の項目 2・retro 1040 の項目 2)。rule (`verify-execution-not-just-exit-code.md`) はあるが、レビューの結果と検査が並んで返る場面で読み直されず、規範だけでは止まっていない。

## 対応

- `scripts/tree_fingerprint.sh` (`print` / `check`): 作業ツリーの指紋 = HEAD・`git status --porcelain -z --untracked-files=all` の一覧 (`tmp/` は除く。CI の checkout では ignore されない)・
  変更と untracked の各パスの mode と大きさ・symlink の指す先・読める普通のファイルの中身の hash (`git hash-object --stdin-paths` の 1 プロセス)。repo の root で読む
- `Makefile`: `test` は `serial-run` の順番待ちの後に `test-guarded` を通り、`test-all` の前後で指紋を比べる。変わったら、または比べられなかったら赤にする
  (本体の赤はそのまま残す)。待ちの前には取らない (待っている間の編集は検査に混ざらない)
- CI は `make test` を呼ばず下位の target を呼ぶので、CI には効かない (手元の入口の守り)

## 検出しないもの (script の冒頭が正本)

走っている間に変えて元に戻した編集・読めないファイルの大きさを変えない書き換え・ignore されたものと `tmp/`・HEAD を動かさない ref の変更・submodule の中身・index だけの変化・改行を含むパス。
更新時刻は入れない (中身を変えない保存し直しで赤にしない)。

## 進捗

- [x] 実装とテスト (`tests/scripts/test_tree_fingerprint.sh` 19 件。global の git 設定を切り離し、一覧が同じまま属性だけが変わる状態から試す・`test-guarded` の rc を本物の make で試す)
- [x] 敵対的レビュー 4 周 (opus。1042 と同じ周で回した。採否は下)
  - 1 周目 P1: 一覧の最後が消えたファイル・rename の旧パスだと、ループの `[ -f ] && printf` が rc=1 を返し、pipefail で指紋が取れない扱いになり、`make test` が黙って緑になる → `if` にした。変異でも同じ形を先に見つけていた
  - 1 周目 P2: 比べられないときに rc を変えない → 赤にした / mode の変更を見逃す → mode を入れた / テストが削除・rename・mode・symlink を試していない → 足した
  - 2 周目: 足したテストのうち 3 本 (削除・mode・symlink) が一覧の変化だけで赤になり、属性を検査していなかった (変異 3 本が緑) → 状態の作り方を直した / 更新時刻で中身を変えない保存し直しが赤になる → 外した / サブディレクトリから打つと中身の比べが消える → root で読む
  - 3 周目: 読めないパスで `hash-object` が止まり、後ろのファイルの中身の比べが消える → 読めないファイルを渡さない
  - 4 周目: 読めないファイルのテストが、並び順の都合で検査になっていない → 読めないファイルを一覧の前に来る追跡済みの変更にした
  - 記録のみ: 中断 (Ctrl-C) で check は走らない (rc は残る)・`make -n test` でも指紋の取得が走る (読むだけ)・変更の多い木では stat を 1 件ずつ起こす
- [x] 変異で red を確認 (mutate-verify): 中身の hash を外す・mode を外す・symlink の行を外す・tmp/ の除外を外す・HEAD を外す・root へ移らない・読めないファイルを渡す・
  `test` が `test-guarded` を通らない・本体の赤を潰す・check の赤を無視する。等価だったもの: 属性のループを `&&` に戻す (今の並びでは { } の最後の文でない)
- [x] `tests/scripts/test_make_serial_run.sh` の `test` の配線の検査を `test-guarded` に合わせた
- [x] root の `make test` rc=0 (最終の木で。この守り自身が走り、発火しなかった)
