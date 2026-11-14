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

- [ ] 入力の集まり (直す前と後の組) で、本家と同じ出力 (stdout・`--json`・rc) を出す (`--endings` と引数の誤りは済み。検査の本体は、群 AC で `diff()` の組み立てと CLI に配線するまで未)
- [ ] 検出の種類ごとに、当たる組と当たらない組が入力の集まりにある (1073 の空振りを止める検査) (71 種のうち群 A〜L の分と、群 Z・AA の候補の種類 (硬化 5・勢い 5) と、群 M・N・O・Q・P・T・S・U・X・W の段は、記録が届く回数を数える検査がある。V 以降は未)
- [ ] `internal/difflib` が、本家の使う全 API で Python の `difflib.SequenceMatcher` と同じ値を返す (1073 の関数の単位の記録との突き合わせ)
- [ ] 本家の速い照合を写したものが、`internal/difflib` と同じ答えを返す試験がある
- [ ] 長い文書の所要を本家と Go の両方で測った数字がある
- [ ] 検出の群ごとに最低 1 つ、実装を外して落ちることを確かめた (`mutate-verify`) (群 A・B・C・E・F・G・H・I・R・Z・AA・M・N・O・Q・P・T・S・U・X・W は済み。V Y・AB 以降は未)
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
  `_analyze` の bare_url の範囲を縮める処理 (`_URL_CHARS`) は、全文差分 (群 J) が読まないので 2 本目では写さなかった。3 本目で群 L が使うようになったので、`newSides` の `trimBareURLs` として写した (3 本目の敵対的レビューの P1-1)
- 記録: 関数の記録に `full_diff` を足した。`diff-base.json` は 8xx case・全呼び出しを比べて通る (`full_diff` は 485 呼び出し。2 本目の敵対的レビューの後に 496: 入力の集まりと手で作った入力の全組・手書きの組 50 本・狙いを定めた組 5 本・固定の種 (PCG 1076/4) の行の組 140 と繰り返しの多い組 60 と文の組 40)。
  `diff-cli.json` は 8,002 件 (新しい入力 `actor-material-order` の分が 54 件増え、既存の行の中身の変更は 0。`req` の欄が本体まで進む 5,6xx 件に付いた)
- 本体へ渡す値 (1 本目の敵対的レビュー P2-6 への手当て): `run` を `prepare` (本体を呼ぶ前まで。request を返す) と `compare` に分けた。本家の `_run` が `diff()` に渡す引数 (原文・書き直した文・立場・注記・種類・読者・語の一覧の context・資料) を、
  記録の driver (`oracle/py/record_diff_cli.py`) が `diff` を差し込んで控え、正規の JSON の SHA-256 を `req` に記録する。`TestRequestMatchesPythonRecording` が Go の `prepare` の戻り値と全件突き合わせ、本体まで進む記録と Go が 1 件ずつ一致すること・全部の欄 (立場・種類・読者・語の一覧・資料・注記) に値の入った記録があることも見る。
  これで本体 (群 AC) が入るまでの間も、本体へ渡す値は機械で守られる。`bodyImplemented` は本体 (AC) が入ったときに真にする (今のまま)。**資料の順が実際に使われる所 (added_actors) の出力は、群 AB が入るまで比べられない** (渡す値の順だけは今守っている)
- 大きい入力 (`full-budget`・`full-long-fragment`) を、オプションを通す組から外したままにする: 外した理由は本家の実行時間で、オプション (種類・読者・立場・語の一覧・資料) の処理は入力の大きさに依らず、本体へ渡す値の突き合わせはこの 2 つも通常の呼び出し (text・`--json`・`--fail-on`) で受けているため、オプション付きで増やしても守るものが増えない。全文差分の上限の経路は関数の記録で通す (`full-budget` は `too_large`、新しい `budget-second-hunk` は `budget`、`full-long-fragment` と `long-one-side-pairs` は長い断片)
- わざと壊して red を確かめた (`bin/mutate-verify-list`。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf2/mut-j4/): 9 本、全部想定の検査で red。動かした行の取り合いの確認の外し・近さの窓 8 → 2・同点の後の候補を取る形 (`>=` と枝刈りの `<=` → `<` の 2 か所)・引用の中の行の種類・合計の上限の引き算の外し・断片の長さの判定の `||` → `&&`・改行コード none の除外の外し・元と後の入れ替え・資料の順の逆転。
  最初に当てた 9 本のうち 7 本は緑のまま通った (手書きの入力が分かれ目に当たっていなかった)。固定の種の組と狙いを定めた組を足して 9 本とも red にした。`>=` だけを壊した形は枝刈りの `<=` のせいで等価 (同点の候補が ratio を求める前に落ちる) で、2 か所を一緒に当てた
- 本家と揃わなかった所: 無い (関数の記録 全件・本体へ渡す値 全件が一致)
- 検査 (最後の編集の後の木。commit 146fe40e3 の内容): `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` が rc 0 (ログは tmp/yomiyasu-gap-2026-11-12/diff-wf2/chk-*.log)
- 確かめていないこと:
  - 全文差分の所要 (長い入力) は測っていない。関数の記録の入力 (`full-budget` の 11 万字など) を含めて記録の突き合わせのテストは 11 秒で通るが、本家との比は未測定
  - 1 つの文書に lone surrogate を含む入力は記録に無い (入力の読みは UTF-8 の厳密な解読で、表に出ない)
  - `_analyze` の AssertionError (`<ſcript>` の行) は `FullDiff` が error を返すが、本家との突き合わせは無い (関数の記録の例外は 0 件のまま)
- 残り (この係では手を付けていない): 群 K (見出しの構造) → L (文の組と本文) → AD (人が読む出力)・以降の ④〜⑧。K 以降は `_Sides` を引数に取る段の記録 (`oracle/py/record_diff_stages.py`) が先に要る
  (`call_function.py` の `save` で `_Sides(orig, rw)` を覚え、`_build_full_diff` の hunks を引数に渡す口を足す設計。K の `_heading_order_pairs` は (件数, 似かたの和) の組の max と、組の等しさの比べ (`best[a][b] == best[a-1][b]`) が float のまま要る)

受け入れ条件の状況: 「入力の集まりで本家と同じ出力」は本体 (AC) 待ち・「検出の種類ごとの当たる組と当たらない組」は 1 本目で済み (M・N・O・Q の段は 6 本目で数える検査を足した)・「difflib が全 API で同じ値」「速い照合の試験」は 1081・「長い文書の所要」は未測定・「群ごとに最低 1 つ壊して落ちる」は (この節の時点では) A B C E J K L が済み (その後の本数は受け入れ条件のチェックボックスと各本目の節が正)・「`make test-dupscan-gate`」は今の木で通る。

### 2 本目の敵対的レビュー (opus。diff-wf2 の f6614b8d3..6ca4e6615)

全文は tmp に出したレポート (消える) なので、結論と扱いをここに移した。本家と違う答えを返す入力は見つからなかった。主張の誤り 2 件と、記録が分かれ目に当たっていない 8 か所が指摘で、全部直した。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P1-1 README の件数と表が群 J の前のまま | 直した (84cf4fcf5) | README の件数を今の数 (CLI 8,002 件・比べた 2,390 件・関数 1,098 case・66,489 呼び出し) に直し、`full_diff` と `TestRequestMatchesPythonRecording` を表に足した |
| P1-2 「手書きの組 54 本」は 50 本 | 直した | 実物は `diffFullCases` の手書き 50 本 + `diffFullTargeted` 5 本 + 固定の種 240 で、記録の `full/` の 295 case と合う。この issue の 2 本目の節を 50 本に直した |
| P2-1 `isBlank` を `strings.TrimSpace` にしても緑 | 直した (7a3f6493c) | U+001C だけの行が両側にある組 `anchor-python-space-only` を足した |
| P2-2 近さの窓の比べ `<` → `<=` | 直した | 窓の境目の組 `gap-window-eight` を足した。fuzz の hit を 1 行ずつ縮めた入力で、空行を挟んだ形 (読みやすく作り直せていない) |
| P2-3 間の積の上限 400 → 300 | 直した | 積がちょうど 400 の組 `gap-product-400` (20×20) と 320 の組 `gap-product-320` (20×16) を足した。ちょうど 400 は 399 の変更も落とす |
| P2-4 見出しの段の注記を 1 行の hunk に限らない | 直した | 2 行の見出しの hunk `heading-level-two-lines` を足した |
| P2-5 塊の上限 `<=` → `<` | 直した | 片側が 500 字ちょうどの塊 `block-500` (99 字 + 句点の文 5 つを入れ替える) を足した |
| P2-6 断片の上限 `>` → `>=` | 直した | 句点を含めて 2,000 字ちょうどの断片の組 2 本 `fragment-2000-replaced`・`fragment-2000-added` を足した。`pairSimilar` 側の `>=` も同じ組で落ちる (レビューは等価と見ていたが、ちょうど 2,000 字の組で答えが変わる) |
| P2-7 似かたの下限 0.45 → 0.46 | 直した | 似かたが 0.4545 の組 `pair-ratio-0.4545` を足した |
| P2-8 前後の同じ字の重なりを許す | 直した | `ああ` と `あああ` の両向き `affix-shrink`・`affix-grow` を足した |

- 追加は 11 本 (`oracle/diffbase_full_cases.go` の `diffFullBoundaries`)。`diff-base.json` の既存の行は消えず、`full_diff` が 485 → 496 呼び出し (numstat は 12 追加・1 削除で、削除は集計の行)
- 組を作った方法: レビューの fuzz の hit を本家と Go の変更体で比べながら縮め、重要でない部分を落とした。その後、読める形 (kanji の連なり・語の並び) に作り直せたものは作り直した (`gap-window-eight` だけは縮めた入力のまま)
- わざと壊して red を確かめた (`bin/mutate-verify-list`。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf2/mutlog/): 10 本、全部 `TestBaseMatchesPythonRecording` で red。直す前は同じ変更の 10 本が緑のまま通っていた (レビューの記録による。重なりの変更体はレビューのものと書き方が違い、`charlimit-ge-pair` はレビューが等価と見たもの)。内訳: isBlank を TrimSpace・窓 `<=`・間の積の上限 300 と 399・見出しの注記の行数 `>=1`・塊の上限 `<`・断片の上限 `>=` (計画の側と組み方の側)・似かたの下限 0.46・同じ字の数え方の重なり
- 壊せなかったところ (レビューの結果): full_diff の fuzz 15,811 件で本家との食い違い 0。大きい入力 (6 万字の 1 行・段落 900・22 万字の budget / too_large・8,500 行の並べ替え・60〜80 段の入れ子) も一致。`TestRequestMatchesPythonRecording` は空振りしていない (記録 8,002 件のうち `req` 付き 5,612 件、`diff()` はグローバル名で呼ばれ driver の差し込みが効く)。lint と共有した部品 (`WordsContext.Value`・`pyjson.Strings`・`OptionalString`) は答えを変えない
- 等価と判断して直さない: `markMoved` の空白の行の除外を片方ずつ外す (もう片方の除外があるので等価)・枝刈りの `upper <= bestRatio` を外す (速くするための枝刈りで結果は同じ)・smallGap 399 は 400 ちょうどの組で既に落ちる。再提起するなら、片方の除外だけを外した変更体で、記録にも fuzz にも無い入力が本家と違う答えを返す実測が要る
- 未確認リスク (直さず記録):
  - `alignDepth` (64): 4・16・63 に下げても、記録・fuzz・60〜80 段の入れ子のどれでも答えが変わらなかった。上限で答えが変わる入力が在るのか無いのかは示せていない。再提起には、上限の手前と向こうで本家と違う答えを返す入力が要る
  - `pairSimilar` の `same` を常に偽にする変更: 予算 (budget / too_large) の境目でだけ出力が変わる見込みで、届く入力を作っていない
  - 全文差分の所要: fuzz の全件 (15,811 件) は 11.6 秒で処理するが、入力ごとの本家との比は測っていない (前の節と同じ未測定)

### 3 本目 (diff-wf3。2026-11-13。worktree で作業し、push していない)

- commit (群 K・L まで。AD (人が読む出力) は着手していない):
  - a136ae14f feat(ja-writing): diff の見出しの構造 (群 K) と、全文差分の値を引数に取る段の記録の口を作る (issue 1076)
  - bc01091ef feat(ja-writing): diff の文の組と本文 (群 L) を作り、本家の _sentence_pairs と _all_bodies と突き合わせる (issue 1076)
  - 各 commit は単独で `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-dupscan-gate` を通した (L の commit の dupscan は、手書きの組を作る関数の写し 1 件を `diffDoc` に寄せて通した)。`make test-lint` は最後の木だけで回した (下の「最終の検査」)
- 段の記録の口 (2 本目の進捗の設計案から変えた所): `_Sides(orig, rw)` を別に覚えず、`_build_full_diff(orig, rw, None)` の戻り値 (full, sides) の tuple を `save` で覚え、後の呼び出しの引数を `{"$ref": 名前, "path": [添字・鍵...]}` で
  tuple の一部 (`[1]` が `_Sides`、`[0, "hunks"]` が hunks、`[0]` が full) にした。`shapes` のように呼んだ後の中身を記録したい引数は `args_out_only: [添字]` (結果が `[戻り値, [その引数]]`)。
  `quiet` の呼び出しは戻り値が JSON にできるかを確かめない (`_Sides` を含む tuple を覚えるだけ)。どれも付けなければ前と同じ出力 (driver の冒頭に書いた)
- 段の Go 側の入口: `HeadingStructure(orig, rw, withShapes)`・`SentencePairs(orig, rw)`・`AllBodies(orig, rw, side)`。記録の突き合わせは、直前の `_build_full_diff` の 2 つの文から Go の入口で同じ段を作り直して digest を比べる
  (入口を production から呼べる形で出したのは、`unused` が「テストからだけ呼ばれる関数」を落とすため。群 M 以降は同じ `sides` と hunks を直接渡す)
- 群 K (`diff/headings.go`): `_heading_structure` と `_heading_rows` `_heading_key` `_heading_counterpart` `_heading_order_pairs` ほか。`_heading_order_pairs` の (件数, 似かたの和) の組の max と等しさの比べ (`best[a][b] == best[a-1][b]`) は、float のまま `headingScore` の比べ (件数、同じなら和) と `==` で写した
  (足すのは加算だけなので融合は起きない)。見出しの本文の 2 文字の組が空のときは、空文字 1 つの集合 (本家の `frozenset((key,))`) で、両側が空の本文どうしは似かた 1.0 になる (写しで残した)
- 群 L (`diff/sentences.go`): `_line_sentences` `_sentence_pairs` `_hunk_group` `_blank_between` `_run_shifted` `_paragraph_count` と `_Masker` `_Body` `_all_bodies`。`_group_index`・`_sentence_finder`・`_hunk_finder` は、呼ぶ検出 (群 M 以降・risk の並べ替え) が入るときに足す
  (今は production から呼ぶ所が無く、`unused` が落とす。値を取る入口を作ってまで先に記録する価値が無いと見た)。行頭の記号の繰り返しの「空の一致で止める」判定は、どの選択肢も 1 字以上を取るので到達できず写さなかった。`sides` に各行の文書の中の位置 `starts` を足した
- 記録: `diff-base.json` は 1,098 → 1,947 case・66,489 → 71,583 呼び出し (3 本目の敵対的レビューの後に 1,960 case・71,661 呼び出し。下の節)。足したのは `heading/` 671 case と `sentence/` 178 case (計 849 組。入力の集まりと手で作った入力の全組・全文差分の手書きの組・見出しの手書きの組 (組み直す上限 300 の手前と向こう (両側の数が同じ形と違う形)・
  似かたの下限 0.1 ちょうど (2 文字の組の共通 1 つ・和集合 10)・段が同じの足し 0.2 が効く組と効かない組・節の本文 300 字の上限で重なりがちょうど 0.1 と 0.0997 になる組・見出しだけの hunk の番号と次の行の判定・順に組にする似かた 0.5 ちょうどなど)・
  文の組の手書きの組・固定の種 (PCG 1076/5 の見出し 120 組、1076/6 の文 140 組))。既存の case は消えず、既存の digest は前の値を先頭に持つ (記録の作り直しの前後を python で照合し、崩れ 0)。
  段の呼び出し (`_heading_structure` 1,698・`_sentence_pairs` 849・`_all_bodies` 1,698) は全件 digest を比べて通る。`diff-cli.json` は変えていない
