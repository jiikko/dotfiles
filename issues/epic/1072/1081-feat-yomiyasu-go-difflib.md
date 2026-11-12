# 1081 (feat): yomiyasu の diff の照合 (difflib と本家の速い照合) を Go で作り直す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-13〜）

起票日: 2026-11-13

## 概要

1076 (diff) の一部を切り出したもの (dotfiles-c5 からの分担。担当は dotfiles-bb)。本家の `yomiyasu_diff.py` は照合を `difflib.SequenceMatcher` に頼り、
長い文書で速くするために、difflib と同じ答えを別の求め方で出す `_ExactSequenceMatcher` を持つ (v2.0.1 と v2.2.2)。
この 2 つを `src/ja-writing/internal/difflib` に写し、本家の Python と同じ答えを返すことを oracle の記録で確かめる。1076 はこの package を使う。

## 本家の使い方 (v3.1.0、`db78b9bc`)

- 照合を作る呼び出しは全部 `SequenceMatcher(None, a, b, autojunk=False)` か `_ExactSequenceMatcher(None, a, b, autojunk=False)` の形 (`isjunk` を渡さず、`autojunk` は常に False)。
  呼び出しの数は記録の driver が `ast` で数えて記録に残す (1076 の本文の「21 か所」は字面の数で、docstring の言及を含みうる)
- 使う API: `get_matching_blocks`・`get_opcodes`・`ratio`・`quick_ratio`・`real_quick_ratio`・`set_seq1` / `set_seq2` (同じ照合を使い回して比べる相手だけを替える)。
  `find_longest_match` は `_ExactSequenceMatcher` が上書きしているが、本家の他のコードから直接は呼ばない。`get_grouped_opcodes` は使わない
- 比べる並び: 文字列 (1 字ずつ。Python の str の添字 = コードポイント) と、文字列のリスト (行・見出しの文字)
- `_ExactSequenceMatcher` (yomiyasu_diff.py の `_MATCH_SMALL_AREA` から `_dp` まで) は、文字列どうしで捨てる要素の無いときだけ速い求め方を使う。
  求め方は範囲の大きさで選ぶ: difflib と同じ DP・`str.find`・長さちょうどの部分文字列の表 (`_probe`・`_probe_local`)・種で探す (`_seeded`)・接尾辞オートマトン・長さの二分探索 (`_bisect_length`、1 字ずつずらす hash)。
  どれも「いちばん長く、a で最も前、同じ a なら b で最も前」を求めるので、選び方 (定数) は時間だけを変える、というのが本家の主張

## 対応方針

1. **API** (Go の generic): `New[T comparable](a, b []T)` が `SequenceMatcher(None, a, b, autojunk=False)`、`NewExact(a, b []rune)` が `_ExactSequenceMatcher(None, a, b, autojunk=False)`。
   メソッドは `SetSeq1` `SetSeq2` `SetSeqs` `FindLongestMatch` `GetMatchingBlocks` `GetOpcodes` `Ratio` `QuickRatio` `RealQuickRatio`。
   - `isjunk` と `autojunk` の引数は持たない。本家は使わず、使わない引数を写すと記録で確かめられない経路が増える。本家が使い始めたら記録の driver の `ast` の検査が止まる (下の 3)
   - 文字列は `[]rune` で渡す (Python の str の添字と同じく 1 コードポイント = 1 要素。単独のサロゲートは 1073 の WTF-8 の読み方で 1 要素にする)
   - `FindLongestMatch` は範囲が並びの中にある呼び方だけを受ける (本家は範囲の外を渡さない。外なら panic)
   - `Ratio` などの float は Python と同じ式 (`2.0 * matches / length`、length が 0 なら 1.0) で求める
