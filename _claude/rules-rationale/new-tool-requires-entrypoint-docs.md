# 新しいツール・スクリプトを導入したら、同じ変更で「入口のドキュメント」を更新する — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/new-tool-requires-entrypoint-docs.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## なぜ（起源: ThumbnailThumb `cutout-single`, 2026-08-16）

素材生成パイプラインで、既存の `cutout-sheet`（連結成分 = 別素材として分割）が
「1 つの記号が離れた島に分かれている素材」を扱えないと分かり、`cutout-single` を新設した。
ツールは commit し、**設計理由もファイル冒頭に丁寧に書いた**。

しかし **`.claude/skills/asset-gen/SKILL.md` の切り抜き手順を更新しなかった**。
手順書は `cutout-sheet` だけを指示したままで、`assets/README.md`（規約の正本）にも記載がない。

結果として「ツールは存在するが、次にこの作業をする人は手順書どおり `cutout-sheet` を使い、
同じ 4 分割を踏む」状態が出来上がった。翌日ユーザーに
「使い方は skill かな？明文化できているの？」と聞かれて初めて気づいた。

**道具を作った時点の満足感が、明文化の動機を消す。** 作った本人は使い方を覚えているので、
書かれていないことに気づけない。気づけるのは「知らない人」であり、その人は困るまで現れない。

## 本文から移した実例 (2026-11-20。claude-md-compact で、常時読み込みの本文の括弧書きから移した)

- 道具を commit する変更に入口を含める理由: 起源の実例では翌日まで放置された

## 別の人が今後も使う前提で、help と導線を整える (ユーザー指示 2026-11-21。vlc-multi-video-player issue 551)

- 検査の道具を 4 つ足した (`make smart-test`・`bin/verify-record`・`bin/verify-audit`・`bin/record-app-audio`)。
  使い方は verify skill と docs/testing-guide.md に書いた。
  ところが、`bin/verify-record --help` と `bin/verify-audit --help` は引数の名前しか出さなかった。`bin/record-app-audio --help` は使い方の誤りとして rc 2 で落ちた。
  説明はどれもスクリプトの冒頭にあった
- 毎回読み込まれる root の CLAUDE.md の推奨ワークフローは「開発中は `make quick-test`」のままだった。verify skill をいつ Read するかも書いていなかった。
  observe の集計の日 (2026-12-05) は issue の本文にしか無かった。ユーザーに問われるまで、どれにも気づかなかった
- 直した形: `--help` に冒頭の説明を出す。CLAUDE.md の手順を新しい道具へ書き換え、🚨 の行に発動点を書く。集計の日は issue のメタ行に `期限:` を置き、SessionStart の hook に出させる
