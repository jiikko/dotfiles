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

- [ ] 鳴らす範囲を決める (既存のファイルと過去の入力で鳴る頻度を数える)
- [ ] hook (Edit・Write。PostToolUse) を足す
- [ ] hook (Bash。PreToolUse) を足す
- [ ] rule の項を直す
- [ ] hook のテスト (settings.json の配線の検査を含む)
- [ ] 入口 (repo の CLAUDE.md・scripts/CLAUDE.md) に載せる