2. **速い照合の写し**: 範囲ごとの求め方の選び方 (定数と順) は本家と同じにする。Go に無い道具の所だけ置き換え、答えは変えない:
   - Python の `hash(str)` は使えないので、部分文字列の hash は 1 字ずつずらす多項式の hash (法 2^61-1。本家の `_rolling_hashes` と同じ式) で取る。hash は表を引く手がかりにだけ使い、引いた位置は必ず字で比べる (本家と同じ)
   - `str.find` は `[]rune` の上の探索 (KMP など、最悪でも線形) にする。`_common_prefix_len` は Go では 1 字ずつ比べる (本家の倍々の切り出しは C の速さで比べるための工夫で、答えは同じ)
   - 置き換えた所は README の表に書く
3. **本家と同じ答えの確かめ (記録)**: 自分用の driver (`oracle/py/record_difflib.py`) と記録 (`testdata/golden/difflib.json`)。入力は oracle の Go が決まった算法で作り (乱数は使わない)、
   driver は同じ入力を Python の `difflib.SequenceMatcher` と本家の `_ExactSequenceMatcher` の両方に通して、それぞれの結果を残す。
   - 入力: 手で選んだ端 (空・片方が空・同じ・共通なし・同じ字の繰り返し・サロゲート・BMP の外の字)・小さい字の種類の全組み合わせ (DP の同点の選び方)・
     字の種類の少ない長い文字列・同じ段落を何十回も繰り返して数か所を直した文書・入力の集まりの直す前と後の組 (`raw_ai` と `yomiyasu_rewritten`)・文字列のリスト (行の並び。空行の繰り返しを含む)・
     `set_seq1` / `set_seq2` で使い回す手順 (本家の 3005 行あたりの形)
   - 大きい入力は記録に中身を書かず、作り方 (部分の文字列と繰り返しの数の列) を書き、Go と driver が同じ作り方で組み立てる
   - driver は本家のソースの照合を作る呼び出しを `ast` で全部見て、`(None, a, b, autojunk=False)` の形でないものがあれば止まり、数を記録に残す
   - header は他の記録と同じ (`TestGoldenFilesShareOneEnvironment` に足す)
4. **Go の中の突き合わせ**: `New` と `NewExact` が同じ答えを返す試験 (同じ字が何度も出る文書・長い文書・字の種類の少ない文字列)。
   速い照合の求め方ごとに、試験の入力で 1 回以上通ったことを数えて確かめる (どこかの求め方が試験から外れたまま緑にならないように)
5. **所要**: 同じ入力を Python の 2 つと Go の 2 つで交互に複数回測り、README に書く (起動の時間は分ける)。テストの合否に時間は使わない
6. 共有のファイル (`oracle/cmd/golden/main.go`・`oracle/golden_test.go`・README・`src/PACKAGES.md`) への追記は登録の数行と表の行に留める (1074 の係が同じファイルを触っている)

## 受け入れ条件

- [ ] `internal/difflib` の `New` が、記録の全入力で Python の `difflib.SequenceMatcher` と同じ値を返す (使う API 全部)
- [ ] `NewExact` が、記録の全入力で本家の `_ExactSequenceMatcher` と同じ値を返す
- [ ] Go の中で `New` と `NewExact` が同じ答えを返す試験があり、速い照合の求め方が全部試験で通る
- [ ] 記録の driver が本家の照合の呼び出しの形を `ast` で確かめ、数を記録に残す
- [ ] 所要を Python と Go の両方で測った数字が README にある
- [ ] 新しい試験は実装を壊すと落ちることを `mutate-verify-list` で確かめた
- [ ] `make -C src/ja-writing lint test`・root の `make test-lint`・`make test-dupscan-gate` が通る

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_diff.py` (`_MATCH_SMALL_AREA` から `class _ExactSequenceMatcher` の `_dp` まで)・Python 3.15 の `Lib/difflib.py`
- `src/ja-writing/internal/difflib/`・`src/ja-writing/oracle/py/record_difflib.py`・`src/ja-writing/oracle/difflib.go`・`src/ja-writing/testdata/golden/difflib.json`
- 1076 (この package を使う diff)・1073 (oracle の仕組み)

## 進捗

- (未着手)
