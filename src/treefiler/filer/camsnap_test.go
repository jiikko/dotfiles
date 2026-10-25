package filer

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"
)

// camModel は a (n 件を開いた) / b / c の木で、カーソルを a に置いた Model。
func camModel(t *testing.T, unsynced bool, n int) *Model {
	t.Helper()
	isolateConfig(t)
	root := t.TempDir()
	for i := range n {
		mkfile(t, filepath.Join(root, "a", fmt.Sprintf("f%02d.txt", i)), "")
	}
	mkfile(t, filepath.Join(root, "b", "x.txt"), "")
	mkfile(t, filepath.Join(root, "c", "y.txt"), "")
	m, err := New(root, Options{Now: func() time.Time { return fixedNow }, Unsynced: unsynced})
	if err != nil {
		t.Fatal(err)
	}
	m.Resize(120, 40)
	settle(m)
	m.HandleKey("tab") // a を開く (カーソルは a のまま)
	settle(m)
	if m.cur.raw != "a" || !m.cur.expanded {
		t.Fatalf("前提: カーソル %q (open=%v)、a を開いた状態にならない", m.cur.raw, m.cur.expanded)
	}
	return m
}

// moveY は key を押して 1 フレーム進め、名前が name のノードの縦の (押す前, 今, 目標) を返す。
func moveY(t *testing.T, m *Model, key, want, name string, at time.Duration) (from, v, to float64) {
	t.Helper()
	a := animOf(t, m, name)
	from = a.y.v
	m.HandleKey(key)
	if m.cur.raw != want {
		t.Fatalf("前提: %s の後のカーソルが %q (want %q)", key, m.cur.raw, want)
	}
	m.Advance(fixedNow.Add(at))
	a = animOf(t, m, name)
	return from, a.y.v, a.y.to
}

func animOf(t *testing.T, m *Model, name string) *anim {
	t.Helper()
	for _, n := range m.order {
		if n.raw == name && m.anims[n] != nil && !m.anims[n].ghost {
			return m.anims[n]
		}
	}
	t.Fatalf("前提: %q のノードが描かれていない", name)
	return nil
}

func TestUnsyncedSnapsVerticalShiftsOverOneRow(t *testing.T) {
	// 同期出力の無い端末: 8 件を開いた a から b へ移ると a の塊が縦に数行動く。流さずに 1 フレームで寄せ切る。
	// 画面の半分に届かない数行のずれで試す (兄弟の移動で実際に起きるのはこの大きさ。遠くへ跳ぶときだけ寄せる形では乱れが残る)
	m := camModel(t, true, 8)
	from, v, to := moveY(t, m, "j", "b", "f04.txt", 16*time.Millisecond)
	if d := math.Abs(to - from); d < 2 || d >= float64(m.canvasH())/2 {
		t.Fatalf("前提: a → b で f04.txt の縦の移動が %.0f 行で、2 行以上・画面の半分未満の動きを試せていない", d)
	}
	if v != to { // Animating は見ない: カーソルの線を走る光 (stepBead) は寄せ切らずに残る
		t.Errorf("Unsynced で大きく動くのに流している (v=%.2f 目標 %.0f)", v, to)
	}
	// 1 行だけのずれ (b → c で兄弟の列が 1 行ずれる) は今までどおり流す
	from, v, to = moveY(t, m, "j", "c", "b", 32*time.Millisecond)
	if from == to || v == to || !m.Animating() {
		t.Errorf("Unsynced でも 1 行のずれは流すはず (from=%.2f v=%.2f 目標 %.0f animating=%v)", from, v, to, m.Animating())
	}

	// 同期出力の効く端末: 大きく動いても今までどおり流す
	m = camModel(t, false, 8)
	_, v, to = moveY(t, m, "j", "b", "f04.txt", 16*time.Millisecond)
	if v == to || !m.Animating() {
		t.Errorf("同期出力の効く端末で寄せ切った (v=%.2f 目標 %.0f)", v, to)
	}
}

func TestUnsyncedSnapThresholdIsTwoRows(t *testing.T) {
	// 開いた a の子が 3 件なら a → b で塊が 2 行、2 件なら 1 行ずれる
	for _, c := range []struct {
		kids int
		snap bool
	}{{3, true}, {2, false}} {
		m := camModel(t, true, c.kids)
		from, v, to := moveY(t, m, "j", "b", "f00.txt", 16*time.Millisecond)
		if d := math.Abs(to - from); (c.snap && d != 2) || (!c.snap && d != 1) {
			t.Fatalf("前提: 子 %d 件で f00.txt の縦のずれが %.0f 行", c.kids, d)
		}
		if (v == to) != c.snap {
			t.Errorf("子 %d 件 (ずれ %.0f 行): 寄せ切った=%v、want %v", c.kids, math.Abs(to-from), v == to, c.snap)
		}
	}
}

