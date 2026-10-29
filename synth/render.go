package synth

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	"evesynapse/internal/intelanalyze"
)

// Span labels: what a stretch of a report's text is.
const (
	LabelSystem   = "system"   // the report's own system
	LabelGate     = "gate"     // a neighbour named as the gate
	LabelDest     = "dest"     // a neighbour the pilots moved or move to
	LabelArea     = "area"     // a region or constellation
	LabelLocation = "location" // a celestial in the system
	LabelPilot    = "pilot"
	LabelShip     = "ship"
	LabelCount    = "count"    // how many hostiles, or how many of a ship
	LabelActivity = "activity" // camp, bubble, cyno, fleet, cloaky, docked, roam, tackle
	LabelState    = "state"    // clear, nv, question
)

// Deliveries: how a report's text arrived.
const (
	// DeliveryChat is a message typed in the game's chat: one
	// line, names dragged in as links carry the client's star.
	DeliveryChat = "chat"
	// DeliveryChatPaste is a Local or d-scan copy pasted into
	// the game's chat: the client glues its lines together.
	DeliveryChatPaste = "chat_paste"
	// DeliveryPaste is a copy pasted straight into the site or
	// Axon: its lines and tabs kept.
	DeliveryPaste = "paste"
)

// Styles: the shape a report takes.
const (
	StyleSighting = "sighting"
	StyleGate     = "gate"
	StyleMove     = "move"
	StyleClear    = "clear"
	StyleQuestion = "question"
	StyleLocation = "location"
	StyleLocal    = "local"
	StyleDScan    = "dscan"
)

// ShipCount is a ship type and how many, 0 when not said.
type ShipCount struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	N    int    `json:"n,omitempty"`
}

// Frame is what one report says.
type Frame struct {
	Style    string      `json:"style"`
	System   int64       `json:"system"`
	Gate     int64       `json:"gate,omitempty"`
	Dest     int64       `json:"dest,omitempty"`
	Area     int64       `json:"area,omitempty"`
	Location string      `json:"location,omitempty"`
	Pilots   []string    `json:"pilots,omitempty"`
	Ships    []ShipCount `json:"ships,omitempty"`
	Count    int         `json:"count,omitempty"`
	Activity string      `json:"activity,omitempty"`
	State    string      `json:"state,omitempty"`
}

// Span is one labelled stretch of text, in bytes.
type Span struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Label string `json:"label"`
	ID    int64  `json:"id,omitempty"`
	Value string `json:"value,omitempty"`
}

// Example is one rendered report.
type Example struct {
	Text     string `json:"text"`
	Delivery string `json:"delivery"`
	Frame    Frame  `json:"frame"`
	Spans    []Span `json:"spans"`
}

// Generator makes examples from a universe.
type Generator struct {
	U         *Universe
	r         *rand.Rand
	nicks     map[string][]string // full lower-cased name -> nicknames
	areaNames map[string]bool     // words of region and constellation names, made on first use
}

// NewGenerator makes a generator; a seed always makes the same
// examples from the same universe.
func NewGenerator(u *Universe, seed int64) *Generator {
	g := &Generator{U: u, r: rand.New(rand.NewSource(seed)), nicks: map[string][]string{}}
	for nick, full := range intelanalyze.ShipNicknames() {
		g.nicks[full] = append(g.nicks[full], nick)
	}
	for _, v := range g.nicks {
		sort.Strings(v)
	}
	return g
}

func (g *Generator) pick(xs []string) string { return xs[g.r.Intn(len(xs))] }
func (g *Generator) chance(p float64) bool   { return g.r.Float64() < p }

var styleWeights = []struct {
	style string
	w     int
}{
	{StyleSighting, 30}, {StyleGate, 18}, {StyleMove, 12}, {StyleClear, 12},
	{StyleQuestion, 5}, {StyleLocation, 9}, {StyleLocal, 7}, {StyleDScan, 7},
}

// Next makes one example.
func (g *Generator) Next() Example {
	total := 0
	for _, s := range styleWeights {
		total += s.w
	}
	x := g.r.Intn(total)
	style := StyleSighting
	for _, s := range styleWeights {
		if x < s.w {
			style = s.style
			break
		}
		x -= s.w
	}
	return g.Render(g.Sample(style))
}

