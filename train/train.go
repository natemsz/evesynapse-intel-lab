// Package train trains the intel tagger (internal/inteltag) on the
// synthetic reports intel-lab/synth makes, and scores it. Local
// only: the site runs the trained model, never this.
package train

import (
	"fmt"
	"hash/fnv"
	"io"
	"math/rand"
	"sort"
	"strings"

	"evesynapse/intel-lab/synth"
	"evesynapse/internal/intelanalyze"
	"evesynapse/internal/inteltag"
)

// Lexicon answers the tagger's name lists from a universe: the
// same lists the site answers from its SDE dictionaries.
type Lexicon struct {
	u      *synth.Universe
	areas  map[string]bool
	nicks  map[string]string
	pilots map[string]bool // the names the site has confirmed as characters
}

// NewLexicon builds the lexicon for u.
func NewLexicon(u *synth.Universe) *Lexicon {
	l := &Lexicon{u: u, areas: map[string]bool{}, nicks: intelanalyze.ShipNicknames()}
	for _, n := range u.Regions {
		l.areas[strings.ToLower(n)] = true
	}
	for _, n := range u.Constellations {
		l.areas[strings.ToLower(n)] = true
	}
	return l
}

// Kinds implements inteltag.Lexicon.
func (l *Lexicon) Kinds(p string) []string {
	var out []string
	if _, ok := l.u.ByName[p]; ok {
		out = append(out, "system")
	}
	for _, c := range []string{p, strings.TrimSuffix(p, "s")} {
		if _, ok := l.u.ShipByName[c]; ok {
			out = append(out, "ship")
			break
		}
		if full, ok := l.nicks[c]; ok {
			if _, ok := l.u.ShipByName[full]; ok {
				out = append(out, "ship")
				break
			}
		}
	}
	if l.areas[p] {
		out = append(out, "area")
	}
	if l.pilots[p] {
		out = append(out, "pilot")
	}
	return out
}

// sample is one example ready to train on: its tokens' feature IDs
// and gold label IDs.
type sample struct {
	feats [][]int32
	gold  []int
}

// goldLabels gives each token of ex its begin, inside or outside
// label; ok is false when a span does not fall on token boundaries
// (a glued paste the tagger cannot split), and the example is not
// used.
func goldLabels(m *inteltag.Model, toks []inteltag.Token, ex synth.Example) ([]int, bool) {
	idx := map[string]int{}
	for i, l := range m.Labels {
		idx[l] = i
	}
	out := make([]int, len(toks))
	for _, sp := range ex.Spans {
		first := true
		for t, tok := range toks {
			end := tok.End
			if strings.HasSuffix(tok.Text, "*") {
				end--
			}
			if tok.End <= sp.Start || tok.Start >= sp.End {
				continue
			}
			if tok.Start < sp.Start || end > sp.End {
				return nil, false
			}
			if first {
				out[t] = idx["B-"+sp.Label]
				first = false
			} else {
				out[t] = idx["I-"+sp.Label]
			}
		}
		if first {
			return nil, false
		}
	}
	return out, true
}

// Options shape a training run.
type Options struct {
	Epochs int
	Seed   int64
	Log    io.Writer
}

// Train learns a model from examples.
func Train(examples []synth.Example, lex inteltag.Lexicon, o Options) (*inteltag.Model, int) {
	m := inteltag.NewModel()
	m.Lex = lex
	L := len(m.Labels)
	ids := map[string]int32{}
	var names []string
	var samples []sample
	skipped := 0
	for _, ex := range examples {
		toks := inteltag.Tokenize(ex.Text)
		gold, ok := goldLabels(m, toks, ex)
		if !ok || len(toks) == 0 {
			skipped++
			continue
		}
		fs := inteltag.Features(toks, lex)
		s := sample{gold: gold, feats: make([][]int32, len(fs))}
		for t, f := range fs {
			for _, name := range f {
				id, ok := ids[name]
				if !ok {
					id = int32(len(names))
					ids[name] = id
					names = append(names, name)
				}
				s.feats[t] = append(s.feats[t], id)
			}
		}
		samples = append(samples, s)
	}
	// Averaged perceptron: w is the running weights, u the
	// step-weighted sum of updates; the average is w - u/c.
	w := make([][]float32, len(names))
	u := make([][]float32, len(names))
	for i := range w {
		w[i], u[i] = make([]float32, L), make([]float32, L)
	}
	tw, tu := make([][]float32, L+1), make([][]float32, L+1)
	for i := range tw {
		tw[i], tu[i] = make([]float32, L), make([]float32, L)
	}
	r := rand.New(rand.NewSource(o.Seed))
	c := float32(1)
	for ep := 0; ep < o.Epochs; ep++ {
		r.Shuffle(len(samples), func(i, j int) { samples[i], samples[j] = samples[j], samples[i] })
		wrong := 0
		for _, s := range samples {
			pred := decodeIDs(m, w, tw, s.feats)
			prevG, prevP := L, L
			for t := range s.gold {
				g, p := s.gold[t], pred[t]
				if g != p {
					wrong++
					for _, f := range s.feats[t] {
						w[f][g]++
						w[f][p]--
						u[f][g] += c
						u[f][p] -= c
					}
				}
				if g != p || prevG != prevP {
					tw[prevG][g]++
					tw[prevP][p]--
					tu[prevG][g] += c
					tu[prevP][p] -= c
				}
				prevG, prevP = g, p
			}
			c++
		}
		if o.Log != nil {
			fmt.Fprintf(o.Log, "epoch %d: %d tokens wrong\n", ep+1, wrong)
		}
	}
	// The averaged weights, small ones dropped.
	m.W = map[string][]float32{}
	for i, name := range names {
		var keep bool
		v := make([]float32, L)
		for l := range v {
			v[l] = w[i][l] - u[i][l]/c
			if v[l] > 0.01 || v[l] < -0.01 {
				keep = true
			}
		}
		if keep {
			m.W[name] = v
		}
	}
	for i := range tw {
		for l := range tw[i] {
			m.Trans[i][l] = tw[i][l] - tu[i][l]/c
		}
	}
	return m, skipped
}

