package main

import (
	"fmt"
	"strings"

	"doctor/disk"
)

// doctor の画面が持つキーは 4 つの名前空間に分かれる (エントリ ID / エントリ+パス / 行 /
// コマンド)。中身はどれも文字列なので、名前付き型にしないと **別の名前空間のキーを渡しても
// compile が通る**。壊れ方は例外ではなく map miss = 無言 (選択が畳まれない / 展開が効かない /
// 二重に数える) で、実際に取り違えた箇所は無いまま「次に足す人が silent に壊せる」状態だった
// (issue 371)。
//
// 🚨 **型が止めるのは map の索引と代入の取り違えまで**。`rowKey("disk:"+string(id))` のように
// 明示変換で組めば迂回できる。そこまで塞ぎに行かない — 迂回は review の責務で、
// うっかり書く典型形をコンパイラが止めれば目的は足りている。

// entryID は disk.Entry.ID。カタログのエントリの同一性。
type entryID string

// itemKey は「どのエントリのどのパスか」の同一性。パスに \x00 は現れない。
type itemKey string

// rowKey は doctor の 1 行の同一性。展開 (expanded) とカーソルの復元が使う。
// 行の並びに依存しない値にすること — 再スキャンで並びが変わっても同じ行を指し続ける。
type rowKey string

// diskItemKey はエントリとパスから itemKey を組む。
func diskItemKey(id entryID, path string) itemKey { return itemKey(string(id) + "\x00" + path) }

// entryID は itemKey の前半 (どのエントリのパスか)。
func (k itemKey) entryID() (entryID, bool) {
	id, _, ok := strings.Cut(string(k), "\x00")
	return entryID(id), ok
}

// belongsTo は「そのエントリの中のパスか」。
func (k itemKey) belongsTo(id entryID) bool {
	return strings.HasPrefix(string(k), string(id)+"\x00")
}

// ---- rowKey の生成 (prefix を直接書くのはここだけ) ----

func diskRowKey(id entryID) rowKey      { return rowKey("disk:" + string(id)) }
func diskItemRowKey(k itemKey) rowKey   { return rowKey("diskitem:" + string(k)) }
func svcRowKey(plistPath string) rowKey { return rowKey("svc:" + plistPath) }

func diskFailRowKey(id entryID, failure string) rowKey {
	return rowKey("diskfail:" + string(id) + ":" + failure)
}

func svcUndiagnosedRowKey(plistPath string) rowKey { return rowKey("svcundiagnosed:" + plistPath) }

func brewRowKey(i int, summary string) rowKey { return rowKey(fmt.Sprintf("brew:%d:%s", i, summary)) }

func brewActionRowKey(i, j int) rowKey { return rowKey(fmt.Sprintf("brewact:%d:%d", i, j)) }

func dockerGroupRowKey(kind string) rowKey { return rowKey("docker:" + kind) }

func dockerItemRowKey(kind, name string) rowKey { return rowKey("dockeritem:" + kind + ":" + name) }

// dockerPruneRowKey は「まとめて回収する」の 1 行 (群に属さないので ID を持たない)。
const dockerPruneRowKey rowKey = "dockerprune"

// diskItemsPrefix は「そのエントリの対象パスの行」を探すための **部分値**。
// 🚨 rowKey の部分文字列は rowKey ではないが、専用の型は作らない — 使うのは
// jumpTo (最初の 1 行へ移る) だけで、型を 1 つ増やす方が読む側の負担が大きい。
func diskItemsPrefix(id entryID) rowKey { return rowKey("diskitem:" + string(id) + "\x00") }

// ---- rowKey の分解 ----

// diskEntryID はエントリの行なら、その ID。
func (k rowKey) diskEntryID() (entryID, bool) {
	id, ok := strings.CutPrefix(string(k), "disk:")
	return entryID(id), ok
}

// diskItemKey は対象パスの行なら、その itemKey。
func (k rowKey) diskItemKey() (itemKey, bool) {
	s, ok := strings.CutPrefix(string(k), "diskitem:")
	return itemKey(s), ok
}

// isBrewAction は brew の手の行か。
func (k rowKey) isBrewAction() bool { return strings.HasPrefix(string(k), "brewact:") }

// isDocker は Docker タブの行か (Space の分岐に使う)。
func (k rowKey) isDocker() bool {
	return strings.HasPrefix(string(k), "docker:") ||
		strings.HasPrefix(string(k), "dockeritem:") ||
		k == dockerPruneRowKey
}

// isDiskRelated は削除の対象になりうる行か (エントリ本体と対象パス)。
func (k rowKey) isDiskRelated() bool {
	return strings.HasPrefix(string(k), "disk:") || strings.HasPrefix(string(k), "diskitem:")
}

// hasPrefix は部分値 (diskItemsPrefix) との前方一致。
func (k rowKey) hasPrefix(p rowKey) bool { return strings.HasPrefix(string(k), string(p)) }

// parentRowKey は子の行から「畳むべき親の行」を返す。対象パスの行
// (disk の diskitem: / docker の dockeritem:) だけが親を持つ。判定を 1 箇所に置くのは、
// 畳めるかを聞く側 (hint) と畳む側 (collapseAtCursor) が別の答えを出さないため。
func parentRowKey(k rowKey) (rowKey, bool) {
	if rest, ok := strings.CutPrefix(string(k), "dockeritem:"); ok {
		kind, _, ok := strings.Cut(rest, ":")
		if !ok {
			return "", false
		}
		return dockerGroupRowKey(kind), true
	}
	item, ok := k.diskItemKey()
	if !ok {
		return "", false
	}
	id, ok := item.entryID()
	if !ok {
		return "", false
	}
	return diskRowKey(id), true
}

// resultEntryID は走査結果のエントリ ID。disk 側の生の string との境界をここに閉じる。
func resultEntryID(r disk.Result) entryID { return entryID(r.Entry.ID) }
