# 1036 (bug): dupscan の注入の hook が、重複を防ぐ印を $TMPDIR に 1 件ずつ置き、消さないので溜まる

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-06〜）

起票日: 2026-11-06

## 概要

`_claude/hooks/inject-dupscan-candidates-on-new-func.sh` は「同じ session × 名前 (型) には 1 回だけ注入する」ための印を、`$TMPDIR` に空ファイル (`claude-dupscan-inject-<session>-<名前>`) として名前ごとに作り、消さない。
注入のたびに 1 件ずつ増える。2026-11-05 の cleanup-tmp-dir では、3 日より古いものだけで 70 件以上が `$TMPDIR` に残っていた (tmpclean の plan の候補 661 件の内訳)。
`$TMPDIR` の件数が多いと起動時の削除が長引くとされる (`_claude/CLAUDE.md`「一時ファイルの配置」)。

## 対応方針

- 印を `${XDG_CACHE_HOME:-~/.cache}/claude-dupscan-inject.seen` の 1 ファイルに「session\t種類 (name / type)\t名前」の行で持つ。ファイルは増えない
- 64KB (`SEEN_MAX`) を超えたら、flock の下で古い半分を捨てる (`.tmp` に書いて `os.replace`)。追記も同じ lock の中で行い、詰め直しの間の他の session の追記を失わない
- 失うもの: 捨てた行の session × 名前は、もう 1 回注入されうる (1 行約 60 バイトなので、最低でも直近の約 540 件の印は残る)
- 消す処理 (掃除) を足さない形を選んだ: 掃除の機構は「どれが自分の印か」の判定を要し、`$TMPDIR` の他人のファイルに届く経路を作る

## 関連ファイル

- `_claude/hooks/inject-dupscan-candidates-on-new-func.sh` (`load_seen` / `mark_seen`)
- `tests/claude/test_inject_dupscan_candidates_on_new_func.sh`

## 進捗

- [x] 印を 1 ファイルの行へ移し、上限で詰め直す (WIP commit)
- [x] テスト: `$TMPDIR` に印のファイルを作らない / 上限を超えたら古い行から捨て新しい行は残す / 詰め直しの後も 2 回目を出さない。全体 ok=65 fail=0
- [x] 変異 3 本で red を確認 (mutate-verify): 印を `$TMPDIR` にも作る・詰め直しを外す・新しい半分でなく古い半分を残す
- [x] 敵対的レビュー (opus 1 本、P1 なし)。採否:
  - 直した: 途中で切れた行 (改行の無い末尾) に次の印が連結されて読めなくなる → 行の頭にも改行を付ける / 詰め直せない (cache dir が書けない・`.tmp` が dir) と黙って上限なく伸びる → stderr に出す (rc は 0 のまま) / 詰め直しのテストが過剰に捨てる変異を見逃す → 中ほどの行 (old1800) も見る。テスト ok=67、変異 3 本 (先頭の改行を外す・失敗を黙らせる・末尾 10 行だけ残す) で red
  - 採らなかった: 「半分にしても上限を超える巨大な 1 行なら空にする」— 行の頭の改行で、レビューが挙げた再現条件 (改行の無い 1 行への連結) は起きなくなる。残るのは改行を含む巨大な行が 2 本ある形だけで、書く経路が無いので推測の防御は足さない
  - 未確認: flock を外す変異を検出するテストが無い (P2)。並行に起こした hook が詰め直しの窓に重なるかは時間次第で、検出が確率的になる。確かめるなら、上限近くまで埋めた `.seen` に hook を 20 本並行で起こし、全 session の行が残るかを flock の有無で比べる
  - 記録のみ: 大小違いの名前 (`readCookies` / `ReadCookies`) が別の印になる (旧版は APFS の大小を区別しないファイル名で 1 つにまとまっていた。別の名前なので直りと読む) / 印を `/tmp` へ書く変異は `$TMPDIR` の検査を素通りする / 同じ session の同じ名前の Edit が 2 本並行すると 2 回出る (旧版の exists → open と同じ窓で、退行ではない)
- [x] 直した差分への 2 周目の敵対的レビュー (opus、P1 なし)。採否:
  - 直した: warn を stderr だけに出しても、rc=0 の hook の stderr はモデルにも画面にも出ない (P2。推測と明記されたが、届く経路が無い側に倒す) → 計測ログ `claude-dupscan-inject.log` にも「時刻 warn 理由」の 3 列の行を書く (`scripts/dupscan_usage_report.py` は 4〜5 列の行だけを数えるので集計は変わらない) / 詰め直し失敗のテストが、注入が止まる退行・追記の段の失敗を区別しない → 注入が出続けること・印が追記されたこと・計測ログの warn 行も見る。テスト ok=67、変異 2 本 (warn を stderr だけにする・warn の後に例外を投げて注入ごと黙る) で red
  - 記録のみ: 印のファイルを読めないときの warn の経路にはテストが無い / 書き込みの途中で切れた行 (`s1\tname\treadCook`) は、前方の別の名前の印として読まれうる。直す前の版 (ファイル名の印) には無かった形だが、書き込みの途中の失敗 (ENOSPC 等) でしか起きず、起きても名前 1 つの初回の注入が消えるだけなので受容する
- [x] 直した差分への 3 周目の敵対的レビュー (opus、P1・P2 なし)。採否:
  - 直した: 計測ログの行数の検査が warn 行も数え、注入の行の欠けを埋める (false green) → 5 列目が name / returns の行だけを数える / 集計が warn 行を数えないことを守る fixture が無い → `tests/scripts/test_dupscan_usage_report.sh` の注入ログに warn 行を足した
  - 再現せず: locale が UTF-8 でないと warn の書き込みが UnicodeEncodeError で注入ごと消える・集計が UnicodeDecodeError で落ちる — Python 3.15.0 は既定で UTF-8 mode (`sys.flags.utf8_mode=1`。`LC_ALL=en_US.ISO8859-1` でも `getpreferredencoding` は utf-8)。再提起するなら、UTF-8 mode が切れる環境 (`PYTHONUTF8=0`・3.14 以前の python3) で詰め直し失敗のケースを走らせ、注入が消えることを添える
  - 記録のみ: 詰め直しの失敗が続く間、1 回の起動で warn 行が最大 7 本出て間引きが無い (失敗が続くのは cache dir が壊れたときだけなので、件数で気づける方を採る)
- 既に `$TMPDIR` に残っている印は、cleanup-tmp-dir (tmpclean の tmpdir、3 日より古いもの) で片付く。この変更では消さない