// Sample draws a frame of a style, with fields left out the way
// limited intel leaves them out.
func (g *Generator) Sample(style string) Frame {
	sys := g.U.reportable[g.r.Intn(len(g.U.reportable))]
	f := Frame{Style: style, System: sys.ID}
	neighbour := sys.Neighbours[g.r.Intn(len(sys.Neighbours))]
	pilots := func(max int, tricky bool) {
		for n := g.r.Intn(max + 1); n > 0; n-- {
			f.Pilots = append(f.Pilots, g.pilotName(tricky))
		}
	}
	ships := func(max int) {
		for n := g.r.Intn(max + 1); n > 0; n-- {
			s := g.U.Ships[g.r.Intn(len(g.U.Ships))]
			if hasShip(f.Ships, s.ID) {
				continue
			}
			sc := ShipCount{ID: s.ID, Name: s.Name}
			if g.chance(0.35) {
				sc.N = 2 + g.r.Intn(8)
			}
			f.Ships = append(f.Ships, sc)
		}
	}
	count := func(p float64) {
		if g.chance(p) {
			f.Count = 1 + g.r.Intn(12)
			if g.chance(0.15) {
				f.Count = 15 + g.r.Intn(40)
			}
		}
	}
	activity := func(p float64, from ...string) {
		if g.chance(p) {
			f.Activity = from[g.r.Intn(len(from))]
		}
	}
	switch style {
	case StyleSighting:
		pilots(3, true)
		ships(2)
		count(0.5)
		activity(0.3, "fleet", "cloaky", "roam", "docked", "cyno")
		if g.chance(0.2) {
			f.State = "nv"
		}
		if len(f.Pilots)+len(f.Ships) == 0 && f.Count == 0 {
			f.Count = 1 + g.r.Intn(5)
		}
	case StyleGate:
		f.Gate = neighbour
		pilots(2, true)
		ships(3)
		count(0.5)
		activity(0.6, "camp", "bubble", "camp", "tackle")
	case StyleMove:
		f.Dest = neighbour
		pilots(2, true)
		ships(2)
		count(0.4)
		activity(0.25, "fleet", "roam")
		if len(f.Pilots)+len(f.Ships) == 0 && f.Count == 0 {
			f.Count = 2 + g.r.Intn(20)
		}
	case StyleClear:
		f.State = "clear"
		if g.chance(0.3) {
			f.Gate = neighbour
		}
	case StyleQuestion:
		f.State = "question"
	case StyleLocation:
		f.Location = g.location()
		pilots(2, true)
		ships(2)
		count(0.3)
		activity(0.4, "camp", "cloaky", "docked", "cyno")
	case StyleLocal:
		n := 2 + g.r.Intn(12)
		for ; n > 0; n-- {
			f.Pilots = append(f.Pilots, g.pilotName(true))
		}
	case StyleDScan:
		ships(6)
		if len(f.Ships) == 0 {
			ships(1)
			if len(f.Ships) == 0 {
				s := g.U.Ships[g.r.Intn(len(g.U.Ships))]
				f.Ships = []ShipCount{{ID: s.ID, Name: s.Name}}
			}
		}
	}
	if style != StyleLocal && style != StyleDScan && style != StyleQuestion && g.chance(0.08) {
		if r, ok := g.area(sys); ok {
			f.Area = r
		}
	}
	return f
}

// location draws a celestial, as the reader names it.
func (g *Generator) location() string {
	switch x := g.r.Float64(); {
	case x < 0.4:
		return fmt.Sprintf("Planet %d Moon %d", 1+g.r.Intn(9), 1+g.r.Intn(12))
	case x < 0.6:
		return fmt.Sprintf("Moon %d", 1+g.r.Intn(12))
	case x < 0.8:
		return fmt.Sprintf("Belt %d", 1+g.r.Intn(8))
	case x < 0.9:
		return fmt.Sprintf("Planet %d", 1+g.r.Intn(9))
	default:
		return "Sun"
	}
}

