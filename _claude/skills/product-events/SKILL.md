---
name: product-events
version: 1.0.0
description: my-products の product-analytics (web/product-analytics) の apps/<app>/events.yml に、App Store で公開された最新バージョンの記録を足す。app をチェックボックスで選ばせ、App Store の lookup API で最新バージョン・公開日時・リリースノートを取ってエントリを書き、commit する。「events.yml に追記」「審査が通ったので events に反映」「バージョン公開を記録」「product-events」で発火。ASO 変更・記事公開などバージョン以外の施策だけを記録するときは、手順 4 から先の書式に従って手で書く。
---

# product-events — App Store の最新バージョンを events.yml に記録する

`events.yml` はダッシュボードのチャートに縦線で重ねる施策ログ。スキーマと「何を記録するか」の正本は
各 `events.yml` の冒頭のコメントで、ここに写さない。

- repo: `PA=${PRODUCT_ANALYTICS_DIR:-$HOME/src/my-products/web/product-analytics}` (my-products の submodule)
- 各 app の App Store の id: `$PA/apps/<app>/config.json` の `appId`

## 手順

1. **最新にする**: `git -C "$PA" status -sb` を見る。clean なら `git -C "$PA" pull --rebase`
   (毎朝の `daily-fetch` が data を commit するので、たいてい behind になっている)。
   自分以外の差分があれば pull せず、umbrella の `bin/wt -C "$PA" create --label events` で worktree を作り、以降はそこで作業する
2. **app を選ばせる**: `ls "$PA"/apps/*/config.json` の app ディレクトリ名を、AskUserQuestion の **multiSelect** で出す。
   - label はディレクトリ名、description は events.yml の最後のエントリの `label` (例: `最後: v1.1.4 公開`)
   - 1 問の選択肢は 4 つまで。5 つ以上なら 4 つずつ別の問いに分けて 1 回の AskUserQuestion にまとめる
   - 依頼文で app が名指しされていて 1 つに決まるなら、問わずにそれを使う
3. **ストアから最新版を取る** (選んだ app ごと):

   ```sh
   for c in jp us; do
     curl -fsS "https://itunes.apple.com/lookup?id=<appId>&country=$c" |
       jq '.results[0] | {version, currentVersionReleaseDate, releaseNotes}'
   done
   ```

   - `results` が空なら、その国のストアでは配信していない。両方空ならその app は「取れなかった」と報告して飛ばす
   - `version` が既に events.yml のどこかの `label` に出ているなら (`grep -n "v<version>" events.yml`)、**記録済み**として足さない
4. **エントリを足す**: events.yml の末尾に、既存の「`vX.Y.Z 公開`」のエントリと同じ形で書く

   ```yaml

   - date: 2026-10-16            # currentVersionReleaseDate を JST に直した日付
     label: v1.1.5 公開
     note: >-
       実際の公開日 (2026-10-15T19:50:40Z = JST 10-16 04:50。itunes lookup API の
       currentVersionReleaseDate で確認)。アプリ側の変更は <jp の releaseNotes の要約>。
   ```

   - **date は UTC のまま書かない**。JST へ直す:
     `TZ=Asia/Tokyo date -r "$(date -j -u -f %Y-%m-%dT%H:%M:%SZ <currentVersionReleaseDate> +%s)" '+%F %R'`
     (`TZ=Asia/Tokyo date -j -u -f ...` を 1 本で打つと UTC のまま出る)
   - note の変更点は jp の `releaseNotes` を 1〜3 文に詰める (無ければ us のものを訳す)。リリースノートに無いことを書かない
   - 書いたら YAML として読めることを確かめる。python に yaml が無いことがあるので ruby で読む:
     `ruby -ryaml -e 'p YAML.load_file(ARGV[0]).last' "$PA/apps/<app>/events.yml"`
5. **commit する** (app ごとに 1 commit。push はしない):

   ```sh
   git -C "$PA" commit -F - -- apps/<app>/events.yml <<'MSG'
   events(<app>): v<version> 公開を記録
   MSG
   ```

   `git -C "$PA" log -1 --stat` で 1 ファイルだけ入ったことを確かめる
6. **報告する**: app ごとに、足したエントリ (date・label・公開日時の UTC) / 記録済みで飛ばした / 取れなかった のどれかを書く。
   ストアの表示 (サブタイトル・キーワード・スクショ) の変更は lookup API では取れないので、**確認していない**と書き、
   変えていたなら別のエントリにすると添える

## push を頼まれたら

product-analytics は umbrella の submodule なので、push は submodule → umbrella の順に行う:

```sh
git -C "$PA" push origin master                       # rc と "master -> master" の行を見る
cd "$PA/../.." && bin/bump-submodule --push web/product-analytics "$(git -C "$PA" rev-parse HEAD)"
```

worktree で作業したなら、`git -C <worktree> push origin HEAD:master` の後に push した SHA を渡し、worktree を消す。
`bump-submodule` の出力に push した commit が出ることと、`git -C "$PA/../.." ls-tree origin/master web/product-analytics` が
その SHA を指すことを確かめてから報告する。
