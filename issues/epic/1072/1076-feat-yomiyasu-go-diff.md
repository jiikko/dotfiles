# 1076 (feat): yomiyasu の diff (推敲の前後の比較) を Go で作り直す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-12〜）

起票日: 2026-11-12

## 概要

本家の `yomiyasu_diff.py` (11,577 行。本家で最も大きい) を `ja-writing diff` として作り直す。直す前と後の文を比べ、意味が変わった疑いのある所を出す道具。
見るのは数値・否定・伝聞・動作主・慎重表現・言い切りの強さなど。2026-11-12 の照合で「直す前後の比較」は、dotfiles に原則の文しか無く、
機械で比べる手段が無かった。

## 本家が持つもの (v3.1.0)

- オプション: `--stance` `--json` `--fail-on` `--doc-type` `--reader` `--words` `--no-words` `--material` `--endings` `--version`。`--json` は `indent=1`
- 照合は `difflib.SequenceMatcher` を土台にしている (21 か所)。どれも `autojunk=False` で、`isjunk` は渡さない
  - 使う API: `get_matching_blocks`・`get_opcodes`・`ratio`・`quick_ratio`・`real_quick_ratio`・`set_seq1` / `set_seq2`・`find_longest_match`
  - 字の列のほか、リストどうしの照合もある
  - 本家は 2.0.1 と 2.2.2 で「difflib と同じ答えのまま」照合を速くした (同じ段落を何十回も繰り返す文書で遅かったため)
- `fractions.Fraction` は 6 回使うが、読点の増え方の閾値の比較の 1 か所だけ。出力の数は `{:.2f}` と `round(x, 2)` の float
- 正規表現の後読み 265 か所・先読み 256 か所 (本家で最も多い)
- 語の一覧のうち 4 本 (nominalization・intensifiers・feelings・hedges) を読む
- 絵文字の数え方は lint の `_emoji_matches` を読み込んで使い、読めなければ簡単な数え方に切り替える

## 対応方針

- `internal/difflib` (Python の `difflib.SequenceMatcher` と、本家の速い照合 `_ExactSequenceMatcher` の写し。本家の `yomiyasu_diff.py` の 1306〜1832 行) は、
  [1081](done/1081-feat-yomiyasu-go-difflib.md) に切り出した (dotfiles-bb が担当。2026-11-13)。下の 2 項のうち difflib と速い照合の写しは 1081 の範囲で、この issue はそれを使う側を作る
  1081 の API (2026-11-13 に master に入った): `difflib.New[T](a, b []T)` (= `SequenceMatcher(None, a, b, autojunk=False)`) と `difflib.NewExact(a, b []rune)` (= 本家の `_ExactSequenceMatcher`)。
  メソッドは `SetSeq1` `SetSeq2` `SetSeqs` `FindLongestMatch` `GetMatchingBlocks` `GetOpcodes` `Ratio` `QuickRatio` `RealQuickRatio`。文字列は `pyunicode.Runes` で `[]rune` にして渡す。
  本家の照合の呼び出しは ast で 15 か所 (`SequenceMatcher` 11・`_ExactSequenceMatcher` 4)。記録は `testdata/golden/difflib.json`
- 本家の diff は太字の判定 (`yomiyasu_diff.py` の 1923〜2685 行) を lint と同じ中身で自分でも持つ (dotfiles-24 が AST で比べ、答えを変える違いは無かった)。
  Go では lint と共有の `internal/bold` ([1083](done/1083-feat-ja-writing-lint-bold-not-rendered.md)) を使い、写さない。1083 が本家の lint 側と diff 側の両方の関数を記録に当てて、同じ答えになることを確かめる
- `difflib.SequenceMatcher` (Python 3.15 の `Lib/difflib.py`) を `internal/difflib` に写す。本家が使う API だけでよい。
  autojunk は本家が使わない。引数の形は Python と同じにし、本家と同じく常に `autojunk=False` で呼ぶ