// area is a region or constellation near sys to name in passing.
func (g *Generator) area(sys *System) (int64, bool) {
	if g.chance(0.7) {
		if _, ok := g.U.Regions[sys.Region]; ok {
			return sys.Region, true
		}
	}
	if _, ok := g.U.Constellations[sys.Constellation]; ok {
		return sys.Constellation, true
	}
	return 0, false
}

// builder writes text and records spans as it goes.
type builder struct {
	b     strings.Builder
	spans []Span
	lower bool
}

func (b *builder) lit(s string) {
	if b.lower {
		s = strings.ToLower(s)
	}
	b.b.WriteString(s)
}

// sp writes one space unless the text is empty or already ends in one.
func (b *builder) sp() {
	s := b.b.String()
	if s != "" && !strings.HasSuffix(s, " ") && !strings.HasSuffix(s, "\n") && !strings.HasSuffix(s, "\t") {
		b.b.WriteByte(' ')
	}
}

func (b *builder) tag(s, label string, id int64, value string) {
	if b.lower {
		s = strings.ToLower(s)
	}
	start := b.b.Len()
	b.b.WriteString(s)
	b.spans = append(b.spans, Span{Start: start, End: b.b.Len(), Label: label, ID: id, Value: value})
}

// Render writes a frame as one message.
func (g *Generator) Render(f Frame) Example {
	ex := Example{Frame: f, Delivery: DeliveryChat}
	b := &builder{}
	switch f.Style {
	case StyleLocal:
		ex.Delivery = g.pasteDelivery()
		g.renderLocal(b, f, ex.Delivery)
	case StyleDScan:
		ex.Delivery = g.pasteDelivery()
		g.renderDScan(b, f, ex.Delivery)
	default:
		// A scout who types fast types lower case.
		b.lower = g.chance(0.15)
		filler := f.Style != StyleQuestion
		if filler && g.chance(0.06) {
			b.lit(g.pick([]string{"heads up ", "fyi ", "inc "}))
		}
		g.renderProse(b, f)
		if filler && g.chance(0.06) {
			b.lit(g.pick([]string{" o7", " ty", " pls", " !"}))
		}
	}
	ex.Text = b.b.String()
	ex.Spans = b.spans
	return ex
}

func (g *Generator) pasteDelivery() string {
	if g.chance(0.5) {
		return DeliveryChatPaste
	}
	return DeliveryPaste
}

// sysName writes a system as a scout would: its name, dragged in
// as a link in chat (the star), or typed lower case.
func (g *Generator) sysName(b *builder, id int64, label string) {
	s := g.U.Systems[id]
	name := s.Name
	if g.chance(0.12) {
		name = strings.ToLower(name)
	}
	b.tag(name, label, id, "")
	if g.chance(0.15) {
		b.lit("*")
	}
}

// pilotNames writes pilots the way a sighting lists them: dragged
// links two spaces apart, or typed with commas or spaces.
func (g *Generator) pilotNames(b *builder, names []string) {
	sep := g.pick([]string{"  ", "  ", ", ", " "})
	star := g.chance(0.25)
	for i, n := range names {
		if i > 0 {
			b.b.WriteString(sep)
		}
		b.tag(n, LabelPilot, 0, n)
		// A name that is also a word is one a scout drags in as
		// a link, and the star says so.
		if star || isTricky(n) {
			b.b.WriteString("*")
		}
	}
}

