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
（[`list-masked-failure-modes-before-removing-guard.md`](../_claude/rules/list-masked-failure-modes-before-removing-guard.md)）:

- (a) `.envrc` の in-place 編集の即時反映
- (b) 別端末で `direnv allow` した後の反映
- (c) `.envrc` を含むディレクトリが後から作られた場合

### 却下済みの案

**`DIRENV_WATCHES` を突き合わせて fork を飛ばす**案は却下（research issue 324 参照）。
direnv の内部表現に依存し、ツール側の更新で無言で壊れる。

## ③ シェル起動ごとに結果が定数または no-op の外部コマンドを 2 本 fork している

`eval "$(direnv hook zsh)"` と ssh-agent の鍵登録（`ssh-add -l`）。
起動 1 回ぶんなので precmd ほどは効かないが、②と同じ commit で見直せる。

## 受け入れ条件

- [ ] ①: `zsh -f` の隔離実験で widget テーブルを diff し、**1 行で足りるのか
      「source 後に 1 回 bind」が要るのか**を確定させる（推測で入れない）
- [ ] ①: 入れた後、**paste / `^R` / 補完受理**を人手で確認する（human issue に回してもよい）
- [ ] ②③: 外すなら failure mode を列挙してからユーザーに判断を仰ぐ
- [ ] 効果を報告するときは**測った経路と計器の分解能**を併記する（「prompt_lag が改善」とは書かない）

## 関連

- issue 323（この改善を**観測できる**計器にする話。①の 6.69ms は現行の予算では見えない）
