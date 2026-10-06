# 641 (refactor): zundamon-kaisetsu の dialogue_video.py を Go で書き直す

起票日: 2026-10-06

## 概要

`_claude/skills/zundamon-kaisetsu/scripts/dialogue_video.py`（合成・組み立て・mp4 化・kana など。約 830 行）を Go で書き直し、
`src/zundamon-kaisetsu/` の Go module と `bin/zundamon-kaisetsu` のラッパーに置き換える。置き換えが済んだら Python 版は消す
（2 実装を並べて保守しない。ユーザー決定 2026-10-06）。

- 対象外: `scripts/psd_faces.py`（立ち絵の PSD 書き出し）。psd-tools に相当する Go のライブラリが無いので Python のまま残す
  （ユーザー決定 2026-10-06）
- 置き場所: dotfiles/src/ の Go module（ユーザー決定 2026-10-06）。src/README.md の 3 点セット（Makefile の lint / test・go.mod・
  `.github/workflows/src_zundamon-kaisetsu.yml`）と `.golangci.yml` を揃える

## 守る不変条件（Python 版との互換）

1. **合成のキャッシュの鍵が Python 版と 1 バイトも違わない**。鍵は `json.dumps(params, sort_keys=True, ensure_ascii=False)` の
   sha256 の先頭 16 桁。Go の encoding/json は `1.0` を `1` と書き、`<` `>` `&` と U+2028/2029 をエスケープするので、Python の
   直列化（float の repr・区切りの `", "` / `": "`・エスケープする文字）を自前で再現する。違うと既存の `<台本>.work/` の合成結果が
   全部使えなくなり、全行を合成し直す羽目になる
2. **build が作るプレイヤーのデータ（行の開始・終了秒、口の開き、フレームの状態の列、チャプター）が Python 版と同じ値**。
   Python の `round()`（偶数丸め・小数 3 桁は正確な 2 進値からの丸め）と `math.ceil` / `int()` の切り捨てを再現する
3. 台本の検証（`load_script`）の受け入れ / 拒否が同じ。拒否の文言は同じ内容を保つ
4. サブコマンドとオプションは同じ（check / up / down / speakers / kana / synth / build、`--engine`・`VOICEVOX_URL`・`--force`・
   `-o`・`--format`・`--jobs`・`--bitrate`・`--script`・`--who`・`--style-id`）。check の `python` の行は Go 版では外す

## 設計

- **正解役は Python 版**（rules の 0-B）。消す前に、Python 版の関数を直接呼ぶ使い捨ての生成スクリプトで golden を作り、
  `src/zundamon-kaisetsu/testdata/` に置く: キャッシュの鍵（文字・数値の境界を含む入力の組）、`mouth_track`、`spoken_text`、
  台本の検証の拒否、build のデータ（合成済みの wav と query を fixture にして assemble を通した JSON）。Python 版を消した後も
  回帰テストとして残る
- **E2E の突き合わせ**: 実際に作った台本（87 行・合成済み）で、Python 版と Go 版の HTML の埋め込みデータ（JSON として比較）と
  連結した音声が同じであること、mp4 の状態数が同じであることを確かめる
- **テンプレート `templates/player.html` は skill に置いたまま、実行時に読む**（当初は Go module に移して `go:embed` する案だったが、
  設計レビュー P1-1 で撤回: `bin/lib/go_autobuild.zsh` の再ビルドの判定は .go と go.mod / go.sum しか見ないので、焼き込むと
  テンプレートだけを直したときに古いバイナリが古い見た目で作り続ける。既存の pro-con も `go:embed` の .md で同じ穴を持つが、今回は
  共有の仕組みには手を入れない）
- skill のディレクトリ（テンプレートと立ち絵の既定の置き場）は、`bin/zundamon-kaisetsu` が環境変数 `ZUNDAMON_KAISETSU_SKILL_DIR` で渡す。
  台本を読むコマンド（synth / build / kana --script）は、この変数が無ければ止める（黙って続けると、立ち絵の既定の置き場を見ずに
  丸アバターの動画ができる。設計レビュー P2-10）
- `os.getloadavg` は Go の標準に無いので、macOS は `sysctl -n vm.loadavg`、Linux は `/proc/loadavg` を読む。読めなければ待たずに撮る旨を出す
- SKILL.md / README.md の手順・ファイル一覧を Go 版に書き換える（`$DV` の呼び方、`templates/` の移動）

## Python 版と意図的に変えたこと