// shipText writes one ship with its count in one of the ways
// counts are written.
func (g *Generator) shipText(b *builder, s ShipCount, article bool) {
	name := s.Name
	full := strings.ToLower(s.Name)
	if nicks := g.nicks[full]; len(nicks) > 0 && g.chance(0.35) {
		name = nicks[g.r.Intn(len(nicks))]
	} else if g.chance(0.4) {
		name = strings.ToLower(name)
	}
	n := strconv.Itoa(s.N)
	if s.N == 0 {
		if article && g.chance(0.15) {
			b.lit("a ")
		}
		b.tag(name, LabelShip, s.ID, s.Name)
		return
	}
	switch g.r.Intn(4) {
	case 0: // 3 sabres
		b.tag(n, LabelCount, 0, n)
		b.lit(" ")
		b.tag(name+"s", LabelShip, s.ID, s.Name)
	case 1: // 3x sabre
		b.tag(n+"x", LabelCount, 0, n)
		b.lit(" ")
		b.tag(name, LabelShip, s.ID, s.Name)
	case 2: // sabre x3
		b.tag(name, LabelShip, s.ID, s.Name)
		b.lit(" ")
		b.tag("x"+n, LabelCount, 0, n)
	default: // sabre 3x
		b.tag(name, LabelShip, s.ID, s.Name)
		b.lit(" ")
		b.tag(n+"x", LabelCount, 0, n)
	}
}

func (g *Generator) ships(b *builder, ships []ShipCount) {
	sep := g.pick([]string{", ", " + ", " ", " and "})
	for i, s := range ships {
		if i > 0 {
			b.lit(sep)
		}
		g.shipText(b, s, true)
	}
}

// countText writes the hostile count.
func (g *Generator) countText(b *builder, n int) {
	v := strconv.Itoa(n)
	switch g.r.Intn(5) {
	case 0, 1:
		b.tag("+"+v, LabelCount, 0, v)
	case 2:
		b.tag(v+" reds", LabelCount, 0, v)
	case 3:
		b.tag(v+" neuts", LabelCount, 0, v)
	default:
		b.tag(v+" hostiles", LabelCount, 0, v)
	}
}

var activityWords = map[string][]string{
	"camp":   {"gate camp", "camp", "camped", "camping", "gc"},
	"bubble": {"bubble up", "bubbled", "drag bubble", "bubbles up"},
	"cyno":   {"cyno up", "cyno lit", "cyno"},
	"fleet":  {"fleet", "gang", "blob"},
	"cloaky": {"cloaky", "cloaked", "cloaky camper"},
	"docked": {"docked", "docked up"},
	"roam":   {"roaming", "roam"},
	"tackle": {"tackled", "tackle"},
}

func (g *Generator) activityText(b *builder, a string) {
	b.tag(g.pick(activityWords[a]), LabelActivity, 0, a)
}

// locationText writes a celestial in one of the shorthands.
func (g *Generator) locationText(b *builder, loc string) {
	var p, m int
	switch {
	case loc == "Sun":
		b.tag(g.pick([]string{"sun", "the sun", "star"}), LabelLocation, 0, loc)
		return
	case strings.HasPrefix(loc, "Belt "):
		b.tag(strings.ToLower(loc), LabelLocation, 0, loc)
		return
	case strings.HasPrefix(loc, "Moon "):
		b.tag(g.pick([]string{"moon ", "Moon "})+strings.TrimPrefix(loc, "Moon "), LabelLocation, 0, loc)
		return
	}
	if _, err := fmt.Sscanf(loc, "Planet %d Moon %d", &p, &m); err == nil {
		forms := []string{fmt.Sprintf("p%dm%d", p, m), fmt.Sprintf("P%dM%d", p, m), fmt.Sprintf("p%d m%d", p, m),
			fmt.Sprintf("planet %d moon %d", p, m)}
		if p < 10 && m < 10 {
			forms = append(forms, fmt.Sprintf("%d-%d", p, m))
		}
		b.tag(g.pick(forms), LabelLocation, 0, loc)
		return
	}
	if _, err := fmt.Sscanf(loc, "Planet %d", &p); err == nil {
		b.tag(g.pick([]string{fmt.Sprintf("p%d", p), fmt.Sprintf("planet %d", p)}), LabelLocation, 0, loc)
	}
}

func (g *Generator) stateText(b *builder, s string) {
	switch s {
	case "nv":
		b.tag(g.pick([]string{"nv", "nv", "no vis", "novis"}), LabelState, 0, s)
	case "clear":
		b.tag(g.pick([]string{"clr", "clr", "clear", "cleared", "clr now"}), LabelState, 0, s)
	}
}