// decodeIDs decodes over interned features and the running
// weights, for training speed.
func decodeIDs(m *inteltag.Model, w [][]float32, trans [][]float32, feats [][]int32) []int {
	L := len(m.Labels)
	emit := make([][]float64, len(feats))
	for t, fs := range feats {
		e := make([]float64, L)
		for _, f := range fs {
			for l, v := range w[f] {
				e[l] += float64(v)
			}
		}
		emit[t] = e
	}
	m.Trans = trans
	return m.DecodeScores(emit)
}

// Score is a model's span-level result per kind.
type Score struct {
	kinds   map[string]*[3]int // right, extra, missed
	Scored  int
	Skipped int
}

// Evaluate tags examples and compares spans with the labels.
func Evaluate(m *inteltag.Model, examples []synth.Example) *Score {
	s := &Score{kinds: map[string]*[3]int{}}
	add := func(k string, i int) {
		if s.kinds[k] == nil {
			s.kinds[k] = &[3]int{}
		}
		s.kinds[k][i]++
	}
	for _, ex := range examples {
		toks := inteltag.Tokenize(ex.Text)
		if _, ok := goldLabels(m, toks, ex); !ok {
			s.Skipped++
			continue
		}
		s.Scored++
		want := map[[3]string]bool{}
		for _, sp := range ex.Spans {
			want[[3]string{fmt.Sprint(sp.Start), fmt.Sprint(sp.End), sp.Label}] = true
		}
		for _, sp := range m.Tag(ex.Text) {
			k := [3]string{fmt.Sprint(sp.Start), fmt.Sprint(sp.End), sp.Kind}
			if want[k] {
				add(sp.Kind, 0)
				delete(want, k)
			} else {
				add(sp.Kind, 1)
			}
		}
		for k := range want {
			add(k[2], 2)
		}
	}
	return s
}

// Write prints the score.
func (s *Score) Write(w io.Writer) {
	fmt.Fprintf(w, "%d examples scored, %d left out (spans inside a glued paste)\n\n", s.Scored, s.Skipped)
	var ks []string
	for k := range s.kinds {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var all [3]int
	for _, k := range ks {
		v := s.kinds[k]
		for i := range all {
			all[i] += v[i]
		}
		fmt.Fprintf(w, "%-9s precision %5.1f%%  recall %5.1f%%  (%d right, %d extra, %d missed)\n",
			k, pct(v[0], v[0]+v[1]), pct(v[0], v[0]+v[2]), v[0], v[1], v[2])
	}
	fmt.Fprintf(w, "%-9s precision %5.1f%%  recall %5.1f%%\n", "all", pct(all[0], all[0]+all[1]), pct(all[0], all[0]+all[2]))
}

func pct(a, b int) float64 {
	if b == 0 {
		return 100
	}
	return 100 * float64(a) / float64(b)
}

// KnowPilots adds the pilots the site would have confirmed by the
// time it reads these examples: about frac of their names, chosen
// by name so a name is known everywhere or nowhere, and every
// tricky name (EVE confirms those as characters too).
func (l *Lexicon) KnowPilots(examples []synth.Example, frac float64) {
	if l.pilots == nil {
		l.pilots = map[string]bool{}
	}
	for _, n := range synth.TrickyNames() {
		l.pilots[strings.ToLower(n)] = true
	}
	for _, ex := range examples {
		for _, p := range ex.Frame.Pilots {
			h := fnv.New32a()
			h.Write([]byte(strings.ToLower(p)))
			if float64(h.Sum32()%1000) < frac*1000 {
				l.pilots[strings.ToLower(p)] = true
			}
		}
	}
}