- 本家の速い照合も写す。「difflib と同じ答え」が本家の前提なので、Go でも 2 つを突き合わせる試験を残す
- `Fraction` は閾値の比較の 1 か所だけなので `math/big.Rat` で持つ。出力の数は 1073 の `internal/pyjson` と同じ規則 (Python の `round` と `repr`、`format` の `.2f`) で書く
- 語の一覧は 1073 で埋め込んだものを使う
- 11,577 行を検出の群ごとに分けて commit する。分け方は本家のコードを読んでから決め、群の一覧をこの issue に書く
- 所要: 同じ段落を繰り返す長い文書 (本家の `tests/test_v202_diff_bold_performance.py` などの入力) を本家と Go で測る。
  起動の時間と処理の時間を分け、同じ入力を交互に複数回測る。テストの合否には時間を使わない

### 群と順 (2026-11-13 に本家 db78b9bc を数え直した地図の要点)

本家の `yomiyasu_diff.py` は 409 個のトップレベルの def / class・module の読み込みでできる正規表現 402 個・後読み 265・先読み 256。`main` から辿れるのは 373 個。
「単独で通る」は、その群だけを commit して build と go test を通せること。「記録」の関数は `oracle.Call` で呼べるもの、「段」は `_Sides` などを引数に取る検出で、段ごとの driver (`oracle/py/record_diff_stages.py`。これから足す) で本家の戻り値を取る。

| 群 | 本家の行 | 中身 (主な関数) | 依存 | 記録の取り方 |
|---|---|---|---|---|
| A | 11188〜11577 | CLI の骨組み (`_build_parser` `_run` `main` `_fail_on` `_fail_on_line` `_read_input` `_path_problem`) | 1074 (visibility) | CLI。`--version`・rc 2 の経路は diff 本体なしで通る |
| B | 1066〜1126・2683〜2694 | 9 キーの下ごしらえ (`normalize` `has_list` `sentences` `paragraphs` `logic_points` `count` `context` `_bold_short`) | なし | 関数 |
| C | 1129〜1303・1833〜1915 | 文末の種類と立場 (`bare_end` `register` `ending_kind` `ending_units` `stance_flags` `_ending_changes_from_units`) | B・difflib | 関数 (`ending_units` `stance_flags` `ending_changes`)。E と合わせて `--endings` の CLI が通る |
| D | 1306〜1832 | 照合 | 1081 の `internal/difflib` | 別の係 (済) |
| E | 1918〜2680 | 太字 (`bold_problems`)。lint と同じ中身 | 1074・1083 の `internal/bold` | 関数。写さず `internal/bold` を呼ぶ |
| F | 166〜638 | 文の種類の印 (`type_marks` `_type_changes` `_pair_gone_sentences` `_widen_to_sentences`) | difflib (opcodes) | 関数 `type_marks`・段 |
| G | 2697〜2765 | 9 キーの組み立て (`_rule_candidates`) | B・C・E・F | 段 |
| H | 641〜808 | 許可・能力 (`_modality_changes`。`_shared_ranges` が F の `_LAST_TYPE_RANGES` を使い回す) | F | 段 |
| I | 811〜1063 | 選ぶ範囲・因果 (`_logic_changes`) | F・H | 段 |
| J | 2768〜3294 | 全文差分 (`_align` `_fill_gap` `_pair_similar` `_char_segments` `_Sides` `_build_full_diff` `_encoding_notes`) | difflib・1074 | 関数 `full_diff(orig, rw)` |
| K | 3296〜3651 | 見出しの構造 (`_heading_structure`) | J・difflib | 段 |
| L | 3654〜3836・5128〜5157・6268〜6343 | 文の組と本文 (`_risk` `_sentence_pairs` `_hunk_group` `_Body` `_Masker`) | J | 段 (groups) |
| M | 3839〜5125 | 数値 (`extract_numbers` `_number_risks`。正規表現 40) | L・difflib | 関数 `extract_numbers`・段 |
| N | 5160〜5348 | 否定 | L・difflib | 段 |
| O | 5351〜5416 | 主語と目的語の入れ替え | L | 段 |
| P | 5419〜5632 | 問い・変えない箇所・印の候補 | L・M・F・H・I | 段 |
| Q | 5635〜5953 | 概数と範囲の語 | M・L | 段 |
| R | 6040〜6265 | 語の一覧 (`parse_list` `_list_info`) | `lists/` | 関数 `parse_list` |
| S | 6346〜6874 | 否定対比の逆転 (正規表現 34) | L | 段 |
| T | 6877〜6933 | 気持ちの語 | L・R | 段 |
| U | 7960〜8354 | 確度 (正規表現 50) | L・F・N | 段 |
| V | 8357〜9140 | 状態の語・約束・因果の書き足し (正規表現 100。いちばん多い) | L・U・X・difflib | 段 |
| W | 9143〜9367 | かぎかっこ・文体の切り替え | L・C・difflib | 段 |
| X | 9480〜9819 | 慎重表現・動かしてはいけない値・敬語 | L・R・C・M | 段 |
| Y | 5956〜6037 | 意味が変わる恐れの組み立て (`_meaning_risks`) | M〜X の全部 | 関数 `meaning_risks` |
| Z | 6936〜7212 | 文体の硬化 | L・R | 関数 `stiffness` `stiffness_metrics` |
| AA | 7215〜7552 | 勢い | L・R・lint の `EmojiMatches` | 関数 `tone` |
| AB | 9822〜10243 | 足した人や担当 | L・difflib | 段 |
| AC | 10246〜10528 | 読点・見出しの形・`diff()` の組み立て (Fraction はここ 1 か所) | 全部 | CLI の記録が全部揃う |
| AD | 10531〜11180 | 人が読む出力 (`report`。JSON の dict だけを読む) | JSON の形 | 関数 `report(d)` に本家の `--json` の出力を渡す |
| AE | 7555〜7957・9370〜9475 | check が呼ぶもの (`ground` `fact_gaps` `detector_summary`) | M・L・R | diff の CLI からは届かない。1077 で作る |

