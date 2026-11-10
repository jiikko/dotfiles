# 1058 (feat): ツールの入力に書いた \uXXXX の表記が字そのものに変わったことを hook で知らせる

起票日: 2026-11-10

## 概要

Edit・Write・Bash の入力に書いた「バックスラッシュ + u + 16 進 4 桁」の表記が、字そのもの (\u3000 なら全角空白 U+3000) に変わって届く。
rule (`_claude/rules/verify-execution-not-just-exit-code.md` の「ファイルを書くツールは…」の項) を知っていても、1 セッションで 3 回起きた (retro 1056)。
守るのを人の注意に頼らず、変わったことを hook で知らせる。

## 詳細

### 起きたこと (2026-11-10。retro 1056)

1. Edit で Go の文字列 `" \u3000"` を書いた。保存されたファイルには全角空白の字が入っていた。保存の直後には気づかず、
   変異の置換 (`.{7}` で \u3000 の 7 字を当てる) が「当たっていない」(mutate-verify の rc=4) で止まって初めて分かった
2. Write で retro の下書き (markdown) に `\u3000` と書いた。ファイルには全角空白の字が入っていた (読み直して気づいた)
3. Bash の heredoc (`<<'EOF'` で展開を止めた形) で commit message に `\u3000` と書いた。ファイルには全角空白の字が入っていた

### 実測 (2026-11-10・この環境の Claude Code)

Bash で `printf '%s' 'X\u3000Y' | od -An -tx1` を打つと `58 e3 80 80 59` (X・全角空白・Y) が出た。シェルの単一引用符の中なので、
シェルは \u を解釈しない。つまり Bash に届く前に字に変わっている。バックスラッシュを 2 つ書いた `'X\\u3000Y'` は
`58 5c 5c 75 33 30 30 30 59` で、2 つのまま届いた。

- 変わるのは「バックスラッシュ 1 つ + u + 16 進 4 桁」の形。バックスラッシュが 2 つなら変わらない (2 つのまま届くので、1 つを渡したいときの書き方にはならない)
- 16 進でない `\uXXXX` (X のまま) は変わらない (この issue の本文がそのまま残っている)
- どの段で変わるか (道具の呼び出しの入力をデコードする段か) は確かめていない。Edit・Write・Bash の 3 つで同じ形が起きた
- rule は「ファイルを書くツールは…」と書いているが、Bash の入力でも起きる。rule の範囲の書き方も直す

### 害

- 全角空白・NBSP・ゼロ幅の字は画面でほとんど見えないので、diff を読んでも気づきにくい
- コードの文字列なら挙動は同じでも、表記で探す検査 (grep・変異の置換・lint) が当たらなくなる。markdown なら、書いたつもりの説明が別の字になる
- 書き手は表記を書いたつもりなので、確かめる動機が無い

## 対応方針 (案)

1. PostToolUse (Edit・Write) の hook: 道具の入力 (Write の `content`、Edit の `new_string` のうち `old_string` に無い分) に見えにくい字
   (U+3000・U+00A0・U+200B〜U+200D・U+FEFF など) があれば、「表記のつもりなら字に変わっている。python の chr(0x5c) などで表記を組み直す」と知らせる。
   止めない (本当にその字を書きたいこともある)
   - ファイルの git の差分ではなく入力を見る: 起きた 2 件目は git の外 (ignore された tmp/) の markdown で、差分では取れない
     (`_claude/hooks/comment-provenance-on-edit.sh` は git の root を判定して、外のファイルを見ない)
   - 置き場: `_claude/hooks/` に足し、`_claude/settings.json` の PostToolUse の `Write|Edit` に並べる (今は gofmt-on-edit.sh・comment-provenance-on-edit.sh)
2. Bash は PreToolUse で、コマンドの文字列に同じ字があれば止めずに知らせる (additionalContext。例: `_claude/hooks/suggest-dupscan-for-go-edit.sh`)。
   PostToolUse では、実行が済んで commit message やファイルに字が書かれた後になり、rc≠0 で終わったコマンドは PostToolUse に来ない
3. rule の項を直す: 「ファイルを書くツールは」を「Edit・Write・Bash の入力では」に広げ、バックスラッシュ 1 つの表記は届く前に字に変わること、
   表記を残したいときは python の chr(0x5c) や `\x5c` で組むことを書く。hook ができたら hook が正本で、rule は案内だけにする
4. hook のテスト: 字を含む入力で鳴る・字を含まない入力では鳴らない・Edit で `old_string` に元からある字では鳴らない、を `tests/claude/` の既存の hook のテスト
   (例: `test_comment_provenance_on_edit.sh`) に倣って足す。冒頭で `_claude/settings.json` の matcher の配線を `jq -e` で確かめる形も同じにする
5. 入口に載せる: 同種の hook を並べている repo の `CLAUDE.md` (コメントの経緯の hook の項) と `scripts/CLAUDE.md` に 1 行足す

### 検出しないもの・誤検出 (案の段階の見込み)

