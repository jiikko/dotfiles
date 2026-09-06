# 検証は「exit code」ではなく「実行された証拠」で判定する — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/verify-execution-not-just-exit-code.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## なぜ (2026-08-23 obaket、同一セッションで 3 回踏んだ)

`mutation-verify-new-tests.md` は「green は正しいではない」を扱うが、その **手前** に
「そもそも走ったのか」という段がある。ここが抜けると変異検証すら空振りする
(壊す変更を当てても、走っていない検査は元から red にならない)。

実際に踏んだ 3 形態:

| 症状の出方 | 誤読 | 実態 |
|---|---|---|
| **緑** | 「gate 通過」 | 新設 gate が集約 target に未配線で **1 度も走っていない** |
| 赤 (generic message) | 「どれかの gate が落ちた」 | gate は 1 つも走らず、`make` が target 不在で即死 |
| 赤 (別 exit code) | 「パス解決を直せば通る」 | その実行環境に検査対象のソースが存在しない |

**最も危険なのは 1 つ目 (緑)**。赤なら調査が始まるが、緑は指摘されるまで気づけない。
しかも「検査を足す」= 安全側の作業ほどこの穴を踏む (足した本人は守ったと思い、実際は守られない)。

同型の先例として obaket issue 556 がある: 検査を `make lint` の gate へ移したが、
CI は `make lint` を呼んでいなかったため **移した先で 1 度も走らなくなっていた**。
症状 (テストが落ちる) だけが消えて、回帰検出は失われていた。

## ルール本文から移した実例

本文には規範だけを残し、その根拠になった実例をここへ移した（元の文脈のまま）。

**実害は「待ち直し」では済まない** (2026-08-25 obaket 581): 早すぎた判定が
「相手 (codex) が空振りした」という**誤った因果**を作り、(1) 代替作業へ切り替え、
(2) 遅れて完了した出力が同じパスを上書きし、(3) **commit された成果物の出所を取り違えた**
(自分が書いた版のつもりで別主体の版を commit した)。
「まだ走っている」を「壊れている」と読むと、後続の判断が全部その誤診の上に建つ


## 2026-08-28: 実行環境を変えたら、安全機構のテストが 2 本消えていた (dotfiles issue 133)

CI の runner を ubuntu から macOS へ移した。全 workflow が green になり、テストファイル数も
**96 → 96 で同数**だったので完了と読みかけたが、敵対的レビューが両 run の出力を全行 A-B して
`✓` 行が **68 件消えている**ことを見つけた。内訳は 2 本:

- `tests/claude/test_deny_bare_tmux_kill.sh` — macOS に `timeout(1)` が無く**丸ごと skip**。
  60 件の assert が消えたのに runner の集計は `[ok]` を出す。守っていたのは本番 tmux サーバ
  誤 kill の再発防止ゲートで、消えた中には「入力長で hook が timeout に殺されて deny が消える」
  回帰テストも含まれていた
- `tests/tmux/test_fork_scratch.sh` — `uname == Darwin` で skip する設計だったため CI から消えた

自分でも後者は「skip しそうなガードを grep する」で見つけたが、それは**自分が思いつく形しか
見つけられない**。前者 (依存コマンドの不在で skip) は全行 diff でしか出なかった。

## 2026-09-01 obaket 688 C7 — 「増えていない」が証拠にならなかった

E2E の credential を実 Keychain に書かなくする変更 (in-memory 実装への差し替え) の証拠として、
**実行後に keychain の item が増えていないこと**を挙げかけた。しかし E2E は最後に接続を削除するため、
**差し替えの有無に関わらず増えない**。対立仮説を棄却できない観測だった。

取り直したのは**実行中**の 10 秒間隔サンプリング。差し替えが無ければ、接続の作成〜削除の間だけ
一時的に増えるので区別できる。結果は全サンプルで baseline (過去の残骸 3) のまま。

同日、前後比較で抽出方法を変えてしまい 30 と 49 という比較不能な数字を出す失敗もした
(`grep -oE '"svce"<blob>=...'` と素の `grep -c`)。**比較は同じ方法で採る**。

## 出力を `tail` で削って失敗の原因を消した実例 (2026-09-02〜03, dotfiles / retro 195 項目 1)

同じセッションで 2 回踏んだ。どちらも「status を取り違えた」のではなく「**読むべき出力を自分で捨てた**」形。