commit の順: ① A → B → C → E (`--endings` の CLI が端から端まで通る) ② J → K → L ③ AD (本家の `--json` を入力にして `report` と突き合わせ、全部の節の文面を先に固める) ④ F → G → H → I
⑤ R → Z → AA ⑥ M → Q → N → O → P → S → T → U → W → X → V → Y ⑦ AB → AC ⑧ AE は 1077 と一緒に。M〜X は検出どうしが打ち消し合う (確度と印・状態と確度・引用と数値) ので、CLI の `meaning_risks` が全部揃うのは Y まで待つ。段ごとの記録が無いと、その間は本家と比べられない。

### Python 特有のずれ (後の群の係が読む要点)

- 位置と長さは全部 code point (rune)。`strip()` 引数なしは `str.isspace` の字 (`pystr`)。スライスは範囲の外を詰めて返す (`s[max(0,i-3):j+3]`)。`lower()` は 1 か所 (`--fail-on`)
- 負の数の切り捨て割り算 (7405 の `head + (start - head) * (new_end - head) // max(1, old_end - head)`)。Go の `/` は 0 の方向なので `floorDiv` を書く
- dict の挿入の順が出力の順になる所が多い (`MARKERS.items()` `before.items()` `Counter.most_common()`。JSON のキーの順も)。Go の map を回さず、順を持つ値 (`pyjson` の値) で組む
- `sorted` / `sort` の key は安定。`max` は最初の最大、`min` は最初の最小。`sorted(set(...), key=len, reverse=True)` で同じ長さの語の並びは set の順 (7293・9505)。Go は一覧の順を保つ安定な並びに決める。`patterns.json` の文字列は seed 0 のもので、文字列でなく当たりで比べる
- float: `round(x, 2)` は `pyjson` の round と repr。7153 は丸めた値どうしの引き算、10434 の `{:.1%}` は double のまま 100 倍、10981 は `{:.0f}` で整数にした値を比べる。割ってから比べる順を保つ
- `id()` と `is` で同じものかを見る所 (532〜598・5997〜6021・1287 `r is not rows[-1]`)。値の比較に直すと答えが変わる。Go はポインタか番号
- `Fraction` は `_comma_increase` の 1 か所 (`new_rate - old_rate < 3/10`)。整数の比較 (`new_commas*old_sentences*10 - old_commas*new_sentences*10 < 3*old_sentences*new_sentences`) か `math/big.Rat`
- `_LAST_TYPE_RANGES` (355) は `_type_changes` が置き `_shared_ranges` が使い回すグローバル。Go は引数で渡す
- 正規表現: 後読みが多い長い選択肢を文章全体に当てる所 (`TYPE_MARKERS[推量]` 780 字・`_ADD_STATE_ANY` 1,825 字) は、本家の「必ず含む文字列が無ければ当てない」の前置きの検査 (`_TYPE_LITERALS` `_MODALITY_LITERALS` `_LOGIC_LITERALS`) も写す。同じ文を何度も調べないキャッシュ (`_metrics_of` `_AdditionDocument` `_ActorSource`) も写さないと、同じ段落を繰り返す文書で遅くなる
- 照合の呼び出しは 15 か所 (`internal/difflib` の API: `New` `NewExact`)。`_pair_similar` は 2,000 字を超える組に `ratio()` をかけない、`_ending_changes_from_units` は字数の積が 160,000 を超える組だけ Exact を使う
- 絵文字は lint の `EmojiMatches` (`_FALLBACK_EMOJI` の経路は写さない)。`context.words.path` は cwd に依るので、記録の cwd の実パスは `<<ROOT>>` に置き換える
- `CLI から届かない 36 個` (`type_marks` `ending_changes` `full_diff` `meaning_risks` `stiffness` `tone` ほか): 関数の記録の入口として残す。どこからも呼ばれないもの (`_addition_old_future_count` ほか 4 個と太字の後方互換 9 個) は写さない (写すと `unused` が落とす)

