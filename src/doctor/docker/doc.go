// Package docker は Docker Desktop が抱えている「使っていない資源」を数える。
//
// 対象は 4 つ: 停止したコンテナ / どのコンテナからも使われていないイメージ /
// ビルドキャッシュ / 参照コンテナの無いボリューム。**削除は一切しない** —
// disk と違って Delete に相当する経路を持たず、提示するのは人が読んで叩くコマンドだけ。
// 理由は 2 つ: prune は取り消せず、ボリュームはユーザーデータそのものになりうる。
//
// 材料は `docker system df -v --format json` の 1 回だけ (実測 0.32 秒 / images 42・
// containers 15・volumes 9・build cache 154 件)。ボリュームの作成日だけは df に無いので
// `docker volume inspect` を 1 回足す。
package docker
