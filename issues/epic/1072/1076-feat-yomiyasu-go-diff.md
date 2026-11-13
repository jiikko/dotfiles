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

受け入れ条件の状況: 「入力の集まりで本家と同じ出力」は本体 (AC) 待ち・「検出の種類ごとの当たる組と当たらない組」は 1 本目で済み・「difflib が全 API で同じ値」「速い照合の試験」は 1081・「長い文書の所要」は未測定・「群ごとに最低 1 つ壊して落ちる」は A B C E J K L が済み・「`make test-dupscan-gate`」は今の木で通る。

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