func TestUnsyncedSnapAlsoSnapsCameraX(t *testing.T) {
	// : で a の塊から離れた c/y.txt へ飛ぶと、塊が縦にずれ、カメラも横へ動く。縦だけ寄せて横を流すと、飛んでから滑る 2 段の動きになる
	// (h / l は選択線の先に前の子が続くので縦は動かない。縦と横が同じ回に動くのはこういう離れた移動)
	m := camModel(t, true, 8)
	camFrom, yFrom := m.camX.v, animOf(t, m, "f00.txt").y.v
	m.HandleKey(":")
	setLine(m, "c/y.txt")
	m.HandleKey("enter")
	if m.cur.raw != "y.txt" {
		t.Fatalf("前提: : で c/y.txt へ飛ばない (cur=%q)", m.cur.raw)
	}
	m.Advance(fixedNow.Add(16 * time.Millisecond))
	a := animOf(t, m, "f00.txt")
	if math.Abs(a.y.to-yFrom) < 2 || a.y.v != a.y.to || camFrom == m.camX.to {
		t.Fatalf("前提: 塊が縦に 2 行以上寄せ切られ、カメラの横の目標が変わる形になっていない (y %.1f → %.2f 目標 %.0f、camX %.1f → 目標 %.1f)", yFrom, a.y.v, a.y.to, camFrom, m.camX.to)
	}
	if m.camX.v != m.camX.to {
		t.Errorf("縦を寄せ切った回にカメラの横を流している (camX %.2f、目標 %.2f)", m.camX.v, m.camX.to)
	}
}

func TestUnsyncedKeepsSpawnAnimationWhenTreeDoesNotMove(t *testing.T) {
	// 下に兄弟の無いフォルダを開くと、木は動かず子が親から湧くだけ。寄せ切らずに流す
	isolateConfig(t)
	root := t.TempDir()
	for i := range 5 {
		mkfile(t, filepath.Join(root, "z", fmt.Sprintf("k%d.txt", i)), "")
	}
	m, err := New(root, Options{Now: func() time.Time { return fixedNow }, Unsynced: true})
	if err != nil {
		t.Fatal(err)
	}
	m.Resize(120, 40)
	settle(m)
	m.HandleKey("tab")
	m.Advance(fixedNow.Add(16 * time.Millisecond))
	a := animOf(t, m, "k4.txt")
	if a.y.to < 2 {
		t.Fatalf("前提: k4.txt の目標の行 %.0f が親から 2 行以上離れていない", a.y.to)
	}
	if a.y.v == a.y.to {
		t.Errorf("木が動かないのに、湧いた子を寄せ切った (k4.txt y=%.2f)", a.y.v)
	}
}

func TestUnsyncedSnapDropsFadingNodes(t *testing.T) {
	// a と b を両方開いて a を畳むと、b の子の塊が a の子の分だけ上がって寄せ切る。畳んだ a の子は a の右端で消えかけのまま
	// 上がってきた行に重ならず、その回に消える
	m := camModel(t, true, 8)
	m.HandleKey("j")
	m.HandleKey("tab") // b を開く
	m.HandleKey("k")
	settle(m)
	from := animOf(t, m, "x.txt").y.v
	m.HandleKey("tab") // a を畳む
	m.Advance(fixedNow.Add(16 * time.Millisecond))
	if x := animOf(t, m, "x.txt"); math.Abs(x.y.to-from) < 2 || x.y.v != x.y.to {
		t.Fatalf("前提: a を畳んで b の子 x.txt が 2 行以上寄せ切られていない (%.1f → %.2f 目標 %.0f)", from, x.y.v, x.y.to)
	}
	for n, a := range m.anims {
		if a.ghost {
			t.Errorf("寄せ切った回に消えかけの %q が残った (alpha %.2f)", n.raw, a.alpha.v)
		}
	}
}

func TestUnsyncedKeepsSiblingStepWithRowSpacing(t *testing.T) {
	// 行間 1 だと兄弟へ 1 歩で列が 2 行ずれる。1 歩は流す (j / k の動きを消さない)。a は畳んで、どのノードも 1 歩だけずれる形にする
	m := camModel(t, true, 1)
	m.set.RowSpacing = 1
	m.HandleKey("tab")
	settle(m)
	from, v, to := moveY(t, m, "j", "b", "b", 16*time.Millisecond)
	if math.Abs(to-from) != 2 {
		t.Fatalf("前提: 行間 1 で b の縦のずれが %.0f 行 (2 のはず)", math.Abs(to-from))
	}
	if v == to {
		t.Errorf("行間 1 の兄弟への 1 歩を寄せ切った (b y=%.2f)", v)
	}
}

