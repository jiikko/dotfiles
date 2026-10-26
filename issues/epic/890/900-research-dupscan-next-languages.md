# 900 (research): dupscan の次の言語 (Go・zsh) の読み取りを測る

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-26〜）

起票日: 2026-10-24

> **zsh / shell script は対応しない** (ユーザー決定 2026-10-26。理由と、対応するなら要る改修は `docs/dupscan-overview.md` §4 の言語の表)。この issue に残るのは Go だけ。
>
> **再開** (2026-10-26。ユーザー指示: Go を go/ast の adapter で実装する。手順は「対応方針」)。以下は保留していたときの記述: **保留** (2026-10-25)。ユーザー指示 (2026-10-25: epic 890 は Go・zsh 対応を除いて done にする)。**再開の条件**: dupscan を Go の repo でも使いたくなったとき。

## 概要

[891 (要件の正本)](done/891-design-dupscan-requirements.md) の「構想: 対応する言語」節。Swift の次は Go と zsh が候補 (ユーザー 2026-10-24)。足す前に、Swift と同じく
「対象の repo の全ファイルを読ませて、読めない構文 (ERROR / MISSING) が出るファイルを数える」を測ってから決める。
zsh は tree-sitter-bash を使う案で、zsh だけの構文 (`${(f)...}`・glob の修飾子など) を読めるかが分かれ目。Go は tree-sitter-go か標準の `go/ast`。

Go の重複のチェックは dupl (golangci-lint) が dotfiles の Go の 7 module (doctor・glogx・mac-lan-manager・pro-con・ratelimit・treefiler・tuikit) で有効。閾値はどれも 120 トークンで、tuikit 以外はテストを除外 (`src/*/.golangci.local.yml`)。
Go を足す価値は、索引と `gate` ([905](done/905-feat-dupscan-ci-check-gate.md)) の側と、dupl の閾値より小さいヘルパーの写しにある。

## 対応方針 (2026-10-26。ユーザーと合意)

- dupl・gopls のラッパーにはしない。価値は `index` と `gate` (dupl に無い) にあり、gate の判定は単位ごとのハッシュで base と head を比べる形なので、dupl の出力 (一致した区間) には乗らない。dupl は lint に残して並べて比べる
- 構文は標準の `go/ast` で読む。コアは tree-sitter の木ではなく `internal/tree` の中立の `Unit` / `Node` だけを見るので、adapter が go/ast をそこへ写せばよい
- `def` / `refs` / `callers` は Go では後回し (Claude Code の LSP ツールで gopls が使える)
- 手順: ① コマンド層が `lang/swift` を直に呼んでいる所を、拡張子で adapter を選ぶ口に寄せる (挙動は変えない) ② Go の adapter ③ dotfiles の Go の 7 module で較正 (dupl の閾値より小さい写しが拾えるか) ④ Go の module の make test に gate を入れるかを決める

## 受け入れ条件

- [x] dotfiles の Go のファイルで、読めない構文があるファイルの数と所要を測って書く (下の「較正」)
- [x] 足すかと、足すならどの文法・道具かを決める → go/ast の adapter で足した (`src/dupscan/internal/lang/golang`)
- [x] Go の module の make test・CI に gate を入れるか → 入れた (ユーザー指示 2026-10-26「gateを入れてみて」。下の「gate を make test に入れるか」)

## 進捗

- 2026-10-24 起票 (epic 890 の分解)
- 2026-10-24 Go は dupl が既に回っていることを書いた (901。反証レビューで 3 → 7 module に直した)
- 2026-10-26 zsh / shell script は対応しないと決めた (ユーザー決定)。overview §4 の表に理由・要る改修・再評価の条件を書いた。受け入れ条件を Go だけに絞った
- 2026-10-26 実装した (commit: refactor(dupscan): コマンドが言語を拡張子で選ぶ口 (internal/langs) を通るようにする / feat(dupscan): Go のファイルを読む)
  - 手順 ①: コマンド層の lang/swift の直参照を internal/langs に寄せた。挙動が変わらないことは、変更前後のバイナリを flickorder-client・DualNoteApp に当て、
    report・gate・index・callers・def・refs・near・explain の stdout / stderr / rc が全部一致したことで確かめた
  - 手順 ②: go/ast の adapter。def / refs は Go のファイルを読まず、読まなかった数を stderr に出す。index は Go の小文字の関数に `[unexported]`
  - 敵対的レビュー (opus) を 5 周。直した指摘: 戻り値の名前がハッシュに入る・struct の tag が落ちる / tag が field から外れる・型の字面が改行で変わる・
    field と宣言のコメントが型の字面に入る・テスト関数の判定・`//line` の行・EOF で閉じない関数・チャネルの送受信の L3・型の別名・言語の分離 (種類名に `go.`)。
    直さず README に記録: map のキーの局所の変数・型引数の制約・余分な `}` で手前の関数も読めない扱い・生の tag と解釈済みの tag
  - 変異: 新しいテストに 40 本余り当て、全部が狙ったテストで red (緑だった変異はテストを直すか捨てた)

