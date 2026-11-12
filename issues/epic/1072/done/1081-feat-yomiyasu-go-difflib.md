# 1081 (feat): yomiyasu の diff の照合 (difflib と本家の速い照合) を Go で作り直す

起票日: 2026-11-13

## 概要

1076 (diff) の一部を切り出したもの (dotfiles-c5 からの分担。担当は dotfiles-bb)。本家の `yomiyasu_diff.py` は照合を `difflib.SequenceMatcher` に頼り、
長い文書で速くするために、difflib と同じ答えを別の求め方で出す `_ExactSequenceMatcher` を持つ (本家のソースの注記では v2.0.1 から)。
この 2 つを `src/ja-writing/internal/difflib` に写し、本家の Python と同じ答えを返すことを oracle の記録で確かめる。1076 はこの package を使う。

## 本家の使い方 (v3.1.0、`db78b9bc`)

- 照合を作る呼び出しは全部 `SequenceMatcher(None, a, b, autojunk=False)` か `_ExactSequenceMatcher(None, a, b, autojunk=False)` の形 (`isjunk` を渡さず、`autojunk` は常に False)。
  `ast` で数えると照合を作る呼び出しは 15 か所 (`SequenceMatcher` 11・`_ExactSequenceMatcher` 4。1076 の本文の「21 か所」は字面の数で、docstring とコメントの言及を含む)。
  本家が使う `difflib.` の属性は `SequenceMatcher` と `Match` だけで、`get_close_matches`・`ndiff` など中で autojunk=True の照合を作る関数は使わない
- 使う API: `get_matching_blocks`・`get_opcodes`・`ratio`・`quick_ratio`・`real_quick_ratio`・`set_seq1` / `set_seq2` (同じ照合を使い回して比べる相手だけを替える)。
  `find_longest_match` は `_ExactSequenceMatcher` が上書きしているが、本家の他のコードから直接は呼ばない。`get_grouped_opcodes` は使わない
- `set_seq1` / `set_seq2` の使い回しは 2 つの形: 普通の照合で `set_seq2(直す前)` のあと `set_seq1(直した文)` を回す形 (3005 行あたり) と、
  `_ExactSequenceMatcher(None, "", 文)` を作って `set_seq1(元の文)` で a だけを替えて何度も `ratio` を求める形 (1902 行あたり。b から作った種の表と長さちょうどの表は a をまたいで残る)
- 比べる並び: 文字列 (1 字ずつ。Python の str の添字 = コードポイント) と、文字列のリスト (行・見出しの文字・文・文の断片。tuple などのリストは無い)
- `_ExactSequenceMatcher` (yomiyasu_diff.py の `_MATCH_SMALL_AREA` から `_dp` まで) は、文字列どうしで捨てる要素の無いときだけ速い求め方を使う。
  求め方は範囲の大きさで選ぶ: difflib と同じ DP・`str.find`・長さちょうどの部分文字列の表 (`_probe`・`_probe_local`)・種で探す (`_seeded`)・接尾辞オートマトン・長さの二分探索
  (`_bisect_length`。長さ 256 以上は 1 字ずつずらす hash、それ未満は部分文字列の hash と `str.find`)。二分探索に入るのは b の範囲が 65,536 字を超え、種の段が候補の多さで止まったときだけ。
  どれも「いちばん長く、a で最も前、同じ a なら b で最も前」を求めるので、選び方 (定数) は時間だけを変える、というのが本家の主張

## 対応方針