// who writes the pilots, ships and count a frame names, in a
// scout's order.
func (g *Generator) who(b *builder, f Frame) {
	parts := []func(){}
	if len(f.Pilots) > 0 {
		parts = append(parts, func() { g.pilotNames(b, f.Pilots) })
	}
	if len(f.Ships) > 0 {
		parts = append(parts, func() {
			if len(f.Pilots) > 0 && len(f.Ships) == 1 && g.chance(0.4) {
				open := g.pick([]string{"in a ", "in ", "("})
				b.lit(open)
				g.shipText(b, f.Ships[0], open == "in ")
				if open == "(" {
					b.lit(")")
				}
				return
			}
			g.ships(b, f.Ships)
		})
	}
	if f.Count > 0 {
		parts = append(parts, func() { g.countText(b, f.Count) })
	}
	for i, p := range parts {
		if i > 0 {
			b.sp()
		}
		p()
	}
}

// renderProse writes a typed report.
func (g *Generator) renderProse(b *builder, f Frame) {
	tail := func() {
		if f.Activity != "" {
			b.sp()
			g.activityText(b, f.Activity)
		}
		if f.State == "nv" {
			b.sp()
			g.stateText(b, f.State)
		}
		if f.Area != 0 {
			b.sp()
			b.lit(g.pick([]string{"from ", "came from ", "heading to "}))
			name := g.U.Regions[f.Area]
			if name == "" {
				name = g.U.Constellations[f.Area]
			}
			b.tag(name, LabelArea, f.Area, "")
		}
	}
	switch f.Style {
	case StyleSighting, StyleLocation:
		sysFirst := g.chance(0.8)
		if sysFirst {
			g.sysName(b, f.System, LabelSystem)
			b.sp()
		}
		if f.Location != "" && g.chance(0.5) {
			g.locationText(b, f.Location)
			b.sp()
		}
		g.who(b, f)
		if f.Location != "" && !g.hasLabel(b, LabelLocation) {
			b.sp()
			b.lit(g.pick([]string{"at ", "on ", "@ "}))
			g.locationText(b, f.Location)
		}
		tail()
		if !sysFirst {
			b.sp()
			b.lit(g.pick([]string{"in ", "in ", "@ "}))
			g.sysName(b, f.System, LabelSystem)
		}
	case StyleGate:
		if g.chance(0.5) {
			// "1DQ1-A NOL-M9 gate camp 3 sabres"
			g.sysName(b, f.System, LabelSystem)
			b.sp()
			b.lit(g.pick([]string{"", "on ", "@ "}))
			g.sysName(b, f.Gate, LabelGate)
			b.lit(" gate")
			if f.Activity != "" {
				b.sp()
				g.activityText(b, f.Activity)
				f.Activity = ""
			}
			b.sp()
			g.who(b, f)
		} else {
			// "3 sabres on NOL-M9 gate in 1DQ1-A"
			g.who(b, f)
			if f.Activity != "" && g.chance(0.5) {
				b.sp()
				g.activityText(b, f.Activity)
				f.Activity = ""
			}
			b.sp()
			b.lit(g.pick([]string{"on ", "at ", "@ "}))
			g.sysName(b, f.Gate, LabelGate)
			b.lit(" gate ")
			b.lit(g.pick([]string{"in ", "in ", ""}))
			g.sysName(b, f.System, LabelSystem)
		}
		tail()
	case StyleMove:
		g.sysName(b, f.System, LabelSystem)
		b.sp()
		g.who(b, f)
		b.sp()
		b.lit(g.pick([]string{"jumped to ", "jumping ", "heading to ", "going to ", "-> ", "warping to ", "moving to "}))
		g.sysName(b, f.Dest, LabelDest)
		tail()
	case StyleClear:
		if f.Gate != 0 {
			g.sysName(b, f.System, LabelSystem)
			b.sp()
			g.sysName(b, f.Gate, LabelGate)
			b.lit(" gate ")
			g.stateText(b, f.State)
			return
		}
		if g.chance(0.3) {
			g.stateText(b, f.State)
			b.sp()
			g.sysName(b, f.System, LabelSystem)
		} else {
			g.sysName(b, f.System, LabelSystem)
			b.sp()
			g.stateText(b, f.State)
		}
	case StyleQuestion:
		if g.chance(0.5) {
			b.lit(g.pick([]string{"status ", "stat ", "anyone in "}))
			g.sysName(b, f.System, LabelSystem)
			b.tag("?", LabelState, 0, "question")
		} else {
			g.sysName(b, f.System, LabelSystem)
			b.lit(" ")
			b.tag(g.pick([]string{"status?", "stat?", "?"}), LabelState, 0, "question")
		}
	}
}

