# 610 (bug): bin/ のスクリプトが失敗を成功に見せる・不正な値を ffmpeg に渡す (concat_movies / karabiner / ci-log)

起票日: 2026-10-02

## 概要

bin/ のシェルスクリプトを安定性の観点で監査した (sonnet の read-only 監査 → 主要な指摘を main で再現して裏取り)。
直すものと、記録だけにするものを分ける。

## 直すもの

### 1. `bin/concat_movies`: `bc` が 1 未満の値の先頭の 0 を落とし、ffmpeg が fade を拒否する

- `fade_st=$(echo "$x - $fadeout" | bc)` は `1 - 0.3` に対して `.7` を返す (実測)。ffmpeg は
  `Unable to parse "st" option value ".7"` で落ち、`set -e` でスクリプトが止まる
- 発火: `--thumbnail-duration 1 --fadeout 0.3`、または 1 秒未満のクリップに `--fadeout`
- 副次: ffprobe の duration が `N/A` (mkv 等で stream に duration が無い) のとき、`echo "N/A - 0.7" | bc` は
  **黙って 1.3 を返す** (実測。bc が N と A を 16 進の桁として読む)。fade の開始位置が嘘のまま変換が進む
- 直し方: 引き算を awk で行い、入力が数値でなければエラーで止める (1 つの関数に寄せる)

### 2. `bin/restore_karabiner_config.sh` / `bin/backup_karabiner_config.sh`: 失敗しても成功の文言を出して rc=0

- `jq ... > tmp && mv tmp target` が失敗しても、次の行の「(jis に自動設定)」が出て rc=0。0 バイトの `karabiner.json.tmp` が残る
  (監査で再現: 壊れた JSON + JIS を返す ioreg のスタブ)
- 冒頭の `cp "$src" "$target"` も未検査で、失敗しても古い sha を「restore 済み」として記録する。backup 側の `cp` も同じ
- 直し方: 失敗した段で `.tmp` を消して非 0 で止める

### 3. `bin/ci-log`: `gh` が失敗すると「失敗した run はありません」と出る

- `mapfile -t failed < <(gh run list ...)` はプロセス置換なので gh の rc が見えない。認証切れ・ネットワーク断で
  **「HEAD に失敗した run はありません」** を出し、その分岐の末尾の明示の `exit 0` で **rc=0** で終わる
  (監査で再現、反証レビューで rc を確認)。認証切れでも「CI は緑」と読める
- 同じ分岐の「HEAD より前の赤」の走査 (`done < <(gh run list ...)`) も gh の失敗を見ない
- `mapfile` は bash 3.2 に無い (shebang は `env bash`。PATH に Homebrew bash が無い環境で落ちる)
- 直し方: 2 か所の gh の出力を変数へ取って rc を見る。`mapfile` を `while read` にする

## 記録だけにするもの (今回は直さない)

| 箇所 | 内容 | 直さない理由 / trigger |
|---|---|---|
| `bin/mutate-verify` | `--verify` の run に timeout が無い。変異で hang すると止まらない (mutate-verify-list も詰まる) | 変異検証の判定 (第 3 の結果) に関わる安全機構の変更で、単独の作業として敵対レビュー込みで行うべき。trigger: hang する変異に実際に当たったとき |
| `bin/codex-fanout` | watchdog が TERM を 1 回送るだけで KILL へ昇格しない | 推測 (未再現)。codex を使う作業でのみ発火。trigger: codex-fanout が timeout 後も返らなかったとき |
| `bin/concat_movies` | 音声の無いクリップを混ぜると、音声の欠けた連結を「完了」として出す | 実害 (音ズレの程度) が未検証。trigger: 音声なしの素材を連結する用途が出たとき |
| `bin/repair_avi_vorbis_audio.sh` / `bin/repair-avcc-avi` | `ffmpeg -y` で既定の出力名を無警告で上書きし、途中失敗で壊れた出力が残る | 修復ツールの出力は都度確認して使う運用。trigger: 上書きで成果物を失った実例が出たとき |
| `bin/lgtm.sh` | 元画像を一時ファイルなしで上書きする | 使い捨ての画像加工で、元画像は通常別に残っている |
| `bin/skill-eval` | `claude plugin eval` に壁時計の上限が無い (費用上限のみ) | hang するかは未確認 |

監査で問題無しとしたもの: go_autobuild.zsh (起動時の指紋は glogx で約 7 ms、pro-con で約 14 ms。どちらも再帰 glob が律速で、
安全側の設計 (mtime の順序比較に戻さない) を崩さずに削れる幅が小さいので見送り) ほか。
動画系の本体 (`zshlib/*.zsh`) はこの監査の範囲外。

## 受け入れ条件

- [ ] 1〜3 を直し、それぞれの失敗を再現する入力で直ったことを確かめる (結果を下に)
- [ ] concat_movies の引き算の関数に回帰テストを足し、bc に戻す変異で red を見る

## 関連ファイル

- `bin/concat_movies` / `bin/restore_karabiner_config.sh` / `bin/backup_karabiner_config.sh` / `bin/ci-log`

## 進捗