- まだ入っていない群 (F G H I・M〜Y・R Z AA・AB AC) の検出が出す部分を、比べる範囲から外した件数: 段の記録は K・L の関数だけを呼ぶので、外す部分は無い。CLI の記録のうち本体まで進む 5,612 件は 2 本目と同じく「比べていない」とログに出す (本体 AC が入るまで)
- わざと壊して red を確かめた (`bin/mutate-verify-list`。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf3/mut-k・mut-k2・mut-l・mut-l2): K 19 本・L 15 本、全部 `TestBaseMatchesPythonRecording` で red。最初に当てた K 22 本のうち 9 本と L 18 本のうち 5 本は緑のまま通り (手書きの入力が分かれ目に当たっていなかった)、
  分かれ目にちょうど当たる組を足して red にした。K: 似かたの下限 `>=`→`>`・段が同じの足し 0.2→0.1 と 0.3・上限 300 の `>`→`>=` と `||`→`&&`・挙げる数・行の幅 30・見出しの形の字数 15・順に組にする似かた 0.5 の `>=`→`>` と 0.51・消えた見出しの数・本文の字数 300・1 字の見出しの扱い・
  組み直しの戻りの向き・相手の側の行の位置の計算・順の番号の比べ・見出しだけの hunk の次の行の判定。L: 1 行対 1 行の判定の `&&`→`||` 2 本・並びをまとめる置き換えの数と判定・間の行の判定・似かた付きの組み方・文に分けない種類・文の始まりの位置の補正・伏せる範囲 (始まりの探し方・入れ子の終わり・種類)・本文に数えない種類・空の文を除く判定・段落の数え方
- 等価と判断して直さない (緑のまま):
  - K: 引き分けの選び方の `choice.less` と `cand.less` の向き (値が同じになるだけ)・本文の打ち切りの `size >= 300` → `>` (打ち切った後に 300 字で切るので同じ)
  - L: 伏せる範囲の `right <= start` → `<` と `left >= end` → `>` (どちらも、文の始まりで終わる・文の終わりで始まる範囲は、伏せる結果を変えない)
  - 再提起するなら、その向きだけを変えた変更体で、記録にも手書きにも無い入力が本家と違う答えを返す実測が要る
- 本家と揃わなかった所: 無い (K・L の段の記録 全件が一致)
- 確かめていないこと:
  - 群 AD (人が読む出力 `report`) は着手していない。`report(d)` は本家の `--json` の出力 (`d`) を入力にするので、検出が全部入るまで Go は `d` を作れず、記録に `d` の JSON も持つ必要がある (新しい記録の置き場と、`report` が読む他の群の表 `_CERTAINTY_LEVELS` `_MODALITY_NAMES` `_QUALIFIER_LABELS` `NEW_REVIEW_SECTIONS` の写しが先に要る)。量が多く、半端な群を commit しないため止めた
  - 全文差分と同じく、K・L の所要 (長い文書・見出しや範囲の多い文書) は測っていない。K の `_heading_order_pairs` は上限 300 × 300 の DP を 1 hunk ごとに回す (記録の `pair-limit` の組が 300 × 299 まで通る。時間は未測定)
  - `_heading_counterpart` の「行が hunk の中にあるときの式」は、本家の式を写したままで、その行が来る入力を作れていない (呼び出し元が hunk の外の行しか渡さない)
  - 1 つの文書に lone surrogate を含む入力は K・L の記録に無い (全文差分と同じ)
  - 伏せる範囲の始まりの探し方 (`bisect_right`) は、入れ子の範囲の中で文が始まる合成の組 (`![説明。`b`](c.png)`) で落ちることを確かめたが、実際の文書で届くかは未確認
- 最終の検査 (docs の commit を含む最後の木。rc は全部 0): `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` (ログは tmp/yomiyasu-gap-2026-11-12/diff-wf3/chk-*.log)

### 3 本目の敵対的レビュー (opus。diff-wf3 の 73a5dbfe5..d2933f724)

本家と違う答えを返す入力を 1 種類 (P1-1)、実装を壊しても記録の検査が緑のまま通る所を 6 か所 (P2) 見つけた。指摘は全部、本家 (python) の答えと Go の答えを並べて再現してから直した。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P1-1 裸の URL の後に続く本文を伏せる (`_all_bodies` の text) | 直した。`newSides` で `_analyze` の bare_url の縮め (`trimBareURLs`・`isURLChar`) を写した | URL の直後が本文・句点・閉じ括弧・URL に使える全部の字の組 7 本を記録に足し、本家と一致。縮めを外す・字の範囲の端 (0 9 a z A Z ~ % [ ] ( )) を 1 つずつ外す・種類の判定を反転する形 11 本で red。足す前は `~` `%` と数字の上限の 3 本が緑のままだった |
| P2-1 `merged.tag = "replace"` を消す | 直した (最小の入力を `sentence/merged-first-hunk-insert` に) | red |
| P2-2 `headingText` の strip を外す | 直した (`heading/shape-crlf-heading-text`) | red |
| P2-3 `headingOrderPairs` の `!usedOld[i]` を外す | 直した (`heading/shape-order-used-old`) | red |
| P2-4 本文の行を残す条件の `headingKeys` を `allKeys` に | 直した (`heading/shape-bold-line-body-kept`) | red |
| P2-5 setext の下線の strip を外す | 直した (`heading/shape-setext-underline-space`) | red |
| P2-6 `headingBody` で行を strip しない | 直した (`heading/shape-body-line-strip`) | red |
| P3 `invokeStage` が `$ref`・`args_out_only` を照合しない | 採らない。今の記録の形 (「直前の build」と引数の数) では正しく比べており、答えが違う入力を出せていない。再提起するなら、記録の形を変える変更 (別の段を `build` の後に挟む等) で誤った比べになる実測が要る | - |
| P3 「伏せた文」の数え方が P1-1 の形を区別しない | 採らない。P1-1 は記録の突き合わせで直接守った (上の 11 本) ので、数え方で守る必要が無い | - |

- 記録: `diff-base.json` は 1,947 → 1,960 case・71,583 → 71,661 呼び出し。足したのは `sentence/` 8 case と `heading/` 5 case で、`heading/` 676・`sentence/` 186 (計 862 組)。段の呼び出しは `_heading_structure` 1,724・`_sentence_pairs` 862・`_all_bodies` 1,724。
  既存の case は消えず、既存の case の digest も変わらない (作り直しの前後を python で照合)。README の件数も記録から数え直して直した。commit は、縮めの写しと URL の組、見出しと文の組の 2 つに分けた
- 緑のまま通った壊した形のうち、`end < span.End` を `end <= span.End && end != span.End` にした形は、元と同じ式なので等価 (直さない)
- 壊せなかったもの (敵対的レビューの報告): `_heading_structure` (shapes あり)・`_sentence_pairs` は 4,000 組の fuzz で食い違い 0、DP の max と戻り・bisect・`_blank_between`・counterpart の値の下限・並びの安定性は本家と行ごとに読み比べて差なし
- 確かめていないこと (レビューの報告から。issue に残す): fuzz が対象の形の入力を作れておらず、等価かどうか分からない壊した形が 4 本ある (引用の中の段落の行・`#` だけの空の見出し・`**a__b**` の太字だけの行・形の変化が 2 件以上で並びが位置の順でない組)。これらは本家と違う答えを返す入力を示せていないので、防御は足さない

### 4 本目 (diff-wf4。2026-11-14。worktree で作業し、push していない)

- commit (群 F・G・H・I まで。AD (人が読む出力)・M 以降は着手していない):
  - 070385cc5 feat(ja-writing): diff の文の種類の印 (群 F) を作り、本家の type_marks と _type_changes を記録と突き合わせる (issue 1076)
  - bdd656746 feat(ja-writing): diff の 9 つのキーの組み立て (群 G) を作り、本家の _rule_candidates を記録と突き合わせる (issue 1076)
  - 26d81ada0 feat(ja-writing): diff の許可・能力 (群 H) と選ぶ範囲・因果 (群 I) を作り、本家の _modality_changes と _logic_changes を記録と突き合わせる (issue 1076)
  - 群 H と I は段の記録 (`modal/`。`_modality_changes` と `_logic_changes` が同じ呼び出しの組を共有する) が 1 つなので 1 つの commit にした (分けると記録を 2 回作り直し、片方の commit では記録の半分が空になる)
  - 各 commit は単独で `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-dupscan-gate` を通した (rc 0 を commit の前に確かめた。`make test-lint` は最後の木だけで回した。下の「最終の検査」)
- 群 F (`diff/types.go` `diff/types_ranges.go`): `type_marks`・`_type_changes` と、`_type_ranges`・`_pair_gone_sentences`・`_widen_to_sentences`・`_slide_to_sentence_edges`。`_LAST_TYPE_RANGES` のグローバルは `typeCache` (`typeShared` を 1 件持つ) にして引数で渡す
  (`TypeChanges` が置き、`ModalityChanges`・`LogicChanges` が `cache.shared` で使い回す。印がどちらにも無く範囲を作らなかった場合は `haveRanges` が偽で、`shared` が後から作る)。`id()` で見ていた groups と moved の同一性はポインタ (`*rangeItem`) にした。
  `TYPE_MARKERS` の長い正規表現の前置きの検査 (`literals`) は写した。本家が `_pair_gone_sentences` の入力で使う `opcodes` は使わない引数なので写していない
- 群 G (`diff/rules.go`): `RuleCandidatesOf` (本家の `_rule_candidates`)。`MARKERS` 11 本・`CONTENT` と、`context` (`pyContext`)。立場 4 通り (指定なし・勧め・決まり・説明) で全件比べる
- 群 H (`diff/modality.go`)・I (`diff/logic.go`): `ModalityChanges`・`LogicChanges`。`_LOGIC_LITERALS` と問いの「か」の判定・`AもBも` の最初の項の広げ方 (`choiceItemStart`)・範囲のすぐあとに足した文 (`logicTails`) も写した
- 記録の口: driver に `method` (関数かクラスを呼んだ戻り値のメソッドを引数なしで呼んで覚える。`_ExactSequenceMatcher(None, o, r, autojunk=False).get_opcodes()` の結果を `_type_changes` へ渡す) を足した (`oracle/py/call_function.py`・`Call.Method`)。足さなければ前と一字も同じ出力。
  段の記録は `normalize` を `Save` して `{"$ref"}` で渡す形 (群 K・L の `_build_full_diff` の口とは別。`Quiet` の呼び出しは結果の digest が 8 つの 0)。定数 (正規表現の文字列・語の並び・種類ごとの必ず含まれる文字列) は `Get` で記録して Go の表と比べる (lint の前例)
- 記録: `diff-base.json` は 1,960 → 16,434 case・71,661 → 136,591 呼び出し (7.3MB。4 本目の敵対的レビューの後に 16,474 case・136,718 呼び出し。下の節)。足したのは 定数 (群 F 10 本・G 3 本・H と I 14 本)・`type_marks` 18,071・`_type_changes` 3,240・`_rule_candidates` 4,588・`_modality_changes` と `_logic_changes` 各 7,321。
  組は群 K・L までと同じ入れ物 (入力の集まりと手で作った入力の全組・全文差分の手書きの組・見出しと文の手書きの組) に、各群専用の手書きの組と固定の種の組 (PCG 1076/7・8・9・10。群 F は印の選択肢ごとに当たる文と当たらない文 185 文、範囲づくりの枝を狙う手書きの組 60 組余り、印を混ぜた固定の種の組 400、書き出しを共有する文の言い換え 1,500 組) を足した。
  既存の case は消えず、既存の case の digest も kept も変わらない (記録の作り直しの前後を python で照合し、missing 0・changed 0。足した case を直すたびに作り直した)。`diff-cli.json` は変えていない
  - 手で書いた組の出力が空でないこと (空振りを止める検査の代わりの確認。作り直した記録を数えた): `_type_changes` の変化 491 組・`_rule_candidates` の markers・new_words・lost_words・structure・spans・logic・bold・endings の 3 欄・type_changes がどれも 1 組以上・`_modality_changes` の subkind 3 つと `_logic_changes` の subkind 3 つがどれも 30 組以上 (one_to_all 384・all_to_one 376・relation_to_cause 619・permission_lost 132・permission_added 109・ability_lost 39)
- わざと壊して red を確かめた (`bin/mutate-verify-list`。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf4/mutlog-f・mutlog-f2・mutlog-g・mutlog-g2・mutlog-h・mutlog-h2・mutlog-h3):
  🚨 **この節の本数と緑のまま通った一覧は誤り**: 9 本の壊した形は、書き換え前のコード (`bisectLeft`・`containsRune` など。commit した木には 1 度も入っていない) に当てていて、commit した木には当たっていなかった。commit した木で当て直した数は「4 本目の敵対的レビュー」の節にある (群 F 78 本のうち 50 本が red・群 G 23 本のうち 21 本が red・群 H・I 43 本のうち 29 本が red)。以下は当て直す前の記録のまま残す
  - 最初に当てた群 F の run は、list の JSON のパスが相対で、壊した形が 1 本も当たっていなかった (rc 2 の「使い方の誤り」のまま全 78 本が通り、壊した形を当てる手前で止まっていた)。絶対パスにして当て直した。結果の表の rc を読んでから結論を書く
  - 手書きの組と固定の種の組だけでは緑が多かった (最初の run で 49 本が緑か構文エラー)。書き出しを共有する文の言い換えの組 1,500 を足して 10 本が red になった。群 H・I の `AもBも` の最初の項の長い連なり・足した文の組を足して 2 本が red になった
  - 群 F で red にした主なもの: 残った文の下限 (`max(3, ceil(body/4))` の 3 と 4 分の 1)・6 割の下限 (`>=` と 2 割)・置き換えの数え方の割合と上限・Dice の上限 0.5 と 0.7・字面で組にする共通の組の下限 (4 組・半分・外の組)・消えた文の組み合わせの順・範囲の重なりの `<` と `<=`・丸ごと移した文の同じ文の取り方・書き出しの重なり・文の境目へずらす (引き分けの向き以外)・句点の直後の挿入・最初の同じ文を残す・文を分ける規則 (コード・表)・例えの対の止める位置・印の重複の除き方。群 G で red にしたもの: 数の増減・語の部分文字列・並び・構造の 3 つ・足した部分の前後 3 字・context の幅・立場の取り違え・太字の frontmatter。群 H・I で red にしたもの: 許可と能力の取り違え・打ち消しの能力・依頼との重なりの判定・種類の名前・問いの「か」の窓と右の判定・`すべての条件` の昇格・`CかD` の言い換えの除外・因果の足した文・広い因果
- 等価と判断して直さない (緑のまま): 群 G の `o != r` のときだけ足した部分を作る判定 (`o == r` は insert も replace も無い)・`notInO` の覚え (速さだけ)。群 H・I の `o == r` で早く返す判定 (範囲は変化の無い組を出さない。本家の早い戻りと同じ結果になるが、実測で等価と示してはいない)
- 緑のまま通った壊した形のうち、**等価かどうか分かっていないもの** (直さず記録。再提起するなら、その向きだけを変えた変更体で、本家と違う答えを返す入力の実測が要る。当て直した後の一覧は「4 本目の敵対的レビュー」の節):
  - 群 F (40 本): `f-owns-whole0` (書き直した文の中身が句読点だけの文で 6 割の判定が動く組を足したが緑のまま)・`f-dice-lt-le`・`f-least-5`・`f-least-7`・`f-shared-lt`・`f-owner-b1`・`f-owner-shrink-end-b1`・`f-owner-shrink-start`・`f-slide-right`・`f-slide-left`・`f-slide-tie`・`f-mapstart-prev-i1`・`f-mapend-bare-off`・`f-whole-delete`・`f-group-merge-max`・`f-found-overlap-lt`・`f-found-overlap2`・`f-found-len-ge`・`f-found-trim-off`・`f-trim-chars-comma`・`f-empty-body-skip`・`f-overlap-reach`・`f-overlap-end`・`f-widen-rends`・`f-widen-rstarts`・`f-widen-ordered-off`・`f-widen-skip-self`・`f-widen-fallback-le`・`f-pair-after-off`・`f-pair-before-off`・`f-pair-after-blank`・`f-pair-sort-off`・`f-notinstops-empty`・`f-spans-code`・`f-spans-strip`・`f-cand-order-gi`・`f-cand-order-ix`・`f-simile-paired-off`・`f-simile-start-len`・`f-simile-stable`。
    ほとんどは範囲を広げる・文の境目へずらす・消えた文を字面で組にする所の境目 (ちょうど `<` と `<=` の境にあたる入力が必要) で、1,500 組の言い換えと手書きの組のどれも当たっていない。本家と違う答えを返す入力を示せていないので、防御は足さない。本家の値と同じことは、全記録 (`type/` 31,041 呼び出し) で確かめている
  - 群 H・I (21 本): `h-name-break`・`h-sort-stable`・`h-eq-guard`・`l-ka-left`・`l-head-no-start`・`l-head-okuri-eq`・`l-head-zero`・`l-head-tomo`・`l-head-lt`・`l-tails-two`・`l-tails-owned-ge`・`l-one-all-any`・`l-one-all-fallback`・`l-all-one-before`・`l-all-one-r`・`l-any-change`・`l-tails-cond`・`l-unique-old`・`l-sort-stable`・`l-eq-guard`・`l-tails-newline`。
    `l-head-tomo` と `l-head-zero` は、入力で届かない (「とも」の前は「はがも」の後読みで名詞の字でない・位置 0 は後読みで当たらない) ので等価の見込みだが、示してはいない
