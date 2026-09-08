# perf: precmd のコストの大半は repo 自前ではなく第三者 hook 側にある（要ユーザー判断）

起票日: 2026-09-07
カテゴリ: perf
優先度: 中（🚨 効果は**人が知覚する閾値にも通しの計器の分解能にも届かない**。
severity が「驚きの大きさ」を表さないよう medium に置く。修正が安価なので着手はしてよい）
出典: /audit performance 2026-09-06（forge Minimum+）

## 前提: repo 自前の precmd fork は 0 件だった

「precmd / preexec から `$(...)` を呼ぶ形」（repo の規範は
`_claude/rules/` の「REPLY で返す」）を攻めた結果、**自前のコードには 0 件**。
precmd コストの **99% は第三者 hook 側**にあった。

## ① `zsh-autosuggestions` が毎プロンプト 421 widget を再バインドしている

`_zsh_autosuggest_start` が precmd に常駐し、毎回 `_zsh_autosuggest_bind_widgets` を呼ぶ。

### 実測（N=200）

| | precmd 1 サイクル |
|---|---|
| 現状 | **12.00 ms** |
| `ZSH_AUTOSUGGEST_MANUAL_REBIND=1` | **5.31 ms** |

差 **-6.69 ms（-56%）**。`_zsh_autosuggest_bind_widgets` 単体で 5.97 ms/call。
repo 内に `ZSH_AUTOSUGGEST` の設定は **0 件**（私が grep で確認）。

### 🚨 「1 行足すだけ」ではない可能性がある（実験で確定させること）

一次報告は「source 直前に `ZSH_AUTOSUGGEST_MANUAL_REBIND=1` を 1 行」で済むとしたが、
安全性の全数勘定に穴がある。数えたのは `zle -N` の**リテラル 5 箇所**だけで、
**`zsh-syntax-highlighting` が autosuggestions の後に source されており、
これはリテラルの `zle -N` ではなくプログラム的に全 widget をラップし直す**
＝ grep に出ない「初回 bind より後に widget を触る主体」。

したがって「5 件すべてが source 行より前 → 初回 bind が全 widget を覆う」は**成立しない**。

🚨 ただし「だから paste / `^R` が壊れる」と**断定するのも推測**。
**`zsh -f` の隔離シェルで同じ source 順の最小 rc を作り、`MANUAL_REBIND` の有無で
`zle -l` / widget テーブルを diff して確定させる**こと。

ラッパが残らないなら、対応は 1 行ではなく
**「`MANUAL_REBIND=1` ＋ 全プラグイン source 後に `_zsh_autosuggest_bind_widgets` を 1 回」**になる。

### 🚨 「prompt_lag が改善する」とは書かないこと

通しの計器（`tests/zshrc/bench_zsh.sh` の prompt_lag, min-of-5）では
BASE 26.0 / 24.2 / 24.7 vs FIX 18.7 / 24.1 / 22.2 で**分布が重なり判定不能**＝分解能不足。
これは issue 323（予算・計器の分解能）と同じ根。

## ② `direnv` の hook が毎プロンプト外部コマンドを fork する

`_direnv_hook` が precmd と chpwd の**両方**に登録されており、`.envrc` の有無に依らず定数コストを払う。

| | 時間 |
|---|---|
| 対話シェル内 | 4.14〜4.79 ms |
| standalone `direnv export zsh` | 5.14〜5.38 ms |
| （参考）素の fork 下限 | 1.135〜1.292 ms |

第三者ツールなので fork 自体は設計上不可避。取れる手は
**「precmd から外し、chpwd + シェル起動時 1 回だけにする」**方向のみ。

🚨 **未実測・ユーザー判断待ちとして扱う。** 外す前に failure mode を列挙すること
（[`list-masked-failure-modes-before-removing-guard.md`](../../_claude/rules/list-masked-failure-modes-before-removing-guard.md)）:

- (a) `.envrc` の in-place 編集の即時反映
- (b) 別端末で `direnv allow` した後の反映
- (c) `.envrc` を含むディレクトリが後から作られた場合

### 却下済みの案

**`DIRENV_WATCHES` を突き合わせて fork を飛ばす**案は却下（research issue 324 参照）。
direnv の内部表現に依存し、ツール側の更新で無言で壊れる。

## ③ シェル起動ごとに結果が定数または no-op の外部コマンドを 2 本 fork している

`eval "$(direnv hook zsh)"` と ssh-agent の鍵登録（`ssh-add -l`）。
起動 1 回ぶんなので precmd ほどは効かないが、②と同じ commit で見直せる。

## 対応 (2026-09-08)

commit `perf(322): zsh-autosuggestions の毎プロンプト再バインドを止める`。

### ①「1 行では足りないかも」の懸念は実験で否定された

`for f in $precmd_functions; do $f; done` でプロンプト 1 回ぶんを再現してから `zle -l` を比べた
（🚨 最初 rc の中で `zle -l` を見て「base でも 13 widget しか無い」と読みかけた。
**bind は precmd で起きる**ので、precmd を走らせる前の観測は 3 条件とも同じ値になり、
実験として成立していない）:

| 条件 | `zsh -f` 最小 rc | 実 rc |
|---|---|---|
| 現状 | 388 | 417 |
| `MANUAL_REBIND=1` だけ | 388（**diff 0**） | 417（**diff 0**） |
| `MANUAL_REBIND=1` + source 後に 1 回 bind | 388（diff 0） | — |

**初回の bind は MANUAL_REBIND でも走る**ので、後から source される zsh-syntax-highlighting の
ラップも覆われる。よって**対応は 1 行**で、「source 後にもう一度 bind」は不要。

### ① 効果 (直接計測。prompt_lag では見えない)

実 rc・N=200・3 run で precmd 1 サイクル:

| | 実測 |
|---|---|
| 現状 | 9.49 / 9.69 / 9.64 ms |
| `MANUAL_REBIND=1` | 3.28 / 3.38 / 3.38 ms |

**-6.3 ms (-65%)**。一次報告の 12.00 → 5.31 ms とは絶対値が違う（測定時の負荷差）が、
削減幅は同じオーダー。🚨 **この差は `bench_zsh.sh` の prompt_lag では観測できない**
（run 間変動が 16〜22 ms。issue 323 で予算は締めたが、分解能の問題は残っている）。
効果を語るときはこの直接計測を出すこと、を `_zshrc` のコメントにも書いた。

### ②③ は人の判断へ → [338](../338-human-zsh-precmd-verification-and-direnv-decision.md)

direnv を precmd から外すと失われる挙動 4 つ（`.envrc` の即時編集反映 / 別端末の allow /
後から作られたディレクトリ / watch 対象の別ファイル）を列挙した。**どれも `cd .` で戻る**が、
`.envrc` を書きながら試す作業では (a) を頻繁に踏むので、トレードオフの判断は人に委ねる。
①の動作確認（paste / `^R` / 補完受理）も同じ issue に置いた。

## 受け入れ条件

- [x] ①: 隔離実験で widget テーブルを diff し、**1 行で足りる**ことを確定させた
- [x] ①: 入れた後の人手確認 → [338](../338-human-zsh-precmd-verification-and-direnv-decision.md) へ
- [x] ②③: failure mode を列挙して [338](../338-human-zsh-precmd-verification-and-direnv-decision.md) で判断を仰ぐ形にした
- [x] 効果は**測った経路と計器の分解能**を併記した（「prompt_lag が改善」とは書いていない）

## 関連

- issue 323（この改善を**観測できる**計器にする話。①の 6.69ms は現行の予算では見えない）
