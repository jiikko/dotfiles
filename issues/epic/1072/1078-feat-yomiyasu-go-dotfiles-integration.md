# 1078 (feat): yomiyasu の hook を Go で作り直し、ja-textlint・ja-textlint-on-edit・ja-writing に組み込む

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-12〜）

起票日: 2026-11-12

## 概要

本家の `yomiyasu_hook.py` (944 行) を `ja-writing hook` として作り直し、1075〜1077 の Go の実装を dotfiles の文章の検査の入口に組み込む。
ユーザー決定 (2026-11-12): textlint に新しい規則は足さず、Go の実装に寄せる。

## 本家の hook が何をするか (v3.1.0)

- `--host claude-code|codex` でホストのイベントの JSON を標準入力から読む。`--print-config` は設定の例を出すだけで、設定ファイルを書き換えない
- 対象は `yomiyasu_check.py start` で登録した原稿だけ
  - 返答の終わり (Stop) に、最終稿が検査記録と合うかを確かめ、合わなければ作業を続けさせる
  - 登録した原稿に関係する投稿で、本文が記録と照合できないときは止める
  - lint は動かさない。登録していない原稿には何もしない
- イベントごとの時間の上限 (`_BUDGETS`: UserPromptSubmit 4 秒・PreToolUse 7 秒・Stop 20 秒など) と、止める回数の上限 (2) を持つ

dotfiles の `_claude/hooks/ja-textlint-on-edit.sh` (Markdown を書いた直後に、書いた行の textlint の指摘を知らせる) とは役割が別で、置き換えにはならない。

## 今の ja-textlint の利用者と契約 (変える前に全部を並べ、表をこの issue に書き足す)

- 呼び出し元: `tests/bin/test_ja_textlint.sh`・`tests/claude/test_ja_textlint_on_edit.sh`・`_claude/hooks/ja-textlint-on-edit.sh`・
  `_claude/skills/ja-writing/SKILL.md`・`mac/README.md`・`_claude/CLAUDE.md` の skill の表・どの repo からも PATH で呼ぶ人と Claude
- 契約
  - 残りの引数は textlint にそのまま渡り、`--fix` も使える
  - rc は textlint のまま (0 / 1 / 2) で、依存が無いときは 3
  - `ja-ai-vocabulary` の warning だけなら rc 0 (テストが契約にしている)
- hook の時間の上限: textlint を `bin/runtimeout 10` で止め、hook の全体は 15 秒。指摘の有無の判定に使えない終わり方 (rc 2 / 3・時間切れ) のときは、何も知らせずに通す

## 対応方針

1. **`ja-writing hook`**: 本家と同じ入出力で作り直す (入力の集まりは、イベントの JSON と状態フォルダの組)。
   dotfiles の `_claude/settings.json` に登録するかは、登録した原稿が無いときの所要 (全イベントで起動する) を測ってからユーザーに聞く。
   登録しないと決まった場合は、その決定と理由をこの issue に書いて、この項目を閉じる
2. **`bin/ja-textlint`**: textlint の後に `ja-writing lint` を同じファイル (と `--stdin`) に掛け、両方の指摘を出す
   - textlint だけに渡す引数 (`--fix` など) の扱いを決める (`ja-writing lint` は受けない)
   - rc の合成の表を作る。今の契約 (warning と info だけなら rc 0) を保つのを基本にする。片方が rc 2 / 3 やビルドの失敗で終わっても、もう片方の指摘は捨てない
   - 決めた表に合わせて、上の呼び出し元のテストと文書を同じ変更で直す
3. **`ja-textlint-on-edit.sh`**: 書いた行に当たった `ja-writing lint` の指摘も知らせる
   - 知らせる重さ (error・warn・info) と規則は、dotfiles の文書に掛けた当たりを抜き取りで読んでから決める (retro 1064 の D: 当たりを読まずに広げない)
   - 知らせない規則は dotfiles の側 (`--json` の出力の jq の絞り込み) で外す。`ja-writing lint` に規則を外す引数を足すと本家の答えと揃わなくなるので、足さない
   - Go のビルドを hook の時間に入れない。hook からは `go_autobuild_exec --no-build` で起こし、ビルドが無ければ知らせずに通す (npm の `JA_TEXTLINT_NO_INSTALL` と同じ扱い)。
     `--async` は古いバイナリの結果を今の結果と取り違えるので使わない
   - 時間の上限の中に収まるかを、ビルド済みで cache が冷えた状態と温まった状態の両方で測る