- 本家と揃わなかった所: 無い (群 F〜I の全記録が一致)。最初の記録で一致し、直した所も無い
- 確かめていないこと:
  - 上の緑のままの壊した形 (群 F 40 本・群 H・I 21 本。当て直して 28 本・13 本になった。下の節) の入力が本家と違う答えを返すか
  - 群 F〜I の所要 (数十万字・同じ段落を繰り返す文書の `typeRanges`・`pairGoneSentences`・`logicTails`) は測っていない。本家の `_overlapping`・`_widen_to_sentences`・`passed_by_grams` の覚えは写した (結果が同じなので、記録では遅さが見えない)
  - `diff()` の全体の流れ (群 AC) で `typeCache` を使い回すとき、`_LAST_TYPE_RANGES` を使い終えたら空に戻す所 (本家の `diff()` と `meaning_risks()`) は、群 AC が入るまで未確認。`TypeChanges` が cache に置き、`shared` が使い回す形だけを記録で見ている (`ModalityChanges`・`LogicChanges` は cache を空で渡す形で比べた)
  - 単独のサロゲートを含む文書は、群 F〜I の記録に入れていない
- 最終の検査: 下の「最終の検査 (4 本目)」

#### 最終の検査 (4 本目)

最後の編集 (docs の commit の直前の木。コードは 26d81ada0 と同じ) で `make -C src/ja-writing lint test`・repo の root で `make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` を回し、rc は全部 0 (ログは tmp/yomiyasu-gap-2026-11-12/diff-wf4/fin-*.log)。

### 4 本目の敵対的レビュー (opus。diff-wf4 の 070385cc5..36a7d2212)

群 F〜I の Go の写しは、本家と違う答えを 1 件も返さなかった (fuzz 26,300 組。下)。一方、この issue の 4 本目の節に書いた「わざと壊した」の結果には誤りがあった。9 本は commit した木に無いコードに当てていて、緑のまま通ったとした形のうち 18 本は、本家と違う答えを返す入力を作れた。指摘は、本家 (python) の答えと壊した形の答えを並べて再現できたものだけ直した。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P1-1 4 本目の節の壊した形のうち 9 本が commit した木に当たっていない (`bisectLeft` → `sort.SearchInts`・`!(after \|\| before)` → `!after && !before`・`containsRune` → `slices.Contains`・並べ替えを `sortMarks` へ寄せた、の書き換えの前のコードに当てていた) | 直した。一覧を commit した木のコードに合わせて作り直し (`f-overlap-start`・`f-widen-left-k`・`f-widen-right-k`・`f-marks-dedupe`・`f-pair-after-off`・`f-pair-before-off`・`l-tails-newline`)、commit した木 (56db85d7f) で全部当て直した。`h-sort-stable` と `l-sort-stable` は `sortMarks` 1 か所への同じ変更で `f-simile-stable` と同じ形なので一覧から外した | 当て直した本数は下。7 本のうち 4 本は記録の検査で red (`f-overlap-start`・`f-widen-left-k`・`f-widen-right-k`・`f-marks-dedupe`)、`f-pair-after-off`・`f-pair-before-off`・`l-tails-newline` は P2 の入力を足して red |
| P1-2 「本家と違う答えを返す入力を示せていない」は誤り (下の 18 本で入力を作れた) | 直した | 4 本目の節の「等価かどうか分かっていないもの」から 18 本を外した (上の注記) |
| P2 群 F の 12 本 (`f-dice-lt-le`・`f-least-5`・`f-shared-lt`・`f-owner-b1`・`f-mapend-bare-off`・`f-whole-delete`・`f-widen-rends`・`f-pair-sort-off`・`f-spans-strip`・`f-cand-order-ix`・`f-pair-after-off`・`f-pair-before-off`) | 直した。壊した形ごとの最短の入力を `oracle/diffbroken_cases.go` に置き、群 F の組 (群 G・H・I の段にも通る) に足した (`f-pair-before-off` だけは `_modality_changes` の組で、群 H・I の組に足した)。本家の答えは記録 (python) との突き合わせで確かめた | 12 本とも `TestBaseMatchesPythonRecording` で red |
| P2 群 H の 1 本 (`h-name-break`)・群 I の 5 本 (`l-one-all-any`・`l-all-one-before`・`l-all-one-r`・`l-unique-old`・`l-tails-newline`) | 直した。同じ `oracle/diffbroken_cases.go` の群 H・I の組に足した | 6 本とも red |

- 記録: `diff-base.json` は 16,434 → 16,474 case・136,591 → 136,718 呼び出し。足したのは群 F の組 11 本 (`type/`・`rule/`・`modal/type/` の 3 か所に通る 33 case) と群 H・I の組 7 本 (`modal/`。`f-pair-before-off` の入力は `_modality_changes` の組なのでこちら) の計 40 case (`type/` 31,041 → 31,085・`rule/` 4,591 → 4,602・`modal/` 29,298 → 29,370 呼び出し。`_type_changes` 3,251・`_rule_candidates` 4,599・`_modality_changes` と `_logic_changes` 各 7,339)。`type_marks` は 18,071 のまま。
  既存の case は消えず、既存の case の digest も kept も変わらない (記録の作り直しの前後を python で照合し、missing 0・changed 0・足した case 40)。README の件数も記録から数え直して直した (関数ごとの呼び出し数の合計と、各 case の呼び出し数の合計の両方が 136,718)
- わざと壊して red を確かめた (commit した木 56db85d7f で `bin/mutate-verify-list`。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf5/mutlog-f・mutlog-g・mutlog-h と mut-f.out・mut-g.out・mut-h.out、一覧は list-f.sh・list-g.sh・list-h.sh と mutants.json): 144 本のうち 100 本が `TestBaseMatchesPythonRecording` で red・43 本が緑のまま・1 本が構文エラー。
  - 群 F 78 本: 50 本が red・28 本が緑のまま。群 G 23 本: 21 本が red・2 本が緑のまま。群 H・I 43 本: 29 本が red・13 本が緑のまま・1 本が構文エラー (`l-tails-newline-off`。条件ごと消した形で、同じ所を 'x' にした `l-tails-newline` が red なので当て直さない)
  - レビューが指した 18 本と、当てる先を直した 4 本は全部 red (名前を出して rc 0 を確かめた)
- 緑のまま通った壊した形 (43 本。**等価かどうか分かっていない**。直さず記録。再提起するなら、その向きだけを変えた変更体で、本家と違う答えを返す入力の実測が要る。レビューは別に 10,000 組の fuzz でも当てたが、これらは緑のままで、本家と違う答えを返す入力を作れていない):
  - 群 F (28 本): `f-owns-whole0`・`f-least-7`・`f-owner-shrink-end-b1`・`f-owner-shrink-start`・`f-slide-right`・`f-slide-left`・`f-slide-tie`・`f-mapstart-prev-i1`・`f-group-merge-max`・`f-found-overlap-lt`・`f-found-overlap2`・`f-found-len-ge`・`f-found-trim-off`・`f-trim-chars-comma`・`f-empty-body-skip`・`f-overlap-reach`・`f-overlap-end`・`f-widen-rstarts`・`f-widen-ordered-off`・`f-widen-skip-self`・`f-widen-fallback-le`・`f-pair-after-blank`・`f-notinstops-empty`・`f-spans-code`・`f-cand-order-gi`・`f-simile-paired-off`・`f-simile-start-len`・`f-simile-stable` (`f-simile-stable` は `sortMarks` の安定な並べ替えを外す形。印は種類ごとにまとめて使うので、読んだ限りでは答えは変わらない。実測はしていない)
  - 群 G (2 本): `g-spans-guard`・`g-notin-cache` (どちらも速さだけで等価と読んだ。実測はしていない)
  - 群 H・I (13 本): `h-eq-guard`・`l-ka-left`・`l-head-no-start`・`l-head-okuri-eq`・`l-head-zero`・`l-head-tomo`・`l-head-lt`・`l-tails-two`・`l-tails-owned-ge`・`l-one-all-fallback`・`l-any-change`・`l-tails-cond`・`l-eq-guard`。`l-head-tomo` と `l-head-zero`・`h-eq-guard`・`l-eq-guard` は、入力で届かない (「とも」の前は「はがも」の後読みで名詞の字でない・位置 0 は後読みで当たらない・`o == r` の早い戻り) ので等価の見込みだが、示してはいない
- 壊せなかったもの (敵対的レビューの報告): fuzz は 2 本の生成器で 26,300 組 (印の付け外しと入れ替え・文の分割と結合・Markdown の記号・CRLF・BOM・ゼロ幅の字・本家のコメントの例文・入力の集まりの行、選ぶ範囲と因果に寄せた組)。比べた段は `_type_changes`・`_modality_changes`・`_logic_changes`・`_rule_candidates` (立場 4 通り)・`type_marks` で、`_rule_candidates` の直後に cache を使い回す modality と logic (本家の `diff()` と同じ順) も比べた。食い違いは 0 件。19 万字の繰り返しと 2,000 文の並べ替えでも答えは同じで、所要は Go の方が短い (本家 3.46 秒・Go 1.05 秒)。cache の使い回し (F の範囲を H・I が受け取る所)・bisect の書き換え・byte と文字の添字・`notInStops`・float の計算の順・並べ替えの順は本家と行ごとに読み比べて差なし。共有の部品の付け替え (`replay.Strings`・`replay.Pattern`・`method`) で答えは変わらない
- 本家と揃わなかった所: 無い
- 確かめていないこと (レビューの報告から): 単独のサロゲートを含む文書 (Go の `encoding/json` が U+FFFD に置き換えるので、記録の道具では渡せない)・群 AC の cache の後始末 (未実装)・上の緑のまま通った 43 本が等価か
- 再提起に要る実測: 緑のまま通った 43 本について、本家と違う答えを返す入力 (o・r の 2 つの文書と、本家・壊した形の答え) を python と Go の両方で示す。レビューの 10,000 組の fuzz では作れなかった

### CI の時間切れ (2026-11-14)

- 1916a1ef9 の CI (`src/ja-writing`) が 2 回とも赤。lint のパッケージの `go test -race` が上限 10 分を超えた (600.1 秒)。lint のコードは変えていない。同じ runner で並ぶ diff のテストが記録の増加で 230 → 248 → 335 秒と伸び、緑だった run でも lint は 552・569 秒で上限すれすれだった。
- 手元の race の所要 (4 本目の木): lint の `TestCLIMatchesPythonRecording` 266 秒・`TestRawFindingsMatchPythonRecording` 96 秒、diff の `TestBaseMatchesPythonRecording` 172 秒。パッケージ全体は lint 387 秒・diff 232 秒。
- 3 本とも 1 つの goroutine で順に呼ぶだけなので、race で見つけられるものは無い。pyre の全パターンの突き合わせの前例に揃え、`//go:build !race` のファイルへ移し、`make test` が race なしで別に回す (`Makefile` の `NORACE_CHECKS`)。上限を延ばす形は採らない (diff の群が増えるたびに同じことが起きる)。
- 別の run は各テストの `--- PASS: <名前>` の行を見る。無いテスト名を渡すと rc 1 で落ちることを確かめた。

### 5 本目 (diff-wf5。2026-11-14。worktree で作業し、push していない)

- commit (群 R・Z・AA まで。AB 以降は着手していない。M〜Y・AC・AD も手を付けていない):
  - aa1c1a3a1 feat(ja-writing): diff の語の一覧の読み (群 R: parse_list・_load_list・_list_info) を作り、lint と行の読みを lists に共有して、本家の記録と突き合わせる (issue 1076)
  - 0a397b081 feat(ja-writing): diff の文体の硬化 (群 Z: stiffness・stiffness_metrics) を作り、本家の記録と突き合わせる (issue 1076)
  - 053737731 test(ja-writing): 群 Z の記録に、漢数字で始まる語を含む 3 つの名詞の「の」の連続の組を足す (issue 1076)
  - 071393ef9 feat(ja-writing): diff の勢い (群 AA: tone) を作り、本家の記録と突き合わせる (issue 1076)
  - f2faaac5c test(ja-writing): 群 AA の記録に、緑のまま通った壊した形 7 本のうち 6 本の入力を足す (issue 1076)
  - 501b370d2 test(ja-writing): 群 AA の記録に、見出しの字数で絵文字を最初の 1 つだけ外す判定を分ける文字列を足す (issue 1076)
  - c7f85f1f3 docs(issues): 1076 に 5 本目 (群 R・Z・AA) の進捗と、わざと壊した形の結果・確かめていないことを書き、README と epic の状態を今に合わせる (issue 1076)
  - 666b9cfbe fix(ja-writing): 群 R の冒頭のコメントから issue の番号の参照を外す (make test-lint の test-comment-provenance が止めた)
  - 各 commit は、commit の前の木で `make -C src/ja-writing lint test` (rc 0)・`make test-unused-excluding-tests`・`make test-dupscan-gate` を通した (rc を 1 つずつ確かめた。071393ef9 は unused と dup を、テストの `fmt.Sprint` を `strconv.Itoa` に直す 1 行の前の木で回し、その後の `make lint test` は rc 0)。`make test-lint` は最後の木だけで回した (下の「最終の検査 (5 本目)」)。🚨 そのため aa1c1a3a1 から c7f85f1f3 までの各 commit は、`diff/wordlist.go` の冒頭のコメントに issue の番号があり `make test-lint` の `test-comment-provenance` が落ちる (666b9cfbe が直した。ほかの 3 本の検査は上のとおり commit ごとに通っている)
- 群 R (`diff/wordlist.go`・`lists/parse.go`): `ParsePatternList` (本家の `parse_list` の pattern が真。書き方の誤りは `*ListError` = ValueError)・`LoadList` (`_load_list` の items)・`ListSource` (`_list_info` の list。list_problem はいつも null)。語だけの一覧の読みは `lists.Words`。
  BOM・CRLF・空行・「#」の行の読みは lint の `parseList` と `parseThinList` が写していたので、`lists.Lines` に寄せて両方がこれを使う (行末の `\r` は本家の `rstrip("\r")` と同じく全部落とす。lint の一覧は固定で答えは変わらない)。
  本家の内蔵の一覧 (`BUILTIN_LISTS`)・`_LIST_PROBLEMS`・`_any_of` の「読めなかった」経路は写さない (決めたこと 5)。oracle は `lists` を import しない (`lists` が使う `internal/pystr` の試験が `oracle` を import していて循環する)。`--words` / `--no-words` の経路 (`visibility.ResolveWords`) と `context.words` は触っておらず、diff の CLI の記録 (`--words` の組を含む) は変わらず通った
- 群 Z (`diff/stiffness.go`): `Stiffness`・`StiffnessMetrics`・`ChangedExcerpt`。文字と数字・長さに数える句読点・漢字・平仮名は本家と同じ正規表現で数える (`_measure` の範囲を字の範囲で書き直すと、定数の記録と結び付かない)。
  割り算と `round` は Python の float と同じ順 (`100.0 * 漢字 / 字数`・`round(x, 2)`・文書全体の条件は丸めた値どうしの引き算)。名詞化の正規表現の前置き `anyOf` (`_any_of`) と文ごとの覚え (memo) も写した。
  本家の `_stiffness` が `sides.texts` の同一か hunk の無いときに早く返す所は、texts が同じなら hunk も無いので hunk の有無だけを見る。`no_nouns` の閾値は本家の中でも読まれず、Go に表を持たない (「の」の連続の正規表現の `{2,}` が名詞 3 つ以上を表す)
- 群 AA (`diff/tone.go`): `Tone`・`PseudoHeading`・`MarkSpans`・`ToneExcerpt`。絵文字は lint の `EmojiMatches` (`_FALLBACK_EMOJI` は写さない)。`masker` に数えない行の種類を渡せるようにした (`newMaskerWith`。勢いは表を除くが見出しは数える)。
  🚨 **本家の `_tone_excerpt` は、印が共通の前の部分をまたぐと推敲後の側の位置が負になり、Python の添字 (負は後ろから数える) で読む**。長さを超える負の位置は `IndexError` で `tone()` ごと失敗する (元の文が「とても」だけ、推敲後が「とてつもなく」で始まる長い文など。記録に 28 件ある)。
  負の添字・切り出しを同じ規則で写し、IndexError は `*visibility.Error` で返す。**群 AC で CLI に配線するとき、この error を rc 3 (`yomiyasu_diff.py: 内部エラー: IndexError: string index out of range`) にする**