1. **API** (Go の generic): `New[T comparable](a, b []T)` が `SequenceMatcher(None, a, b, autojunk=False)`、`NewExact(a, b []rune)` が `_ExactSequenceMatcher(None, a, b, autojunk=False)`。
   メソッドは `SetSeq1` `SetSeq2` `SetSeqs` `FindLongestMatch` `GetMatchingBlocks` `GetOpcodes` `Ratio` `QuickRatio` `RealQuickRatio`。
   - `isjunk` と `autojunk` の引数は持たない。本家は使わず、使わない引数を写すと記録で確かめられない経路が増える。本家が使い始めたら記録の driver の `ast` の検査が止まる (下の 3)
   - 文字列は `[]rune` で渡す (Python の str の添字と同じく 1 コードポイント = 1 要素。単独のサロゲートは 1073 の WTF-8 の読み方で 1 要素にする)
   - `FindLongestMatch` は範囲が並びの中にある呼び方だけを受ける (本家は範囲の外を渡さない。外なら panic)
   - `Ratio` などの float は Python と同じ式 (`2.0 * matches / length`、length が 0 なら 1.0) で求める
2. **速い照合の写し**: 範囲ごとの求め方の選び方 (定数と順) は本家と同じにする。Go に無い道具の所だけ置き換え、答えは変えない:
   - Python の `hash(str)` は使えないので、部分文字列の hash は全部 1 字ずつずらす多項式の hash (法 2^61-1。本家の `_rolling_hashes` と同じ式) で取る。
     本家の式は Python の多倍長整数と正の剰余に頼るので、Go では 128 ビットの積 (`bits.Mul64`) で剰余を取り、引き算は法を足して負にしない。
     ずらして求めた値と、部分を直接求めた値が同じことを試験で確かめる (同じ並びが別の値になると、hash の合わない位置を飛ばす所で一致を取りこぼす)。
     hash は表を引く手がかりにだけ使い、引いた位置は必ず字で比べる (本家と同じ)
   - `str.find` は `[]rune` の上の探索 (KMP など、最悪でも線形) にする。`_common_prefix_len` は Go では 1 字ずつ比べる (本家の倍々の切り出しは C の速さで比べるための工夫で、答えは同じ)
   - 置き換えた所は README の表に書く
3. **本家と同じ答えの確かめ (記録)**: 自分用の driver (`oracle/py/record_difflib.py`) と記録 (`testdata/golden/difflib.json`)。入力は oracle の Go が決まった算法で作り (乱数は使わない)、
   driver は同じ入力を Python の `difflib.SequenceMatcher` と本家の `_ExactSequenceMatcher` の両方に通し、**2 つが違えば止まる** (本家の前提を Python の側で確かめる。
   記録は 1 つの結果だけを持ち、Go の `New` と `NewExact` の両方をそれと比べる)。
   - difflib が長さの 2 乗で遅くなる大きい入力 (同じ字の 6 万字を超える繰り返しなど。二分探索の経路) は、本家の `_ExactSequenceMatcher` だけを通して記録する (記録に印を付け、Go は `NewExact` だけを比べる)
   - 入力: 手で選んだ端 (空・片方が空・同じ・共通なし・同じ字の繰り返し・サロゲート・BMP の外の字)・小さい字の種類の全組み合わせ (DP の同点の選び方)・
     サロゲートの組 (上位と下位を別の 2 コードポイントで持つ並びと、本物の U+1F600。Python では長さ 2 と 1 で等しくない)・
     字の種類の少ない長い文字列・同じ段落を何十回も繰り返して数か所を直した文書・入力の集まりの直す前と後の組 (`raw_ai` と `yomiyasu_rewritten`)・
     文字列のリスト (行の並び。空行の繰り返し・長い要素・同じ要素の重複を含む)・`set_seq1` / `set_seq2` で使い回す手順 (上の 2 つの形。長い b (4,096 字超え) に長さの違う a を何本も当てる)
   - 大きい入力は記録に中身を書かず、作り方 (部分の文字列と繰り返しの数の列) を書き、Go と driver が同じ作り方で組み立てる
   - driver は本家のソースを `ast` で読み、次のどれかがあれば止まる: 照合を作る呼び出しが `(None, a, b, autojunk=False)` の形でない・`difflib.` の属性が `SequenceMatcher` と `Match` の外・
     照合のメソッドの名前 (`get_grouped_opcodes`・`find_longest_match` を `_ExactSequenceMatcher` の外から呼ぶ形など) が Go の持つ範囲の外。呼び出しの位置を記録に残す
   - header は他の記録と同じ (`TestGoldenFilesShareOneEnvironment` に足す)