**1 回目**: `make test 2>&1 | tail -25` で回したら `test-lint` が落ちた。しかし `tail -25` に残ったのは
集約 target が最後に出す `✗ 失敗したターゲット: test-lint` の 1 行だけで、**どの検査が何で落ちたかは
スクロールの上で捨てられていた**。全ログを取り直して再実行すると rc=0 (緑) で、
**1 回目の失敗は原因不明のまま閉じるしかなかった**。`make test-lint` 単独でも rc=0 だった。

集約 target は「全部走らせてから失敗をまとめて返す」設計 (dotfiles の `run_all_targets` がそう) なので、
**結論行は必ず末尾に来る**。`| tail -n` はその結論だけを残し、理由を落とす。最も捨ててはいけない側が消える。

**2 回目**: 変異検証で `bash step.sh 2>&1 | tail -7; echo "step rc=$?"` と書き、`$?` が `tail` のものになって
「変異を当てたのに rc=0 = 検査が素通りした」と読んだ。`bash step.sh > out 2> err; echo $?` に分けて
測り直したら rc=1 で、検査は正しく red だった。🚨 **この 2 回目は、同じ日に自分が doctor の CI へ
入れた gate (「テストが 1 本も走らなくても go test は rc=0」) が守ろうとしている罠そのもの**で、
ルールを書いた直後に同じ形を踏んだことになる。

教訓は「pipefail を使え」ではない (1 回目は status の問題ではない)。
**検証は出力をファイルへ落としてから読む**。削るのは読んだ後でいい。

## 「抽出が空 = 違反 0 件 = 緑」の実例 (2026-09-03, dotfiles issue 203)

予防的 lint 2 本 (`scripts/check_go_project_lanes.sh` / 公開ラッパーのガード静的検査) を
書いた 1 セッションで、**同じ形の false green を 4 回作った**。いずれも検査は走り、出力も出て、
「違反 0 件」と報告した。

1. **`set -euo pipefail` 下の無マッチ grep**: `refs=$(... | grep -oE ... | sort -u)` は
   無マッチ (rc=1) で**代入ごとスクリプトを殺す**。直後に置いた「抽出できなければ FAIL」の
   ガードには到達せず、1 本目の関数で静かに終わって rc=1 になった。`|| true` を落とすたびに
   再発し、このセッションで 3 回踏んだ
2. **フィルタがパス名を前提にしていた**: 関数の定義元を `[[ "$src" == *"/dotfiles/"* ]]` で
   絞ったところ、**worktree (`/private/tmp/.../wt-a2`) では 1 件も一致せず**、走査 0 件のまま
   緑になった。変異 9 本が全部素通りして初めて気づいた (変異検証がこの穴の発見器になった)
3. **sed の式の破損**: 括弧の対応を崩した sed がエラーを出しつつ空を返し、refs が空 = 違反 0 件
   として緑になった
4. **`comm` の locale**: `sort` だけ `LC_ALL=C` にして `comm` を既定 locale のままにしたため、
   comm から見て入力が未ソートになり**引き算が黙って崩れた** (`local _l` の除外が効かず、
   逆に誤検出した)

塞いだ形: 抽出を 1 関数 (`sk_refs_of`) に集約し、**canary 3 本が同じ関数を通る**ようにした
(参照の抽出 / 文字列リテラルの除外 / `local` の引き算)。さらに「この repo なら必ず居る関数
(`concat` / `av1ify`) が列挙に入っているか」を確かめる canary を足し、2 の形を塞いだ。

出典: `issues/done/203-test-lint-candidates-preventive.md` /
`issues/done/208-retro-tmux-indicator-and-lint-checks-2026-09-03.md` 項目 2。


## 確認を求める外部コマンドは、非対話だと「何もせず rc=0」で返る

起源: dotfiles, 2026-09-04 (glogx doctor の Docker タブに実行の導線を足したとき)。

`docker ... prune` を画面から実行する配線を書き、確認画面まで作った段階で、実際に
何が起きるかを対象 0 件の filter で試した:

```
$ docker builder prune --filter unused-for=876000h </dev/null
rc=0
--stdout
WARNING! This will remove all dangling build cache. Are you sure you want to continue? [y/N]
--stderr
(空)
```

**rc=0 で、stdout にはそれらしい出力があり、しかし何も起きていない。** TTY が無いので
stdin が即 EOF になり、docker は「N」と解釈して中止する。この配線のまま出していたら、
確認画面で `y` を押したユーザーに「実行しました」と報告しながら 1 バイトも減らない。

