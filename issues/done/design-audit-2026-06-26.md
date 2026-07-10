# 設計監査 (2026-06-26)

> **対応完了 (2026-07-11)**: 確定 4 件すべて対応済み (ユーザーの明示依頼を trigger として着手)。
> #1=9ae7ec9 (_ffprobe_helpers.zsh 新設、~30 箇所移行。複数行取得の acodec_names は対象外のまま) /
> #2=c049050 (__concat_resolve_sequence 抽出、ロジック無変更の移動) /
> #3=82dfed5 + 34fbbe8 (_reload_then_call 集約 + repair/repair_mp4 対応。repair_mp4 の直接
> 呼び出しが一時 regression し codex review が検出 → 修正済み) /
> #4=72dee45 (_AV1IFY_DENOISE_PRESETS 一元化)。
> 全体 codex review 済み (指摘は上記 P2 の 1 件のみ、対応済み)。make test-zshrc 緑。

調査日: 2026-06-26
調査方法: 直接実行 — Workflow で 4 タイプ (duplication / responsibility / design / polymorphism) を並列調査し、各 finding をアドバーサリアル検証 (意図的設計 / false-positive / 既存 issue 重複 / 「分割しても複雑性が下がらない」を除外)。計 12 エージェント。
対象: `zshlib/` `scripts/` `_zshrc` `_claude/hooks/` (vendor 除く)
判定方針: `_claude/rules/verify-design-intent-before-refactor.md` に従い、行数ではなく「複雑性が実際に下がるか」で判定。

調査 8 件 → 確定 4 件 / 棄却 4 件。**確定はすべて P3**（今すぐ着手すべきものは無く、該当箇所を次に触る際に検討する watch-item）。

---

## 確定した課題 (4 件、すべて P3)

### 1. [duplication] ffprobe 単一フィールド取得の定型句が ~28 箇所にコピペ
- **ファイル**: `zshlib/_av1ify_encode.zsh`(167,236,273,535-539,598-606,692) / `_av1ify_postcheck.zsh`(72,80,143-144,198-199,218-219,233-234,275) / `_video_health.zsh`(22,31,112) / `_validate_mp4.zsh`(37,43,49) / `_repair_mp4.zsh`(92,97,134,139) / `_repair_mp4_timebase.zsh`(46,78)
- **内容**: `ffprobe -v error [-select_streams X] -show_entries Y -of default=nk=1:nw=1 -- "$f" 2>/dev/null | head -n1` という単一フィールド取得が、select/entries の 2 トークンだけ差し替えて約 28 箇所に手書き反復。`-v error`・`-of default=nk=1:nw=1`・`2>/dev/null`・`| head -n1` の 4 定型を毎回手書きしており typo の温床。`_concat_helpers.zsh:303-331` は既に `__concat_get_*` で同種を named-probe 集約済みで、av1ify/postcheck/repair/video_health 系だけ取り残されている。
- **対応 (trigger 待ち)**: `__ff_stream_field <file> <select> <entries>` / `__ff_format_field <file> <entries>` の 2 ヘルパー（中身は既存と同一）を新規 `zshlib/_ffprobe_helpers.zsh` に置き、各 source ツリー (`_av1ify.zsh` / `_concat.zsh` / `_validate_mp4.zsh` / `_repair.zsh`) から source。`-of csv=p=0`・`-read_intervals` 付き・JSON 取得は形が違うので対象外、`default=nk=1:nw=1 | head -n1` の単一フィールドだけ移行。**着手前に codex review**。
- **検証メモ**: `_av1ify_encode.zsh:525-533` の「1 呼び出しに *統合* する案は mock 依存で見送り」コメントは「複数フィールドの統合」を禁じるもので、本件の「各呼び出しを薄く *ラップ*」とは別物（mock は arg 文字列 substring で分岐、1 フィールド 1 呼び出しを保つラッパは arg を変えず mock を壊さない）。行移動でなく idiom 一元化なので複雑性は下がる（効能は modest）。

