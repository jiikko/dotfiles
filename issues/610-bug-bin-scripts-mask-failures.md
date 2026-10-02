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
| `bin/mutate-verify` | ~~`--verify` の run に timeout が無い~~ → **2026-10-02 に直した** (下の進捗) | 613 の作業で hang する変異に実際に当たり、手で止めた (trigger が成立) |
| `bin/codex-fanout` | watchdog が TERM を 1 回送るだけで KILL へ昇格しない | 推測 (未再現)。codex を使う作業でのみ発火。trigger: codex-fanout が timeout 後も返らなかったとき |
| `bin/concat_movies` | 音声の無いクリップを混ぜると、音声の欠けた連結を「完了」として出す | 実害 (音ズレの程度) が未検証。trigger: 音声なしの素材を連結する用途が出たとき |
| `bin/repair_avi_vorbis_audio.sh` / `bin/repair-avcc-avi` | `ffmpeg -y` で既定の出力名を無警告で上書きし、途中失敗で壊れた出力が残る | 修復ツールの出力は都度確認して使う運用。trigger: 上書きで成果物を失った実例が出たとき |
| `bin/lgtm.sh` | 元画像を一時ファイルなしで上書きする | 使い捨ての画像加工で、元画像は通常別に残っている |
| `bin/skill-eval` | `claude plugin eval` に壁時計の上限が無い (費用上限のみ) | hang するかは未確認 |

監査で問題無しとしたもの: go_autobuild.zsh (起動時の指紋は glogx で約 7 ms、pro-con で約 14 ms。どちらも再帰 glob が律速で、
安全側の設計 (mtime の順序比較に戻さない) を崩さずに削れる幅が小さいので見送り) ほか。
動画系の本体 (`zshlib/*.zsh`) はこの監査の範囲外。

## 受け入れ条件

- [x] 1〜3 を直し、それぞれの失敗を再現する入力で直ったことを確かめる (結果を下に)
- [x] concat_movies の引き算の関数に回帰テストを足し、bc に戻す変異で red を見る

## 関連ファイル

- `bin/concat_movies` / `bin/restore_karabiner_config.sh` / `bin/backup_karabiner_config.sh` / `bin/ci-log`

## 進捗

- 2026-10-02 「fix(bin): concat_movies の秒の引き算・karabiner の失敗・ci-log の gh の失敗を成功に見せない (610)」
  - 1: `sub_seconds` (数値の検査 + awk の `%.3f`) に寄せ、bc を依存から外した。stream の duration が `N/A` / 空なら
    `format=duration` へ落とす。フェードアウトより短い長さは非 0 で止める (以前は負の st を ffmpeg に渡していた。ffmpeg が受けたかは未実測)
  - 2: restore は cp / jq / mv / 記録のどれが失敗しても非 0 で止め、`.tmp` を消す。「restore 済み」の記録は全段が成功した後だけに移した。
    backup は cp と記録の失敗で止める
  - 3: 2 か所の `gh run list` の出力を変数へ取り、失敗なら「CI の状態は分かりません」で rc=1。`mapfile` を `while read` にした
  - テスト (どれも偽の ffmpeg / ffprobe / gh / jq、HOME の差し替え): `tests/bin/test_concat_movies.sh` 6 件 /
    `test_karabiner_config_scripts.sh` 8 件 / `test_ci_log.sh` 10 件 (bash 5 と /bin/bash 3.2 の両方)
  - 変異 (mutate-verify、すべて想定の検査で red): bc に戻す → 「1 未満の開始位置」/ N/A の落とし先を外す → 「N/A の落とし先」/
    restore の jq 失敗時の exit を `:` に → 「jq の失敗」/ backup の cp の検査を外す → 「backup のコピーの失敗」/
    ci-log を旧実装に戻す → 「1 本目の gh の失敗」/ 2 本目の `|| gh_failed` を外す → 「2 本目」/ restore の記録の失敗の exit を `:` に → 「記録の失敗」
  - 敵対レビュー (sonnet、read-only、1 周): P2 1 件 = restore が記録の失敗 (mkdir / 書き込み) を握り潰して rc=0 (再現あり) → 直した (変異で red を確認)。
    P3: CR 付きの長さ (`5.0\r`) を拒否する = macOS の ffprobe は CR を出さないので記録のみ / フェードアウトより短いと止まる = 意図どおり /
    ci-log の HEAD より前の赤の走査が未検査 = テストを足した。2 周目は §7 の例外 (判定ロジックを新設せず、各修正を変異で直接確認) で打ち切り
  - 残り: 上の「記録だけにするもの」の表 (mutate-verify の timeout ほか)。`ci-log <run-id>` の経路も `gh run view ... 2>/dev/null || true` で
    gh の失敗を「失敗した job はありません」にしうるが、成功した run に `--log-failed` を当てたときの gh の rc を実測していないので触っていない
  - `make test` (worktree): 新しい 3 本と tmux shim の 2 本は [ok]。全体は rc=2 で、落ちたのは今回の変更と無関係な 2 本:
    test-yaml (`src/tuikit/.golangci.yml:56` の line-length。2fa1692e で入った。持ち主のセッションへ連絡済み) と
    `tests/claude/test_deny_piped_push_then_destroy.sh` (9MB 入力で timeout rc=124。ロードアベレージ 27 の最中だった。単独の再実行は rc=0)