func (g *Generator) hasLabel(b *builder, label string) bool {
	for _, s := range b.spans {
		if s.Label == label {
			return true
		}
	}
	return false
}

// renderLocal writes a Local copy: names one a line pasted
// straight in, or glued end to end through the game's chat.
func (g *Generator) renderLocal(b *builder, f Frame, delivery string) {
	g.sysName(b, f.System, LabelSystem)
	if g.chance(0.4) {
		b.lit(fmt.Sprintf(" %d in local:", len(f.Pilots)))
	}
	if delivery == DeliveryChatPaste {
		b.lit(" ")
		for _, n := range f.Pilots {
			b.tag(n, LabelPilot, 0, n)
		}
		return
	}
	for _, n := range f.Pilots {
		b.lit("\n")
		b.tag(n, LabelPilot, 0, n)
	}
}

// renderDScan writes d-scan rows: type ID, the ship's own name,
// its type, the distance. Through the game's chat the tabs come
// out as runs of spaces and the rows run together.
func (g *Generator) renderDScan(b *builder, f Frame, delivery string) {
	tab, nl := "\t", "\n"
	if delivery == DeliveryChatPaste {
		tab, nl = "    ", ""
	}
	if g.chance(0.5) {
		g.sysName(b, f.System, LabelSystem)
		b.lit(" ")
	}
	first := true
	for _, s := range f.Ships {
		n := s.N
		if n == 0 {
			n = 1
		}
		for k := 0; k < n && k < 4; k++ {
			if !first {
				b.lit(nl)
			}
			first = false
			b.lit(strconv.FormatInt(s.ID, 10) + tab)
			own := g.pick([]string{PilotName(g.r, false) + "'s " + s.Name, s.Name, syllableName(g.r)})
			b.lit(own + tab)
			b.tag(s.Name, LabelShip, s.ID, s.Name)
			b.lit(tab + g.distance())
		}
	}
}

func (g *Generator) distance() string {
	switch g.r.Intn(4) {
	case 0:
		return "-"
	case 1:
		return fmt.Sprintf("%d m", 100+g.r.Intn(900))
	case 2:
		return fmt.Sprintf("%s km", strconv.Itoa(1+g.r.Intn(9000)))
	default:
		return fmt.Sprintf("%.1f AU", 0.1+g.r.Float64()*20)
	}
}

func hasShip(ships []ShipCount, id int64) bool {
	for _, s := range ships {
		if s.ID == id {
			return true
		}
	}
	return false
}

// pilotName draws a made-up pilot whose words are not a system,
// region or ship by chance: a name that collides that way is a
// different kind of hard case than the tricky names, and real
// scouts link such a pilot rather than type them.
func (g *Generator) pilotName(tricky bool) string {
	for {
		n := PilotName(g.r, tricky)
		if isTricky(n) || !g.collides(n) {
			return n
		}
	}
}

func (g *Generator) collides(name string) bool {
	if g.areaNames == nil {
		g.areaNames = map[string]bool{}
		for _, n := range g.U.Regions {
			for _, w := range strings.Fields(strings.ToLower(n)) {
				g.areaNames[w] = true
			}
		}
		for _, n := range g.U.Constellations {
			g.areaNames[strings.ToLower(n)] = true
		}
	}
	for _, w := range strings.Fields(strings.ToLower(name)) {
		if _, ok := g.U.ByName[w]; ok {
			return true
		}
		if _, ok := g.U.ShipByName[w]; ok {
			return true
		}
		if g.areaNames[w] {
			return true
		}
	}
	return false
}