### 2. [responsibility] concat() に結合パイプライン全責務が集中
- **ファイル**: `zshlib/_concat.zsh` の `concat`（L13-692、単一関数 ~680 行）
- **内容**: 純粋ヘルパーは `_concat_helpers.zsh` に切り出し済みだが、orchestration + ドメインロジックが本体に同居: オプション解析(88-116) / dispatch(119-156) / バリデーション+prefetch(162-207) / **連番解決の 3 段リトライ状態機械(216-362)** / 欠落検査(379-438) / codec+time_base 整合(440-515) / 出力名(520-545) / ffmpeg 結合+診断+検証+cleanup(597-663) / trash(678-689)。特に連番解決は `temp_numbers/temp_prefixes/temp_suffixes/retry_*` 等 ~9 個の中間 local を本体スコープに漏らしている。
- **対応 (trigger 待ち)**: 次に連番命名規則 or オプションを 1 つ足す trigger が来た時、連番解決ブロック(216-362)を `__concat_resolve_sequence`（入力 stems[]、出力 = numbers/common_prefix/first_suffix/use_stripped_stems/detected_common_suffix）へ抽出を最初に検討。codec/time_base(440-515)も純判定関数化の候補だが、色付きエラー出力(498-512)と交絡しており presentation 分離が要るので一段クリーンでない。着手前に `test_concat` 系カバレッジと callsite を確認。
- **検証メモ**: ヘッダ(7-9)・本体に「単一関数を意図」する根拠コメントは無し（intentional でない）。連番解決は薄い委譲でなく入出力契約の明確な状態機械で、抽出は「scope-visible local の削減＋名前付き契約化」= 複雑性削減に該当（行数分割ではない）。

### 3. [design] lazy-reload ラッパー idiom が 2 系統に分岐 + `_repair` だけ非対応
- **ファイル**: `_zshrc:604-631`(av1ify/concat/validate-mp4)、`_zshrc:612`(_repair)、`zshlib/_tmux_session.zsh:161-162`(t/tt)
- **内容**: 「lib 編集を再起動なしで次回反映」という同一目的に 2 idiom が併存。(1) av1ify/concat/validate は `local _saved=${functions[X]}; source; X "$@"; functions[X]=$_saved` を **3 回コピペ**（lib が公開名と同名で関数を再定義するため自己上書きの save/restore が必須＝stateful な実ロジックの重複）。(2) t/tt は公開名≠実体名(`_t_impl`)で source 冪等＝復元不要（`_tmux_session.zsh:9-11,157-160` に意図明記）。(3) `_repair` は eager source のみで reload ラッパー無し、理由コメントも無し。
- **対応 (trigger 待ち)**: 3 コピペを `_reload_then_call <func> <lib>` 的ヘルパーに集約すれば重複が消え `_repair` も 1 行で揃う。t/tt は別 idiom で正当に動くので統合しない。次に動画系コマンドを足す時が自然な trigger。**今すぐは `_zshrc:612` に「なぜ repair だけ reload 非対応か」をコメントで残すだけでも食い違いが消える**（[`pending-issue-rationale-in-code.md`] 準拠）。
- **検証メモ**: 自己上書きを戻す save/restore は薄い委譲でなく state を持つ重複なのでヘルパー化で複雑性が下がる。意図コメントは「目的」のみで「なぜ 3 コピペ / repair 除外か」の rationale は無い。

### 4. [polymorphism] denoise レベルのマッピングが 3 箇所に分散
- **ファイル**: `zshlib/_av1ify_encode.zsh:201-217`(`__av1ify_decide_denoise` dispatch) / `zshlib/_av1ify.zsh:360-362`(検証 case) / `zshlib/_av1ify.zsh:447-449`(help テキスト)
- **内容**: denoise レベル(light/medium/strong)の振り分けが重複。検証 case が有効集合 `light|medium|strong` を列挙、dispatch case が各レベルを hqdn3d 値(2:2:3:3 / 4:4:6:6 / 6:6:9:9)と命名タグ(dn1/dn2/dn3)へ写像、help が値を再掲。dispatch の各 branch は 2 変数代入 + 同型 print のみで branch 固有ロジック無し。
- **対応 (trigger 待ち)**: 連想配列 1 個（例 `_AV1IFY_DENOISE_PRESETS=([light]='hqdn3d=2:2:3:3 dn1' …)`）に集約し、検証は `(( ${+_AV1IFY_DENOISE_PRESETS[$x]} ))`、dispatch はキー lookup + 共通 print に。有効集合と値が単一の真実源になる。値は 3 固定で変更頻度が低いので、次に denoise を触る変更（レベル追加・値調整）の trigger 待ち。help はドキュメントなので残してよい（配列をコメント参照させると乖離に気づきやすい）。
- **検証メモ**: コード側の真の重複は 2 箇所（検証 case と dispatch case の有効集合列挙）。テーブル化は列挙の重複除去＋有効集合の単一化で、薄い委譲の移動ではない。

