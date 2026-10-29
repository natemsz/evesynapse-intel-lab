package synth

import (
	"fmt"
	"hash/fnv"
	"io"
	"sort"
	"strings"
	"time"

	"evesynapse/internal/intelanalyze"
	"evesynapse/internal/intelchat"
)

// Score measures the deterministic reader (intelchat, then
// intelanalyze) on examples whose text arrived as one chat line,
// against the labels the examples were made with. A pasted copy
// with its lines kept goes through the site's paste readers, not
// this one, and is left out.
//
// worst makes the pilot lookup answer every string as some
// character, as EVE's own name lookup nearly does; otherwise only
// the examples' own pilots are characters.
type Score struct {
	Examples, Scored, Skipped int
	// MaxMisses is how many misses to keep per field for the report.
	MaxMisses int
	fields    map[string]*tally
	misses    map[string][]string
}

type tally struct{ tp, fp, fn, ok, n int }

func (s *Score) add(field string) *tally {
	if s.fields[field] == nil {
		s.fields[field] = &tally{}
	}
	return s.fields[field]
}

func (s *Score) miss(field, text, detail string) {
	if len(s.misses[field]) < s.MaxMisses {
		s.misses[field] = append(s.misses[field], fmt.Sprintf("%q: %s", text, detail))
	}
}

// Evaluate scores the reader on examples.
func Evaluate(u *Universe, examples []Example, worst bool, maxMisses int) *Score {
	s := &Score{Examples: len(examples), MaxMisses: maxMisses, fields: map[string]*tally{}, misses: map[string][]string{}}
	pilots := map[string]string{}
	for _, ex := range examples {
		for _, p := range ex.Frame.Pilots {
			pilots[strings.ToLower(p)] = p
		}
	}
	// The site's verified names, which a glued Local copy is
	// segmented against (intelanalyze.NameIndex), as the site does.
	var known []intelanalyze.Pilot
	for l, name := range pilots {
		known = append(known, intelanalyze.Pilot{ID: nameID(l), Name: name})
	}
	index := intelanalyze.NewNameIndex(known)
	areas := map[string]int64{}
	for id, n := range u.Constellations {
		areas[strings.ToLower(n)] = id
	}
	for id, n := range u.Regions {
		areas[strings.ToLower(n)] = id
	}
	lookup := func(l string) (int64, string, bool) {
		if sys, ok := u.ByName[l]; ok {
			return sys.ID, sys.Name, true
		}
		return 0, "", false
	}
	d := intelanalyze.Dict{
		Ship: func(l string) (int64, string, string, bool) {
			sh, ok := u.ShipByName[l]
			return sh.ID, sh.Name, sh.Class, ok
		},
		Pilot: func(l string) (int64, string, bool, bool) {
			if name, ok := pilots[l]; ok {
				return nameID(l), name, true, true
			}
			if worst {
				return nameID(l), l, true, true
			}
			return 0, "", false, true
		},
		System: func(l string) (int64, bool) {
			if sys, ok := u.ByName[l]; ok {
				return sys.ID, true
			}
			return 0, false
		},
		Gate: func(from int64, l string) (int64, bool) {
			sys, ok := u.ByName[l]
			if !ok || u.Systems[from] == nil {
				return 0, false
			}
			for _, n := range u.Systems[from].Neighbours {
				if n == sys.ID {
					return n, true
				}
			}
			return 0, false
		},
		Area: func(l string) (int64, bool) {
			id, ok := areas[l]
			return id, ok
		},
	}
	at := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for _, ex := range examples {
		// A paste naming no system takes the poster's own location on
		// the site; a pasted copy with its lines kept goes through the
		// site's paste readers. Neither is this reader's to score.
		if ex.Delivery == DeliveryPaste || !hasSpan(ex, LabelSystem) {
			s.Skipped++
			continue
		}
		s.Scored++
		f := ex.Frame
		out := intelchat.Parse("[ 2026.10.26 12:00:00 ] Scout > "+ex.Text, lookup, at)
		if f.State == "question" {
			t := s.add("question")
			t.n++
			if out.Questions == 1 && len(out.Reports) == 0 {
				t.ok++
			} else {
				s.miss("question", ex.Text, fmt.Sprintf("%d reports, %d questions", len(out.Reports), out.Questions))
			}
			continue
		}
		t := s.add("system")
		t.n++
		if len(out.Reports) != 1 || out.Reports[0].SystemID != f.System {
			got := int64(0)
			if len(out.Reports) == 1 {
				got = out.Reports[0].SystemID
			}
			s.miss("system", ex.Text, fmt.Sprintf("read %s, want %s", u.name(got), u.name(f.System)))
			continue
		}
		t.ok++
		r := out.Reports[0]
		a := intelanalyze.Analyze(intelanalyze.Input{System: r.SystemID, Text: r.Text, Hostiles: r.Hostiles, Clear: r.Clear, NoVisual: r.NoVisual}, d)

		if want := f.Gate + f.Dest; want != 0 || a.Gate != 0 {
			t := s.add("gate")
			t.n++
			if a.Gate == want {
				t.ok++
			} else {
				s.miss("gate", ex.Text, fmt.Sprintf("read %s, want %s", u.name(a.Gate), u.name(want)))
			}
		}
		var got, want []string
		roster := a.Pilots
		if f.Style == StyleLocal {
			if seg, cov, total := index.Segment(r.Text); intelanalyze.PassesRosterBar(seg, cov, total) {
				roster = seg
			}
		}
		for _, p := range roster {
			got = append(got, strings.ToLower(p.Name))
		}
		for _, p := range f.Pilots {
			want = append(want, strings.ToLower(p))
		}
		s.setScore("pilots", ex.Text, got, want)
		got, want = nil, nil
		for _, sh := range a.Ships {
			got = append(got, sh.Name)
		}
		for _, sh := range f.Ships {
			want = append(want, sh.Name)
		}
		s.setScore("ships", ex.Text, got, want)
		if f.Location != "" || a.Location != "" {
			t := s.add("location")
			t.n++
			if a.Location == f.Location {
				t.ok++
			} else {
				s.miss("location", ex.Text, fmt.Sprintf("read %q, want %q", a.Location, f.Location))
			}
		}
		if f.Count > 0 {
			t := s.add("count")
			t.n++
			if r.Hostiles == f.Count {
				t.ok++
			} else {
				s.miss("count", ex.Text, fmt.Sprintf("read %d, want %d", r.Hostiles, f.Count))
			}
		}
		if f.State == "clear" || r.Clear {
			t := s.add("clear")
			t.n++
			if r.Clear == (f.State == "clear") {
				t.ok++
			} else {
				s.miss("clear", ex.Text, fmt.Sprintf("read clear=%v", r.Clear))
			}
		}
	}
	return s
}