- hook が受け取る入力は、字に変わった後のもの。「表記を書いて字に変わった」と「初めから字を書いた」を hook は区別できない。知らせは「字が入っている」までしか言えない
- 見えにくい字の一覧に無い字に変わった形 (u0041 の表記が A になる、など)。普通の字に変わると区別できない
- 誤検出: 意図して書いた字でも鳴る。repo には NBSP・全角空白を意図して書いたテストがあり (`tests/zshrc/av1ify/test_av1ify_clipboard.sh`)、
  日本語の文書・commit message の全角空白も普通に使う。止めないので害は小さいが、毎回鳴ると読み飛ばされる。
  鳴らす範囲 (字の種類・Bash は全角空白を見るか・ファイルの種類) は、既存のファイルと過去の入力で鳴る頻度を数えてから決める

## 関連ファイル

- `_claude/rules/verify-execution-not-just-exit-code.md` (「ファイルを書くツールは…」の項)
- `_claude/settings.json` (PostToolUse)
- `_claude/hooks/comment-provenance-on-edit.sh` (Edit・Write の後に、そのファイルだけを見て知らせる hook の例)
- issue 1056 (retro。起きたことの記録)

## 進捗

- [x] 鳴らす範囲を決める: 過去の transcript (Bash・Write・Edit の入力 42,474 回) を数えた。案の一覧の字を含んだのは 42 回で、字を絞らずに全部鳴らす。
  広げた字 (U+2000〜200A・LRM/RLM・双方向の制御・U+202F・U+205F・U+2061〜2064・U+180E・ハングルの埋め字・CGJ) は 0 回。
  U+FE0F (絵文字の異体字セレクタ) は 423 回あり、絵文字の一部として普通に書くので外した (2026-11-10)。
  42 回の中には、表記のつもりが字に変わった形 (Go の rune の BOM・`strings.TrimRight` の全角空白) が実際に混ざっていた
- [x] hook を足す: `_claude/hooks/warn-invisible-chars-in-tool-input.sh`。案から 1 つ変えた: Edit・Write も PostToolUse でなく PreToolUse にし、
  Bash と同じ 1 本にした (書く前に知らせる方が早く、hook も 1 本で済む。入力を見るので git の外のファイルでも鳴る)
- [x] rule の項を直す (`verify-execution-not-just-exit-code.md` の「道具の入力」。Bash でも起きること・表記の組み方・hook が知らせる範囲と知らせない字)。
  checklist の同じ項は rule を正本として指すだけにした
- [x] hook のテスト: `tests/claude/test_warn_invisible_chars_in_tool_input.sh`。settings.json の配線 (Bash と Write|Edit) を `jq -e` で検査し、
  macOS の `/usr/bin/jq` (1.7.1) と PATH の jq (1.8.2) の両方で全ケースを回す
- [x] 入口: hook の一覧を持つ文書は無く、使う人が読むのは毎セッション読まれる rule の項なので、そこに hook を書いた。
  案の repo の CLAUDE.md・scripts/CLAUDE.md には載せない (どちらもコメントの経緯の検査の文脈で、この hook の入口にならない)
- opus の敵対的レビュー 1 周目で 5 件を直した:
  - jq 1.7.1 の `index` はバイトの位置を返し、和文の行で「該当」が空になる → `split` の最初の部分の字数で位置を取る
  - 一覧に無い見えにくい字 (LRM など) が鳴らず、rule の文面と食い違う → 一覧を広げ、rule を合わせた
  - Edit で、増えた字の行でなく元からある行を出す → old_string に無い行を先に出す
  - 字の無い行が多い入力 (65 万行) で 10 秒を越える → 字ごとに `split` し、出現の行番号を前から数個だけ求める (150 万行で約 3 秒)
  - テストが窓の位置・幅・字の抜けを観測できない → ケースを足した
- opus の敵対的レビュー 2 周目で、1 周目の直し (行を単位に「字が増えた行」を探す) が次の形で壊れた。行の単位をやめ、字ごとに前から 3 か所までの出現の周り
  (同じ行の中の前 40 字・後ろ 39 字) を合わせて 5 か所まで出す形にした:
  - Edit で字を含む行を書き換えた形・元からある字が 195 個以上ある形で、増えた行を当てられない
    → 直さない。Edit の「該当」は元からある字の周りも出る (要約の数は増えた分だけで正しい)。hook の冒頭に限界として書いた
  - 同じ行が 2 回出る・CR が入る → 同じ周りは 1 回だけ出し、CR は落とす
  - 7〜14MB の合成入力で所要が 1 周目の版の 5〜10 倍 (行の集合・行番号の累積) → 行を作らない形で 4.5MB の 1 行に 45 種 × 10 万個が約 3 秒、
    150 万行の末尾に全部の字が約 0.5 秒 (jq 1.7.1 / 1.8.2)
- 変異 (最終の形で 13 本が全部 red): Edit の旧い字を無視・空の文字列の split・Bash の分岐・字の抜け (NBSP / LS)・字ごとの上限 3 → 4・重複の除去・CR の除去・
  前と後ろの行の切り出し・窓の左 40 → 60・右 39 → 59・周りの別の種類の字の印。1 周目の版では配線を外す変異も red