- 共有: `pyjson.MapList` を足し、`replay.MapList` と lint の試験の `valuesOf` を寄せた。`replay.ExceptionDigest` (例外の digest) と、テストの `outcomeDigest` (parse_list の ValueError と tone の IndexError を同じ形で突き合わせる)。例外を投げうる関数は `oracle.DiffRaisingFunctions` (記録に例外と成功の両方がなければ落ちる)
- 記録: `diff-base.json` は 16,474 → 34,077 case・136,718 → 178,780 呼び出し (7.3MB → 9.8MB)。足した呼び出し (関数ごと):
  - 群 R: `parse_list` 2,580 (うち本家が ValueError を投げる 760)・`_load_list` 4・`_list_info` 4 (`_load_list` は群 Z の 1・群 AA の 245 の覚えるだけの呼び出しを足して計 250。5 本目の敵対的レビューの後は 254)。入力は 8 本の一覧そのもの (両方の読み方)・手書き (BOM・CRLF・空行・コメント・タブの数・3 つ目の欄・誤りの行の表示の 40 字ちょうどと 41 字)・固定の種 (PCG 1076/11) の 1,200 文
  - 群 Z: `stiffness` 9,048・`stiffness_metrics` 17,546・`_measure` 152・`_ratio` 99・`_nominal_hits` 152・`_no_chains` 152・`_changed_excerpt` 155・定数 14。組は群 F〜I までと同じ入れ物 (入力の集まりと diff の入力の全組・全文差分・見出し・文・種類・許可・規則の手書きの組) に、閾値の分かれ目の格子 (1 文の漢字の割合 +8.0 ポイントの手前と向こう・漢字 +2 字・平仮名 −2 字・文書全体 +3.0 ポイント・名詞化 +2)・本家のコメントの例文・固定の種 (PCG 1076/12) の 900 組
  - 群 AA: `tone` 9,717 (うち IndexError 28)・`_tone_level` 98・`_pseudo_heading` 215・`_heading_chars` 52・`_mark_spans` 261・`_tone_excerpt` 85 (うち IndexError 5)・`_tone_lines` 976・`_intensifier_pattern` 245 (覚えるだけ)・定数 17。組は群 Z と同じ入れ物に、見出しの字数の 7 割の分かれ目・印の数の増減・本家のコメントの例文・固定の種 (PCG 1076/13) の 900 組・IndexError になる組・緑のまま通った壊した形を分ける組を足した
  - 既存の case は消えず、既存の case の digest も変わらない (記録の作り直しの前後を python で照合し、missing 0・changed 0。f2faaac5c で足した `tone/found` だけは 501b370d2 で呼び出しを 4 件足したので、digest の先頭は同じまま長くなった)
  - 記録が届く範囲を数える検査を足した: 群 Z は候補の 5 種類・level の 2 つ・候補なしのどれにも 20 回以上、群 AA も同じ (`checkStiffHits`・`checkToneHits`。記録の組に Go の答え (= 記録と同じ) を通して数える)
  - 記録の作り直しは約 55 秒 (`TMPDIR=<上に .git の無い dir> go run ./oracle/cmd/golden -only diff-base`)。本家の Python は 3.15.0
- わざと壊して比べた (`bin/mutate-verify-list`、commit した木で。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf5/mutlog-r・mutlog-z・mutlog-z2・mutlog-a・mutlog-a2・mutlog-a3):
  - 群 R (aa1c1a3a1): 11 本のうち 11 本が red (誤りの行の表示の字数 40 と `>` の向き・正規表現の欄の strip・名前の strip・3 つ目の欄の strip・list の出どころの文字列・BOM・行末の `\r` を 1 つだけ落とす形・コメントの `lstrip`・空行の判定・語の strip)
  - 群 Z (0a397b081): 27 本のうち 24 本が red・3 本が緑のまま。緑の `z-numeral-head` (漢数字で始まる語を漢語に数えない判定を外す) は、名詞が 3 つちょうどで 1 つが漢数字で始まる連続の組 4 つを足して (053737731) red になった。
    red のもの: 1 文の漢字の割合・漢字・平仮名の 3 つの条件の向き・文書全体の割合・漢字・組の数・名詞化の数と漢字・漢語の数と漢字 2 字・文書全体の例に選ぶ組の同点の取り方 (最初の最大)・重なった当たりの残し方・丸めの桁 (平均の長さ・割合)・1 文の条件で報告した行を名詞化で数え直さない判定・置き換えの塊の組・抜き出しの幅と左へ戻る条件・補足カッコを外した推敲を出さない判定・「の」の連続の数の比べ・長さに数える句読点の集合・平仮名の範囲・「の」の連続の量指定子・`_ratio` の字数 0
  - 群 Z の緑のまま (等価): `z-hits-sort` (同じ始まりの当たりは長いほうを先に並べる。名詞化の 12 本の正規表現は、先頭の 2 字までで互いに違い、同じ位置から始まる 2 つの当たりは作れない。入力の集まりの 5,605 行を本家で数えても、同じ位置から始まる当たりは 0)・`z-paired-first` (同じ新しい文の位置が 2 回出たら先のものを残す。位置は文ごとに別なので、同じ鍵は出ない。実測はしていない)
  - 群 AA (071393ef9): 定義 33 本。最初の run (31 本) は 22 本が red・7 本が緑・1 本の目標が合わず (`a-indirect-cond`。直した後の比べの向きを `>=` にする形は、減りが 0 のとき足すのが 0 で等価)・1 本が構文エラー (`a-pseudo-paragraph`。重複した case。`a-pseudo-list-nobold` に替えた)。
    緑の 7 本のうち 6 本 (`a-excerpt-len-old`・`a-excerpt-len-new`・`a-excerpt-overlap`・`a-excerpt-branch2`・`a-excerpt-strip`・`a-heading-emoji-all`) は、本家のコピーに同じ壊した形を入れて探した入力 (乱数で 600,000 組・手で作った 40 字の境の両側など) を足して red になった (f2faaac5c・501b370d2)。
    置き換えた `a-pseudo-list-nobold` と足した `a-indirect-or` も red。最終は 33 本のうち red 30 本・緑 2 本 (等価)・構文エラー 1 本 (当て直さず、別の形に替えた)
  - 群 AA の緑のまま (等価): `a-heading-zero` (元の見出しの字数が 0 のとき `新しい字数 < 0 × 0.7` は新しい字数が 0 以上なので偽。条件を外しても答えは変わらない)・`a-indirect-cond` (上)。red のもの: 見出しの割合 (0.7 と `<` の向き)・数の比べの向き・間接の問いの減りの引き方と判定の `&&`・level の判定 (種類ごとの絵文字と見出しの参考・参考にする文書の種類・節の level の基の値)・promo の番号つき項目と太字だけの行を見出しに数える判定・数えない行の種類 (表)・抜き出しの幅と長さの境 (消えた行だけ・組の行)・印の選び方 (重なる条件・最初の候補)・推敲後の位置 (印が変わった所の直後・負の数の割り算・負の添字の読み方)・文の終わりの判定・文の終わりのあとが空白だけのときの始め方・組になる行の選び方 (最後の行・break・最初の候補・見出しのある行の絞り込み・合計の比べ)・強める語の並び・絵文字の位置の進め方・見出しの字数で絵文字を外す回数
- 本家と揃わなかった所: 無い (群 R・Z・AA の全記録が最初の記録で一致し、直した所も無い。上の IndexError も本家と同じに写した)
- 確かめていないこと:
  - 群 Z・AA の所要 (数十万字・同じ段落を繰り返す文書の `stiffness` の `noChains`・`nominalHits`・`tone` の `toneLinesOf`)。名詞化の前置き (`anyOf`) と memo は写したが、結果が同じなので記録では遅さが見えない
  - `_stiffness` と `_tone` の「texts が同じなら早く返す」・`texts` が同じときの `_tone_lines` の使い回しは、hunk の有無だけにした (等価と読んだ。texts が同じなら行も同じで hunk が無い)。実測で示してはいない
  - 絵文字の数え方は lint の `EmojiMatches` で、本家が lint を読めないときの `_FALLBACK_EMOJI` は写していない (読めない経路は無い)。一覧の `list_problem` が非 null になる経路 (一覧が読めない) も写していない
  - 単独のサロゲートを含む文書・巨大な数の文字列は、群 R・Z・AA の記録に入れていない
  - `tone()` が IndexError で落ちる経路の CLI (rc 3 と標準エラーの 1 行目) は、群 AC の CLI への配線までは比べていない。`diff()` の全体の流れで `stiffness`・`tone` を呼ぶ形・`--fail-on` との関係も群 AC・AD まで未確認
  - 本家の `tests/` (本家のテスト) を Go から動かしてはいない
- 5 本目の敵対的レビューは下の節 (指摘の扱いと結果)

#### 最終の検査 (5 本目)

最後のコードの編集 (666b9cfbe) の後の木で、`make -C src/ja-writing lint test`・repo の root で `make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` を回し、rc は全部 0
(ログは tmp/yomiyasu-gap-2026-11-12/diff-wf5/fin-ltest.log・fin-un.log・fin-tl.log・fin-dup.log と、同じ名前の .rc)。この節を足す docs の commit の後は、`make test-lint` と上の 3 本をもう一度回した (rc は fin2-*.rc)。

### 5 本目の敵対的レビュー (opus。diff-wf5 の 28ef53f40 の木。直したのは diff-wf6)

`stiffness`・`tone`・`stiffness_metrics` で、本家と違う答えを返す入力は 1 件も作れなかった。fuzz は 35,255 組 (一般 20,300・`_tone_excerpt` に寄せた 20,000・変わった文書の種類 255。例外の型と文面も比べた。本家が IndexError を 64 回投げた組でも、Go は型と文面が合った)。指摘は、本家 (python) の答えと壊した形の答えを並べて再現できたものだけ直した。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P1-1 `diff/tone.go` の `ToneExcerpt` のコメントの例 (`tone('とても', 'ともかく'+'あ'*50)` は失敗する、と読める) が誤り | 直した。本家で `tone('とても','ともかく'+'あ'*50)` は candidate_count 1・`'とてつもなく'+'あ'*50` は IndexError を python で確かめた。コメントの例を後者にし、前者は失敗しないと書いた (cadd1aaf4) | 直した |
| P1-2 この issue の群 R の `_load_list` が 5 (実際は 4。ほかは群 Z の 1 と群 AA の 245 で、合計 250 は正しい) | 直した (上の記録の行) | 直した |
| P1-3 README の「印が共通の前の部分をまたぐと IndexError で失敗する」が強すぎる | 直した。位置が負になって文字列の長さを超えるときだけ失敗する、と条件を書いた (cadd1aaf4) | 直した |
| P2 `m-cut-whole`・`m-strip-blank`・`m-best-sorted`・`m-pseudo-text`・`m-question-mask`・`m-excl-len1`・`m-excerpt-strip` (壊しても記録の検査が緑のまま通る 7 本。どれも本家の答えと違う入力がある) | 直した。レビューが示した入力を `oracle/diffbroken_tone_cases.go` (tone 4 組・stiffness 1 組) に置き、`tone/broken/`・`stiff/broken/` の case にした。`m-excl-len1` は「絶対値」「絶対パス」「絶対的」の後ろに強める語が続く文 3 つを `_mark_spans` に、`m-excerpt-strip` は節の端の「，」の組 4 つを `_changed_excerpt` に足した (78b1e3cdd) | 7 本とも red (下の表) |
| P3 `lint/g15_renyo.go` に消した `parseList` の doc コメントが残っている | 直した。`renyoPatterns` の説明に書き直した (cadd1aaf4) | 直した |
| P3 `diff/tone_oracle_test.go` の `_tone_lines` の突き合わせが、記録の強める語の引数を読まない | 直した。`recordedIntensifiers` で、記録の引数が一覧から作った正規表現の参照 (`ip`) でも null でもなければ落とす (7d32241f8)。ただし、参照が指す中身 (どの一覧から作ったか) までは見ない。今の記録は 1 種類だけで、答えは変わらない | 直した (中身は未確認。下) |
| 緑のまま通ったが、本家と違う入力を作れなかった 3 本 (`m-default-kind`・`m-key-runes`・`m-hunks-only`) | 採らない (レビューも等価と見ている)。直さず記録。再提起するなら、その向きだけを変えた変更体で、本家と違う答えを返す入力 (o・r・文書の種類と、本家・壊した形の答え) を python と Go の両方で示す | 未証明 |

- 記録: `diff-base.json` は 34,077 → 34,082 case・178,780 → 178,871 呼び出し (`tone` 9,717 → 9,721・`stiffness` 9,048 → 9,049・`stiffness_metrics` 17,546 → 17,548・`_tone_lines` 976 → 992・`_mark_spans` 261 → 279・`_pseudo_heading` 215 → 230・`_heading_chars` 52 → 55・`_changed_excerpt` 155 → 175・`_load_list` 250 → 254・`_intensifier_pattern` 245 → 249・`_build_full_diff` 1,106 → 1,110)。README の件数は、記録の関数ごとの合計と case ごとの合計の両方 (178,871) から数え直して合わせた。IndexError は `tone` 28・`_tone_excerpt` 5 のまま (足した組に落ちるものは無い)。
  書き足しだけ: 既存の case は消えず (missing 0)、変わったのは `stiff/sentences` (711 → 731 呼び出し)・`tone/strings` (619 → 655) の 2 つで、どちらも digest と kept が前の値を先頭に持つ (呼び出しを末尾に足した)。足した case は 5 (`stiff/broken/stiff-best-tie`・`tone/broken/` の 4 つ)。python で照合した。
- わざと壊して red を確かめた (commit した木 78b1e3cdd で `bin/mutate-verify-list`。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf5/wf6/mutlog と mut.out、一覧は wf6/list.sh、壊し方はレビューの rt.P8Qkcn/mymut.json): 12 本のうち 12 本が `TestBaseMatchesPythonRecording` で red (rc 0)。
  - 群 Z (5 本): `m-best-sorted` (同点の最大の選び方。`stiff/broken/stiff-best-tie` で落ちた)・`m-excerpt-strip` (節の端の「，」。`stiff/sentences` で落ちた)・`m-hira-swap` (平仮名の比べの向き)・`m-paired-nostart` (組の前置きの条件)・`m-raw-eq` (同じ文の判定)
  - 群 AA (7 本): `m-cut-whole` (`tone/broken/tone-cut-whole-promo`)・`m-strip-blank` (`tone-strip-blank-mail`)・`m-pseudo-text` (`tone-pseudo-line-promo`)・`m-question-mask` (`tone-question-mask-heading`)・`m-excl-len1` (`tone/strings` と `tone-pseudo-line-promo`)・`m-slice-neg` (負の位置の切り出し)・`m-emoji-bytes` (絵文字の位置)
  - レビューが緑のまま通ったとした 7 本は、足す前の木では緑・足した後は全部 red で、最初に落ちた case が足した入力 (上の case 名) であることをログで確かめた
- 壊せなかったもの (敵対的レビューの報告): 上の fuzz 35,255 組で 0 件の食い違い。fuzz の道具が壊した形を見つけることも確かめた (レビューが当てた 21 本のうち 16 本が食い違いを出した)。実装の係が等価とした 4 本 (`z-hits-sort`・`z-paired-first`・`a-heading-zero`・`a-indirect-cond`) は fuzz でも 0 件で、レビューも等価と読んだ。埋め込みの語の一覧 4 本は本家の `references/lists` とバイト単位で同じ。99,000 字の文書 (同じ段落を 3,000 回) で 3 つの関数の合計は Go 0.25 秒・本家 0.26 秒 (0.17 + 0.09)
- 本家と揃わなかった所: 無い
- 確かめていないこと (レビューの未確認のリスクと、この係の分):
  - 単独のサロゲートと不正な UTF-8 (fuzz に無い。`ParsePatternList` の 40 字の打ち切りと `toneCut` の空の代わりの長さは Go の `[]rune` で数えていて、`pyunicode.Runes` ではない。記録の道具は Go の `encoding/json` が U+FFFD に置き換えるので渡せない)
  - BOM: 本家の `_load_list` は BOM を 2 回落とす (`utf-8-sig` と `parse_list`) が、Go の `LoadList` は 1 回。埋め込みの一覧に BOM は無いので今は届かない。一覧を差し替えられるようになったら食い違いうる
  - 所要は 1 つの文書の形 (同じ段落の繰り返し) だけ。名詞化の連続・「の」の連続・数万字の 1 行は測っていない
  - `recordedIntensifiers` は参照の名前 (`ip`) までしか見ず、参照が指す一覧の中身は見ない
  - 3 本 (`m-default-kind`・`m-key-runes`・`m-hunks-only`) が等価かどうか (上)
- 再提起に要る実測: 上の 3 本について、本家と違う答えを返す入力 (o・r と、本家・壊した形の答え) を python と Go の両方で示す

#### 最終の検査 (5 本目の敵対的レビューの後)