4. **Go の中の突き合わせ**: `New` と `NewExact` が同じ答えを返す試験 (同じ字が何度も出る文書・長い文書・字の種類の少ない文字列)。
   速い照合の求め方ごとに、試験の入力で 1 回以上通ったことを数えて確かめる (どこかの求め方が試験から外れたまま緑にならないように)。数える枝: 小さい範囲の DP・`find` の 3 通り・
   長さちょうどの表の 4 通り (範囲だけ・b 全体・表を作らない a 側 / b 側) と表の追い出し・種の段の 4 通り (見つかった・早い打ち切り・無かった・候補が多すぎた)・残りの DP・オートマトン (上限で止まった枝を含む)・二分探索・hash が同じで字の違う位置。
   閾値の定数は「時間だけを変え答えは変えない」ので、試験は閾値を小さくした matcher と hash を弱くした matcher で、短い入力でも全部の枝を通す
   (二分探索と hash の衝突の枝は、本家と同じ閾値では大きい入力か偶然にしか通らない)
5. **所要**: 同じ入力を Python の 2 つと Go の 2 つで交互に複数回測り、README に書く (起動の時間は分ける)。テストの合否に時間は使わない
6. 共有のファイル (`oracle/cmd/golden/main.go`・`oracle/golden_test.go`・README・`src/PACKAGES.md`) への追記は登録の数行と表の行に留める (1074 の係が同じファイルを触っている)

## 受け入れ条件