- **文字列でない `text` / `read` を拒否する**。Python 版は `null` や数を `str()` で通し、キャッシュの鍵には文字列でないまま入れていた
  （Go 版で同じ鍵を作れない）。黙って別の鍵にするより、検証で止める（設計レビュー P2-5）
- check の `python` の行を外した
- 数値の文字列で、Python の `float()` / `int()` だけが受け付ける形（`"1_0"`、全角数字）は受け付けない。JSON の `NaN` / `Infinity` の
  リテラルも Go の JSON は読めない。どちらも台本に書く理由が無いので、合わせない
- Python 版でトレースバックで落ちていた入力（最上位が配列の台本・オブジェクトでない行など）は、理由つきの rc=1 で止める

## 設計レビュー（2026-10-06、opus の反証レビュー）の対応

- 採用: P1-1 テンプレートの焼き込み（上記）/ P1-2 テンプレートを先に移して Python 版の build を壊していた（移動を戻した）/
  P1-3 引数の解析を argparse と同じ意味に（位置引数とオプションの混在・長いオプションの略記・`--engine` はサブコマンドの前だけ・
  解析の誤りは rc=2）/ P1-4 Python 3.12 以降の `sum()` の Neumaier の補正（`pySum`。golden `sum.json` で固定）/
  P2-6 golden の fixture の偏り（wav の長さと lead_in・gap・pause_after を半端な値にして丸めを効かせる、1 行目をチャプター無しにする、
  1 人だけの台本 `solo.json` を足す、`face_not_exported` が本当に拒否されるよう書き出した表情を一部にする、丸めの golden `round.json`）/
  P2-7 timeout で kill した後に孫プロセスがパイプを持っていても待ち続けない（`cmd.WaitDelay`）/
  P2-8 Ctrl-C でも一時ディレクトリを消す（`onInterrupt`）/ P2-9 audio_query の結果は map のまま運ぶ / P2-11 台本のパスは symlink を
  解決する（`resolvePath`）/ P2-12 `ZSH_SYNTAX_FILES` への登録 / P3 の `VOICEVOX_URL=""`・Path.stem・文字数での切り出し・
  並列撮影の失敗はグループ順・mp4 の total を丸めた duration から作る
- 不採用: P3 の `--jobs` / `--bitrate` の Unicode の数字（書く理由が無い）。golden の生成スクリプトを残す案（Python 版を消すので
  動かなくなる。今後のケースは Go のテストとして足す）

## 進捗

- [x] golden を Python 版から作る（`src/zundamon-kaisetsu/testdata/`: キャッシュの鍵 22 件・口の開き 15 件・spoken_text 7 件・
  台本の検証 21 件・build のデータ 2 台本・丸め 20 件・sum 41 件。fixture の台本は中立な文で、顧客の資料の文は使っていない）
- [x] Go 版の実装とテスト（golden_test.go 7 本・cli_test.go 7 本。`make lint` 0 件、`go test -race` 緑）
  - テストで見つけた不具合: HTML に埋め込むデータの `<` のエスケープが抜け、`</script>` を含む字幕でデータが途中で切れていた（修正済み）
  - 変異検証（`mutate-verify`）10 本すべて想定のテストが red: 浮動小数の `.0` を外す / sum を素直な加算に / round を素朴な四捨五入に /
    `<` のエスケープを外す / U+2028 をエスケープする / チャプター番号を 1 ずらす / 登場しないキャラの声のクレジットも入れる /
    skill のディレクトリの確認を外す（初回は緑で、テストを案内の文言まで見る形に直して red）/ 文字列でない text を通す /
    位置引数で解析をやめる
- [x] E2E の突き合わせ: 87 行の台本（合成済み）で Python 版と Go 版を比べ、HTML の埋め込みデータ（音声・立ち絵・フレームの状態・行・
  チャプター）とテンプレート部分が完全に一致。mp4 も状態 349 種類・Chrome 18 回が一致し、全 18,189 フレームの framemd5 と音声の md5 が一致
- [x] bin ラッパー・Makefile・workflow・.golangci.yml（Go プロジェクト共通の検査と go_autobuild の検査は緑）
- [x] SKILL.md / README.md の書き換え、Python 版の削除
- [x] 敵対的レビュー（下節。4 周で打ち切り）
- [x] push 後の CI: 39fe8c02 で Lint の `test-go-project-lanes` が「go.sum が無い」で落ちた（手元では module の `make lint` / `make test` と
  Go プロジェクト共通の検査を個別に回しただけで、root の `make test-lint` を回していなかった。上の「Go プロジェクト共通の検査は緑」は
  その範囲での結果）。空の go.sum を置いて cff4d18d で修正し、手元で `make test-lint` の緑を確認。src/README.md の 3 点セットにも
  go.sum と `make test-lint` を書き足した。振り返りは issue 642