最後のコードの編集 (8edca7083。78b1e3cdd のコメントを `make test-lint` の `test-comment-provenance` が止めたので直した) の後の木で、`make -C src/ja-writing lint test`・repo の root で `make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` を回し、rc は全部 0
(ログは tmp/yomiyasu-gap-2026-11-12/diff-wf5/wf6/fin-lt.log・fin-un.log・fin-tl.log・fin-dup.log と fin-rc.txt)。この節の docs の commit の後に `make test-lint` をもう一度回した。cadd1aaf4・7d32241f8 は単独では lint test を回していない (コメントとテストの引数の確認だけの変更で、78b1e3cdd の木で全部通した)。

### 6 本目 (diff-wf6。2026-11-14。worktree で作業し、push していない)

- 順は M → N・O → Q (Q は N・O のあとにした。Q の正規表現が名前つきグループで、先に小さい群を通した)。群 P 以降 (P S T U V W X Y・AB・AC・AD) は手を付けていない
- commit:
  - bd056ab37 feat(ja-writing): diff の数値 (群 M: extract_numbers・_number_risks)
  - d054d5ba7 test(ja-writing): 群 M の記録に、緑のまま通った壊した形の入力を足す
  - b9f15352d feat(ja-writing): diff の否定 (群 N) と主語と目的語の入れ替え (群 O)。N と O は 1 つの commit (記録の作り直しが 2 回に分かれると、片方の commit では記録の半分が空になる)
  - 4581c661f feat(ja-writing): diff の概数と範囲の語 (群 Q)
  - 各 commit で `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-dupscan-gate` を通した。ただし bd056ab37 は、入力の語を言い換えて記録を作り直す前の木で通し、直した後は diff のテストと記録の突き合わせだけを回した。`make test-lint` は最後の木だけ (rc 0)
- 群 M (`diff/numbers.go`・`numrisks.go`・`quantity.go`): 本家の `_find_numbers` ほか。`_negative_zu` と「少ない」などの語 (`negationWords`) は群 N・S の持ち物だが、`_quantity_negated` が使うので M に入れた。
  🚨 `_cancel_notation` は何も打ち消せないとき渡された Counter をそのまま返す (本家と同じ。続く `_drop_weak` が書き換えるので、別のオブジェクトにすると `raw_added - added` の値が変わる)。Go は map の参照で同じ形にした。巨大な整数は本家が ValueError で止まるが Go は止まらない (記録に無い。止まる条件は 6 本目の敵対的レビューの節に書いた)
- 群 N (`diff/negation.go`)・O (`swap.go`)・Q (`qualifier.go`): 本家の関数を写した。Q の正規表現は本家の `pattern` 属性から生成して貼った (手で写さない)
- 記録: `diff-base.json` は 34,082 → 71,277 case・178,871 → 312,293 呼び出し (この commit の時点。6 本目の敵対的レビューの直しで 71,290 case・312,340 呼び出しになった)。足したもの (呼び出し数): `extract_numbers` 19,293・`_number_risks` 9,557・`_negation_risks` 9,516・`_swap_risks` 8,600・`_qualifier_risks` 9,513・各群の関数と定数。組は群 F〜I と同じ入れ物 (`diffBags`) に、各群の手書きの組と固定の種 (PCG 1076/14〜19) を足した。書き足しだけ (既存の case の削除・変更 0 を python で照合)。作り直しは約 70〜90 秒
  - 届き方を数える検査: 群 M は replaced・added・lost・order・文書全体の added と lost・何も出ない組のどれにも 20 回以上、N・O・Q も同じ (`checkNumberHits`・`checkNegationSwapHits`・`checkQualifierHits`)
- 本家と揃わなかった所: 無い (全記録が最初の記録で一致した)
- わざと壊して比べた (`bin/mutate-verify-list`。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf6/mut-m・mut-f):
  - 群 M の 1 回目 (bd056ab37 の木): 48 本のうち 26 本が red・19 本が緑のまま・3 本が構文エラー。緑のうち 5 本 (`m-ichiji-start`・`m-jippun-maniau`・`m-whole-period-year`・`m-kana-strict`・`m-weak-repeated`) は入力を足して (d054d5ba7) red にした (4581c661f の木で確かめた)。構文エラー 3 本は当て直して、`m-phrase-previous2` と `m-q-keep-before2` は緑のまま、`m-q-text-rest2` は再び構文エラー (当て直していない)
  - 群 N: 9 本全部 red。群 O: 4 本全部 red。群 Q: 8 本のうち 4 本 red (`q-reach`・`q-yaku-noun`・`q-place`・`q-bound`)・4 本が緑のまま (`q-version`・`q-strict-count`・`q-subkind`・`q-compound`)
  - 緑のまま通った形は、本家と違う答えを返す入力を作っていない。等価かどうかは未確認 (群 M: `m-follow-8` (長い単位が無く等価と読んだ)・`m-units-fit-range`・`m-cancel-sorted`・`m-moved-ratio`・`m-lead-width`・`m-ctx-words`・`m-weak-hasunit`・`m-sentencefinder`・`m-q-single-betsu`・`m-q-extra-subject`・`m-q-text-ratio-limit`・`m-phrase-previous2`・`m-q-keep-before2`。群 Q: `q-version`・`q-compound`)。ここから外した 5 本 (`m-mask-newline`・`m-cancel-alias`・`m-cluster-kept`・`q-strict-count`・`q-subkind`) は、6 本目の敵対的レビューが本家と違う答えを返す入力を作れた (等価ではなかった)。直した結果は 6 本目の敵対的レビューの節。再提起するなら、その向きだけを変えた変更体で、本家と違う答えを返す入力 (o・r と両方の答え) を示す
- 確かめていないこと:
  - 群 M〜Q の所要 (長い文書・同じ段落の繰り返し) は測っていない。`_quantity_changed` の動的な正規表現は組み直しを覚えている (`dynamicPattern`)
  - 群 M の CLI への配線は群 AC まで未 (`meaning_risks` の組み立ては群 Y)。`ExtractNumbers`・`NumberRisks` ほかは production から呼ばれる入口 (exported) として残した
  - 単独のサロゲートと不正な UTF-8 は記録に入れていない
  - 群 Q の `_QUALIFIER_LABELS` (報告の文面用) は群 AD で足す
- 最終の検査 (4581c661f の木。rc は全部 0): `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate`。この docs の commit の後の確認は報告に書く

### 6 本目の敵対的レビュー (opus。diff-wf6 の 8e0a9e1bb..440a3da08。群 M・N・O・Q。直したのは diff-wf6 の係)

