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
| `templates/player.html` | HTML プレイヤー (mp4 の絵もこれで描く。`zundamon-kaisetsu` コマンドが実行時に読む) |
| `examples/script.json` | 台本の見本 |
| `readings.json` | 読み間違えやすい語の辞書 (SKILL.md の手順で台本の `readings` に合流させる) |
| `improving.md` | 改善点を挙げるときの観点と測り方 (適宜更新する。今動いている改善は issue の epic 674) |
| `acronyms.json` | 1 文字ずつ読むのが正しい略語 (CPU・OS 等)。`kana --script --check` がこの語を「1 文字ずつ読んだ誤読」として警告しない |
| `assets/zundamon-kaisetsu/` | 立ち絵の置き場 (`psd/` に元の素材、`faces/<キャラ>/` に書き出した表情)。**同梱しない** (下の Setup で用意する) |

合成・組み立ての本体は Go 製のコマンド `zundamon-kaisetsu` (`check` / `up` / `down` / `speakers` / `kana` (文か台本の全行の読み。`--check` で読みの機械検査) /
`synth` / `build`)。ソースは dotfiles の `src/zundamon-kaisetsu/`、入口は dotfiles の `bin/zundamon-kaisetsu` で、
このディレクトリ (テンプレートと立ち絵の既定の置き場) を環境変数 `ZUNDAMON_KAISETSU_SKILL_DIR` で渡す。

## Setup

macOS で動作を確かめている (Linux は対象外)。Homebrew が入っている前提で書く。

### 0. 入手と配置