4. **語の一覧**: dotfiles で定義した用語 (正本・ゲート・台帳・入口・印。ユーザー決定 2026-11-11) を外す一覧を dotfiles に置く
   - どの repo から `ja-textlint` を呼んでも同じ用語が外れるように、`ja-writing lint` に `--words` で dotfiles の一覧を渡す。repo が自分の `.yomiyasu-words/` を持つときにどちらを使うかを決める
     (本家の `--words` はフォルダを 1 つだけ受ける)
   - 当たりを読んで、ほかに定義した用語があれば足す
5. **dotfiles の textlint の規則の整理**
<!-- textlint-disable ja-ai-vocabulary -->
   - `ja-ai-vocabulary.js`: Go の実装の規則 (`metaphor_verb` など) が同じ語を同じ箇所で拾うことを入力で確かめた語から外す
   - dotfiles だけの語 (既定値・既定で・既定の・変異・ソフトウェアの「版」。ユーザー指示) は、本家の `flag.txt` / `flag-patterns.txt` で表せるかを確かめる。
     `flag-patterns.txt` は先読み・後読みを受けるが、上限の無い `*` `+` と `\p{...}` を断り、Python の後読みは固定幅だけを受ける。今の「版」の除外がこの範囲に収まらなければ `ja-ai-vocabulary.js` に残す
   - `ja-bold-not-rendered.js`: 本家の `bold_not_rendered` (error) から着想した規則で、on-edit の hook が知らせる 2 規則のもう一方。
     Go の実装の `bold_not_rendered` が同じ箇所を拾うことを確かめてから外し、hook の jq の絞り込みも直す
<!-- textlint-enable ja-ai-vocabulary -->
6. **ja-writing (`_claude/skills/ja-writing/SKILL.md`)**
   - 規範の文を足すことと、76 項目のうちコードでない規範の行き先を決めることは、[1080](done/1080-docs-ja-writing-yomiyasu-norms.md) に切り出した (dotfiles-24 が担当。2026-11-13)。
     この issue に残るのは「ja-textlint の中身」の節の書き直しと、`ja-writing diff` を使う手順の文
   - 1080 からの申し送り (dotfiles-24、2026-11-13): 1080 は規範 8 (否定対比)・9 (擬人化)・10 (前置き・定型の結び・言い直しの締め) を新しく足し、
     規範 3 に直訳語の型、規範 7 に含みと慣用句を残す話を足した。「ja-textlint の中身」の「見ないもの」の行は規範 8〜10 を挙げていないので、書き直すときに
     `ja-writing lint` が拾う範囲と合わせて直す。hook (`ja-textlint-on-edit.sh`) が引く「規範 7」の番号は変わっていない
   - 否定対比・擬人化・前置きと定型の結び・直訳語の型・直す前後の比較 (`ja-writing diff`) の規範の文を足す
   - 「ja-textlint の中身」の節を書き直す
   - SKILL.md を直したら、観点を変えた整合の確認を 3 回と `bin/skill-eval` を回す
   - 2026-11-12 の照合の要点 76 項目のうち、Go の lint / diff でも ja-writing の文でも扱わないもの (本家の SKILL.md の推敲の手順・読み手の段階・事実を補う手順など、
     コードでない規範) の行き先を、項目ごとに「ja-writing に足す / 足さない (理由)」で決めて書く

## ユーザーに聞くこと (着手のときに)

- `ja-writing hook` を dotfiles の settings に登録するか (所要の数字を添えて)
- 装飾の記号の規則 (`dash_decoration` などの 3 本。warn なので `--strict` の判定にも入る): dotfiles の issues と `_claude` の Markdown 606 本が「—」を使っている
  (2026-11-12 に `git grep -l` で数えた)。書いた行だけを知らせる hook でも、Claude が規範の文書に「—」を書くたびに知らせが出る。知らせるか、外すか

## 受け入れ条件

