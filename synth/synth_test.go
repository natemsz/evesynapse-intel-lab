package synth

import (
	"reflect"
	"strings"
	"testing"
)

// testUniverse is a few real systems and ships, gates between them.
func testUniverse() *Universe {
	systems := []*System{
		{ID: 30004759, Name: "1DQ1-A", Region: 10000060, Constellation: 20000696, Security: -0.38, Neighbours: []int64{30004761, 30004760}},
		{ID: 30004761, Name: "NOL-M9", Region: 10000060, Constellation: 20000696, Security: -0.4, Neighbours: []int64{30004759}},
		{ID: 30004760, Name: "8QT-H4", Region: 10000060, Constellation: 20000696, Security: -0.4, Neighbours: []int64{30004759}},
		{ID: 30000142, Name: "Jita", Region: 10000002, Constellation: 20000020, Security: 0.95, Neighbours: []int64{30000144}},
		{ID: 30000144, Name: "Perimeter", Region: 10000002, Constellation: 20000020, Security: 0.95, Neighbours: []int64{30000142}},
	}
	ships := []Ship{
		{ID: 22456, Name: "Sabre", Group: "Interdictor"}, {ID: 12019, Name: "Sacrilege", Group: "Heavy Assault Cruiser"},
		{ID: 24702, Name: "Hurricane", Group: "Combat Battlecruiser"}, {ID: 11381, Name: "Harpy", Group: "Assault Frigate"},
		{ID: 23913, Name: "Nyx", Group: "Supercarrier"},
	}
	return NewUniverse(systems, map[int64]string{10000060: "Delve", 10000002: "The Forge"},
		map[int64]string{20000696: "O-EIMK", 20000020: "Kimotoro"}, ships)
}

// Every span names its own text, in order, inside the text, and
// the ids it carries are the universe's.
func TestSpansTrue(t *testing.T) {
	u := testUniverse()
	g := NewGenerator(u, 1)
	styles := map[string]int{}
	for i := 0; i < 3000; i++ {
		ex := g.Next()
		styles[ex.Frame.Style]++
		last := -1
		for _, sp := range ex.Spans {
			if sp.Start < last || sp.End <= sp.Start || sp.End > len(ex.Text) {
				t.Fatalf("bad span %+v in %q", sp, ex.Text)
			}
			last = sp.End
			text := ex.Text[sp.Start:sp.End]
			switch sp.Label {
			case LabelSystem, LabelGate, LabelDest:
				if s := u.Systems[sp.ID]; s == nil || !strings.EqualFold(s.Name, text) {
					t.Fatalf("%s span %q is not system %d in %q", sp.Label, text, sp.ID, ex.Text)
				}
			case LabelShip:
				if sp.ID == 0 || sp.Value == "" {
					t.Fatalf("ship span %q without its type in %q", text, ex.Text)
				}
			case LabelPilot:
				if !strings.EqualFold(text, sp.Value) {
					t.Fatalf("pilot span %q is not %q in %q", text, sp.Value, ex.Text)
				}
			}
		}
		if ex.Frame.Gate != 0 && !hasSpan(ex, LabelGate) {
			t.Fatalf("a gate frame with no gate span: %q", ex.Text)
		}
		if ex.Delivery == DeliveryChat && strings.ContainsAny(ex.Text, "\n\t") {
			t.Fatalf("a typed line with a line break or tab: %q", ex.Text)
		}
	}
	for _, s := range []string{StyleSighting, StyleGate, StyleMove, StyleClear, StyleQuestion, StyleLocation, StyleLocal, StyleDScan} {
		if styles[s] == 0 {
			t.Errorf("style %s never drawn", s)
		}
	}
}

// A seed makes the same examples every time.
func TestSeedRepeats(t *testing.T) {
	a, b := NewGenerator(testUniverse(), 42), NewGenerator(testUniverse(), 42)
	for i := 0; i < 200; i++ {
		if x, y := a.Next(), b.Next(); !reflect.DeepEqual(x, y) {
			t.Fatalf("example %d differs:\n%+v\n%+v", i, x, y)
		}
	}
}

// Local copies glue through the game's chat and keep their lines
// pasted straight in.
func TestLocalDelivery(t *testing.T) {
	g := NewGenerator(testUniverse(), 3)
	f := Frame{Style: StyleLocal, System: 30004759, Pilots: []string{"Kara Venn", "Tor Ash", "Mirx88"}}
	b := &builder{}
	g.renderLocal(b, f, DeliveryChatPaste)
	if strings.Contains(b.b.String(), "\n") || !strings.Contains(b.b.String(), "Kara VennTor AshMirx88") {
		t.Errorf("chat paste: %q", b.b.String())
	}
	b = &builder{}
	g.renderLocal(b, f, DeliveryPaste)
	if !strings.Contains(b.b.String(), "Kara Venn\nTor Ash\nMirx88") {
		t.Errorf("straight paste: %q", b.b.String())
	}
}

// The reader scores well on what the generator makes from this
// universe: a floor, so a change that breaks either side shows.
func TestEvaluateFloor(t *testing.T) {
	u := testUniverse()
	g := NewGenerator(u, 9)
	var exs []Example
	for i := 0; i < 1500; i++ {
		exs = append(exs, g.Next())
	}
	s := Evaluate(u, exs, false, 3)
	for field, min := range map[string]float64{"system": 97, "gate": 97, "count": 97, "clear": 97} {
		if tl := s.fields[field]; tl == nil || pct(tl.ok, tl.n) < min {
			var b strings.Builder
			s.Write(&b)
			t.Fatalf("%s below %.0f%%:\n%s", field, min, b.String())
		}
	}
	if tl := s.fields["pilots"]; pct(tl.tp, tl.tp+tl.fp) < 97 {
		var b strings.Builder
		s.Write(&b)
		t.Fatalf("pilot precision below 97%%:\n%s", b.String())
	}
}