本家と違う答え (P1) は、壊していない Go では 1 件も作れなかった。約 38,000 組 (数の表記と Markdown を重ねた 27,000・入れ替え 6,000・否定の 2,000 字の境 1,086・概数と範囲の語 8,000) と前の係の 15,000 組を本家と比べ、食い違いは既知の 4,300 桁の ValueError だけだった。指摘は、本家 (python) の答えと壊した形の答えを並べて再現できたものだけ直した。直すときは、再現した入力を群ごとの手書きの組 (`oracle/diff{qualifier,negation,number}_cases.go`) に足して記録を作り直した (13 case の書き足しだけ。既存の case の削除・変更 0 を python で照合。71,277 → 71,290 case・312,293 → 312,340 呼び出し。群ごとの case 数は number 9,564・negation 9,523・swap 8,602・qualifier 9,519)。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P2-1 `q-strict-count` (`qualifier.go` の `len(olds) == 1 && len(news) == 1` → `>= 1`) | 直した。`最大3日近く　以上。` → `最大3日超　以上。` を `strict-count-wide-space` に足した | 記録が red (`qualifier/hand/strict-count-wide-space`) |
| P2-2 `q-subkind` (subkind の条件から strict を外す) | 直した。`期間は約3日から5日以上。` → `期間は最大3日から5日超。` を `subkind-strict-to-range` に足した | 記録が red |
| P2-3 `x-q-keys-rev` (keys を逆の順に積む) | 直した。`3日程度〜ほぼ3〜5日` → `3日超え〜少なくとも3〜5日` を `keys-order` に足した | 記録が red |
| P2-4 `x-q-zip-old` (組にする数を 1 つに絞る) | 直した。`3〜5日以下、3〜5日以下` → `3〜5日まで、およそ3〜5日以下` を `same-value-twice` に足した | 記録が red |
| P2-5 `x-n-align-inner` (`negation.go` の残りの上限の `>` → `>=`) | 直した。前後の同じ字を外した残りがちょうど 2,000 字の組を `long-rest-exact-new`・`long-rest-exact-old` (残りが後の側・元の側) と、1 字多い `long-rest-one-over` に足した | 記録が red (exact の 2 本) |
| P2-6 `x-n-align-ge` (文の字数の上限の `>` → `>=`) | 直した。片側がちょうど 2,000 字で、`>=` にすると autojunk が効かず対応が変わる組 2 本 (`long-side-exact-a`・`long-side-exact-b`。レビューの fuzz が見つけた 1,997 字 → 2,000 字の組を、繰り返しの単位と挿入に分けて Go の式で作り直した) を足した。README に、2,000 字の分かれ目 2 つの組が否定にも入ったと書いた | 記録が red |
| P2-7 `m-cancel-alias` (`_cancel_notation` が何も打ち消せないときの戻り値を clone に) | 直した。`` `八０いつつⅢ,． `` → `` `八０つつⅢ`,． `` を足した。本節の 1 本目の「緑のまま」の一覧から外した (この形で値が変わることは群 M の節に自分で書いていた) | 記録が red (`number/hand/cancel-alias-backtick`) |
| P2-8 `m-cluster-kept` (`kept[k] \|\| cancelled[k] > 0` から後ろを外す) | 直した。時刻の組と、コードの囲みが挟まる組 (`3〜5日。> 3日3s` → ``3〜```\n3日\n```5日。> 3日3 日3s``) を足した | 記録が red (2 本) |
| P2-9 `m-mask-newline` (`numbers.go` の伏せる範囲の改行を残す `chars[i] != '\n'` → `true`) | 直した。行頭の `1.` が箇条書きの番号と読まれる組 (前の係の fuzz が作った組) を足した | 記録が red |
| P3-1 「4,300 桁を超える数字は ValueError」が狭い | 直した (記録には入れない)。python で確かめた: `1`×4,298 + `千円` は ValueError・`1`×4,297 + `千円` は通る。算用数字だけの並びは 4,301 桁でも通る (`1`×4,301 + `円`)。漢数字だけの並び (`二`×4,301 + `日`) は止まる。止まる所は、位取りの掛け算や漢数字の変換で int の結果を 10 進の文字にする所。食い違いは `extract_numbers`・`_number_risks` に加えて `_qualifier_risks` にも出る。Go は止まらず数を返す。本家と同じ ValueError を Go が投げるか、入力の上限として扱うかは決めていない (実際の文書で 4,298 桁の数字は出ない。再提起するなら、実在する入力でこの食い違いが出た例を添える) | 未解決 (記録に無いと明記) |
| P3-2 受け入れ条件の状況の行が群の一覧を A B C E J K L のまま | 直した。時点の記録だと書き、正はチェックボックスと各本目の節とした | 直した |

壊した形の結果 (`bin/mutate-verify-list`。commit した木 (53a39b003) で最後に回し、本数はこの run から数えた。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf6/fix/mutlogs): 上の 9 本に、群 N の `n-place-gap-lt` (変わった箇所をまとめる間隔の比べを `<=` → `<`)・`n-limit-const` (`charAlignLimit` 2000 → 1999) を足した 11 本が、全部 rc 0 (想定どおり red)。群ごとの本数は M が 3・N が 4・Q が 4。足した case が落ちたことを各ログの case 名で照合した (`n-place-gap-lt` は足した case ではなく、既存の別の case が落とした)。

壊せなかったところ (レビューの報告から): 記録の件数 (71,277 case・312,293 呼び出し。群ごとの case 数) は記録のファイルから数えて issue・README と合った。群 M・N・O・Q の届き方の検査は `TestBaseMatchesPythonRecording` の中から呼ばれ、このテストは `NORACE_CHECKS` に入っていて空振りしない。`x-n-limit`・`x-o-honor-reach`・`x-o-particle-reach`・`x-o-passive`・`x-q-reach`・`x-q-yaku-start`・`x-q-chikaku`・`x-q-splitdec` は記録が red で、本番の関数を通っている。共有の部品の変更の後も、コピーの `go test ./...` は rc 0。`x-q-reach-ge` は等価の見込み。次の 16 本は fuzz 52,002 組でも記録でも緑のままで、本家と違う入力を作れなかった: `x-q-reach-ge`・`q-version`・`q-compound`・`m-follow-8`・`m-units-fit-range`・`m-cancel-sorted`・`m-moved-ratio`・`m-lead-width`・`m-ctx-words`・`m-weak-hasunit`・`m-sentencefinder`・`m-q-single-betsu`・`m-q-extra-subject`・`m-q-text-ratio-limit`・`m-phrase-previous2`・`m-q-keep-before2` (`m-q-text-rest3` は記録で red)。

未確認リスク: 単独のサロゲートと不正な UTF-8 は試していない。所要は測っていない (fuzz の 1 万 5 千組で Go は 4〜12 秒)。int の桁数の上限で止まる形が、まだ写していない P 以降の群にもある見込みだが、確かめていない。

### 7 本目 (diff-wf7。2026-11-14。worktree で作業し、push していない)

- 順は P → T。群 S (否定対比の逆転)・U (確度) と、その後ろの V W X Y・AB・AC・AD は手を付けていない。S は本家 6346〜6874 行 (正規表現 34・関数 20 あまり。逆転の判定が 3 つの語の表と十数個の分岐でできている)、U は 7960〜8354 行 (正規表現 50) で、どちらも半端な群を commit しない方針の中で書き切って、本家と全件比べ、壊した形を当てる量に届かなかったので、ここで止めた。T (本家 6877〜6933 行) は語の一覧 (群 R) と文の組 (群 L) だけに依る小さい群なので、S の前に通した。
  次の係は S から。S が使う `negativeZu` は群 M が写してあり、`negationWords` は群 M・N にある。S の見出しの行を数える masker は `newMaskerWith(side, skip)` で作れる (`_CONTRAST_SKIP_KINDS` は本文の数えない種類から見出しと表を外したもの)
- commit:
  - 1ec6dbb6d feat(ja-writing): diff の問い・変えない箇所・印の候補 (群 P) を作り、本家の記録と突き合わせる
  - f2fb21a8b test(ja-writing): 群 P の記録に、最後の行だけに変えない箇所がある省いた置き換えの塊の組を足す
  - f1dde9d4e feat(ja-writing): diff の気持ちの語 (群 T: _feeling_risks) を作り、本家の記録と突き合わせる
  - fd27e5ea5 test(ja-writing): 群 T の記録に、行の継ぎ目の後読みと run の 2 つ目の hunk の組を足す
  - この節を足す docs の commit
  - 1ec6dbb6d と f1dde9d4e は、commit の前の木で `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-dupscan-gate` を通した。1ec6dbb6d は最初の木で dupscan gate (テスト用の入力を作る closure の写し 5 組と helper の写し 1 件) と lint の staticcheck (QF1012) が落ちたので、入力の書式を表にして `diffTemplate` に寄せ、直してから commit した。f1dde9d4e は staticcheck (QF1001) を直してから通した。🚨 f2fb21a8b と fd27e5ea5 は記録の case を足しただけの変更で、単独では `make lint test`・unused・dupscan gate を回していない (回したのは `TestBaseMatchesPythonRecording` と、その commit の木での `mutate-verify`)。`make test-lint` は最後の木だけ (下の「最終の検査 (7 本目)」)
- 群 P (`diff/question.go`・`diff/protected.go`): `_is_question` `_only_final_period_removed` `_question_risks` (問い。台詞の最後の「。」だけを外した組は出さない)・`_protected_risks` `_protected_spans` `_protected_contents` `_protected_hits` `_is_quote_row` (変えない箇所。文字の対応のある塊・省いた置き換えの塊・追加と削除の塊の 3 通り)・`_find_line` `_marks_risks` `_modality_risks` `_logic_risks` (印・許可・能力・選ぶ範囲と因果の変化を意味が変わる恐れの形にする)。
  本家の `_protected_risks` は groups を使わず全文差分の hunk だけで決まる。`_marks_risks` などは `type_changes` を受けるので、Go の入口 `ChangeRisksOf(orig, rw)` が本家の `diff()` と同じ順 (`_type_changes` → `_modality_changes` → `_logic_changes`。範囲づくりは 1 回) で作って渡す。`sides` に `normalizedLines` (行ごとに正規化した文。`_find_line` が引く) を足した。群 F〜I の試験が持っていた `kindHitsValue` と別の形の変換 (`kindHitsList`) を production に置き、試験の `typeLostValue` がそれを通る形にした
- 群 T (`diff/feeling.go`): `_feeling_risks`。語の家族は群 R の `LoadList("feelings")` を読み、原文を調べるのは推敲後の文に当たった家族だけ。3 つ目の欄 (原文の言い方)・原文では表を数え推敲後は数えない違い・コード・先頭の設定・HTML の行を数えない範囲・原文の行をつなぐ字 (改行) を写した。語の家族の正規表現を 1 つにまとめた前置きの検査 (`anyOf`) も写した
- 記録: `diff-base.json` は 71,290 → 105,525 case・312,340 → 481,116 呼び出し (13.7MB → 17.4MB)。足した呼び出し (関数ごと): `_question_risks` 8,602・`_is_question` 93・`_only_final_period_removed` 16・`_protected_risks` 8,445・`_protected_spans` 16,890・`_find_line` 100・`_marks_risks` `_modality_risks` `_logic_risks` 各 8,203・`_feeling_risks` 8,981・群 P の定数 6 本 (`_QUESTION` `_NOT_QUESTION_KA` `_FINAL_PERIOD_IN_QUOTE` `_SENTENCE_END` `_PROTECTED_SPANS` `_PROTECTED_LINES`)。
  組は群 F〜I と同じ入れ物 (`diffBags`) に、各群専用の手書きの組と固定の種 (PCG 1076/20 問い 900・21 変えない箇所 700・22 印の候補 500・23 気持ちの語 900) を足したもの。問いの語彙 (形容動詞の語幹と「ほか」の除外・閉じ括弧・台詞の最後の「。」)・変えない箇所の種類ごと (コード・インラインのコード・URL・リンク先・先頭の設定・HTML・引用・画像のパス・リンクの定義)・文字の対応を省く大きさの塊 (合計の上限を超える・2 つ目の塊が残りに入らない・最後の行だけに変えない箇所がある)・語の家族ごとの文 (気持ちの語 178 文と、先読み・後読みで外す形)・原文の言い方の欄を持つ家族の組を手書きで入れた。
  既存の case は消えず、既存の case の digest も変わらない (記録の作り直しの前後を python で照合し、missing 0・changed 0。足した case を直すたびに作り直した)。作り直しは約 120 秒。`diff-cli.json` は変えていない
  - 届き方を数える検査を足した (`checkQuestionHits`・`checkFeelingHits`): 問いが出る組と出ない組・変えない箇所の subkind 7 つ (code・inline_code・url・link・frontmatter・html・quote) と出ない組・印・許可 (消えた・増えた)・能力が消えた・選ぶ範囲 (一つ → 全部・全部 → 一つ)・因果と、それぞれ出ない組・気持ちの語が出る組と出ない組のどれにも 20 回以上。最初の記録で先頭の設定が 15 回だったので、固定の種の組の 5 分の 1 に先頭の設定を足した
- 本家と揃わなかった所: 無い (群 P・T の全記録が最初の記録で一致した。直した所も無い)
- 4,300 桁を超える数字で本家が ValueError で止まる件 (6 本目の敵対的レビュー P3-1): 群 P・T には数字を int にする処理も int の桁の上限に当たる所も無い (`_find_line` は文字列の位置だけ、`_protected_*` は範囲の位置だけ)。記録にも入れていない。群 AC で CLI に配線するときの扱いは変わらず未決
- わざと壊して比べた (`bin/mutate-verify-list`。commit した木で回した。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf7/mutlog-p・mutlog-p2・mutlog-t・mutlog-t2。本数は各 run の表から数えた):
  - 群 P: 20 本のうち 17 本が最初に red・3 本が緑のまま。緑の 1 本 (`qp-high`: 範囲を読む上限を最後の行の終わりまでにしない) は、最後の行だけに変えない箇所がある塊の組 2 本を足して (f2fb21a8b) red にした (その commit の木で当て直して rc 0)。最終は 20 本のうち 18 本が red・2 本が緑
    - red: 台詞の最後の「。」を外した組の両向き・形容動詞の語幹の一覧・文末の rstrip・問いの片方だけの判定・`_find_line` の 20 字・文の終わりでの切り・modality_review の判定・logic の old_hits・マークの側の取り違え・正規化した行・画像のパスの始まり・リンクの定義の末尾の改行・二分探索の始まり (`bisect_right - 1`)・同じ subkind の最初の行・1 行が 1 行に変わる塊の相手の行・hunk の中の文字の位置の進め方・省いた置き換えの塊の中身の比べ・上の `qp-high`
    - 緑のまま (2): `qp-quote-break` (引用の行の `break` を外す。同じ subkind の 2 回目以降は `hit` が行番号を上書きしないので、`break` は速くするだけで等価と読んだ。実測はしていない)・`qp-sort-kind` (範囲を並べる最後の鍵 `kind` を逆にする。始まりも終わりも同じ範囲が 2 つの種類で出る入力を作れなかった。13 通りの書き方で `protectedSpansOf` を調べ、どれも 1 つの種類だけだった)。再提起するなら、始まりと終わりが同じで種類が違う範囲が出る文書と、本家・壊した形の答えを示す
  - 群 T: 11 本のうち 7 本が red (最終の木で。最初は 5 本が red・6 本が緑のまま)・4 本が緑のまま。`qt-join` (原文の行を空でつなぐ。前の行の終わりの「売上が」が次の行の「落ち込」の後読みに掛かる) と `qt-group-index` (risk の hunk の番号を group の最初の hunk にする) は、組 3 本を足して (fd27e5ea5) red にした。red のもの: 3 つ目の欄・推敲後の文で表を数えない・原文で数えない行の種類・原文の伏せ・risk の元の行番号・上の 2 本
    - 緑のまま (4): `qt-old-texts` (原文と同じ文を飛ばす判定を外す。同じ文は原文にあるので結果が変わらないと読んだが、誤り。7 本目の敵対的レビューで直した。下の節)・`qt-words-dedupe` (同じ当たりの文字列を 2 回数えない判定を外す。今の一覧の 51 家族は同じ文字列に当たらない。気持ちの語の 178 文で確かめた (python)。一覧を差し替えると食い違いうる)・`qt-pair-first` (同じ後の文に組が 2 つあるときの先・後の取り方。そういう組を作れていない)・`qt-new-masker-skip-frontmatter` (推敲後の masker の数えない種類のうち、コード・先頭の設定・HTML は、そもそも文を作らない行なので、表だけが効く)。いずれも本家と違う答えを返す入力を作っていない。等価かどうかは未確認
- 確かめていないこと:
  - 群 S U V W X Y・AB・AC・AD は未着手 (上)。`diff()` の組み立て (群 AC) と CLI への配線までは、検出の群は production から `FeelingRisks` などの入口でしか呼ばれない
  - 群 P・T の所要 (長い文書・同じ段落の繰り返し・変えない箇所が数万個ある文書) は測っていない。`_protected_hits` は範囲を二分探索で引くので、hunk ごとの費用は範囲の数に依らないが、実測はしていない。`go test -race ./diff` は手元で 82 秒 (最も重い `TestRequestMatchesPythonRecording` が 27 秒) で、CI の上限 600 秒の半分に遠い。`TestBaseMatchesPythonRecording` は前の係のとおり race なしで回す
  - `ChangeRisksOf` が `o == r` のとき型の変化の入力を空にする (本家の `diff()` と同じ) 点は、記録の本家 (いつも `_ExactSequenceMatcher` の opcodes を渡す) との突き合わせで同じになることを確かめた (同じ文書の組が記録に入っている)。ただし `meaning_risks()` の `finally` で `_LAST_TYPE_RANGES` を空にする所は、群 AC まで未確認
  - 単独のサロゲートと不正な UTF-8 は記録に入れていない (Go の `encoding/json` が置き換えるので渡せない)
  - `_find_line` の「見つからない」(None) は記録に入っている (`find-line/` の case)。ただし行が複数の hunk にまたがる組での振る舞いは、`_marks_risks` の組 (印が文の途中で変わる組) 経由でしか見ていない

#### 最終の検査 (7 本目)

最後のコードの編集 (fd27e5ea5) の後の木 (README・epic・この issue の docs の変更を含む) で、`make -C src/ja-writing lint test` (lint 0 issues・`go test -race ./...` 全部 ok)・repo の root で `make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` を回し、rc は全部 0
(ログは tmp/yomiyasu-gap-2026-11-12/diff-wf7/fin-lt.log・fin-un.log・fin-tl.log・fin-dup.log と同じ名前の .rc)。この節を足す docs の commit の後に `make test-lint` をもう一度回した (rc は fin2-tl.rc)。

### 7 本目の敵対的レビュー (opus。diff-wf7 の 07c0b2ae9..a02adb095。群 P・T。直したのは diff-wf7 の修正の係)

壊していない HEAD は、約 48,800 組 (Markdown の包み・問いの文末・印・許可・選ぶ範囲・気持ちの語・CRLF・BOM・ゼロ幅の字・行をまたぐ伏せ・文字の対応を省く塊) の fuzz で本家と食い違わず、panic も無かった。記録の件数 (105,525 case・481,116 呼び出し) は記録のファイルから数えて合った。
指摘は 4 本とも、壊した形と入力を自分で再現できた (本家の答えは python で確かめた。fuzz の食い違いは t1 86 件・g2 1 件・g2 88 件・b1 131 件・q1 1 件)。入力を手書きの組に足し、記録を作り直した (書き足しだけ: 105,525 → 105,529 case・481,116 → 481,131 呼び出し。既存の case は消えず、既存の digest も変わらない。python で照合し missing 0・changed 0)。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P1 / P2-1 `qt-old-texts` の等価の理由が誤り。原文を調べる側は原文を伏せてから探すので、原文の同じ文が行をまたぐインラインのコードの中にあり、推敲後では伏せられていないと、家族は「原文にない」になる (入力: 原文 `a \`a` / `嬉しかった。\``、推敲後 `a \`a` / `嬉しかった。`) | 直した。`feeling/hand/same-sentence-masked-in-original` を足した。7 本目の節の「同じ文は原文にあるので結果が変わらない」は誤りだったので書き直した | `qt-old-texts` が red。落ちた case は足した case |
| P2-2 `a-default-quote-first` (`subkind == "" &&` を外す。コードの囲みの中の `> >` の行を quote にしてしまう) | 直した。`protected/hand/code-fence-holds-quote-row` を足した | `qp-default-quote-first` が red。落ちた case は足した case |
| P2-3 `a-quote-hit-first-only` (引用の行を塊の最初の行でしか数えない。記録の `too-large-quote-lines` は引用が 1 行目にしかなかった) | 直した。引用が 5 行目にある `protected/budget/too-large-quote-fifth-line` を足した | `qp-quote-hit-first-only` が red。落ちた case は足した case |
| P2-4 `a-contents-high-min` (`min(sp.right, high)` の切りを外す。インラインのコードが等しい行をまたいで 2 つ目の hunk まで伸びる) | 直した。`protected/budget/too-large-code-spans-equal-line` を足した | `qp-contents-high-min` が red。落ちた case は足した case |
| P3 `qt-new-masker-skip-frontmatter` の等価の理由の依存先が書かれていない | 採った。等価の根拠は、群 L の `_sentence_pairs` (Go は diff/sentences.go) が code・先頭の設定 (frontmatter)・HTML の行を文の組に入れないことに依る。その不変条件を守る検査は群 P・T の側に無く、群 L の記録の側にある。群 L の振る舞いを変えるなら、この壊した形の等価を測り直す | 等価のまま (fuzz と手の入力で破れず) |

壊した形の結果 (`bin/mutate-verify-list`。commit した木 (24605315d) で最後に回し、本数はこの run から数えた。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf7/fix/mutlog): 7 本が全部 rc 0 (想定どおり red)。群 P が 4 本 (`qp-high`・`qp-default-quote-first`・`qp-quote-hit-first-only`・`qp-contents-high-min`)、群 T が 3 本 (`qt-join`・`qt-old-texts`・`qt-group-index`)。足した case が落ちたことを、4 本の新しい壊した形のログの case 名で照合した。7 本目の節の緑のまま (4) から `qt-old-texts` を外した (残りは `qt-words-dedupe`・`qt-pair-first`・`qt-new-masker-skip-frontmatter` の 3 本と、群 P の `qp-quote-break`・`qp-sort-kind`)。

壊せなかったところ (レビューの報告から): `qt-words-dedupe`・`qt-pair-first`・`qt-new-masker-skip-frontmatter`・`qp-sort-kind`・`qp-quote-break` は fuzz で食い違い 0 (`qp-sort-kind` は始まりと終わりが同じで種類が違う範囲を 19 通りの書き方で探して出なかった)。レビューが当てた壊した形のうち a-final-plus・a-qend-paren・a-ofp-rstrip・a-findline-cut・a-findline-empty・a-findline-strip・a-block-last・a-hits-ge・a-f-oldrows の 9 本は記録で red。a-opcodes-always・a-one-to-one-tag・a-f-combined・a-f-newskip-code・a-f-oldnew-skip は記録でも fuzz でも緑で、等価と読まれた。等価かどうか未確認なのは a-image-gt (画像のパスが空のときの `left >= right` → `>`)・a-contents-ge (`sp.right > low` → `>=`) の 2 本で、p1 の組でも食い違い 0。
再提起に要る実測: 上の等価・未確認の壊した形について、本家と違う答えを返す入力 (o・r と、本家・壊した形の答え) を python と Go の両方で示す。

未確認リスク: 単独のサロゲートと不正な UTF-8 は試していない。`meaning_risks()` の `_LAST_TYPE_RANGES` の後始末は群 AC で確かめるまで未確認。群 S 以降に要る写し残しは、群 S を見ていないので確かめていない。
所要: `go test -race -json ./diff` は手元で 82 秒 (最も重いのは `TestRequestMatchesPythonRecording` 27 秒・`TestCLIMatchesPythonRecording` 26 秒)。6 本目の後の CI の race の run (165 秒) は、CI の上限 600 秒の半分 (300 秒) に届かないので、`NORACE_CHECKS` は増やしていない。`TestBaseMatchesPythonRecording` は前のとおり race なしで回し、手元で 26 秒。足した case は 4 つだけで、所要はほぼ変わらない。

### 8 本目 (diff-wf8。2026-11-14。worktree で作業し、push していない)

- 順は S → U。V W X Y・AB・AC・AD は手を付けていない。本家の該当の範囲の一覧は、群 S が関数 16 (うち `_negative_zu` は群 M で写し済み)・`_lazy_compile` 34、群 U が関数 14・`_lazy_compile` 11 と表 43 行。全部写した (S は関数 15・定数 44、U は関数 14・定数 19)。写さなかったのは、この範囲にあるが後の群だけが使う `_CERTAINTY_LEVELS` (群 AD の `report`)・`_DIALOGUE`・`_QUOTE_CLOSE`・`_QUOTE_DROP` (群 W)。
- commit:
  - e81c960da feat(ja-writing): diff の否定対比の逆転 (群 S: _contrast_risks) を作り、本家の記録と突き合わせる
  - d293bb810 test(ja-writing): 群 S の記録に、緑のまま通した壊した形 (打ち消しの判定・下げる形・肯定した側の語・原文の肯定) を落とす入力を足す
  - 47afbb0f6 feat(ja-writing): diff の確度 (群 U: _certainty_risks・_classify_hedge_marks) を作り、本家の記録と突き合わせる
  - 39776c82d test(ja-writing): 群 U の記録に、緑のまま通した壊した形 (推量の節の字数・否定の位置・決まりの言い方・分けた文の語の使い回し) を落とす入力を足す
  - この節と README・epic の docs の commit と、`_hedge_polarity` を例外ありの関数の一覧に足した commit (下)
  - 🚨 単独では検査が通らない commit: 39776c82d は `go test -race ./diff` の `TestBaseRecordingCoversEveryFunction` が落ちる (`_hedge_polarity` の例外を一覧に足し忘れた。次の commit が直す)。e81c960da・47afbb0f6 は、commit の前の木で `make -C src/ja-writing lint`・`make test-unused-excluding-tests`・`make test-dupscan-gate` を通し、`go test ./diff ./oracle` も通した (e81c960da は最初の木で dupscan gate が重複 3 件で落ちたので、既にある helper を使う形に直してから commit)。d293bb810 は lint・dupscan gate と `go test ./diff` までで、`make test` の全体は回していない
