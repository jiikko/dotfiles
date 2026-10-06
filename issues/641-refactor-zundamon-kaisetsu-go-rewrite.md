# 641 (refactor): zundamon-kaisetsu の dialogue_video.py を Go で書き直す

> 🚨 **担当中: zundamon-kaisetsu の Go 化セッション**（2026-10-06〜）

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
- **テンプレート `templates/player.html` は Go module に移し、`go:embed` で埋め込む**（バイナリだけで HTML / mp4 を作れるようにする）
- 立ち絵の既定の置き場（skill の `assets/zundamon-kaisetsu/faces`）は、`bin/zundamon-kaisetsu` が skill のディレクトリを環境変数で渡す
- `os.getloadavg` は Go の標準に無いので、macOS は `sysctl -n vm.loadavg`、Linux は `/proc/loadavg` を読む。読めなければ待たずに撮る旨を出す
- SKILL.md / README.md の手順・ファイル一覧を Go 版に書き換える（`$DV` の呼び方、`templates/` の移動）

## 進捗

- [ ] golden を Python 版から作る
- [ ] Go 版の実装とテスト（golden の突き合わせ・変異検証）
- [ ] E2E の突き合わせ（HTML のデータ・音声・mp4）
- [ ] bin ラッパー・Makefile・workflow・.golangci.yml
- [ ] SKILL.md / README.md の書き換え、Python 版の削除
- [ ] 敵対的レビュー
