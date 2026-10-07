# zundamon-kaisetsu

四国めたん (解説役) とずんだもん (聞き手) の掛け合いで、資料を解説する動画を作る Claude Code の skill。
台本 (JSON) から VOICEVOX でセリフを合成し、字幕・立ち絵 (場面に合わせた表情)・口パク付きの
**HTML プレイヤー 1 ファイル**か **mp4 (720p)**、またはその両方に書き出す。

使い方・台本の書き方・仕組みは [`SKILL.md`](SKILL.md)。この README は初めて使う環境の準備だけを書く。

## 構成

パスはすべてこのディレクトリからの相対。

| パス | 中身 |
|---|---|
| `SKILL.md` | skill の本体 (手順・台本の書き方・表情の語彙) |
| `scripts/psd_faces.py` | 立ち絵の PSD から表情 × 口 3 段階の画像を書き出す (psd-tools を使う) |
| `faces/<キャラ>.json` | 表情ごとに使う PSD のレイヤーの定義 |
| `templates/player.html` | HTML プレイヤー (mp4 の絵もこれで描く。zundamon-kaisetsu が実行時に読む) |
| `examples/script.json` | 台本の見本 |
| `assets/zundamon-kaisetsu/` | 立ち絵の置き場 (`psd/` に元の素材、`faces/<キャラ>/` に書き出した表情)。**同梱しない** (下の Setup で用意する) |

合成・組み立ての本体は Go 製のコマンド `zundamon-kaisetsu` (`check` / `up` / `down` / `speakers` / `kana` (文か台本の全行の読み) /
`synth` / `build`)。ソースは dotfiles の `src/zundamon-kaisetsu/`、入口は dotfiles の `bin/zundamon-kaisetsu` で、
このディレクトリ (テンプレートと立ち絵の既定の置き場) を環境変数 `ZUNDAMON_KAISETSU_SKILL_DIR` で渡す。

## Setup

コマンドは**このディレクトリで**実行する (パスはここからの相対)。`zundamon-kaisetsu` は dotfiles の `bin/` を PATH に入れて使う
(初回の起動で Go のツールチェーンがビルドする)。

### 1. コマンド

```sh
zundamon-kaisetsu check
```

足りないものを `NG` で示す。目安:

| 用途 | 必要なもの |
|---|---|
| 合成・HTML | Go (`zundamon-kaisetsu` のビルド)、ffmpeg (無ければ macOS の afconvert) |
| VOICEVOX エンジン | Apple の `container` か `docker` (どちらも無ければ VOICEVOX のデスクトップアプリ) |
| mp4 | ffmpeg (H.264) と Google Chrome か Chromium (`CHROME` 環境変数で場所を指定できる) |
| 立ち絵の書き出し | `uv` (psd-tools を一時的に入れて動かす。`scripts/psd_faces.py` は Python のまま) |

### 2. VOICEVOX エンジン

```sh
zundamon-kaisetsu check  # container か docker が使えれば、エンジンが止まっていても OK
```

- `synth` / `kana` / `speakers` は、エンジンが止まっていれば自分で起動し (container を優先し、無ければ docker)、見張りのプロセスを残す。
  見張りは最後にエンジンを使ってから 10 分で止めて終わる (印・最後に使った時刻・見張りのログは `~/Library/Caches/zundamon-kaisetsu/`)
- 起動したままにしたいときだけ `zundamon-kaisetsu up` で起動し、`zundamon-kaisetsu down` で止める (`up` で起動したものは自動では止めない)

- イメージは `voicevox/voicevox_engine:cpu-latest` (約 3.7GB。初回の取得に数分かかる)
- `container` は初回に `container system start` が要る (Linux カーネルを入れるか聞かれる)
- 声の利用規約: キャラクターごとに VOICEVOX 公式サイトで確認する。動画には `VOICEVOX:四国めたん` `VOICEVOX:ずんだもん` の
  クレジットが必須 (HTML はクレジット欄に build が自動で入れる。mp4 の映像には入らないので、概要欄などに自分で書く)

### 3. 立ち絵

立ち絵は坂本アヒルさんが pixiv で配布している素材を使う。

| キャラ | 配布ページ | 動作を確かめた版 |
|---|---|---|
| ずんだもん | https://www.pixiv.net/artworks/92641351 | ずんだもん立ち絵素材 2.3 |
| 四国めたん | https://www.pixiv.net/artworks/92641379 | 四国めたん立ち絵素材 2.1 |

1. 各ページの案内に従って素材の zip を入手して展開する
2. 同梱の `readme.txt` と公式ガイドライン (https://zunko.jp/guideline.html) を読む。動画への利用・改変は可、
   クレジットは任意 (build は HTML のクレジット欄に「立ち絵: 坂本アヒル」を自動で入れる。mp4 には入らない)。**素材を公開リポジトリに置かない** (再配布になる)
3. 展開したフォルダを `assets/zundamon-kaisetsu/psd/` に置く

   ```
   assets/zundamon-kaisetsu/psd/
   ├── ずんだもん立ち絵素材2.3/ずんだもん立ち絵素材2.3.psd (+ readme.txt)
   └── 四国めたん立ち絵素材2.1/四国めたん立ち絵素材2.1.psd (+ readme.txt)
   ```

4. 表情を書き出す (1 キャラ 1〜2 分)

   ```sh
   A=assets/zundamon-kaisetsu
   uv run --with psd-tools python scripts/psd_faces.py \
     "$A/psd/ずんだもん立ち絵素材2.3/ずんだもん立ち絵素材2.3.psd" faces/zundamon.json "$A/faces/zundamon" --preview zundamon.png
   uv run --with psd-tools python scripts/psd_faces.py \
     "$A/psd/四国めたん立ち絵素材2.1/四国めたん立ち絵素材2.1.psd" faces/metan.json "$A/faces/metan" --preview metan.png
   ```

   `--preview` の画像に全表情 × 口 3 段階が並ぶので、目で確かめる。
   上の表と違う版の素材ではレイヤー名が変わっていることがあり、そのときは無いレイヤーを名指しして止まる。
   `faces/<キャラ>.json` のレイヤー名を素材に合わせて直す。

立ち絵を用意しなくても動く (名前入りの丸アバターで出る)。

**dotfiles で使う場合**: `assets/` は非公開リポジトリ `jiikko/assets` のサブモジュールで、上の素材と書き出した表情が入っている。
権限のあるアカウントなら `git submodule update --init` で取得すれば 3. は要らない。

### 4. 動作確認

```sh
mkdir -p /tmp/zk && cp examples/script.json /tmp/zk/
zundamon-kaisetsu synth /tmp/zk/script.json   # エンジンを自動で起動する (使わなくなって 10 分で止まる)
zundamon-kaisetsu build /tmp/zk/script.json -o /tmp/zk/out --format both   # out.html と out.mp4 (エンジンは使わない)
```