- [x] `internal/difflib` の `New` が、記録の全入力で Python の `difflib.SequenceMatcher` と同じ値を返す (使う API 全部)
- [x] `NewExact` が、記録の全入力 (difflib が遅すぎて本家の `_ExactSequenceMatcher` だけを通した入力を含む) で本家と同じ値を返す
- [x] 記録の driver が、difflib と本家の `_ExactSequenceMatcher` の答えが同じことを全入力で確かめている
- [x] Go の中で `New` と `NewExact` が同じ答えを返す試験があり、速い照合の求め方が全部試験で通る
- [x] 記録の driver が本家の照合の呼び出しの形・`difflib` の属性・照合のメソッドの名前を `ast` で確かめ、呼び出しの位置を記録に残す
- [x] 所要を Python と Go の両方で測った数字が README にある
- [x] 新しい試験は実装を壊すと落ちることを `mutate-verify-list` で確かめた
- [x] `make -C src/ja-writing lint test`・root の `make test-lint`・`make test-dupscan-gate` が通る

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_diff.py` (`_MATCH_SMALL_AREA` から `class _ExactSequenceMatcher` の `_dp` まで)・Python 3.15 の `Lib/difflib.py`
- `src/ja-writing/internal/difflib/`・`src/ja-writing/oracle/py/record_difflib.py`・`src/ja-writing/oracle/difflib.go`・`src/ja-writing/testdata/golden/difflib.json`
- 1076 (この package を使う diff)・1073 (oracle の仕組み)

## 進捗

- 実装 (2026-11-13、dotfiles-bb): `feat(ja-writing): internal/difflib に difflib と本家の速い照合を写す (issue 1081)`
  - `internal/difflib`: `New[T]` (difflib.SequenceMatcher)・`NewExact` (本家の _ExactSequenceMatcher。閾値は本家と同じ値を `defaultTuning` に持ち、試験だけが小さくする)
  - 記録: `oracle/py/record_difflib.py`・`oracle/difflib.go`・`oracle/difflib_cases.go`・`testdata/golden/difflib.json` (約 560 KB)。入力 559 件 (字の列 478・リスト 76・使い回し 2。本家の速い照合だけを通した大きい入力 6)。
    driver は全入力で difflib と本家の照合の答えが同じことを確かめた (違えば止まる)。本家の照合の呼び出しは ast で 15 か所、使うメソッドは 8 種 (`find_longest_match` `get_matching_blocks` `get_opcodes` `quick_ratio` `ratio` `real_quick_ratio` `set_seq1` `set_seq2`)
  - 他の記録 (patterns・pyre など) は作り直しても 1 バイトも変わらなかった
- 試験: `TestMatchesPythonRecording` (記録の 559 件で `New`・`NewExact` が Python と一致。本家と同じ閾値で、DP・種・候補の多さ・オートマトン・二分探索・b 全体の表の枝を通ることも見る)・
  `TestExactMatchesPlain` (閾値 5 通り × 入力 380 組で `NewExact` と `New` が一致。19 の枝が全部 1 回以上通る)・`TestExactReuseWithSetSeq`・`TestIndexRunesMatchesStringsIndex`・`TestRollingHashesMatchPolyHash`
- 実装を壊して落ちることの確認 (`mutate-verify-list`): 13 本中 12 本が想定どおり赤 (DP の同点・quick_ratio の残り数・左の範囲の上限・種の格子・種の後ろ向きの伸ばし・オートマトンの first・a の側の表の最も前・hash の衝突の比べ直し・b を替えたときの表の破棄・hash の top・KMP の失敗関数・負の rune の hash)。
  緑のまま通った 1 本 (`GetMatchingBlocks` の隣り合う一致をつなぐ処理を外す) は等価: 捨てる要素が無いと隣り合う一致は生まれない (隣り合うなら親の範囲の答えを伸ばせる)。敵対的レビューも Python で乱数 30,000 組を数えて 0 箇所 (isjunk を渡すと 7,496) を確かめた。理由はコードのコメントに書いた
- 敵対的レビュー (opus 1 体、壊す観点): 閾値を乱数で選んだ Go の 83,000 組と、本家の定数を差し替えた Python の 40,000 組で、食い違い・panic・無限ループは出なかった。
  再現したのは P3 の 1 件: 負の rune で hash の前提 (2^61 未満) が崩れ、`NewExact` が `New` と違う答えを返す (Python の str と `pyunicode.Runes` は負の値を作らず、呼び出し元も無い)。
  `code()` で uint32 に収めて直し、試験に負の値を足した (戻すと `TestRollingHashesMatchPolyHash` が赤)。未確認として残したもの: `extendSeeds` の「同じ a で b の位置だけが前」の比べ方は通る入力が 0 回 (同じ格子の位置の種は u の小さい順に並ぶので等価と見ている)
- CI (2026-11-13、227012130): src/ja-writing・Lint・dupscan gate・unused・Tests (heavy) が緑。Tests (rest) の赤は `tests/scripts/test_enumerations_are_derived.sh` の 1 件で、先に入った 1082 の `src/ja-writing/bench/pybench.sh` が shell の lint の対象の外にあるもの (1081 は shell を足していない。1082 の係に伝えた)
- 所要: README の「`difflib` の作りと揃わない範囲」の表 (Go の `NewExact` は本家の Python の速い照合の 2〜9 倍、Go の `New` は Python の difflib の約 40 倍速い)
- 反証レビュー (opus 1 体、2026-11-13): P1 1・P2 3・P3 5。全部を本文に入れた (大きい入力は本家の速い照合だけを記録する・1902 行の使い回し・driver で 2 つの一致を確かめる・
  hash の 128 ビットの剰余・枝の数え方・ast でメソッドと属性も見る・サロゲートの組・本文の数と出典)。反証できなかった主張: 呼び出しの形 15 か所・並びの型・find_longest_match を外から呼ばない・本家の docstring の証明 (読んだ範囲)
