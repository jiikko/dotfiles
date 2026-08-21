# 089 bug: `print -P` にファイル名を埋めている箇所で、ファイル名内の `$(...)` が実行される

起票日: 2026-08-21
種別: bug
優先度: **P1** (対話シェルでの**任意コマンド実行**。`touch` が実行されるところまで実測済み。
トリガは「攻撃者が名前を選べるファイルに対して `video_health` / `av1ify` / `concat` /
`repair_mp4` を実行する」= ダウンロードしたファイルをまとめて処理する平常の使い方。
`av1ify` は `_zshrc` でラップ定義され対話シェル起動直後から呼べる)

出典: issue [086](done/086-bug-git-prompt-percent-injection.md) の修正時の同型バグ横展開
(`~/.claude/CLAUDE.md`「不具合対応の原則」の「直したバグは別の場所にもある前提で grep する」)。
086 は prompt へブランチ名を埋める話で **表示の破壊だけ・コマンド実行は反証済み**だったが、
`zshlib/` の `print -P` 群は **086 が「危険な形」として反証に使った配線そのもの**だった。

## 実測 (2026-08-21)

**実経路で再現** (mock でなく実 ffprobe / 実ファイル):

```zsh
setopt prompt_subst                        # _zshrc:231 と同じ (対話シェルの既定)
source zshlib/_video_health.zsh
ffmpeg -loglevel error -f lavfi -i testsrc=duration=1:size=64x64:rate=10 \
  -c:v mpeg4 -y 'x$(id -u)y.mp4'
video_health 'x$(id -u)y.mp4'
# → ❌ ESC[31mESC[1mESC[31m x501y.mp4 ...      ← ファイル名の $(id -u) が実行され 501 に置換された
```

`zshlib/_video_health.zsh:192` の `print -P -- "❌ %F{red}%B${f:t}%b%f"` が、既にシェル展開済みの
文字列 (= ファイル名を含む) を prompt 展開へ通すため。`prompt_subst` 下の prompt 展開は
**渡された文字列の中の `$(...)` を実行する**。

**読み取りに留まらない (2026-08-21 追試)**: 名前 `evil$(touch pwned.txt)file.mp4` の実 mp4 で
`video_health` を呼ぶと `pwned.txt` が**実際に作られる**。表示は `evilfile.mp4` (置換結果が空) に
見えるので、画面からは何も起きていないように読める。

⚠️ 再現には**表示経路まで到達する入力**が必要 (中身が壊れたダミーは rc=2 の skip で何も print
されず、実行されない)。「再現しなかった」で false positive 扱いにしないこと。

### 対策候補の実効性 (すべて実測)

| 形 | ファイル名の `$(...)` | ファイル名の `%F{red}` |
|---|---|---|
| 現状 `print -P -- "... $f ..."` | **実行される** | 解釈される |
| `%%` 畳みのみ (`${f//\%/%%}`) | **実行される** | 字として出る |
| `emulate -L zsh` を関数頭に足す | **実行される** (⚠️ `-R` 無しの `emulate` は構文系オプションだけを戻すので `prompt_subst` は落ちない) | 解釈される |
| `setopt localoptions no_prompt_subst` + `%%` 畳み | 字として出る | 字として出る |
| `emulate -LR zsh` + `%%` 畳み | 字として出る | 字として出る |
| **色を先に解決してデータは `print -r`** | 字として出る | 字として出る |

## 件数 (2026-08-21 時点、`zshlib/*.zsh`)

`print -P` に変数を埋めている行が **37 件**。うち `%` を畳んでいるのは **1 件だけ**
(`_video_health.zsh:195` の ffprobe 出力。ここは畳んでいるが `$(...)` は依然実行される)。

| ファイル | 件数 |
|---|---|
| `zshlib/_av1ify_encode.zsh` | 24 |
| `zshlib/_av1ify.zsh` | 4 |
| `zshlib/_concat.zsh` | 3 |
| `zshlib/_repair_mp4_timebase.zsh` | 3 |
| `zshlib/_video_health.zsh` | 3 |

**攻撃者が実際に制御できるのはファイル名だけ** (`${f:t}` / `$in` / `$final_out` /
`${file:t}` / `${origin:t}` / `${input_files[$i]:t}`)。

ffprobe 由来の値 (`$acodec` / `$src_abitrate` / `$current_tb` / `$REPLY`) は現状は無害で、
ファイル名と同じ危険度で語るのは誇張だった (2026-08-21 の反証で訂正):

- `$src_abitrate` は取得直後に `^[0-9]+$` でガードされ、数字以外は即 return
  (`_av1ify_encode.zsh:298`)
- `$acodec` (`stream=codec_name`) は libavcodec のコーデック名テーブル由来の列挙値、
  `$current_tb` (`stream=time_base`) は `num/den` の有理数表記
- `$REPLY` (`_av1ify_encode.zsh:587,590`) は `__video_health_check` が組む**固定の日本語文言 +
  整数**(`_video_health.zsh` の `issues[]`)。ffprobe の生出力は入らない

→ ffprobe 由来は「今は無害。ffprobe の出力仕様が変わったら再評価」の位置づけにする
(修正の対象からは外さない = 直すときは同じヘルパーを通す)。

## 対応方針

**構造で潰す**: データを prompt 展開に通さない。書式 (色) と データ を混ぜているのが原因なので、
色を 1 度だけ ANSI へ解決し、データは `print -r` で出す。

```zsh
# 例 (zshlib 共通のパレットに寄せる)
typeset -g _C_GREEN=$(print -P '%F{green}') _C_OFF=$(print -P '%f')
print -r -- "${_C_GREEN}✅ 完了: ${final_out}${_C_OFF}"
```

- 副作用として prompt 展開が 37 回分消える (毎回の `print -P` は展開器を通す)
- 応急処置で済ませるなら **`setopt localoptions no_prompt_subst` を各エントリ関数の頭へ** +
  データ側に `%%` 畳み。ただし「データを書式として扱う」構造自体は残る
- ⚠️ `emulate -L zsh` は**効かない** (上表)。`-R` を付けないと `prompt_subst` は落ちない

## 変異検証 (必須)

086 の `tests/zshrc/test_git_prompt.sh` に入れた形をそのまま使える:

- 名前に `$(id -u)` を含むファイルを作り、出力に `uid`/実 uid が出ないことを assert
- **陽性対照を同時に置く** (危険な形なら実際に実行されることを示す)。これが無いと
  「そもそも検出できないから緑」の空の主張になる
- `%` 版も同様に「ESC シーケンスが基準から増えない」で assert

## trigger

**単独で着手可 / 優先度は高い**。まず `_video_health.zsh` (3 件) で形を作り、
`_av1ify_encode.zsh` (24 件) へ広げるのが安全。

## レビュー記録 (2026-08-21)

反証レビュー (実コードを走らせての裏取り) で、再現手順・行番号・件数 (37 件 / エスケープ済み
1 件)・対策候補の表 6 行・P1 判定・086 との関係はいずれも**反証できなかった** (記述どおりと
確認)。訂正したのは上記「ffprobe 由来を同列に書いた」1 点。

## 関連

- [086](done/086-bug-git-prompt-percent-injection.md) — 兄弟 issue。あちらは「値を prompt 展開に
  **参照として**渡す安全な形」で、コマンド実行は反証済み。本 issue はその危険形の横断調査
- `_claude/rules/mutation-verify-new-tests.md` — 陽性対照の要求
