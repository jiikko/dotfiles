# 086 bug: branch 名の `%` がプロンプトエスケープとして解釈される (色が後続へ漏れる / 表示を偽装できる)

起票日: 2026-08-21
種別: bug
優先度: **P3** (コマンド実行には至らない。表示の破壊・偽装と色漏れのみ。要件は「敵対的な
branch 名を持つ repo に cd すること」)

出典: 監査 [070](done/070-research-quality-audit-2026-08-20.md) の `070-git-prompt-percent`
(「`zshlib/_git_prompt.zsh:98` が `.git/HEAD` 等由来の文字列を prompt へ素で埋める」)。
監査時は未裏取り。**2026-08-21 に隔離実験で再現し、同時に「より強い読み (任意コマンド実行)」は
反証した**。

## 実験で確定した事実 (2026-08-21)

`zshlib/_git_prompt.zsh:98` は branch 名をそのまま prompt 文字列へ埋める:

```zsh
_DOTFILES_GIT_PROMPT="%F{black}%K{green}[${REPLY}]%f%k"
```

`_zshrc:231` で `setopt prompt_subst`、`:237` で `PROMPT='...${_DOTFILES_GIT_PROMPT}'`。

**再現 (隔離した空 repo / HOME 非依存)**: branch 名 `x%F{red}%#%B` で
`print -P -- 'X${_DOTFILES_GIT_PROMPT}Y'` を評価すると

```
X ESC[30m ESC[42m [x ESC[31m % ESC[1m ESC[31m ESC[42m ] ESC[39m ESC[49m Y
```

- `%F{red}` / `%B` が**解釈されて色と bold が変わる**
- `%#` が `%` として描画される
- 末尾の `%f%k` が「注入された色」を打ち消しきれず、**`]` の後まで赤/太字の状態が漏れる**

→ 攻撃者が branch 名を選べる repo (clone してきた repo・PR ブランチ) に cd すると、
prompt の色を破壊し、prompt の一部 (例: root を示す `#`、別ディレクトリ) を**偽装できる**。

## 反証できた側 (誇張しないこと)

- **任意コマンド実行は起きない**。`PROMPT` に `${_DOTFILES_GIT_PROMPT}` を書く現行配線では
  prompt 展開が 1 パスなので、**変数の値に含まれる `$(...)` は再走査されない**
  (branch 名 `$(id)` で実測: `[$(id)]` のまま出力される)
- 値を**先にシェルが展開してから** `print -P` に渡す形 (例: `print -P -- "$_DOTFILES_GIT_PROMPT"`)
  だと `$(id)` が実行される。現行配線はこの形ではない。**この差が唯一の防波堤**なので、
  prompt 周りを触るときに壊さないこと (`%` 注入の修正と同時にここもテストで pin する)

## 対応方針

branch 名を prompt へ埋める前に `%` をエスケープする (zsh なら `${REPLY//\%/%%}`)。
`.git/HEAD` 由来の文字列は他にも prompt へ流れていないか同時に grep する
(同型バグの横展開: `_git_prompt.zsh` 内の全 `REPLY` 埋め込み箇所)。

## 変異検証

- branch 名 `x%F{red}%B` で `print -P` の出力に ESC シーケンスが**増えない**ことを assert
  (エスケープを外す変異で red)
- 「値の中の `$(...)` が実行されない」ことも別ケースで pin する
  (`print -P -- "$var"` へ書き換える変異で red)
- テストは隔離した空 repo を作って行う (`tests/zshrc/` の既存の TMP_HOME 方式に倣う)

## trigger

単独で着手可 (1 行 + テスト 2 本)。prompt 周りを次に触るときは必ず同時に。
