# human: precmd 最適化の動作確認と、direnv を precmd から外すかの判断

起票日: 2026-09-08
期限: 2026-09-20
出典: [322](done/322-perf-precmd-cost-is-dominated-by-third-party-hooks.md) の受け入れ条件のうち、
人にしかできない 2 つ（実際に触っての確認 / トレードオフの判断）

## ① 動作確認: `ZSH_AUTOSUGGEST_MANUAL_REBIND=1` を入れた後の zle

`_zshrc` に 1 行入れた（commit `perf(322): zsh-autosuggestions の毎プロンプト再バインドを止める`）。
widget テーブルが変わらないことは機械で確認済み（実 rc で 417 件が完全一致、diff 0）だが、
**実際に触ったときの挙動**は人しか見られない。`exec zsh` してから次を試す:

- [ ] **paste**（クリップボードから複数行を貼る。bracketed paste が効いているか）
- [ ] **`^R`**（履歴検索に入り、候補を選んで確定できるか）
- [ ] **補完の受理**（`Tab` で補完 → `→` / `End` で suggestion を受理できるか）
- [ ] **`^C` / `^U` / `^W`** など編集系が普通に効くか

### 🚨 ここが本命: 「最初の bind より後に生えた widget」

敵対レビューの指摘で分かった **MANUAL_REBIND の trade-off**（実測 2026-09-08）:

| | 後から `zle -N` した widget |
|---|---|
| MANUAL_REBIND なし | `_zsh_autosuggest_bound_1_my-late-widget` = **ラップされる** |
| MANUAL_REBIND あり | `my-late-widget` = **されない** |

この repo では `zstyle ':completion:*:default' menu select=1` により `zsh/complist` が
**最初のメニュー補完時**（= 初回 precmd より後）に autoload され、`menu-select` widget が生える。
つまり **`menu-select` が未ラップになる**。実害は未実証（`menu-select` は通常
`expand-or-complete` 経由で内部的に入るため suggestion に影響しない可能性がある）。

- [ ] **メニュー補完を実際に使う**（`Tab` を 2 回押して ←↓↑→ で候補を選び、`Esc` で抜ける）。
      その前後で suggestion の表示・受理がおかしくならないか
- [ ] 未ラップであることの確認（気になる場合）:

```zsh
exec zsh
zle -l | grep -c autosuggest-orig            # (A) を記録
# Tab を 2 回押してメニュー選択に入り、Esc で抜ける
zle -l | grep menu-select                    # menu-select が生えている
zle -l | grep -c autosuggest-orig            # (A) のまま = 未ラップ
```

🚨 いずれかが壊れていたら、`_zshrc` の `ZSH_AUTOSUGGEST_MANUAL_REBIND=1` の行を**消せば**元に戻る。
**`=0` にしても戻らない**（プラグインは `${+VAR}` で**存在**だけを見るため）。
壊れ方をこの issue に書き足してから戻すこと。

## ② 判断: `direnv` の hook を precmd から外すか

`_direnv_hook` が **precmd と chpwd の両方**に登録されており、`.envrc` の有無に依らず
毎プロンプト外部コマンドを fork する（実測 4.14〜4.79 ms / 対話シェル内）。
precmd から外して **chpwd + シェル起動時 1 回**だけにすれば、この定数コストが消える。

**外すと失われるもの（failure mode の列挙。
[`list-masked-failure-modes-before-removing-guard.md`](../_claude/rules/list-masked-failure-modes-before-removing-guard.md)）**:

| 失う挙動 | 外した後どうなるか | 回避手段 |
|---|---|---|
| (a) `.envrc` を**その場で編集**したときの即時反映 | 同じディレクトリに居る限り反映されない | `cd .` で再発火 |
| (b) **別端末で `direnv allow`** した後の反映 | その端末では反映されない | `cd .` |
| (c) `.envrc` を含むディレクトリが**後から作られた**場合 | 既に居るディレクトリなら無反応 | `cd .` |
| (d) `.envrc` が source する**別ファイル**（direnv の watch 対象）の変更 | 反映されない | `cd .` |

どれも「`cd .` を打てば戻る」もので、**壊れるのは自動反映だけ**。
ただし (a) は `.envrc` を書きながら試す作業で頻繁に踏むので、その作業をよくするなら外さない方がよい。

- [ ] **判断**: 外す / 外さない（外すなら別 issue を切って実装する）

## ③ 起動時の 2 fork（`direnv hook zsh` と `ssh-add -l`）

②と同じ commit で見直せるが、**起動 1 回ぶん**なので効果は小さい。②の判断に従う。

- [ ] ②を外すと決めたときだけ、あわせて見直す
