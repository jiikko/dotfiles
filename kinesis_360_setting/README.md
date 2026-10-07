# Kinesis Advantage360 (SmartSet 版) の設定

キーボード内の v-Drive (`Adv360` という名前の仮想ドライブ) にある設定ファイルの写し。
機種は Advantage360 の SmartSet 版 (`settings/settings.txt` の `model=adv360`。ZMK の Pro 版ではない)、ファームウェアは 1.0.5。
使っているのは **プロファイル 1** (`settings.txt` の `profile=1`。中身は `layouts/layout1.txt`)。2〜9 は空。

| ディレクトリ | 中身 |
|---|---|
| `layouts/layoutN.txt` | プロファイル N のキーの割り当てとマクロ |
| `lighting/ledN.txt` | プロファイル N のインジケータ LED の役と色 |
| `settings/settings.txt` | 全体の設定 (今のプロファイル・ファームウェアの版など)。**公式は「編集しない」** |
| `settings/app_settings.txt` | SmartSet アプリの案内の表示の on/off |

## ドライブモード (v-Drive) の開き方・閉じ方

キーの位置: **SmartSet キー** = 右モジュールの歯車アイコンのキー。**Hotkey** = 前面に歯車アイコンと機能名
(Remap / Macro / v-Drive / Refresh) が書かれた 4 つのキーで、順に Hotkey 1〜4。

- **開く: SmartSet を押したまま Hotkey 3 (v-Drive)**。LED が 4 回点滅し、開いている間は青く点滅する。
  Finder のサイドバー (デバイス) かデスクトップに `Adv360` が出る
- **閉じる: Finder で取り出し (eject) してから、もう一度 SmartSet + Hotkey 3**。LED が 2 回点滅する。
  取り出さずに閉じると壊れることがある。macOS は閉じた後にエラーを出すことがあるが無視してよい
- 開いている間は、v-Drive 以外の SmartSet の操作が効かない (ドライブを壊さないため)

## この repo の設定をキーボードへ入れる

1. v-Drive を開く (SmartSet + Hotkey 3)
2. 書き換えるファイルをコピーする。`-X` で拡張属性を付けない (付けると FAT のドライブに `._*` のゴミができる)
   ```sh
   V=/Volumes/Adv360   # ls /Volumes で名前を確かめる
   cp -X kinesis_360_setting/layouts/layout1.txt  "$V/layouts/"
   cp -X kinesis_360_setting/lighting/led1.txt    "$V/lighting/"
   ```
   `settings/` は書き戻さない (公式が編集を禁じている)
3. Finder で取り出してから v-Drive を閉じる (SmartSet + Hotkey 3)。閉じると変更が反映される
   (開いたまま反映するなら、取り出した後に SmartSet + Hotkey 4 = Refresh)
4. プロファイル 1 を読み込む: **SmartSet + 1**

キーボード側で変えた設定を repo に取り込むときは、v-Drive を開いて逆向きにコピーし、取り出して閉じる。

## ほかのキー操作 (SmartSet を押したまま)

| 操作 | キー |
|---|---|
| プロファイルを切り替える (0 は工場出荷のまま・書き換え不可) | 数字の 0〜9 |
| キーをその場で割り当て直す / マクロを記録する | Hotkey 1 / Hotkey 2 |
| 変更を反映する (v-Drive を開いたまま) | Hotkey 4 |
| 設定のロック / 解除 | 右 Ctrl + L |
| 状態の一覧をエディタに打ち出す (先にテキストエディタを開いておく) | 右 Ctrl + 右 Shift + / |
| 今のプロファイルの割り当てとマクロを消す (Soft Reset) | 右 Ctrl + Enter |
| 全部を工場出荷に戻す (Hard Reset) | Hotkey 4 を押したまま USB を挿す |
| ファームウェアを更新する (`.upd` を v-Drive の `firmware/` に置いて閉じてから) | 右 Ctrl + U (45 秒かかる。終わるまで抜かない・打たない) |

出典は `manuals/` に置いた Kinesis 公式の PDF (2026-10-07 に取得。元の URL は各行の括弧):

- [`manuals/users-manual.pdf`](manuals/users-manual.pdf) — User's Manual v10-12-22。キー操作は 6.3〜6.9 節、ファームウェアは 7 章
  ([元](https://kinesis-ergo.com/wp-content/uploads/Advantage360-SmartSet-KB360-Users-Manual-v10-12-22.pdf))
- [`manuals/direct-programming-guide.pdf`](manuals/direct-programming-guide.pdf) — Direct Programming Guide 8-8-25。`layoutN.txt` / `ledN.txt` の書き方
  ([元](https://kinesis-ergo.com/wp-content/uploads/Adv360-SmartSet-Direct-Programming-Guide-Version-8-8-25.pdf))
- [`manuals/quick-start-guide.pdf`](manuals/quick-start-guide.pdf) — Quick Start Guide v5-19-22。キーの位置の図
  ([元](https://ik.imagekit.io/vhucnsp9j1u/pdfs/kinesis-KB360-Quick-Start-Guide-v5-19-22.pdf))
