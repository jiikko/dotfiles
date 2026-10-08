# mp4box

MP4 のトップレベルの box を先頭から辿り、壊れていない box の大きさの合計 (実効サイズ) をバイト数で出す CLI。
どの box からも参照されない末尾のごみ (中断した書き込みの残り等) を数えないための値で、`zshlib/_concat_helpers.zsh` の
`__concat_mp4_effective_size` (concat の出力が入力の合計より小さいときの診断) が `bin/mp4-effective-size` を通して呼ぶ。

```sh
mp4-effective-size <file>   # 常に rc=0 で数を 1 行。読めない・壊れているなら 0
```

数え方 (大きさ 0 は終わりまで・1 は 64 bit の大きさ・型が印字できる ASCII でない / 大きさが 8 未満 / ファイルの外へ出るところで止める) は、
置き換えた Python 版 (`python3 -c`) と同じ。旧版との突き合わせは issue 670。

```sh
make test
make lint
```
