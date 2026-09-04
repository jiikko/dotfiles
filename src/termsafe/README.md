# termsafe — 外部由来の文字列を端末へ出す前に無害化する単一の関門

`glogx` と `doctor` が go.mod の `replace termsafe => ../termsafe` で取り込む共有 module。
依存はゼロ (標準ライブラリのみ)。仕様・トレードオフの一次情報は `termsafe.go` の doc コメント。

## なぜ独立 module か

無害化が要るのは 2 つの module にまたがるため:

- **TUI (`glogx`)** — git / CI ログ / issue markdown / 作業ツリーのファイル名。描画層はセル単位に
  分解するので端末制御そのものは落ちるが、**改行で偽の行**が作れ、SGR が次の行へ滲み、
  `y` / `Y` のコピーは `pbcopy` へ生で渡る (貼った先の端末で OSC52 が発火する)
- **CLI (`doctor` の `bin/diskdoctor` / `bin/svcdoctor`)** — **stdout へ直接**書くので後段が無く、
  「表示しただけ」でクリップボード書き込み・タイトル書き換え・画面消去が起きる

`glogx` は `doctor` を replace で取り込んでいるので、`doctor` から `glogx/termsafe` は引けない
(循環)。両方から引ける位置へ出したのが本 module (issue 228)。

## 使い分け

| 関数 | SGR (色) | タブ | 改行 | 使いどころ |
| --- | --- | --- | --- | --- |
| `DetailLine` | 残す | スペース 4 | 落とす | git / CI ログ (`--color` の出力を出す契約がある) |
| `LineKeepTabs` | 残す | 残す | 落とす | git の subject / message (静的出力とのパリティ契約) |
| `PlainLine` | 落とす | スペース 4 | 落とす | **既定**。ファイル名・issue 本文・診断結果の自由文 |
| `PlainLineKeepTabs` | 落とす | 残す | 落とす | 自前でタブストップ揃えをする整形層の入口 |
| `PlainBlock` | 落とす | スペース 4 | **残す** | 1 件が複数行の塊 (brew doctor の警告本文) |
| `IsPlain` | — | — | — | **書き換えず落とす**判定 (同一性を持つ値: パス / ラベル) |

🚨 **`PlainBlock` を「1 件 = 1 行」の場所に使わない**。偽の行を差し込まれて固定高パネルの
行数が狂う (幅を数えるテストは改行を検出しないので素通りする)。

🚨 **同一性を持つ値は書き換えない**。パスやラベルを無害化して表示すると、
「画面に出ているものと、実際に消す / 案内するものが違う」を作る。落とす側へ倒し、
落とした件数を人に見せる (`disk.DisplayablePath` / `svc` の `displayableIdentity` がその形)。