- 群 S (`diff/contrast.go`・`diff/contrast_words.go`): 否定対比の主張の逆転 (`_contrast_risks`)。原文の「Aではなく B」(では/じゃ/でも + なく・ない・なかっ・ありません、でなく、は使わず・はせず) から名詞を取り (`_negated_tokens`。送り仮名つきの語・「高い品質」の「品質」・主題の「は」・「だけでなく」を外す・反語と理由の節を外す)、推敲後の文で、重みの語 (目的・最優先・まず など 23 語) と同じ節に否定なしで出るか (`_affirmed_with_weight`。比べて下げる形 `_demoted`・否定のある節 `_neg_after`・仮説・逆接を外す)、B と並べて肯定されるか (`_affirmed_plain`) を見る。原文でもともと肯定の A (`_original_affirms`)・B の側の「Aの〜」の句 (`_b_side_phrases`・`_head_clue`)・見出しの行 (`_CONTRAST_SKIP_KINDS` は本文の数えない種類から見出しと表を外したもの。Go は `contrastSkipKinds`、masker に `kind` を足した) を写した。入口は `ContrastRisks(orig, rw)`
- 群 U (`diff/certainty.go`・`diff/certainty_words.go`・`diff/hedge.go`): 確度。表 43 行 (強さ 1 可能性・2 見込み・推量・3 予定・5 確実。前の語と重なる位置は後の語に数えない) の語を取り出し (`_certainty_word_spans`)、文の組ごとに同じ強さを打ち消して (同じ名前から先に。隣の組にならなかった文の語でも打ち消し、使った語は取り除く)、強さが動いた段を出す (`_certainty_change`・`_certainty_risks`。元の文から分けた文の強さ 5 の語も比べる。文書の種類が `COUNTED_TYPES` のときは「場合がある」「ことがある」の言い切りと強さ 5 の足しも数える)。推量の分類 `_classify_hedge_marks` (same・strengthen・weaken・negation・condition・question など) は群 Y が呼ぶので公開の関数 `ClassifyHedgeMarks` にした。入口は `CertaintyRisks(orig, rw, docType)`。`_certainty_risks` が読む「文の種類の印」の恐れは、`marksReview` (旧の行・旧の文・lost と added の種類) に写した
  - 同じ語を「同じものか」で見る所 (`word is b`・`pool.remove(word)`) は、語をポインタで持ち、除くのは値が等しい最初の語にした (Python の `list.remove` と同じ)。隣の文の語のリストは組をまたいで共有され、使った語が減っていく (`pools` を群ごとに持つ)
- 記録: `diff-base.json` は 105,529 → 127,597 case・481,131 → 730,487 呼び出し (17.4MB → 21.0MB。8 本目の敵対的レビューの直しの前は 21,000,285 B)。群 S は 10,198 case (`contrast/`・`_contrast_risks` 10,170 と下の関数 14 本の計 約 72,000 呼び出し・定数 44)、群 U は 11,870 case (`certainty/`・`_certainty_risks` 24,328 と下の関数 12 本の計 約 69,800 呼び出し・定数 19。8 本目の敵対的レビュー P3-3 で数え直した値。覚えるだけの呼び出しを除く)。
  - 群 S の組は、手書き 90 組あまり (本家のコメントと docstring の例を 1 つずつ)・固定の種 PCG 1076/24 の 2,400 組 (原文の書式 44 と推敲後の書式 154 を名詞 48 語で組み合わせる)・入力の集まりと手で作った入力の全組。下の関数は、書式から作った文 1,500 に名詞の組を当てた呼び出しと、手書きの文の組
  - 群 U の組は、手書き 50 組あまり・確度の語の表の文 380 あまりを `遅れます。` と入れ替える組 (3 通り)・固定の種 PCG 1076/26 の 2,600 組 (隣の文を前後に置く)・PCG 1076/28 の強さの違う語を 1 つの文に持たせた 400 組・入力の集まりと手で作った入力の全組。文書の種類は 4 通り (指定なし・article・incident・mail)。下の関数は、表の文とその組み合わせから作った 2,175 文の組
  - 既存の case は消えず、digest も変わらない (cd7616d4a の記録と python で照合: 消えた case 0・変わった case 0・増えた case 22,068。29 件という書き方は、この系列の途中の commit で足した case の変化のことで、cd7616d4a にあった case の変化ではなかった)。作り直しは約 140〜165 秒
  - 届き方を数える検査を足した (`checkContrastHits`・`checkCertaintyHits`): 段は、逆転が出る組と出ない組・確度の候補の 2 つの kind と強まる・弱まる・混ざる・文書の種類で数える (`counted_by`) と出ない組。下の関数は、見つかる・見つからないのどちらにも 20 回以上 (S は 26 項目・U は 17 項目)。最初の記録で `shown_word` の送り仮名を足す側・`continues_word` の真・U の `mixed` が足りなかったので、手書きの組と名詞をつなげた呼び出しを足した (確度は文の組ごとに比べるので、混ざる組は 1 つの文に 2 つの語を持たせる)
- 本家と揃わなかった所: 無い。群 S・U とも、最初の記録の突き合わせで食い違い 0 だった (最初に出た食い違いは、記録の呼び出しの引数が `None` になる、私の入力の誤りで、本家と Go が同じ例外を返していた)
- 4,300 桁を超える数字で本家が ValueError で止まる件 (6 本目の敵対的レビュー P3-1): 群 S・U の範囲に `int(` も数字を数にする処理も無い (python で該当の行を grep して 0 件)。記録にも入れていない。群 AC で CLI に配線するときの扱いは変わらず未決
- わざと壊して比べた (`bin/mutate-verify-list`。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf8/mut-s/log・log2 と mut-u/log・log2。本数は各 run の表から数えた。壊した形の一覧は mut-s/muts.json と mut-u/muts.json にある):
  - 群 S: 47 本。最初の run (e81c960da の木) は 25 本が red・15 本が緑・7 本が構文エラー (使わなくなった変数)。緑の 15 本に、関数の単位の呼び出し (`_neg_after` の並べた語の「の」・`_demoted` の置き換え先と「減らす」・`_affirmed_with_weight` の逆接・前の節の空白・重なる語・`_side_affirmed` の「にも」・置き換え元の窓・退ける言い方・`_original_affirms` の重なる当たりと否定の印の前の節・`_head_clue`) を足し (d293bb810)、構文エラーの 7 本は壊し方を直して、その 22 本を d293bb810 の木で当て直した: 20 本が red・2 本が緑のまま。最終は 47 本のうち 45 本が red
    - 緑のまま 2 本 (どちらも等価と読んだ。実測はしていない): `sc-risk-markstart` (`originalAffirms` に渡す否定の印の始まりを、終わりにする。節の切れ目が否定の印の中に入らないので、`clauseBounds` の位置が同じ節に入ると読んだ。stage で否定の印の直前が節の切れ目なら名詞が取れないので、この形は stage から届かない) と `sc-risk-targets` (`newForOld[i]` が空のリストになることは無い。追加するときは必ず 1 件入る)。再提起するなら、本家と壊した形の答えが違う `_contrast_risks` の入力を示す
    - red の主なもの: 否定の語の外・節の始め・名詞の長さと字の種類・反語の位置・名詞の取り出し・送り仮名の語・重みの語の前の主題の句・仮説・見出しの行・見出しと表の数えない種類・原文の肯定・組の相手・同じ組の重複を出さない判定・B の句を伏せる処理・示す語の送り仮名
  - 群 U: 37 本。最初の run (47afbb0f6 の木) は 28 本が red・6 本が緑・3 本が構文エラー。緑の 6 本に、`_hedge_polarity` (語が無いと添字の IndexError で止まる形も)・200 字より前の字を持つ `_hedge_clause`・語のすぐあとに否定が続く文・原文の隣の文に決まりの言い方がある組・同じ文を 2 つの組が分けた先とみなす組を足し (39776c82d)、構文エラーの 3 本は壊し方を直して、その 9 本を 39776c82d の木で当て直した: 7 本が red・2 本が緑のまま。最終は 37 本のうち 35 本が red
    - 緑のまま 2 本 (等価かどうか未確認): `u-marks-line` (`sameReviewSentence` の旧の行が 0 (行が見つからない) のときの除外を外す。旧の行が見つからない印の変化を作れなかった) と `u-hedge-hits-overlap` (推量の語の範囲と確度の語の重なりの比べの `<` → `<=`。語の終わりちょうどから推量の語が始まる組を作れなかった)。再提起するなら、本家と壊した形の答えが違う入力を示す
    - 注意: 最初の run の red は、その後の commit の木では当て直していない (足したのは入力だけで、既存の case は変わらないので、red のままのはず。実測はしていない)
- 確かめていないこと:
  - 群 V W X Y・AB・AC・AD は未着手。`diff()` の組み立て (群 AC) と CLI への配線までは、`ContrastRisks`・`CertaintyRisks`・`ClassifyHedgeMarks` は試験からしか呼ばれない
  - 群 S・U の所要 (長い文書・同じ段落の繰り返し) は測っていない。群 U の `certaintyWordSpans` は文ごとに表の 43 本の正規表現を当てるので、同じ段落を繰り返す文書では遅い恐れがある (本家の前置きの検査は表に無いので写していない)
  - 単独のサロゲートと不正な UTF-8 は記録に入れていない
  - `_certainty_risks` に渡す `masks` を本家が `None` 以外で呼ぶ経路 (群 Y が作る masker) は、群 Y まで未確認
  - 本家が `_certainty_risks` に渡す `risks` は、文の種類の印の恐れだけでなく全種類の恐れの並びだが、確度が読むのは (kind, subkind) が (sentence_type, marks) のものだけなので、Go は印の恐れだけを受ける。群 Y で本家と同じ並びを渡す形に直す必要は無いと読んだが、配線のときに突き合わせる
  - 群 U の `_hedge_polarity` の記録は、語が無い文では本家が IndexError で止まる形を含む (`DiffRaisingFunctions`)。製品の本家の呼び出しではなく、記録の手順の引数の組み立てで起きる

#### 最終の検査 (8 本目)

最後のコードの編集の後の木で、`make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` を回した。rc は全部 0 (8 本目の敵対的レビューの直しの後の木 3dcfa62f6 + README の件数。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf8/w8/fin.rc)。
所要: `go test -race -json ./diff` は手元で 84 秒 (最も重いのは `TestRequestMatchesPythonRecording` 27 秒・`TestCLIMatchesPythonRecording` 26 秒)。CI の上限 600 秒の半分に遠いので、`NORACE_CHECKS` は増やしていない。`TestBaseMatchesPythonRecording` は前のとおり race なしで回す。

### 8 本目の敵対的レビュー (opus。diff-wf8 の cd7616d4a..9603cffa7。群 S・U。直したのは diff-wf8 の修正の係)

壊していない HEAD は、本家を正解にした fuzz (否定対比 23,000 組・確度 36,000 組・推量の分類の 4 通り・本家の docstring とコメントの例 24,064 組・Markdown・全角・サロゲートペア・結合文字・URL・CRLF・BOM・ゼロ幅の字を差し込んだ 6,517 組) で食い違い 0 だった (P1 は 0 件)。推量の分類 14 種のすべてに届いた。
指摘のうち P2 の 3 本と P3 の 4 本は、発火する入力を自分で再現できた (本家の答えは python で確かめた)。入力を記録に足し、作り直した (書き足しだけ: 127,597 → 127,603 case・730,487 → 734,178 呼び出し。cd7616d4a の記録に対して消えた case 0・変わった case 0。python で照合し missing 0・changed 0)。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P2-1 `c-oldtexts-text` (`oldTexts[newBody.raw]` → `.text`。入力: 原文 `既存施設だけでなく、既存施設のツールではない https://e.x/a\r\n`、推敲後は同じ文の後ろに `前の文です。` を足したもの) | 直した。`contrast/hand/masked-same-sentence-crlf` を足した | `c8-oldtexts-text` が red。落ちた case は足した case |
| P2-2 `c-paired-wordonly` (組の相手の打ち消しの `false` → `true`。入力は表の行・リスト・番号つきの行を混ぜた 4 行) | 直した。`contrast/hand/paired-other-affirms-across-clause` を足した | `c8-paired-wordonly` が red。落ちた case は足した case |
| P2-3 `h-stop-fullcomma` (`hedgePolarity` の区切りの字から全角の「，」を外す。例: `確認しない，遅れるでしょう。` の否定の数が変わる) | 直した。全角の読点・半角の読点・「、」で節を切った 6 組を `certainty/strings/comma` として足した | `h8-stop-fullcomma` が red。落ちた case は足した case。`h8-stop-asciicomma`・`h8-stop-none` も red |
| P3-1 例外の 222 件は記録の手順が参照 `[0]` を解くときの IndexError で、製品の例外ではない。参照の path を読まない | 採った。テストが path を解く形に直し、`DiffRaisingFunctions` の説明と commit の言い方を直した (製品の `hedgePolarity` は検査していない)。語の無い文を記録から外す案は、既存の digest が変わるので採らなかった | f0e937fe6 |
| P3-2 `_certainty_risks` の記録の marks・masks を読まない | 採った。覚えた marks 以外・masks が None 以外なら落とす。今の記録は masks が常に None なので、このガードを外す壊した形は緑のまま通る (将来の群 Y のためのガード。壊した形での確認はできない) | f0e937fe6 |
| P3-3 issue の数の食い違い | 採った。群 U の呼び出しは約 69,800、記録の大きさは 17.4MB → 21.0MB、「changed 29 case」は cd7616d4a に対して変化 0・増えた case と書き直した (上の 8 本目の節) | 数え直した値は記録から取った |
| P3-4 性能 | 採らない (起票の対象)。測った値は下の表 | 群 F の `TypeChanges` の性質で、群 AC で `diff()` を組むときに再測する |
| P3-5 推量の分類の `unanchored` は 2 回・`mixed` は 7 回しか届かない | 採った。`unanchored`・`mixed`・`consolidated`・`not_core`・`unpaired`・`lost`・`added` に本家が返す文の組を 22 組ずつ固定し (`certainty/strings/hedge-classes`)、`checkCertaintyHits` の 20 回以上の一覧に足した | 直した後は `unanchored` 34・`mixed` 29・`consolidated` 46・`not_core` 43・`unpaired` 1,402・`lost` 55・`added` 52 |
| 実装の係が緑のまま残した `u-hedge-hits-overlap`・`u-marks-line`・`sc-risk-markstart`・`sc-risk-targets` | 壊せなかった (下) | 等価と読んだまま |

直したあとで足りなかった 1 本: 私が足した `c8-paired-raw` (組の相手の打ち消しを、伏せた後の文でなく原文で引く) が、最初の記録では緑のまま残った。本家を同じ形に書き換えたものと比べて答えが変わる入力を python で探し (2,349 組)、件数が違う 2 組を `contrast/hand/paired-other-code-holds-weight`・`-b` として足した (3dcfa62f6)。原文のインラインのコードの中に重みの語がある組。

壊した形の結果 (`bin/mutate-verify-list`。commit した木 (3dcfa62f6) で最後に回し、本数はこの run から数えた。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf8/w8/mutlog2。一覧は w8/muts.list): 10 本が全部 rc 0 (想定どおり red)。
- 群 S: `c8-oldtexts-text`・`c8-paired-wordonly`・`c8-paired-gt` (`>= 0` → `> 0`)・`c8-paired-raw`。最初の 2 本と最後の 1 本は、足した case 自身が落ちたことをログの case 名で照合した (`c8-paired-gt` は既存の fuzz の case で落ちた)。
- 群 U: `h8-stop-fullcomma`・`h8-stop-asciicomma`・`h8-stop-none`・`h8-unanchored-disjoint` (`disjoint` の反転)・`h8-notcore`・`h8-mixed` (`> 1` → `> 2`)。`h8-stop-fullcomma` は足した `certainty/strings/comma` が落ちた。

壊せなかったところ (レビューの報告から。再提起に要る実測は、本家と壊した形の答えが違う入力を python と Go の両方で示すこと):
- `sc-risk-markstart`: 否定の印の中に節の切れ目の字は無く、否定の印のすぐ前が切れ目のときは `_negated_tokens` が名詞を取らない。等価と読み、レビューの fuzz (c2・x1) でも食い違い 0
- `sc-risk-targets`: `newForOld` の値は append した後にしか入らない。等価
- `u-hedge-hits-overlap`: 文の断片を 2 つつないだ文 1 万あまりで、推量の語が確度の語の終わりちょうどから始まる文は 0 件だった。壊せなかった
- `u-marks-line`: 旧の行が None になる印の変化は fuzz に 5 件あったが、答えが変わる入力は 5 件の中にも手で組んだ 8 組・約 1.5 万通りの組み合わせにも無かった。到達できないことは示していない
- レビューが足した 36 本のうち、記録も fuzz も緑だった 15 本 (多くは等価と読んだ。`c-oldfornew-first` は、推敲後の 1 文が原文の 2 文と組になる入力を `_sentence_pairs` が出すかを確かめていない)

測った性能 (レビューの値。1 種類の文書で 1 回ずつ。n 文を全部書き換えた文書。単位は秒): `ContrastRisks` は n=100・300・600 で Go 0.15・1.40・5.69、本家 0.10・0.77・3.13 (どちらも n の 2 乗で、Go は約 1.8 倍)。`CertaintyRisks` は Go 0.028・0.36・2.20 で、うち群 F の `TypeChanges` が 0.022・0.33・2.31 (本家の `_type_changes` は 0.011・0.046・0.153。n=600 で 15 倍、n が 2 倍になると 7 倍)。確度の本体だけは本家より速い。
所要: `go test -race -json -count=1 ./diff` は手元で 79 秒 (最も重いのは `TestRequestMatchesPythonRecording` 27 秒・`TestCLIMatchesPythonRecording` 26 秒)。CI の上限 600 秒の半分に遠いので `NORACE_CHECKS` は増やしていない。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf8/w8/race.json。