## 敵対的レビュー（2026-10-06、opus）の対応

- **1 周目**（壊す / 素通り の 2 観点）: 主な経路（合成のキャッシュの名前・wav・HTML のデータ・mp4 の全フレーム）は壊せなかった。
  採用: 整数の `-0` で鍵が違う / `-o=out` / `..` の直前の symlink / 余分な閉じ括弧の受け入れ / 不正な UTF-8 / `-h` の優先 /
  空白を含む値 / 奇数長の data / 拡張形式の float / Python 3.14 の Path.stem / 親だけに SIGINT が届いたときの子 / 出力を最終パスへ
  直に書く（両版共通。一時ファイルに書いてから置き換える形にした）/ 素通りの穴（立ち絵の既定の置き場・ffconcat・「通常」の埋め込み・
  synth の書き込み・無声化母音・境界ちょうど・FMA）。FMA は golden（合成 query の無声化母音 0.95 秒）で実際に再現した:
  arm64 で `acc += s.d * ratio` が FMA にまとめられ、境目がフレームの中点と重なると口の開きが 1 フレーム反転する
- **2 周目**（修正差分）: 採用: 中断の後始末の順序が逆（子を止める前に一時ディレクトリを消す）/ rc=130 で普通に終えて bash のループが
  止まらない / `-o` の相対パスを字面で畳む / 空白を含む `--output=…` を位置引数と見なす / dangling symlink / 出力先の symlink と
  パーミッション / `.part` の名前の衝突 / GUID の先頭 2 バイトだけの確認 / 到達しない分岐。中断は「後始末の登録表」をやめ、
  appCtx の取り消し + 普通の defer + 同じ signal での死に直し、に作り直した
- **3 周目**（作り直しの差分）: 中断の主な経路は壊せなかった（18 通りの時点で signal を送り、一時ファイル・`.part`・孤児プロセスは 0）。
  採用: サブコマンドより前の未知のオプションを黙って捨てる / 起動時に無視されていた SIGINT・SIGHUP の無視を解く（nohup が効かない）/
  symlink のループで指数的に時間を食う（Python の realpath と同じ seen 方式に）/ umask を無視する / 読み取り専用の既存の出力を置き換える /
  中断の文言 / `--help` の略記・先頭の `--`・負の数・kana の飛び飛びの位置引数 / テストの穴（子の起動前に取り消していた・子が止まった
  ことを見ていなかった・main と signal の結合テストが無かった）
- **4 周目**（3 周目の修正の差分）: パスの解決はランダムな 3,300 本の木 × 30 問で Python 3.14 の realpath と突き合わせて不一致 0。
  採用: 読み取り専用の扱いを入れたせいで synth のキャッシュが Python 版より悪くなった（Python 版のキャッシュは「一時ファイルに書いて
  rename」なので、読み取り専用でも置き換え、パーミッションは umask から決まる。出力（write_text の意味）と置き換え方を分けた）/
  kana でオプションの後ろの `--` に続く位置引数を受け入れる / 本文を読む途中の中断の文言。
  **打ち切りの理由**: 4 周目の修正は、既存の書き込みを Python 版の 2 つの意味に分けたことと、既存の数え方を `--` の後ろにも当てたことだけで、
  新しい判定の仕組みを足していない。どちらもテストと変異（red を確認）で直接確かめた
- 記録に留めたもの（直さない）: RIFF のサイズ欄が実際より小さい wav を Go だけ受け入れる（VOICEVOX は出さない）/ `--jobs` などの
  Unicode の数字・`"1_0"` の数値 / 出力のハードリンクが切れる（rename のため。出力にハードリンクを張る運用は想定しない）/
  `synth --force` を query と wav の書き込みの間で中断すると新旧が組になる（Python 版と同じ。退行ではない）/ dict を str にしたときの
  キーの順 / 全角数字の負の数・短いオプションをまとめた `-hh`・サブコマンドより前の未知のオプションと後ろの `--help` の優先・
  エラー文だけの差（argparse との細部の差。書く理由が無い）/ 末尾が `--` だけの kana / build を Python は拒否し Go は通す / headless Chrome が `$TMPDIR` に残す一時ディレクトリ（以前からの Chrome の挙動）
- 変異検証は修正のたびに当て、合計 37 本がすべて想定のテストで red（1 本目で緑だったもの・当て方を誤ったものは、テストを強めるか
  当て直して red を確認）
