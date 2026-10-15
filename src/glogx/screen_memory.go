package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// screenMemory は全画面 (issues / doctor / ratelimit / F / L) を閉じたときの画面の記憶 (1 画面 1 ファイル)。
// C-g で閉じてすぐ開き直したときに同じ画面へ戻すためのもの。保存・読み込み・削除の作法は全画面で同じなので、
// ここに 1 つだけ置く (issue 777)。照合 (別 repo・別の dir・範囲外のタブ) は画面ごとに load の後で行う。
type screenMemory[T any] struct {
	file    string            // cacheBaseDir の下のファイル名
	savedAt func(T) time.Time // 記憶の保存時刻 (TTL の判定に使う)
}

// screenMemoryTTL は記憶の有効期限。C-g のトグル感覚 (閉じてすぐ開き直す) に効かせ、時間が経ってから開いたときは
// 通常どおり git log 一覧から始める (ユーザー選定 2026-07-31。全画面で同じ値・同じ根拠)。
const screenMemoryTTL = 30 * time.Minute

func (m screenMemory[T]) path() (string, error) {
	base, err := cacheBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, m.file), nil
}

// save は記憶を書く。失敗は呼び出し側が握り潰してよい (復元は「できたら嬉しい」機能で、保存できないことを終了時に伝えても
// 行動は変わらない)。
func (m screenMemory[T]) save(v T) error {
	path, err := m.path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	// 🚨 素の os.WriteFile を使わないこと (issue 219 / 286)。writeAtomic は temp + rename で、
	// write / Close / rename の 3 分岐すべてで temp を掃除する。素の WriteFile は O_TRUNC なので
	// 書き込み中に落ちると**途中まで書けた JSON** が残り、次回起動の復元が黙って失敗する。
	// 規律は cache_write_discipline_test.go がソース走査で強制する。
	return writeAtomic(path, data)
}

// remove は記憶を捨てる (その画面を開いていないまま終了したとき。捨てないと 2 回前の画面が蘇る)。
func (m screenMemory[T]) remove() {
	if path, err := m.path(); err == nil {
		_ = os.Remove(path)
	}
}

// load は TTL 内の記憶を返す。ファイル欠損・破損・保存時刻が無い・期限切れ・未来の時刻 (時計のずれ) はすべて「記憶なし」に
// 落とす (記憶の都合で起動を失敗させない・勝手に全画面を出さない方が安全側)。
// 🚨 このファイルは一般ユーザー権限で書き換えられる。値をそのまま添字や照合に使わず、呼び出し側で正規化する。
func (m screenMemory[T]) load(now time.Time) (T, bool) {
	var zero, v T
	path, err := m.path()
	if err != nil {
		return zero, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return zero, false
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return zero, false
	}
	at := m.savedAt(v)
	if at.IsZero() || now.Sub(at) >= screenMemoryTTL || at.After(now) {
		return zero, false
	}
	return v, true
}