func TestUnsyncedSnapsMultiStepShiftWithRowSpacing(t *testing.T) {
	// 行間 1 で a (子 1 件を開いた) から b へ移ると、a の子は 2 歩 (4 行) ずれる。1 歩より大きいので寄せ切る
	m := camModel(t, true, 1)
	m.set.RowSpacing = 1
	settle(m)
	from, v, to := moveY(t, m, "j", "b", "f00.txt", 16*time.Millisecond)
	if math.Abs(to-from) != 4 {
		t.Fatalf("前提: 行間 1 で f00.txt の縦のずれが %.0f 行 (4 のはず)", math.Abs(to-from))
	}
	if v != to {
		t.Errorf("行間 1 で 2 歩ずれるのに流している (f00.txt y=%.2f 目標 %.0f)", v, to)
	}
}

func TestUnsyncedOpenSnapsTreeButKeepsSpawnAnimation(t *testing.T) {
	// a を開いたまま b へ移って b を Enter で開くと、a の子が縦に数行寄る (寄せ切る回)。その回でも b の子が親から湧く動きは流す
	isolateConfig(t)
	root := t.TempDir()
	for _, d := range []string{"a", "b"} {
		for i := range 12 {
			mkfile(t, filepath.Join(root, d, fmt.Sprintf("f%02d.txt", i)), "")
		}
	}
	m, err := New(root, Options{Now: func() time.Time { return fixedNow }, Unsynced: true})
	if err != nil {
		t.Fatal(err)
	}
	m.Resize(120, 40)
	settle(m)
	for _, k := range []string{"enter", "h", "j"} {
		m.HandleKey(k)
		settle(m)
	}
	var aKid *anim
	for _, n := range m.order {
		if n.raw == "f05.txt" && n.parent.raw == "a" {
			aKid = m.anims[n]
		}
	}
	yFrom := aKid.y.v
	m.HandleKey("enter")
	m.Advance(fixedNow.Add(16 * time.Millisecond))
	if m.cur.parent.raw != "b" || math.Abs(aKid.y.to-yFrom) < 2 || aKid.y.v != aKid.y.to {
		t.Fatalf("前提: b を開いた回に a の子が 2 行以上寄せ切られていない (cur=%s/%s、y %.1f → %.2f 目標 %.0f)", m.cur.parent.raw, m.cur.raw, yFrom, aKid.y.v, aKid.y.to)
	}
	b := m.anims[m.cur.parent]
	var kid *anim // 同じ名前の a の子を引かないよう、b の子から探す
	for _, n := range m.order {
		if n.raw == "f05.txt" && n.parent == m.cur.parent {
			kid = m.anims[n]
		}
	}
	if kid == nil {
		t.Fatal("前提: b の子 f05.txt が描かれていない")
	}
	if kid.y.v == kid.y.to || kid.x.v == kid.x.to || !m.Animating() {
		t.Errorf("開いたフォルダの子が湧く動きまで寄せ切った (x %.2f 目標 %.0f、y %.2f 目標 %.0f)", kid.x.v, kid.x.to, kid.y.v, kid.y.to)
	}
	if kid.x.from != b.x.v+float64(b.w) || kid.y.from != b.y.v {
		t.Errorf("b の子が b の右端から湧いていない (from %.1f,%.1f、b の右端 %.1f,%.1f)", kid.x.from, kid.y.from, b.x.v+float64(b.w), b.y.v)
	}
}

func TestUnsyncedSpawnStartsFromSnappedAncestor(t *testing.T) {
	// : で c/y.txt へ飛ぶと c は縦に寄せ切られ、c の子 y.txt は同じ回に湧く。湧く元は寄せる前の c ではなく、寄せた後の c の右端
	// (寄せる前から湧くと、子だけが離れた所から飛んでくる)
	m := camModel(t, true, 8)
	cFrom := animOf(t, m, "c").y.v
	m.HandleKey(":")
	setLine(m, "c/y.txt")
	m.HandleKey("enter")
	m.Advance(fixedNow.Add(16 * time.Millisecond))
	c, y := animOf(t, m, "c"), animOf(t, m, "y.txt")
	if math.Abs(c.y.to-cFrom) < 2 || c.y.v != c.y.to {
		t.Fatalf("前提: c が 2 行以上寄せ切られる形になっていない (y %.1f → %.2f 目標 %.0f)", cFrom, c.y.v, c.y.to)
	}
	if y.x.from != c.x.v+float64(c.w) || y.y.from != c.y.v {
		t.Errorf("y.txt が寄せた後の c の右端から湧いていない (from %.1f,%.1f、c の右端 %.1f,%.1f)", y.x.from, y.y.from, c.x.v+float64(c.w), c.y.v)
	}
	if y.x.v == y.x.to {
		t.Errorf("y.txt の湧く動きまで寄せ切った (x %.2f 目標 %.0f)", y.x.v, y.x.to)
	}
}
