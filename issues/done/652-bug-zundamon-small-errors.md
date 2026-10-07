# 652 (bug): zundamon-kaisetsu の小さな不具合とエラー処理の不揃い

起票日: 2026-10-07

## 詳細

1. **synth が mermaid のキャッシュを「台本から外れた古いファイル」に数える** (`synth.go:cmdSynth`)
   - `os.ReadDir(wd)` の全エントリから keep を除いて数えるので、build が作る `<台本>.work/mermaid/` も数える。そのうえで
     「`<台本>.work/` ごと消して synth し直してもよい」と案内し、従うと mermaid の PNG を描き直すことになる (npx で 1 枚数十秒)
   - 再現 (監査で実施): `s.work/mermaid/` を置いて synth すると「古いファイル 1 件」と出る
   - 直し方: 数えるのを通常ファイルの `*.wav` / `*.query.json` に絞る
2. **中断のときに誤った理由の `error:` が 1 行出る** (`engine.go:startEngine` の `runtimeService` の失敗、`cmdDown` の `stopContainers`)
   - Ctrl-C で runQuietCtx が 130 を返した結果を「サービスが動いていない」「stop が失敗 (rc=130)」として返す。
     他の経路 (image inspect / pull / run) は `interruptedErr()` を先に見ている。終わり方 (同じ signal で死に直す) は正しい
   - 直し方: この 2 か所でも `interruptedErr()` を先に見る
3. **mp4 の出力先が書けないことに、撮影と圧縮を全部終えてから気づく** (`mp4.go:cmdBuild` / `writeMP4`)
   - 最初の確認が `partPath(out)` で、Chrome の撮影と CPU の空き待ち (最大 20 分) の後にある。`-o 無いdir/out --format mp4` で発火 (コードを読んで確認)
   - 直し方: cmdBuild の頭で出力先の dir があり書けるかを見る

## 関連ファイル

- `src/zundamon-kaisetsu/synth.go` / `engine.go` / `mp4.go`。監査の記録: issue 656

## 進捗

- [x] 1 / [x] 2 / [x] 3 — fix(zundamon-kaisetsu) の commit

## 結果 (2026-10-07)

1. synth は通常ファイルの `*.wav` / `*.query.json` だけを「古いファイル」に数える (`TestSynthCountsOnlyStaleCache`)
2. `startEngine` の `runtimeService` の失敗と `stopContainers` の stop の失敗で、`interruptedErr()` を先に見る (`TestEngineInterruptIsNotReportedAsFailure` の up / down)
3. `cmdBuild` の頭で、各出力先に一時ファイルを作れるかを確かめる (`TestBuildChecksOutputDirFirst`: 書けない出力先で ffmpeg / chrome を起こさずに止まる)
- 変異: 3 つのファイルをそれぞれ修正前に戻すと、対応するテストが red

## 敵対的レビュー (2026-10-07、opus、2 周)

- 1 周目 P2 2 / P3 2 (全部採用): down の確認 (system status) の途中の中断で rc=0 のまま印を消していた / synth の案内が「work/ ごと消してよい」のまま /
  中断の判定を appCtx で見ると、見張り (別の ctx) の成功や失敗まで「中断」になる (今回の修正で入った退行。渡された ctx で見る ctxInterrupted に) /
  build の最初の確認が「dir に一時ファイルを作れるか」だけ (置き換えの前提も checkReplaceable で見る)
- 2 周目 P3 3: 自分を指す symlink を「まだ無い」と読む (ErrNotExist のときだけ通す) / doc コメントのずれ (直した) /
  **記録のみ**: sticky bit の dir にある他人の 0666 のファイルは access(W_OK) を通るが rename で EPERM (他人のファイルが要り未再現。前からある挙動)