// setScore counts a set field's hits, extras and misses.
func (s *Score) setScore(field, text string, got, want []string) {
	t := s.add(field)
	w := map[string]int{}
	for _, x := range want {
		w[x]++
	}
	var extra []string
	for _, x := range got {
		if w[x] > 0 {
			w[x]--
			t.tp++
		} else {
			t.fp++
			extra = append(extra, x)
		}
	}
	var missed []string
	for x, n := range w {
		for ; n > 0; n-- {
			t.fn++
			missed = append(missed, x)
		}
	}
	if len(extra)+len(missed) > 0 {
		sort.Strings(missed)
		s.miss(field, text, fmt.Sprintf("extra %q, missed %q", extra, missed))
	}
}

// Write prints the score.
func (s *Score) Write(w io.Writer) {
	fmt.Fprintf(w, "%d examples, %d scored, %d left out (pasted with lines kept, or no system in the text)\n\n", s.Examples, s.Scored, s.Skipped)
	names := make([]string, 0, len(s.fields))
	for k := range s.fields {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		t := s.fields[k]
		if t.n > 0 {
			fmt.Fprintf(w, "%-9s accuracy %5.1f%%  (%d of %d)\n", k, pct(t.ok, t.n), t.ok, t.n)
			continue
		}
		p, r := pct(t.tp, t.tp+t.fp), pct(t.tp, t.tp+t.fn)
		fmt.Fprintf(w, "%-9s precision %5.1f%%  recall %5.1f%%  (%d right, %d extra, %d missed)\n", k, p, r, t.tp, t.fp, t.fn)
	}
	for _, k := range names {
		if len(s.misses[k]) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s, first misses:\n", k)
		for _, m := range s.misses[k] {
			fmt.Fprintf(w, "  %s\n", m)
		}
	}
}

func pct(a, b int) float64 {
	if b == 0 {
		return 100
	}
	return 100 * float64(a) / float64(b)
}

func nameID(l string) int64 {
	h := fnv.New32a()
	h.Write([]byte(l))
	return int64(h.Sum32()) + 90000000
}

func (u *Universe) name(id int64) string {
	if s := u.Systems[id]; s != nil {
		return s.Name
	}
	if id == 0 {
		return "nothing"
	}
	return fmt.Sprint(id)
}

func hasSpan(ex Example, label string) bool {
	for _, sp := range ex.Spans {
		if sp.Label == label {
			return true
		}
	}
	return false
}