未確認リスク: `u-marks-line` が到達できるか・単独のサロゲートと不正な UTF-8 (Go の JSON が U+FFFD に置き換えるので fuzz の道具で比べられない)・群 Y が `_certainty_risks` に masks と全種類の risks を渡す経路・`oldForNew` の上書きの向き (`c-oldfornew-first`)。

### 9 本目 (diff-wf9。2026-11-14。worktree で作業し、push していない)

- 順は X → W のつもりだったが、記録が 1 つの入力の集まりと中間の値 (`_build_full_diff`・`_sentence_pairs`・`_marks_risks`・`_certainty_risks`・`_number_risks`) を共有するので、**コードと記録を 1 つの commit にした** (記録を群ごとに作り直すと、片方の commit では記録の半分を Go が持たず `TestBaseMatchesPythonRecording` が落ちる)。群 V・Y・AB・AC・AD は手を付けていない。本家の該当の範囲の一覧は tmp/yomiyasu-gap-2026-11-12/diff-wf9/inventory.md にあった (群 X は関数 13・正規表現 9・関数の中の `re.sub` 5・`re.findall` 11、群 W は関数 11・定数 3)。`NEW_REVIEW_SECTIONS`・`_NEW_SECTION_NAMES` は群 AD の `report` だけが使うので写していない
- commit:
  - bbf7669cc feat(ja-writing): diff の慎重表現・動かしてはいけない値・敬語 (群 X) とかぎかっこ・文体の切り替え (群 W) を作り、本家の記録と突き合わせる
  - 970c468c7 test(ja-writing): 群 X・W の記録に、緑のまま通った壊した形を落とす入力 (統計の語の大小・窓の端の文) を足す
  - 0fde9f2f9 test(ja-writing): 引用の数字の変化の記録に、古い側の順だけが合う数値の恐れの呼び出しを足す
  - この節と README・epic の docs の commit
  - 🚨 単独で検査が通らない commit は無い: bbf7669cc は `make -C src/ja-writing lint test`・`make test-unused-excluding-tests`・`make test-dupscan-gate` を通した (最初の木は lint の staticcheck 1・unused 1 と dupscan gate の重複 5 で落ち、直してから commit)。970c468c7・0fde9f2f9 は入力と記録だけの変更で、`go test ./diff` の記録の突き合わせだけ回した (`make test` の全体は最後の木で回した)
- 群 X (`diff/hedges.go`・`diff/immutable.go`・`diff/politeness.go`): 慎重な言い方 (`hedges`。語の出現総数が減った文。分割先と移動を数える。文の種類の印で推量が減った文・確度の恐れが出た文と重ねない)・動かしてはいけない値 (`immutableValues`。引用・宛名・メール・科学の単位・統計の値・符号。値を組・同じ塊・文書全体の順に打ち消す `uncancelledTokens`・数値の恐れと重ねない `citationNumberCovered`)・敬語の度合い (`politeness`。mail だけ)。入口は `HedgesSection`・`ImmutableValues`・`PolitenessSection`
  - 本家の `sides.quiet_marks` (`_meaning_risks` が置く) は群 Y が作るので、`hedges` は「印の恐れ (出力から外した分も含む)」と「確度の恐れ」を別の引数で受ける。記録の段は `quiet_marks` が空のまま `_hedges` を呼んだ値と比べる
  - `_uncancelled_tokens` の `places is None` の枝 (`_immutable_values` が `groups` なしで呼ばれる形) は、本家のどの呼び出しも通らないので写していない
- 群 W (`diff/quote.go`・`diff/register.go`): かぎかっこの中の言葉の書き換え (`quoteRisks`。入れ子・文の句点をまたぐ会話・伏せる範囲・窓の端の文)・文体の切り替え (`registerChanges`。敬体の割合の候補が出ていれば数えない)。入口は `QuoteRisks`・`RegisterChanges`。`_DIALOGUE`・`_QUOTE_CLOSE`・`_QUOTE_DROP` (8 本目が写さなかった定数) もここで写した
- 記録: `diff-base.json` は 127,603 → 146,414 case・734,178 → 1,297,026 呼び出し (21.0MB → 27.1MB)。群 X・W は 18,811 case・562,848 呼び出し (`review/`)。
  - 組: 入力の集まりと diff の入力の組・手書き 128 組 (群 X 65・かぎかっこ 38・文体 25。本家のコメントと docstring の例・分かれ目の窓の端 (5・6・7 文)・統計の語の大小)・固定の種 PCG 1076/30 (慎重な言い方 1,800 組)・31 (値 2,800 組)・32 (敬語 1,800 組)・33 (かぎかっこ 2,400 組)・34 (文体 2,200 組。切り替えない文を切り替え先の文体にする組を半分)。下の関数は値の表 95 項目・かぎかっこの文・文体の文・`_citation_number_covered` の手書きの呼び出し 28 本
  - 記録の道具に `args` の `{"$concat": [...]}` と `{"$column": 値, "n": 添字}` を足した (使わなければ前と同じ出力)。`_hedges` の `risks` は印の恐れと確度の恐れをつないで渡し、`_immutable_values` の `risks` は `_number_risks` の戻り値の (番号, 順, risk) から risk を取る
  - 既存の case は消えず、digest も変わらない (bbf7669cc の前の記録と python で照合: 消えた case 0・変わった case 0)。0fde9f2f9 だけは、自分が足した `review/covered` の呼び出しの並びが変わり、その case の digest だけが変わる
  - 届き方を数える検査 (`checkReviewHits`): 慎重な言い方の候補あり・なし・値の 6 種類 (引用・宛名・メール・科学の単位・統計・符号) の取り出しと候補・引用などの恐れの 3 つの subkind × 2 種類・敬語の 2 つの subkind と対象外・かぎかっこの changed・lost・会話の行・文体の to_polite・to_plain・unified・switched・politeness による見送り・候補なし が、どれも 20 回以上 (`covered` だけ手書きなので 5 回以上)。最初の記録で `unified` が 2 回・`covered` が 8・19 回しか届かなかったので、入力を足した (2 回目は 17 回なので割合を半分にして 20 回以上にした)
- 本家と揃わなかった所: 無い。最初の記録の突き合わせで食い違いは 1 件だけで、`_ADDRESSEE` の定数の flags が、本家は先頭の `(?m)` を MULTILINE (8) に数えて 40 を返し、Go の `lazyValue` が 32 を返した検査側の誤りだった (Go の値は直した)
- 4,300 桁を超える数字で本家が ValueError で止まる件 (6 本目の敵対的レビュー P3-1): 群 X・W の範囲 (9143〜9367・9480〜9819 行) に `int(` が無い (awk と grep で 0 件)。数字は `re.findall("[0-9]+")` で文字列のまま比べる。記録にも入れていない。群 AC で CLI に配線するときの扱いは変わらず未決
- わざと壊して比べた (`bin/mutate-verify-list`。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf9/mut/log-x・log-w・log-redo・log-redo2、一覧は同じ dir の muts-x.json・muts-w.json):
  - 群 X: 24 本。bbf7669cc の木で、19 本が red・3 本が緑 (統計の語の大小・文の位置の端・打ち消す数の 0 の項)・2 本が構文エラー (使わなくなった変数)。緑のうち 1 本 (`x-imm-statistic-sign`) に、`p`→`P`・`r`→`t` の組を足し (970c468c7)、構文エラー 2 本は壊し方を直して、その 3 本を当て直した: `x-hedge-cert-strengthened`・`x-imm-statistic-sign` は 970c468c7 の木で red、`x-imm-covered-order` は緑のままだったので古い側の順だけが合う恐れの呼び出しを足し (0fde9f2f9)、0fde9f2f9 の木で red。最終は 24 本のうち 22 本が red
    - 緑のまま 2 本 (どちらも等価と読んだ。実測はしていない): `x-imm-sentence-at` (`sentenceAt` の `position < end` → `<=`。位置が文の範囲の終わりちょうどから始まる値は、文の間の空白だけの所から始まる値のことで、取り出す正規表現のどれも空白から始めない (宛名の `[ \t]*` は文の中)。届かないと読んだ) と `x-imm-removed-count` (増えた数字を数える表に 0 の項を足す。0 の項は `positiveRemainder` が見ないので、答えが同じ)。再提起するなら、本家と壊した形の答えが違う `_immutable_values` の入力を示す
  - 群 W: 22 本。bbf7669cc の木で、18 本が red・3 本が緑 (窓の幅 2 本・伏せる範囲の端)・1 本が構文エラー。窓の端の文 (5・6・7 文) を足して (970c468c7)、構文エラーの 1 本は壊し方を直して、その 4 本を当て直した: 窓の 2 本と `w-reg-kudasai` が red。最終は 22 本のうち 21 本が red
    - 緑のまま 1 本 (等価と読んだ。実測はしていない): `w-quote-blank` (`blankMasked` の `right <= start` → `<`。範囲の終わりが文の始まりちょうどなら、`for k := max(left, start); k < min(right, end)` が空で、文字列は変わらず返る)
  - 合計 46 本のうち 43 本が red・3 本が緑のまま (等価と読んだ)。最初の run の red は、その後の commit の木では当て直していない (足したのは入力だけで既存の case は変わらないので red のままのはず。実測はしていない)
- 確かめていないこと:
  - 群 V Y・AB・AC・AD は未着手。`HedgesSection` ほかの入口は試験からしか呼ばれない (群 AC で `diff()` の組み立てに配線する)。`meaning_risks` に引用の候補を足す並べ替えと、`_meaning_risks` が `_hedges` に渡す全種類の恐れの並び (本家は印の恐れ・確度の恐れ以外も渡すが、`_hedges` は 2 種類しか読まない) は群 Y・AC で突き合わせる
  - 群 X・W の所要 (長い文書・同じ段落の繰り返し・1 行に文が数千ある文書) は測っていない。`valueTokens` は種類ごと・側ごとに文書全体へ正規表現を当てるので、1 つの文書に 12 回走査する。本家の `_sentence_at` の二分探索は写した
  - `valueTokens` は `analysis["lines"][line - 1]` の添字が範囲の外になる場合 (本家は IndexError で止まる) を、種類の判定を飛ばす形にしている。行の数が行の解析と食い違う入力は記録に無く、届くかどうかも未確認
  - 単独のサロゲートと不正な UTF-8 は記録に入れていない
  - `quiet_marks` が空でない (`_meaning_risks` が置いた後の) `_hedges` の呼び出しは、群 Y まで未確認
- 所要: `go test -race -json ./diff` は手元で 88 秒 (最も重いのは `TestRequestMatchesPythonRecording` 28 秒・`TestCLIMatchesPythonRecording` 27 秒)。CI の上限 600 秒の半分に遠いので `NORACE_CHECKS` は増やしていない。`TestBaseMatchesPythonRecording` は前のとおり race なしで回し、手元で 39 秒 (8 本目は 38 秒。記録が 15% 増えても伸びは小さい)

#### 最終の検査 (9 本目)

最後のコードの編集 (0fde9f2f9。以降は docs だけ) の後の木 (3865c1ba3) で、`make -C src/ja-writing lint test`・repo の root で `make test-unused-excluding-tests`・`make test-lint`・`make test-dupscan-gate` を回し、rc は全部 0 (ログは tmp/yomiyasu-gap-2026-11-12/diff-wf9/fin-lt.log・fin-un.log・fin-tl.log・fin-dup.log と fin.rc)。この節の commit は docs だけの差分なので回し直していない。

### 9 本目の敵対的レビュー (opus。diff-wf9 の 43f85c700..b86a474c8。群 X・W。直したのは diff-wf9 の修正の係)

壊していない HEAD は、本家を正解にした比べ (5 つの入口を文書の種類ごとに 22,000 組余り。慎重表現・引用・値・宛名・メール・敬語・文末・かぎかっこ・会話の行・Markdown の印・CRLF・見えにくい字) で食い違い 0・panic 0 だった (P1 は 0 件)。
指摘のうち P2 の 2 本は、本家に同じ入れ替えを当てて答えが変わる入力を自分で再現できた。入力を記録に足して作り直した (書き足しだけ。43f85c700 に対して消えた case 0・変わった case 0。146,414 → 146,425 case・1,297,026 → 1,297,165 呼び出し)。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P2-1 `register.go` の窓の中央の式 (`i*len(news)/max(1, len(olds))` の分子と分母の入れ替え) が緑 | 直した。本家で答えが変わる組 5 を `review/register-hand/window-middle-count-1`〜`-5` として足した (230961110) | `r-middle-swap` が red |
| P2-2 `quote.go` の窓の中央の式 (同じ入れ替え) が緑 | 直した。組 3 を `review/quote-hand/window-middle-count-1`〜`-3` として足した (230961110) | `q-middle-swap` が red |
| P3-1 handback の「最初の run で緑 5 本」は 4 本 | issue の本数 (19/3/2・18/3/1・最終 43/46) は実物と合うので issue は直さない | 数えた値は 9 本目の節のとおり |
| P3-2 `valueTokens` の行の添字が範囲の外になる分岐は 507 文書で届かない | 採らない (分岐は残す)。届かないことの証明ではなく、本家は IndexError で止まるので、止めずに判定を飛ばす形を残す。理由は `diff/immutable.go` のコメント (0e572668d) | 再提起には、本家の `len(analyses[side]["lines"])` が `len(lines)+1` から外れる入力を示すこと |

直したあとで足りなかった 1 本: 窓の上の端 (`min(high, middle+3)` → `middle+2`) の `q-high` が、最初の組では緑のまま残った (レビューが当てた 45 本の外で、私が足したわざと壊した形)。かぎかっこの似た先が窓の上の端の文にある入力を python で探し (23 組)、3 組を `review/quote-hand/window-middle-top-1`〜`-3` として足した (f6e31790f。書き足しだけ: 増えた case 3・呼び出し 43)。

壊した形の結果 (`bin/mutate-verify-list`。commit した木 (f6e31790f) で最後に回し、本数はこの run から数えた。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf9/fix/log。一覧は同じ dir の muts.list): 10 本のうち 8 本が rc 0 (想定の `TestBaseMatchesPythonRecording` で red)、2 本が緑のまま。
- red: `r-middle-swap`・`q-middle-swap` (分子と分母の入れ替え)・`r-low`・`q-low` (`middle-2` → `middle-1`)・`r-high`・`q-high` (`middle+3` → `middle+2`)・`r-gt`・`q-gt` (`high-low > 5` → `> 6`)
- 緑のまま 2 本 (等価と読んだ): `r-min`・`q-min` (`min(len(news)-1, …)` の外し)。`i` は 0 から `len(olds)` (かぎかっこは `oldCount`) 未満なので `i*len(news)/len(olds)` は `len(news)` 未満で、`min` は効かない。再提起するなら、`i >= len(olds)` になる入力か、本家と壊した形の答えが違う入力を示すこと

レビューが壊せなかったところ (報告から): 本家との比べ 22,000 組余り (比べが空振りしていないことは期待値を書き換えた canary で確かめた)。レビュー自身の壊した形 45 本のうち 40 本が red、緑 5 本のうち 2 本が上の P2、3 本は等価 (`r-strip`: masker の body は空白だけの文を返さない・`i-addr-strip`: 宛名の値は前後に空白を含まない・`i-places-overwrite`: 2 つの組が同じ行を持つ例が 0 件)。記録の件数は 3 つの時点を case の名前で突き合わせて issue の数と合った。

所要: `go test -race -json -count=1 ./diff` は手元で 81 秒 (最も重いのは `TestRequestMatchesPythonRecording` 27 秒・`TestCLIMatchesPythonRecording` 26 秒)。入力を足した分の伸びは無く、CI の上限 600 秒の半分に遠いので `NORACE_CHECKS` は増やしていない。ログは tmp/yomiyasu-gap-2026-11-12/diff-wf9/fix/race.json。

未確認リスク (レビューの報告から):
- 群 AC で配線するとき、本家の `_meaning_risks` が `_hedges` に渡す確度の恐れは (1) 全部の恐れと引用の候補を見たうえで作った `_certainty_risks`、(2) 状態の候補に写した certainty_review を取り除いた後のもの。`HedgesSection` は `certaintyRisks(groups, marks, nil, docType)` をそのまま渡し、記録も同じ形で比べているので緑。群 AC で本番と揃え、その形の記録を足す
- 長い文書の所要・単独のサロゲート・不正な UTF-8 は試していない

#### 最終の検査 (9 本目の敵対的レビューの後)

最後のコードと記録の編集 (f6e31790f) の後の木で、`make -C src/ja-writing lint test`・repo の root で `make test-unused-excluding-tests`・`make test-dupscan-gate` の rc は全部 0 (ログは tmp/yomiyasu-gap-2026-11-12/diff-wf9/fix/f-lt.log・f-un.log・f-dup.log と .rc)。この節と README・epic の件数の commit は docs だけの差分で、`make test-lint` を回した結果は報告に書く。