- 2026-10-26 gate を make test と CI に入れた (commit「feat(dupscan): dotfiles の src の Go の make test と CI に gate を入れる」)。上の「gate を make test に入れるか」
- 2026-10-26 dotfiles の `src/` の `.go` を Write / Edit すると、session に 1 回「書く前に dupscan index・commit の前に dupscan gate」を案内する hook と、
  CLAUDE.md の節を足した (ユーザー決定: CLAUDE.md の 1 行 + 書く瞬間の hook。commit: feat(claude): src の Go を書くときに dupscan を案内する)。
  敵対的レビュー 1 周 (P1 なし。テストが rc・stderr を捨てていた偽の緑を直した)・変異 10 本が全部 red

## 較正 (2026-10-26。master f4f463b41)

- dotfiles の `src`: 1211 ファイル・8547 単位・組 70 (L1 45・L2 25)。所要 1 秒未満。読めない構文のファイルは 1 (testdata のわざと壊したファイル)
- dupl (閾値 120 トークン) が出さない小さい写しが出る。例: zundamon-kaisetsu の `InUse` / `ReaperAlive` (ファイル名と失敗時の戻り値だけが違う)
- 過去の first-parent の 150 commit (Go を触ったもの) に `gate -base <親> -head <commit>` を repo 全体で流すと、止まったのは 7 commit (4.7%)。1 本 1 秒以下
  - serialrun が runtimeout の parseSeconds / startErrorRC / waitStatus を写した・JSON の書き出しの写し・tmpclean と doctor の同じクロージャ・test helper の写し
  - 多くは module をまたいだ本物の写し。Go は module をまたいで非公開の helper を共有できないので、寄せるには共有の module が要る。`main` の定型のような意図した形もある

## gate を make test に入れるか (2026-10-26 に入れた)

- 形: root の `make test-src` → `test-dupscan-gate` (`bin/dupscan gate src`。module をまたいだ写しも数える)、`make test-changed` の src 腕、
  GHA `.github/workflows/dupscan-gate.yml` (paths filter つき。base は master で最後に成功した run の commit。PR は対象のブランチ)
- 入れた時点の木は緑 (2.8 秒、初回 build 込み)。`-base HEAD~40` では dupscan の `jsonout` の `Write` / `Line` が codexevents・zundamon の写しとして止まる (入れる前の commit なので数えない)
- 敵対的レビュー (opus) 4 周で決めたこと:
  - base を push の前の commit にしない: concurrency で pending の run が押し出される・赤の run の次の push が緑になる、の 2 つで写しが master に残る
  - 最後の成功が手元に無ければ取りに行き、取れなければ止める (force push の後に前の commit へ逃がすと、2 本目の run で写しが素通る)。逃げ道は手動起動の `base` (sha だけを通す)
  - `-fail-new-exclusions` は付けない: base が最後の成功なので、許可を足した後の run が全部赤のままになる。許可・除外は skill dupscan の 3 節どおり報告に書く
  - 受けた形: 成功した run が 0 本の間は push の前の commit を使う (導入直後だけ) / 手動起動に先端を渡すと、それより前の赤い run の写しを以後数えない /
    base から HEAD の間に別の正当な寄せがあると、新しい写しが相殺されて緑になる (正味の写しの数は増えていない) / 共有 working tree の他人の untracked な .go で手元が赤になる (dotfiles は worktree が既定)
- 以下は入れる前に書いた判断材料:

- repo 全体で回すと上の 4.7% で止まり、多くは module をまたぐ。module ごとの make test で `gate .` を回すと、module の外の写しは数えない (serialrun と runtimeout のような写しは止めない)
- 入れない間も、セルフレビューで `dupscan gate` を回す (skill dupscan の 3 節) ことで同じ判定は得られる