- [ ] `ja-writing hook` が入力の集まりで本家と同じ出力と rc を返す
- [ ] `ja-textlint` が textlint と `ja-writing lint` の両方の指摘を出す。rc の合成の表のとおりに、`tests/bin/test_ja_textlint.sh` が両方を見る
- [ ] `ja-textlint-on-edit.sh` が書いた行の `ja-writing lint` の指摘を知らせる。ビルドが無いとき・時間切れのときに知らせずに通ることもテストで見る。測った所要の数字を書く
- [ ] dotfiles で定義した用語 5 つを、`ja-writing lint` の全規則 (組み込みの一覧を含む) に通して当たりが 0 件になる。dotfiles の外の repo から呼んでも 0 件になる
- [ ] ja-writing の規範と「ja-textlint の中身」が新しい形に合っている
- [ ] CI の Tests の各ジョブで、`test_ja_textlint.sh` が `ja-writing lint` を飛ばして緑になっていない (Go の要るジョブで回っていることを、ログの行で確かめる)

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_hook.py`
- `bin/ja-textlint`・`_claude/hooks/ja-textlint-on-edit.sh`・`_claude/skills/ja-writing/`・`tests/bin/test_ja_textlint.sh`・`tests/claude/test_ja_textlint_on_edit.sh`
- 1075 (lint)・1076 (diff)・1077 (check)

## 進捗

- 方針の 2 (`bin/ja-textlint` に `ja-writing lint` を足す) を入れた (2026-11-20)。
  - 実測 (stdout / stderr / rc を分けて取った): `ja-writing lint` はファイルを 1 つしか受けず、作業フォルダの外と点で始まる名前のファイルを rc 2 で断る。
    そこで `ja-textlint` はファイルごとに標準入力 (`-`) で渡し、出力の前に `ja-writing lint: <名前>` の行を出す。`--fail-on error` を付けると、error の候補 (`bold_not_rendered`) で rc 1、warn と info だけなら rc 0。
    `go_autobuild_exec` はビルドの失敗を rc 1、`--no-build` でビルドが無いときを rc 2 で返し、lint の rc と重なる。そこで先に `lint --version` で起こせるかを確かめ、lint の後も検査の見出しの行が出たかで「走った」を見分ける
  - rc の合成 (rc は textlint の rc と ja-writing lint の rc の大きい方)

    | textlint | ja-writing lint | rc | テスト (`tests/bin/test_ja_textlint.sh`) |
    |---|---|---|---|
    | 0 / 1 (warning だけなら 0) | 0 (warn・info だけ) | textlint のまま | warn だけなら rc 0 で両方の指摘が出る |
    | 0 / 1 | 1 (error の候補) | 1 | ja-writing lint だけが error を出したときも rc 1・複数のファイル |
    | 2 (引数や設定の誤り) | 走らせる | 2 以上 | textlint が rc 2 でも ja-writing lint の指摘を出す |
    | 3 (依存が無い・入れられない) | 走らせる | 3 | textlint の依存が無くても ja-writing lint の指摘を出す |
    | 走らせる | 3 (ビルドの失敗・`JA_TEXTLINT_NO_INSTALL` でビルドが無いか古い・検査の前に終わった) | 3 | ja-writing を起こせない (build / no-install) |
    | 走らせる | 2 (入力の誤り) | 2 以上 | (なし。引数は ja-textlint が組むので、届くのは入力の読みの誤りだけ) |

  - textlint だけで終える形 (標準エラーに「ja-writing lint は掛けない」の 1 行を出す): `--fix` / `--dry-run` (ja-writing lint は直さないので、直した後にもう一度打つ)・
    `-f` / `--format` が stylish 以外・`-o` (機械が読む出力に別の形の文を混ぜない。on-edit の hook は `-f json` を jq で読むので、今の hook の挙動は変わらない)。
    `-h`・`-v`・`--init`・`--print-config`・`--mcp` は textlint だけで何も出さない。ディレクトリと glob は textlint が展開し、`ja-writing lint` は掛けない (標準エラーに出す)
  - Go が無い環境で飛ばして緑にしない: ビルドできなければ rc 3。`test_ja_textlint.sh` は skip せず、`ja-writing lint: <名前>` の行と規則名 (`metaphor_verb`) が出ることを見る。CI の Tests は 2 つのジョブとも setup-go がある
  - 変異 (mutate-verify-list、ログは本体 checkout の `tmp/1072-par-1120/tl/mv/`): rc の合成から ja-writing lint の rc を落とす・textlint の失敗で抜ける・`--fail-on error` を外す・`--version` の確かめを外す・標準入力に掛けない・`--fix` でも掛ける の 6 本は、名指しのテストが red。
    `-f json` でも掛ける変異は、先にある `-f json` の JSON を読むテスト (34 件) が落ちる (rc 7。巻き添えでなく既存の検査が同じ退行を拾う)。検査の見出しの行で「走った」を見分ける分岐は、確かめの後にビルドが壊れる競合でしか通らず、変異で確かめていない
  - 敵対的レビューの指摘への対応 (2026-11-20): textlint の対象の拡張子 (既定の plugin の text と markdown) だけに ja-writing lint を掛ける・
    値を取るオプション (`-c` など) の値をファイルと読まないことをテストで見る・`--stdin-filename` の値が無くても落ちない・
    ja-writing lint の入力の誤り (UTF-8 で読めない) は rc 2 のまま (表の「2 (入力の誤り)」の行に挙動を合わせた)・掛けなかったファイルの件数を標準エラーの最後に出す
  - レビューの指摘 (対応済み 2026-11-20): `.textlintignore` (cwd) と `--ignore-path` に載ったファイルにも ja-writing lint が掛かっていた。
    `bin/lib/ja_textlint_targets.cjs` が textlint の CLI と同じ関数 (引数の読み・設定の読み込み・`searchFiles`・`separateByAvailability`) で対象を決め、そのファイルだけに掛ける。
    ignore の書式は gitignore でなく glob の ignore (`sub/` は sub/ の下に当たらない。textlint 15.8 で実測) なので写さなかった。textlint を 2 回走らせる形は所要が倍なので採らない。
    所要は 1 ファイルで 0.62 秒 → 0.85 秒 (変更前と交互に 5 回)。textlint の依存が無い (rc 3) ときだけ ignore を見ずに拡張子で選ぶ。変異 4 本は red (ログは本体 checkout の `tmp/1078-ignore-1120/mv/`)
  - 2 周目の敵対的レビューの指摘 (2026-11-20、証拠は本体 checkout の `tmp/1078-rv2-1120/`)。根は bash が textlint の引数を読み直していたことなので、
    引数の読みを `bin/lib/ja_textlint_targets.cjs` (textlint の `options.parse` と `cli.js` の分岐に合わせる) に寄せ、bash の引数のループを消した。
    補助は「どう終えるか (lint / only / quiet)・only の理由・標準入力を読むか・標準入力に掛ける名前・ファイルごとの掛ける / 掛けない」を NUL 区切りで返す
    - P2 `-f=json`・`-o=FILE` で標準出力に ja-writing lint が混ざる: 対応済み。optionator の解釈 (`o.format`・`o.outputFile`) で決める。textlint が引数を読めないときも textlint だけ
    - P3 `--stdin` とファイルを一緒に渡すとファイルにも掛かる: 対応済み。標準入力が空でなければファイルに掛けない (textlint の `cli.js` の execute と同じ。空なら textlint と同じくファイルを読む)
    - P3 `--stdin-filename x.go` でも掛かる: 対応済み。名前の拡張子を `separateByAvailability` で見る (textlint は「Not found available plugin」で rc 1)
    - P3 glob の文字を含む名前 (`[x].md`): 対応済み。`found.items` に `path.resolve(name)` があるときだけ掛ける
    - P3 `--` の後ろの `-x.md`: 対応済み。`options.parse` の `_` をそのまま使う
    - 変異で緑のまま通った 3 つの分岐 (補助の失敗の rc 3・依存が無いときの拡張子の絞り込み・`-o`) にテストを足した
  - textlint の依存が無いとき (補助を起こせない) の扱い: 引数を textlint と同じには読めないので、オプションの無い形 (ファイルだけ) のときだけ既定の plugin の拡張子で選んで掛け、
    `-` で始まる引数が 1 つでもあれば掛けない。どちらも rc は 3 で、表の「3 (依存が無い) | 走らせる | 3」の行はファイルだけの形で保つ。
    補助が失敗したとき (rc 2・出力の欄が足りない) は textlint だけを掛けて rc 3 以上 (対象を決めずに掛けると、textlint が読まないものに掛けて rc を変える)
  - ja-writing lint のビルドの確かめは、掛ける対象があるときだけ行う (対象が 0 件ならビルドの失敗で rc 3 にしない)
  - 変異 (mutate-verify-list、ログは本体 checkout の `tmp/1078-fix2-1120/mv/`): -o の分岐・標準入力の拡張子・glob の名前・`--` の後ろの名前・補助の失敗の rc 3・依存が無いときの拡張子の絞り込みとオプションの扱い・標準入力とファイルの組 (2 本) の 9 本は名指しのテストが red。
    format の判定を外す変異は、先にある `-f json` の JSON を読むテストが `set -e` で全体を止める (rc 7。既存の検査が同じ退行を拾う)
  - 残り: `_claude/skills/ja-writing/SKILL.md` の「ja-textlint の中身」(方針の 6)・hook (方針の 3)・CI のログの行での確かめ (受け入れ条件の最後) は未着手
- 1075 からの申し送り (2026-11-13): 1080 の表で lint の規則を当てた 16 項目を照合した。
  当て方の誤り 4 項目 (R2-02・R2-13・R2-16・R2-20) と、一部だけ拾う 5 項目 (R1-14・R2-24・R2-14・R2-15・R2-23) があった。
  本家の lint も拾わない語なので、Go の答えは本家と同じ。lint で拾う範囲を本家より広げるかは、組み込みの後に決める (照合の表は done/1075 の「1080 の 40 項目の照合」)。