### 決めたこと (地図 5.6 の 7 点。lint の前例 [1075](done/1075-feat-yomiyasu-go-lint.md)・README の「揃わない範囲」に揃えた)

1. **`context.words.path`**: cwd に依る欄。記録の作業フォルダは使い捨てなので、文字列の中の実パスを `<<ROOT>>` に置き換えてから比べる (lint の CLI の記録と同じ。`--words 相対` と cwd から見つける形の両方を記録に入れ、形が食い違えばそのとき表に足す)。新しい行は README の「揃わない範囲」の表に足さない (置き換えは比べ方であって、揃わない範囲ではない)
2. **`-h`**: 本家が出した文面 (端末の幅 80 桁) を `diff/help.txt` に持ち、`COLUMNS` で幅を変えても Go は変わらない。文面も rc も記録と比べる。lint の `help.txt` と同じ扱い
3. **rc 3 の traceback**: 文面は揃えない。rc 3 と、1 行目 (`yomiyasu_diff.py: 内部エラー: <型>: <文>`) までにする。lint と同じ
4. **`Error opening file ...（<strerror>）`**: lint が使う `visibility.OSErrorReason` を使う (macOS の `strerror(3)` の表。記録で確かめた errno は lint の README の「揃わない範囲」と同じ)。diff の記録の誤りの呼び出しに無いファイル・ディレクトリ・UTF-8 でないものを入れた。権限なしは記録に入れていない (手で `chmod 000` のファイルを渡すと、本家と Go は同じ「ファイルを開けません（Permission denied）」で rc 2。機械では守っていない)
5. **`list` と `list_problem`**: Go は一覧を埋め込むので、いつも `"references/lists/<名前>.txt"` と `null` を出す (本家が隣のファイルを読めたときの値で、記録の本家も同じ)。内蔵の一覧 (`BUILTIN_LISTS`) と「読めなかった」経路 (`_LIST_NOTE`) は写さない。そのため `report` の「一覧の注意」の節は出ない。JSON の一覧の欄は記録と比べる
6. **`--fail-on` の `lower()`**: lint の `failOnWord` と同じ (`strings.ToLower` に İ の分解を足す。語末の Σ と Python の Unicode のバージョンとの差は写さない)。実装は lint と共有する `internal/pycli` に出した。使えない値の文に出る字だけに現れる
7. **標準エラーの案内の文のコマンド名**: lint と同じく本家の文 (`yomiyasu_diff.py ...`) をそのまま出す。`ja-writing diff` への置き換えは 1078 で lint とまとめて決める (README の表の 2 行目)

## 受け入れ条件