この skill は [jiikko/dotfiles](https://github.com/jiikko/dotfiles) (公開リポジトリ) の一部で、skill のディレクトリだけでは動かない。
コマンド `zundamon-kaisetsu` (Go 製) の入口・ビルドの仕組み・ソースと、この skill のディレクトリが要る。dotfiles 全体は要らないので、
git の sparse checkout で**必要なファイルだけ**を取り出す (約 100 ファイル・`.git` を含めて 10MB ほど)。次を同じシェルで続けて打つ:

```sh
brew install go ffmpeg uv node        # uv は立ち絵の書き出し、node は mermaid の図にだけ要る。mp4 を作るなら Google Chrome も入れる
D=~/src/zundamon-kaisetsu             # 置き場所は任意
mkdir -p "$(dirname "$D")" && git clone --filter=blob:none --no-checkout https://github.com/jiikko/dotfiles.git "$D"
git -C "$D" sparse-checkout set --no-cone \
  /bin/zundamon-kaisetsu /bin/lib/go_autobuild.zsh /src/zundamon-kaisetsu/ /src/proctree/ /_claude/skills/zundamon-kaisetsu/
git -C "$D" checkout master
mkdir -p ~/.claude/skills && ln -s "$D/_claude/skills/zundamon-kaisetsu" ~/.claude/skills/zundamon-kaisetsu   # Claude Code から skill が見える
mkdir -p ~/.local/bin && ln -s "$D/bin/zundamon-kaisetsu" ~/.local/bin/                                      # 入口だけを PATH の dir に置く
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc   # ~/.local/bin が PATH に無ければ。新しいシェルを開く
```

- 取り出すのは、入口 (`bin/zundamon-kaisetsu`。zsh)・ビルドの仕組み (`bin/lib/go_autobuild.zsh`)・本体 (`src/zundamon-kaisetsu/`)・
  依存する module (`src/proctree/`)・この skill の 5 つ。入口は symlink の先の実体の場所から skill と src を探すので、この並びのまま置く
- 新しいシェルで `command -v zundamon-kaisetsu` がパスを返せばよい
- 更新は `git -C "$D" pull` (取り出す範囲はそのまま)
- 初回の起動で Go がビルドする (数十秒〜数分。`zundamon-kaisetsu: building...` と出る。バイナリは `$D/src/zundamon-kaisetsu/` の中にできる)。
  要る Go は 1.25 以上 (`src/zundamon-kaisetsu/go.mod`) で、手元の Go が古くても、要る版 (約 90MB) を初回に自動で取りに行く (ネットワークが要る)
- `assets/` (立ち絵) は非公開のサブモジュールで、取れなくてよい (手順 3 で用意する。`git submodule` は打たない)

以下のコマンドは**この skill のディレクトリで**実行する (パスはここからの相対):

```sh
cd "$D/_claude/skills/zundamon-kaisetsu"
```

### 1. コマンドの確認

```sh
zundamon-kaisetsu check
```

行ごとに `OK` / `NG` と、`NG` なら次の手を出す (足りないものがあれば rc=1)。目安:

| 用途 | 必要なもの | 無いとき |
|---|---|---|
| 合成・HTML | Go (`zundamon-kaisetsu` のビルド)、ffmpeg (無ければ macOS の afconvert) | 必須 |
| VOICEVOX エンジン | Apple の `container` か `docker` (どちらも無ければ VOICEVOX のデスクトップアプリ) | 必須 (手順 2) |
| mp4 | ffmpeg (H.264) と Google Chrome か Chromium (`CHROME` 環境変数で場所を指定できる) | HTML だけ作るなら `NG` のままでよい |
| 立ち絵の書き出し | `uv` | 立ち絵を用意しないなら不要 |
| mermaid の図 (台本の `show` の `mermaid`) | Node の `npx` と Chrome (初回に版を固定した mermaid-cli を取りに行く) | 使わないなら不要。`check` は `npx` を見ないので、使うときに `npx --version` で確かめる |

### 2. VOICEVOX エンジン

エンジンは `synth` / `kana` / `speakers` が自分で起動する (container を優先し、無ければ docker)。使えるようにしておくのは次のどれか 1 つ:

- **Apple の `container`**: Apple シリコンの新しい macOS 向け (要件は公式で確かめる)。入れた後、初回に 1 回 `container system start` を打つ
  (Linux カーネルを入れるか聞かれる。非対話なら `--enable-kernel-install`)
- **docker**: Docker Desktop などを起動しておく
- **VOICEVOX のデスクトップアプリ**: どちらも無いとき。起動している間、エンジンが 127.0.0.1:50021 で待ち受ける

その後 `zundamon-kaisetsu check` のエンジンの行が `OK` (止まっていても「使うときに起動する」と出ればよい) になればよい。

- イメージは `voicevox/voicevox_engine:cpu-latest` (約 3.7GB)。初回の `synth` で取得するので数分止まる。先に取るなら
  `container image pull voicevox/voicevox_engine:cpu-latest` (docker なら `docker pull …`)
- 自動で起動したエンジンは、最後に使ってから 10 分で止まる (印・最後に使った時刻・見張りのログは `~/Library/Caches/zundamon-kaisetsu/`)。
  起動したままにしたいときだけ `zundamon-kaisetsu up` / `down`
- 声の利用規約: キャラクターごとに VOICEVOX 公式サイトで確認する。動画には `VOICEVOX:四国めたん` `VOICEVOX:ずんだもん` の
  クレジットが必須 (HTML はクレジット欄に build が自動で入れる。mp4 の映像には入らないので、概要欄などに自分で書く)

### 3. 立ち絵 (任意。後回しにするなら手順 4 へ)

立ち絵を用意しなくても動く (名前入りの丸アバターで出る)。用意するなら、坂本アヒルさんが pixiv で配布している素材を使う。

| キャラ | 配布ページ | 動作を確かめた版 |
|---|---|---|
| ずんだもん | https://www.pixiv.net/artworks/92641351 | ずんだもん立ち絵素材 2.3 |
| 四国めたん | https://www.pixiv.net/artworks/92641379 | 四国めたん立ち絵素材 2.1 |

1. 各ページの案内に従って素材の zip を入手して展開する
2. 同梱の `readme.txt` と公式ガイドライン (https://zunko.jp/guideline.html) を読む。動画への利用・改変は可、
   クレジットは任意 (build は HTML のクレジット欄に「立ち絵: 坂本アヒル」を自動で入れる。mp4 には入らない)。**素材を公開リポジトリに置かない** (再配布になる。
   下の置き場は clone の中のサブモジュールの場所で、commit・push しなければ公開されない)
3. 展開したフォルダを `assets/zundamon-kaisetsu/psd/` に置く (版が違うとフォルダ名・ファイル名が変わるので、4 のコマンドのパスを実物に合わせる)

   ```
   assets/zundamon-kaisetsu/psd/
   ├── ずんだもん立ち絵素材2.3/ずんだもん立ち絵素材2.3.psd (+ readme.txt)
   └── 四国めたん立ち絵素材2.1/四国めたん立ち絵素材2.1.psd (+ readme.txt)
   ```

4. 表情を書き出す (`uv` が要る。1 キャラ 1〜2 分)

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

(dotfiles の持ち主の環境だけ: `assets/` は非公開リポジトリ `jiikko/assets` のサブモジュールで、素材と書き出した表情が入っている。
権限のあるアカウントなら `git submodule update --init` で取得すれば 3 は要らない。別のユーザーは読み飛ばしてよい)

### 4. 動作確認

見本の台本 (4 行) を合成して書き出す。作業ディレクトリは `$D` の外に作る (中に作ると、git の未追跡のファイルとして残る):

```sh
W=~/zundamon-test && mkdir -p "$W" && cp examples/script.json "$W/"
zundamon-kaisetsu synth "$W/script.json"   # エンジンを自動で起動する (初回はイメージの取得で数分)
zundamon-kaisetsu build "$W/script.json" -o "$W/out" --format both   # Chrome が無ければ --format html
open "$W/out.html" "$W/out.mp4"
```

数秒の掛け合いが、音声・字幕・立ち絵 (用意していなければ丸アバター) つきで再生されれば準備は終わり。
Claude Code からは「ずんだもん解説を作って」で skill が呼ばれる。

止まったら: `zundamon-kaisetsu check` を見直す。エンジンが起動しないときは見張りのログ (`~/Library/Caches/zundamon-kaisetsu/`) を見る。