`-f` を付けて解決したが、そのとき**表示・コピーするコマンドにも同じ `-f` を付ける**方を
選んだ。表示と実行で文字列を分けると「画面に出ているものと走るものが違う」を作るため。
コピーして貼る人はプロンプトを失うので、その事実を群の注記に書いた。

同じ日にもう 1 つ、**`--help` の文面を信じて範囲を誤った**例が出ている
(`measure-external-cli-streams-separately.md` の「`--help` を根拠にしない」節):

```
docker builder prune --help → "-a, --all  Include internal/frontend images"
docker builder prune        → 実行時の警告は "This will remove all **dangling** build cache."
docker builder prune -a     → 実行時の警告は "This will remove all build cache."
```

help を読んで「`-a` は要らない」と判断し、敵対レビューの指摘を**却下してコメントに理由まで
書いた**。警告文を読んで初めて誤りと分かった。破壊的操作の範囲は help ではなく、
**その CLI 自身が実行時に何と言うか**で確定させる。

## 待つと決めたら待つ (retro 259, 2026-09-05)

background で回した計測の完了を待つあいだ、`grep -c '[run]'` を数十回叩いた。`Monitor` /
`run_in_background` の until ループを張った後もポーリングを続けており、待ち方の切り替えができて
いなかった。実害はトークンと応答の水増し (ユーザーから見ると「進行中です」の繰り返し)。


## 2026-09-05 ThumbnailThumb 542 — 判定の入力が「意図したファイル」ではなかった

変異検証の worktree で SPM の解決版が tracked `Package.resolved` と同一かを確かめるスクリプトが、
`find "$WT/tmp" -name Package.resolved | head -1` で**依存パッケージ内部の** `Package.resolved`
(swift-custom-dump のもの) を掴んでいた。diff 出力は「左 1 行 / 右 8 行」で一目で別物だったのに、
スクリプトはパスを echo していたのに読まず、「同一版を確認した」と issue に書いた。気づいた時点で worktree は
削除済みで再確認できず、「未確認」へ訂正した。

- 「抽出・判定を書いたら canary」節の形: 抽出が**別のもの**を掴んでも判定は動き、出力も出る
- 破壊的な後始末 (worktree 削除) は、**その対象を根拠にした主張を全部書き終えてから**行う

## 「出力先は絶対パス」「待機と長寿命プロセスを同居させない」の起源 (obaket 696 / 715, 2026-09-02〜04)

- `$PWD/tmp/...` を出力先にした codex run が別 repo (SnapTrim) の tmp に成果物を出し、待機タスクは obaket 側を永遠に待った
  (696 項目 1)。715 で再発し、逆向きに誤認して `find` で探す往復が出た。codex の起動側は skill (codex-drive) のテンプレに `-C "$ROOT"` を入れた
- `(nohup driver …; echo rc > X.rc) &` で、run 単位の成果物は揃ったのに外側 `X.rc` だけ書かれず `until [ -f X.rc ]` が回り続けた
  (696 項目 10、原因未特定)。待つ対象は driver が必ず書く成果物 (`runs.tsv`) にし、上限を置く
- 待機用 background Bash の中で `nohup` した `make dev-fg` のアプリが、待機タスクの TaskStop で道連れになった (696 項目 9)

## path filter が 1 つ前の赤を隠した (dotfiles, 2026-09-06)

glogx の監査対応で 15 commit のあいだ `make lint` を回さず、`2118a458` で golangci-lint の
`prealloc` が `src/glogx` workflow を落とした。気づけなかったのは、**HEAD (`4f42832c`) が
`src/glogx/**` を触らない chore だったため workflow が起動せず**、`bin/ci-log` が
「HEAD に失敗した run はありません」を返したから。1 つ前が赤いまま HEAD が緑に見える。

同じセッションの終盤にもう一度出た: retro を足した docs のみの commit が HEAD のとき、
Lint / Tests / Bench は緑だが `src/glogx` は起動していない。そのときは
**`src/glogx` の run を run-id で名指しして待つ**ことで回避した (run `34010488115` = success、
建てた commit `57e44221`、ログに `ok glogx 47.398s`)。

この経験から `bin/ci-log` に「未検証 commit」の検出を足した (下記)。