- [ ] 入力の集まり (直す前と後の組) で、本家と同じ出力 (stdout・`--json`・rc) を出す
- [ ] 検出の種類ごとに、当たる組と当たらない組が入力の集まりにある (1073 の空振りを止める検査)
- [ ] `internal/difflib` が、本家の使う全 API で Python の `difflib.SequenceMatcher` と同じ値を返す (1073 の関数の単位の記録との突き合わせ)
- [ ] 本家の速い照合を写したものが、`internal/difflib` と同じ答えを返す試験がある
- [ ] 長い文書の所要を本家と Go の両方で測った数字がある
- [ ] 検出の群ごとに最低 1 つ、実装を外して落ちることを確かめた (`mutate-verify`)
- [ ] `make test-dupscan-gate` が通る

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_diff.py`・`docs/tools.md`
- 1074 (markdown_visibility)・1075 (lint の絵文字の数え方)・1077 (check が diff を読む)

## 進捗

- 2026-11-13 (diff-wf1。worktree で作業し、commit は push していない)
  - commit (本家の群 A・B・C・E まで。issue 1076 の「群と順」の ① が済み、`--endings` の CLI が端から端まで通る):
    - 287d90533 docs(issues): 1076 に群の表・Python 特有のずれの要点・commit の順を移す
    - 8671c3566 docs(ja-writing): diff の 7 つの決めること (words.path・-h・rc 3・strerror・list・lower・コマンド名) を lint の前例に揃えて書く
    - 9eef1dbe9 refactor(ja-writing): lint の遅延 compile と argparse・出力先・入力の読みを internal に出し、diff と共有できる形にする
    - e9b3bc9a1 refactor(ja-writing): CLI の記録の作業フォルダと関数の記録の突き合わせの道具を oracle に出し、共有できる部品を 1 つにする
    - 79f086a58 test(ja-writing): diff の入力 (手で作った 117 組) と、本家の CLI と関数の記録 (diff-cli・diff-base) を作る
    - 20d25d6f2 feat(ja-writing): diff の CLI の骨組みと --endings (群 A・B・C・E) を作り、CLI の記録 2,376 件と関数の記録 65,925 件を本家と同じにする
    - (9eef1dbe9 は単独では `make lint` の unused と `test-go-packages-index` が赤い。次の e9b3bc9a1 が直す)
  - 記録: `diff-cli.json` 7,942 件 (うち比べた 2,376 件: 引数の誤り・`-h`・`--version`・`--endings` の全部。本家が検査の本体まで進む 5,566 件は「比べていない」とログに出す。`bodyImplemented` を真にすると、比べていない件数が 0 でなければ落ちる)。
    `diff-base.json` 787 case・65,925 呼び出しを全件比べた (群 B・C・E の関数と、diff 側の `bold_problems`)。手で作った入力は `testdata/diff/` に 117 組 (corpus に足すと全部の記録が作り直しになるので別の置き場)
  - 検出の種類 71 種 (`oracle.DiffDetectionKinds`) の全部に、出る入力と出ない入力が記録にある (`TestDiffRecordingHasHitAndMissForEveryKind`)。当たる組を作れなかった種類は無い。ただし M〜X の細かい分岐 (各 subkind の全部) は、段の記録を作るときにそれぞれの群で足す
  - わざと壊して red を確かめた (`bin/mutate-verify`、全部 rc 0 = 想定のテストが落ちた): A 2 本 (引数の数の条件・`--fail-on` の文)・B 1 本 (`Normalize` の見出しの除去)・C 3 本 (可能の語尾の判定・`stance_flags` の「最後の行そのもの」の同一性・似かたの下限)・E 1 本 (`--endings` の太字の節)
  - 検査 (最後の編集の後の木): `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` (rc は下の「最終の検査」)
  - 本家と揃わなかった所: 無い (`--endings` と引数の誤りの全記録が一致)。揃えると決めた 7 点は「決めたこと」の節
  - 確かめていないこと:
    - rc 3 の経路 (内部エラー・読む側が閉じたパイプの rc 3 と 120) は diff の記録に無い。lint の `pycli.Finish` と共有で、lint の記録で確かめた形を使っているだけ
    - `context.words.path` (`--words 相対` の形) は、diff の本体が入るまで比べられない (記録は取ってあり、`<<ROOT>>` に置き換えている)
    - `count` と `context` と `_bold_short`: `count` は本家のどこからも呼ばれず写さない。`context` は群 G (`_rule_candidates` の spans) で使うので、そのとき足す。`_bold_short` は `internal/bold` が持つ
    - 群 E の `bold_problems` は、diff 側の関数を直接 Go に写していない (`internal/bold` を呼ぶ)。本家の diff 側と lint 側が全入力で同じ答えなのは `bold.json` と `diff-base.json` の両方が見る
  - 残り (この係では手を付けていない): 群 J・K・L (全文差分・見出しの構造・文の組と本文)・AD (人が読む出力)・以降の ④〜⑧。量が多く、半端な群を commit しないため、ここで止めた。次の係は ② J → K → L (`full_diff` の関数の記録を `oracle/diffbase.go` に足す)、③ AD (本家の `--json` を入力にして `report` と突き合わせる) から始める。
    その前に、`diffHeavy` の入力 (`full-budget` など) と `testdata/diff` の hunk の入力 (`full-*`) が J の記録として足りるかを見る
  - 最終の検査 (commit 20d25d6f2 の木。rc は全部 0): `make -C src/ja-writing lint test` (lint 0 issues・`go test -race ./...` 全部 ok)・`make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` (新しい重複なし)

### 1 本目の敵対的レビュー (opus)

対象は 787f3d6e8..1418b9d18。関数ごとの比較 12,300 件と CLI の比較 1,400 回 (本家 Python 3.15.0 と stdout・stderr・rc) は食い違い 0 件で、壊せなかった。壊せたのは下の 1 件だけ。指摘ごとの扱いと、直した後の状態は次のとおり。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P1-1 `--stance` の空白の判定 (`strings.TrimSpace` は \x1c〜\x1f を空白に数えない) | 直した (`pystr.Strip`)。本家 Python の記録でも「（空）」と出ることを記録の作り直しで確かめた | CLI の記録に `stance-separator-chars`・`stance-ideographic-space`・`stance-separator-endings` を足した。`TrimSpace` に戻すと `TestCLIMatchesPythonRecording` が red |
| P1-2 README の件数の誤り (7,928 と 7,942) | 直した。今の数は CLI 7,948 件・比べた 2,382 件・関数 789 case・65,952 呼び出し (test の出力で確認) | README の件数を揃えた |
| P1-2 「決めたこと」6 の `internal/pyargparse` | 直した (実物は `internal/pycli`) | — |
| P1-2 「決めたこと」4 の権限なし | 主張を直した。記録に権限なしは 0 件で、手で `chmod 000` のファイルを渡すと本家と Go は同じ文で rc 2 | 機械では守っていない (未確認の範囲) |
| P2-1 `ending_changes` の同点の規則 | 再現した。記録の組 `tie` の似かたは同点でなかった | `tie-equal-score` を足した。`>=` に変えると `TestBaseMatchesPythonRecording` が red |
| P2-2 `BareEnd` の空白 (`unicode.IsSpace`) | 再現した | `確認してください。 \x1c(例)` など 3 文を足した。`unicode.IsSpace` に変えると red |
| P2-3 表のセルの `pystr.Strip(cell)` | **採らない** (等価)。`strings.TrimSpace(cell)` に替えても全テストが緑のままで、指摘の「sentence が `\x1c確認します` になる」は再現しない: 続く `s = pystr.Strip(s)` が同じ字を外すので、セルの strip は出力に効かない | 入力 (`table-separator-cell`) は経路の記録として残した。再提起に要るのは、セルの strip の違いが `EndingUnit` の出力の違いになる入力 (実測で本家と Go の答えが変わるもの) |
| P2-4 入力を読む順 (ファイルを全部読んでから資料) | 再現した | `missing-file-and-bad-material`・`bad-file-and-missing-material` を足した。資料を先に読む形にすると red |
| P2-5 `--fail-on İ` の分解 | 再現した (記録に İ が 0 件) | `fail-on-dotted-capital-i` を足した。分解を外すと red |
| P2-6 `bodyImplemented=false` の間は request の組み立てが検査されない | **採らない**。読んで確かめた通りだが、今は本体が無く request は読まれない。本体 (群 J の最初の commit) で `bodyImplemented` を真にすると、比べていない記録 5,566 件が全部比べられ、old と new の入れ替え・SourceInfo・資料の順・語の一覧の解決が記録と突き合わされる | 群 J の最初の commit で `bodyImplemented` を真にすることを必須にする (下の「未確認のリスク」にも書く) |
| P3 rc 3 の `<型>` | 再現する経路は見つからなかったが、コードでは `visibility` の誤り以外は型の名前を出さない | README を実物に合わせた。rc 3 の経路は記録に無い (未確認のまま) |
| P3 答えが変わらない書き換え 4 本 (設定部分の位置・不安定な sort・枝刈りの `<=`・-h と --version の順) | 採らない (本家との答えが変わらないので、テストが赤くならなくてよい) | — |

- わざと壊して red を確かめた (`mutate-verify-list`。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf1/mut-a/ と mut-a2/): 6 本。5 本が想定の検査で red (`--stance` の `TrimSpace`・`--fail-on` の İ・資料を先に読む・同点の `>=`・`BareEnd` の `unicode.IsSpace`)、1 本 (表のセルの `TrimSpace`) は上のとおり等価で緑のまま。最初の `--stance` の 1 本は import が余って構文エラーだったので、当て方を直して red にした
- 記録の作り直しは書き足しだけ: `diff-cli.json` の既存の行の削除 0。`diff-base.json` は集計の行 12 本と `snippet/endings` の 1 行が数え直しで変わるだけで、case は消えていない
- 壊せなかったもの: 関数 12,300 件・CLI 1,400 回・`internal/bold` の共有 (本家の diff と lint の太字の関数は注釈と変数名を除いて同じ)
- 未確認のリスク:
  - 権限なしのファイル (記録に入れられない。手の確認だけ)
  - rc 3 の `<型>`: `visibility` 以外の内部エラーを起こす経路が見つからず、本家との突き合わせはしていない
  - request の組み立て (P2-6) は、群 J の最初の commit まで機械で守られていない
  - `context.words.path` の比べ方は、本体が入るまで確かめられない (最初の進捗と同じ)
- 最後の編集の後の木 (HEAD 時点) の検査: `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` が全部 rc 0 (ログは tmp/yomiyasu-gap-2026-11-12/diff-wf1/chk-*.log)

### 2 本目 (diff-wf2。2026-11-13。worktree で作業し、push していない)

- commit (群 J まで。群 K・L・AD は着手していない):
  - f21a5fe85 refactor(ja-writing): context.words の JSON の組み立てと、文字列・省略できる文字列の JSON 値の変換を visibility と pyjson に出す
  - 146fe40e3 feat(ja-writing): diff の全文差分 (群 J) を作り、検査の本体へ渡す値を本家と突き合わせる
- 群 J: `diff/fulldiff.go` (本家の `_align` `_fill_gap` `_pair_similar` `_char_segments` `_plan_segments` `_make_hunk` `_mark_moved` `_encoding_notes` と `full_diff`)。`_Sides` は行と解析だけ持つ。
  `_analyze` の bare_url の範囲を縮める処理 (`_URL_CHARS`) は全文差分が読まない (`protected_spans` を使うのは群 M 以降) ので写していない。群 M の係が足す
- 記録: 関数の記録に `full_diff` を足した。`diff-base.json` は 8xx case・全呼び出しを比べて通る (`full_diff` 約 480 呼び出し: 入力の集まりと手で作った入力の全組・手書きの組 54 本・狙いを定めた組 5 本・固定の種 (PCG 1076/4) の行の組 140 と繰り返しの多い組 60 と文の組 40)。
  `diff-cli.json` は 8,002 件 (新しい入力 `actor-material-order` の分が 54 件増え、既存の行の中身の変更は 0。`req` の欄が本体まで進む 5,6xx 件に付いた)
- 本体へ渡す値 (1 本目の敵対的レビュー P2-6 への手当て): `run` を `prepare` (本体を呼ぶ前まで。request を返す) と `compare` に分けた。本家の `_run` が `diff()` に渡す引数 (原文・書き直した文・立場・注記・種類・読者・語の一覧の context・資料) を、
  記録の driver (`oracle/py/record_diff_cli.py`) が `diff` を差し込んで控え、正規の JSON の SHA-256 を `req` に記録する。`TestRequestMatchesPythonRecording` が Go の `prepare` の戻り値と全件突き合わせ、本体まで進む記録と Go が 1 件ずつ一致すること・全部の欄 (立場・種類・読者・語の一覧・資料・注記) に値の入った記録があることも見る。
  これで本体 (群 AC) が入るまでの間も、本体へ渡す値は機械で守られる。`bodyImplemented` は本体 (AC) が入ったときに真にする (今のまま)。**資料の順が実際に使われる所 (added_actors) の出力は、群 AB が入るまで比べられない** (渡す値の順だけは今守っている)
- 大きい入力 (`full-budget`・`full-long-fragment`) を、オプションを通す組から外したままにする: 外した理由は本家の実行時間で、オプション (種類・読者・立場・語の一覧・資料) の処理は入力の大きさに依らず、本体へ渡す値の突き合わせはこの 2 つも通常の呼び出し (text・`--json`・`--fail-on`) で受けているため、オプション付きで増やしても守るものが増えない。全文差分の上限の経路は関数の記録で通す (`full-budget` は `too_large`、新しい `budget-second-hunk` は `budget`、`full-long-fragment` と `long-one-side-pairs` は長い断片)
- わざと壊して red を確かめた (`bin/mutate-verify-list`。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf2/mut-j4/): 9 本、全部想定の検査で red。動かした行の取り合いの確認の外し・近さの窓 8 → 2・同点の後の候補を取る形 (`>=` と枝刈りの `<=` → `<` の 2 か所)・引用の中の行の種類・合計の上限の引き算の外し・断片の長さの判定の `||` → `&&`・改行コード none の除外の外し・元と後の入れ替え・資料の順の逆転。
  最初に当てた 9 本のうち 7 本は緑のまま通った (手書きの入力が分かれ目に当たっていなかった)。固定の種の組と狙いを定めた組を足して 9 本とも red にした。`>=` だけの変異は枝刈りの `<=` のせいで等価 (同点の候補が ratio を求める前に落ちる) で、2 か所を一緒に当てた
- 本家と揃わなかった所: 無い (関数の記録 全件・本体へ渡す値 全件が一致)
- 検査 (最後の編集の後の木。commit 146fe40e3 の内容): `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` が rc 0 (ログは tmp/yomiyasu-gap-2026-11-12/diff-wf2/chk-*.log)
- 確かめていないこと:
  - 全文差分の所要 (長い入力) は測っていない。関数の記録の入力 (`full-budget` の 11 万字など) を含めて記録の突き合わせのテストは 11 秒で通るが、本家との比は未測定
  - 1 つの文書に lone surrogate を含む入力は記録に無い (入力の読みは UTF-8 の厳密な解読で、表に出ない)
  - `_analyze` の AssertionError (`<ſcript>` の行) は `FullDiff` が error を返すが、本家との突き合わせは無い (関数の記録の例外は 0 件のまま)
- 残り (この係では手を付けていない): 群 K (見出しの構造) → L (文の組と本文) → AD (人が読む出力)・以降の ④〜⑧。K 以降は `_Sides` を引数に取る段の記録 (`oracle/py/record_diff_stages.py`) が先に要る
  (`call_function.py` の `save` で `_Sides(orig, rw)` を覚え、`_build_full_diff` の hunks を引数に渡す口を足す設計。K の `_heading_order_pairs` は (件数, 似かたの和) の組の max と、組の等しさの比べ (`best[a][b] == best[a-1][b]`) が float のまま要る)

受け入れ条件の状況: 「入力の集まりで本家と同じ出力」は本体 (AC) 待ち・「検出の種類ごとの当たる組と当たらない組」は 1 本目で済み・「difflib が全 API で同じ値」「速い照合の試験」は 1081・「長い文書の所要」は未測定・「群ごとに最低 1 つ壊して落ちる」は A B C E J が済み・「`make test-dupscan-gate`」は今の木で通る。