- 2026-10-02 「feat(mutate-verify): 各 run に --timeout を付け、超えたらプロセスグループごと止めて rc=10 を返す (610)」 (ユーザーの依頼)
  - 4 つの run (構文検査・baseline・`--apply`・変異後の検証) を自分のプロセスグループで背景に起こして `wait` で待ち、上限は別に起こした
    見張りが数える。時間切れなら印を置いてグループを TERM → 5 秒後に KILL。既定 1800 秒 / `MUTATE_VERIFY_TIMEOUT` / 0 で無制限。新しい rc=10
    (`mutate-verify-list` の表にも足した)。標準入力は /dev/null になる (前景でないグループが端末を読むと SIGTTIN で止まるため。冒頭に書いた)
  - 中断 (TERM / INT) の trap が、実行中の run のグループと見張りを止めるようにした。旧実装は「実行中の `--verify` が終わるまで trap が走らない」
  - 🚨 完了を 0.1 秒刻みのポーリングで待つ最初の版は、既存の 42 ケースが 34 s → 58 s に遅くなった。`wait` + 見張りに変えて 34 s に戻した
  - テスト: 44 (変異が hang し、TERM を無視する子が残る形 → rc=10・段の名前・上限内・子が残らない・全体のログ) / 45 (baseline の hang を
    環境変数で → rc=10) / 46 (`--timeout 2s` → rc=2、`--timeout 0` の正常系 → rc=0)。42 (中断) は「ゲートを開ける前に子が止まっていること」を見る形に組み替えた
    (旧実装は trap が子の終了を待つのでゲートが要った)。45 ケース 47 s
  - 変異 (外側は変異前の bin/mutate-verify に --timeout 300 を付けて回した。すべて想定の検査で red): グループを KILL しない → 「TERM を無視する子が
    時間切れの後も残った」/ 見張りが印を置かない → 「hang する変異が rc=7」/ 中断の trap で run を止めない → 「中断しても変異の検証の子が残った」
- 2026-10-02 敵対レビュー 1 周目 (opus): 時間切れが緑 (0 / 6) に化ける経路は無し。直したもの:
  - P2 (再現): run を別のプロセスグループにしたので、`stty tostop` の端末で `--apply` の出力が SIGTTOU で止まり上限まで待って rc=10 /
    Ctrl-Z で run が止まらない → **同じグループの背景の子に戻し**、時間切れ・中断のときは run の**子孫の木**を凍らせてから集め直して
    TERM → 5 秒 → KILL で止める。別グループへ抜けた孫 (setsid / `set -m`) も親が生きていれば止まる (P2-3)。見張りのプロセスは廃止
    (「見張りを止める」と「見張りが撃つ」の競争を避ける)。完了待ちは 10 ms から 200 ms へ伸ばす刻み。pty の上で `stty tostop` にして確かめた:
    新しい版は `--apply` の出力が出て終わる / 前の版は rc=10 (再現)。Ctrl-Z は未実測
  - P3: テスト 45 の印は `bash -c 'sleep 60'` が exec に置き換わって引数から消えていた (何も検査していなかった) → `; :` を足した
  - P3: 桁が多すぎる `--timeout` が黙って無制限になった → 7 桁までに制限
  - 記録のみ: 検証コマンドが `trap 'kill 0' EXIT` で自分のグループを撃つと rc ファイルが書かれず rc=125 → 失敗扱い (旧実装では道具ごと死んでいたので悪化ではない)
  - 1 回の起動の増分は 約 0.1 s (同じ fixture を新旧交互に 3 回: 0.58〜0.77 s → 0.66〜0.68 s。ロードアベレージ 16〜20)
  - テスト 47 (別グループの孫も止める) を足して 46 ケース。変異 (すべて red): KILL しない / 時間切れの段を記録しない / 中断で止めない /
    子孫を辿らず根だけ止める (→ 47 の「別のプロセスグループへ抜けた孫が残った」)