---

## 却下した候補 (再評価不要 — 次回 audit のノイズ削減用)

検証フェーズで意図的設計 / low-value と判定。同じ指摘が再生成されたら以下を根拠に即棄却できる。

### A. [duplication] tmux-resurrect の mtime stale-lock 判定の重複 → low-value
- `scripts/tmux_resurrect_save.sh:94-97`(`tt_save_lock_older_than`) と `tmux_resurrect_debounced_save.sh:115-119`(インライン)の `find -mmin "-$((secs/60+1))"` が構造的に等価。**だが**: 2 スクリプトの lock 契約は正反対（save=「contention 時に skip 禁止・bounded-wait」/ debounced=「skip して譲る」）。save 側は PID+起動時刻フィンガープリント・owner 条件解放を持つ重い superset。共通化は異なる契約を結合し複雑性が上がる。重複は実質 1 行の find idiom のみで、両者は source を共有しておらず配線コストが削減量を上回る。`tmux_resurrect_save.sh:26-27` が「debounced と同方針」と認識済み。

### B. [responsibility] `__av1ify_one()` の責務集中 (~383 行) → low-value
- `zshlib/_av1ify_encode.zsh:428-811`。ファイルレベルの分解は良好。提案された「音声決定木(597-736)を `__av1ify_decide_audio_args` へ抽出」は依存を切らない: retry ループ(751-810)が `use_copy`/`audio_param_error` を制御信号として読み、L777 で args_audio を自前再構築し `aac_ar/aac_ac/aac_bitrate_resolved/did_aac` を再利用する。抽出しても local が関数シグネチャ（戻り値グローバル）へ移動するだけで「消えない」。copy 失敗→AAC リトライは copy/aac 決定木と本質的に結合した domain coupling。ffprobe 項目別呼び出し(534-561)は `527-533` コメントで意図明記済み。

### C. [design] resurrect 保存パイプラインの状態分散 → intentional-design
- 状態が 4 境界（debounce スクリプト / save wrapper / `_tmux.conf` フック / `_tt_wait_for_restore`）に分散し tmux global option を read-modify-write、は事実。**だが** 中核前提「型でもテストでも強制されない暗黙契約」が誤り: 依存は全て該当コード直近にコメント文書化済み（`tmux_resurrect_debounced_save.sh:33-39` 他、[`pending-issue-rationale-in-code.md`] の実践）、契約は `test_debounced_save.sh`/`test_resurrect_save_lock.sh` で回帰テスト済み。save 判定の「二重持ち」も責務分離（debounce=保存可否 / wrapper=直列化）で明記済み。3 実行境界が tmux 唯一の共有機構 global option で協調するのは domain 由来の本質的複雑さ。

### D. [design] DOTFILES_DIR 解決方法の食い違い → intentional-design
- `_tmux.conf`(`${DOTFILES_DIR:-$HOME/dotfiles}`) / `_zshrc`($HOME/dotfiles 直書き) / `_tmux_session.zsh:18`(%x 自己解決) で 3 方式、は事実。**だが** 各機構は実行コンテキストごとに必然的に異なる解決法を選んでいる（tmux hook 文字列に自己位置アンカー無→`${VAR:-default}`、sourced lib→%x、standalone script→$SCRIPT_DIR、いずれもコメント明記済み）。方針「本番は $HOME/dotfiles 固定、DOTFILES_DIR は test seam」は `docs/tmux-plugins.md:171` と `setup.sh` で既に確定。統一は複雑性を下げない。
