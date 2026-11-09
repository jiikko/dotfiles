# 1047 (research): treefiler / tuikit の重複探しで使った dupscan の所見 (2026-11-09)

起票日: 2026-11-09

## 概要

dupscan-dogfood の skill で src/treefiler と src/tuikit の重複を探して直したときの摩擦と提案。摩擦 3 件・提案 1 件。
F1・P1・F3 はこのセッションで dupscan を直した。F2 は直していない (下の対応方針)。既存の issue との重複照合はしていない (skill の 3 節)。
所見の記録と、実装した提案の結果だけなので、反証レビューは省いた (skill の 5 節)。

## 開発タスクの結果

- treefiler: `filer/settings.go` の intItem / choiceItem / boolItem を valueItem に寄せた (sweep の節 2 の L1m の組。設定の項目の「解く → 範囲を確かめる → 書く」は同じ規則)。
  読めない真偽・選択肢に無い値を捨てる経路はどのテストも見ていなかったので、TestLoadSettingsReportsBadLines に足した (選択肢の変異は足す前は緑のままだった)
- 寄せなかったもの (理由):
  - tuikit の foreground.SetStdin/SetStdout/SetStderr・listnav の List.Advance/Pager.Advance: 既に allow-group で「意図して分けている」と決まっている
  - treefiler の explode.finished ~ watcher.pause ほか節 3 の 13 対: lock → defer unlock → 1 文の accessor で寄せる先が無い (F2)
  - treefiler の 6 つの take: 意味が違う (gitWatch は snapshot を版で判定・walker は差分を手放す・watcher / opener は溜まった slice を空にする)
  - glogx の doctorview と tuikit の termwidth のテストの childContext: module をまたぐ写し。寄せ先は testwait くらいで、consumer 6 つの tuikit にテストだけの依存が増える (restartable は擬似バージョンで固定)。doc で互いを指し合っている
  - model.go の init の o / O の closure・tfprobe の cycle / tile の closure: 1 つの呼び出しを 1 回するだけの委譲 (sweep の印のとおり)

## 摩擦

### F1 起点を module に絞った sweep で、allow-group を付けた組が節 2 に出る。案内の文が「出していない」と読める (直した)
- 場面: `dupscan sweep src/tuikit` (と `src/treefiler`)
- 期待: allow-group で「意図して分けている」と決めた組は出ないか、出る理由が分かる
- 実際: 節 2 に 2 組とも出る。冒頭は「人数の合わない allow-group 2 個は、起点の中の L1 の同じ本体の単位が人数より少ないので出していない (…)」。
  出していないのは 🚨 の行で、組は出している (README の仕様どおり。`sweep src` からなら出ない)。tuikit の sweep の出力は 28 行
- その後: README の allow-group の節を読んで仕様と確かめた (Read 2 回)
- 種類: 使い方が分かりにくい (文言)
- 対応: 「… 個は効かせず、その組を下の節に出している (…🚨 の行も出さない。repo の root から打てば効く…)」に直した (commit「feat(dupscan): sweep -focus …」)

### F2 sweep の節 3 で、treefiler が絡む対 13 件が全部「lock → defer unlock → 1 文」の accessor どうし (直していない)
- 場面: `dupscan sweep -limit 0 src` の節 3 を treefiler / tuikit で絞った
- 実際: 13 対 (Dice 0.600〜0.618・w=42) が全部 `mu.Lock(); defer mu.Unlock(); return x.f` / `x.f = v` の形 (explode.finished・watcher.pause と、mac-lan-manager の lanmgrtest の accessor 6 個・pro-con の Stop など)。
  寄せる先は無い。残る 1 対 (pro-con の writeFile ~ treefiler の mustWrite。module をまたぐテストの helper) だけが読む価値のある候補だった
- その後: 本体を 2 つ Read して決まった形と確かめ、見送った
- 種類: 雑音
- 提案: 節 2 の「印: 全員の本体が 1 つの呼び出しを 1 回するだけ」と同じく、「lock と defer unlock の後に文が 1 つ」の対に印を付けるか、畳む。
  言語ごとに形の判定が要る (Go の sync.Mutex と Swift の lock / withLock は別の書き方) ので、このセッションでは直していない

