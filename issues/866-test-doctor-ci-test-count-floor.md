# 866 (test): doctor の CI のテスト本数の下限 (15 本) を今の本数に合わせる

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-22〜）

起票日: 2026-10-22

## 概要

`.github/workflows/doctor.yml` の「doctor テストが実際に走ったことを確認する」step は、行頭の `--- PASS: TestDoctor` (トップレベルのテストだけ。
字下げされたサブテストの行は数えない) を数えて 15 本未満なら落とす。下限はコメントのとおり「実測 2026-09-03 時点で 19 本」に合わせた値だが、今は 108 本走っている
(run 39762353469 = a73854c0 のログに `doctor テスト: 108 本が PASS`)。doctor 画面のテストが `src/glogx/internal/doctorview/` へ
移ってから増えた。

下限 15 のままだと、`-run` のパターンや置き場所の変更で 9 割近いテストが黙って抜けても緑になる。この gate が止めたいのは
「パターンとテスト名が食い違って vacuous pass になる」形 (`_claude/rules/verify-execution-not-just-exit-code.md`) で、
今の下限はその一部 (全部が抜ける・ほぼ全部が抜ける) しか見ていない。

## 対応方針

- 下限を今の本数に近い値へ上げ、コメントの実測値と日付を更新する (どこまで寄せるかは、テストを消す正当な変更で毎回
  下限を直す手間との兼ね合い。例: 100)
- 案: 本数の固定値をやめ、`-run` を付けない `go test -list 'TestDoctor' . ./internal/doctorview` の件数と PASS の件数を突き合わせる
  (下限を手で保守しなくてよくなる)。今は `func TestDoctor` で始まるトップレベル関数が 108 個で、PASS の 108 本と一致する。採るなら:
  - list と run が同じ package 集合を見るよう、同じ変数から組む (今は `. ./internal/doctorview` が直書き)
  - list の出力は `ok  <pkg>` の行が混ざり、`-run` と同じ部分一致なので、gate と同じく `^TestDoctor` で絞る
    (`TestFooDoctor` のような名前が入ると list だけが数えて偽の赤になる)
  - skip が入ると PASS が list より減る。今は skip 0 件だが、突き合わせは SKIP を別に数える形にする
  - `-list` もテストバイナリを作って `TestMain` を走らせる (`src/glogx` と `internal/doctorview` に `main_test.go` がある)。副作用が無いかを見る
- 変えたら、下限を割る変異 (`-run` のパターンを 1 package 分外す等) で step が赤になることを確かめる

## 関連ファイル

- `.github/workflows/doctor.yml` (「glogx の doctor 配線テスト」と「doctor テストが実際に走ったことを確認する」の step)
- `src/doctor/README.md` のテスト本数の下限の項 (2026-10-22 に本数を 108 本へ直した。下限を変えたらここも直す)

## 進捗

- [x] 下限の代わりに、PASS の本数を glogx の中の `func TestDoctor*` の数と突き合わせ、1 本以上を求める形にした (commit「ci(doctor): doctor テストの本数を固定の下限でなく定義の数と突き合わせ、test の step に pipefail を付ける」)。`-run` は `^TestDoctor` に揃えた
  - `go test -list` との突き合わせは採らない: 同じパターンで数えるので、名前を変えて全部外れると両方 0 で一致して通る
  - 敵対的レビュー (opus) の P1 (前からの形): 既定の shell は pipefail を付けず `go test | tee` の rc が 0 になり、テスト関数の外の失敗を通す。test の step に `shell: bash` を付けた
  - 記録だけ: 改行の無い stdout を出すテスト・build tag で外れるファイル・testdata の下の `_test.go` は数え方が合わず偽の赤になりうる (今の木には 0 件)
- [x] gate の script を手元で 3 つの形で回した (CI の上で赤は見ていない): 今のまま 緑 (108/108) / package を 1 つ外す 赤 (4/108) / パターンの書き損じ 赤 (0/108)