### F3 gate -v の大半が、変更と無関係な「効いていない除外」の同じ理由の繰り返し (直した)
- 場面: settings.go を寄せた後、自分が生んだ組・消した組を見るため repo root で `dupscan gate -v` (skill dupscan の 3 節の案内どおり)
- 実際: rc=0、11,377 B。うち「効いていない除外」が 11 件で、全部が同じ 300 字ほどの理由の段落 (「釘の指す単位は釘の本体のまま今も在る…」) を 1 件ずつ繰り返していた。どれも今回の変更と無関係 (doctor・dupscan・glogx の除外)
- その後: head で切って読んだ
- 種類: 出力が大きい
- 対応: 同じ理由の全文は最初の 1 件だけに出し、2 件目からは「上の N 件目の除外と同じ理由」と出す (commit「fix(dupscan): 効いていない除外の同じ理由の全文は …」)。
  「変わった単位に関係しない除外は -v でも数だけにする」案は、gate -v が除外の棚卸しにも使われているので見送った (report -v との役割分担を決めてから)

## 提案

### P1 module をまたぐ相手も見つつ、注目する module が絡む組だけを出したい (実装した: `sweep -focus`)
- 場面: treefiler / tuikit の重複を探す。module の中だけなら `dupscan sweep src/treefiler` だが、他の module との写しが見えない。`dupscan sweep src` は 271 行で、節 3 は 20 対で打ち切り (「… 他 62 対」)
- 今の回避: `sweep -limit 0 src` をファイルへ落とし (45,227 B)、awk で節を切って `grep -E 'treefiler|tuikit'` (3 回)。組の見出しとメンバーが別の行なので、grep だけでは組が切れる
- 提案 (拡張): `sweep -focus <dir>`。起点は広いまま走査し、メンバーに <dir> の単位を 1 つ以上含む組・対・束・型だけを出す
- 確かめたこと: repo root で `sweep -limit 0 -focus src/treefiler src` は 7,643 B (107 行)。節 3 の対は 13 件で、全体の出力を awk で絞った 13 件と一致した。起点の外の -focus は rc=2
  - 実物で打って、静かにした組の数が 10 → 180 になる自分の不具合を見つけた (絞った後に数えていた)。テストでは見えていなかったので、0 で固定するテストを足した
- 失うもの: 無し (表示だけを絞る。走査・静かにした組の数・allow-group の人数は起点の全体で数える)

## 集計

- dupscan を使った回数 約 17 (sweep 7・index 6・search 1・explain 1・callers 1・gate 1)。grep / rg に戻した回数 4 (sweep の出力を節で切って module で絞る 3 回 (P1) / 型引数つきの filter の helper を正規表現で引き直す 1 回 (`index -name filter` と `search` で 0 件だった確認))
- 役に立った場面: `index -returns item` で設定の項目の constructor 4 つが 741 B で出て、寄せる先の helper が無いことを確かめられた。sweep の節 2 の L1m が settings.go の写しを指した (寄せた 1 件)。`callers childContext src` で module をまたぐ写しの呼び出し元が 2 件と分かった
- 重複の組の数 (前後): 未計測 (前後で同じ数え方の `-format json -limit 0 .` を取っていない)。sweep src/treefiler の節 2 の組は 4 → 3 (intItem / boolItem の組が消えた)

## 進捗

- [x] F1 (案内の文言) と P1 (`sweep -focus`) を直した。変異 6 本で red (節 1・2 の組・節 3 の対・節 4 の束の絞り込み・起点の外の確認・注目の行・静かにした組を数える位置)。節 5・6 の絞り込みは fixture に該当が無く確かめていない
- [x] F3 を直した。変異 1 本で red
- [ ] F2 (lock の accessor の対の印) — 未着手。言語ごとの形の判定の設計から
