// Package synth makes synthetic intel reports for training and
// measuring the intel reader, from public data only: the SDE's map
// and ships, made-up pilot names, and the phrasing intel channels
// are known to use. No player's report is read or reproduced.
//
// A report is a Frame (what the scout knows: the system, the gate
// or a celestial, pilots, ships, a count, what they are doing, a
// clear, no visual or a question) with some fields left out, as
// intel often is. Render writes a frame as one message in one of
// the known phrasings and returns it with every entity's span
// labelled, so the labels are true by construction.
//
// How the text arrived is its own variable (Delivery): typed in
// the game's chat, a Local or d-scan copy pasted into the game's
// chat (where the client glues the lines together), or a copy
// pasted straight into the site or Axon (lines and tabs kept).
package synth

import (
	"bufio"
	"compress/bzip2"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"evesynapse/internal/intelanalyze"
)

// System is one solar system.
type System struct {
	ID            int64
	Name          string
	Region        int64
	Constellation int64
	Security      float64
	Neighbours    []int64
}

// Ship is one published ship type.
type Ship struct {
	ID    int64
	Name  string
	Group string
	Class string // intelanalyze's class for the group
}

// Universe is the public data reports are made from.
type Universe struct {
	Systems        map[int64]*System
	ByName         map[string]*System // lower-cased
	Regions        map[int64]string
	Constellations map[int64]string
	Ships          []Ship
	ShipByName     map[string]Ship // lower-cased
	// reportable are the systems intel is told about: k-space
	// systems with at least one gate, weighted toward nullsec.
	reportable []*System
}

// The SDE files LoadSDE reads, as the fuzzwork dump names them
// (each may be present plain or as .bz2).
var SDEFiles = []string{
	"mapSolarSystems.csv", "mapSolarSystemJumps.csv", "mapRegions.csv",
	"mapConstellations.csv", "invTypes.csv", "invGroups.csv",
}

// LoadSDE reads the universe from a directory of SDE CSV files.
func LoadSDE(dir string) (*Universe, error) {
	u := &Universe{
		Systems: map[int64]*System{}, ByName: map[string]*System{},
		Regions: map[int64]string{}, Constellations: map[int64]string{},
		ShipByName: map[string]Ship{},
	}
	err := eachRow(dir, "mapSolarSystems.csv", func(r row) {
		id := r.id("solarSystemID")
		if id == 0 || id >= 31000000 { // wormholes and beyond: no gates
			return
		}
		sec, _ := strconv.ParseFloat(r.s("security"), 64)
		s := &System{ID: id, Name: r.s("solarSystemName"), Region: r.id("regionID"),
			Constellation: r.id("constellationID"), Security: sec}
		u.Systems[id] = s
		u.ByName[strings.ToLower(s.Name)] = s
	})
	if err != nil {
		return nil, err
	}
	if err := eachRow(dir, "mapSolarSystemJumps.csv", func(r row) {
		from, to := u.Systems[r.id("fromSolarSystemID")], u.Systems[r.id("toSolarSystemID")]
		if from != nil && to != nil {
			from.Neighbours = append(from.Neighbours, to.ID)
		}
	}); err != nil {
		return nil, err
	}
	if err := eachRow(dir, "mapRegions.csv", func(r row) {
		if id := r.id("regionID"); id < 11000000 {
			u.Regions[id] = r.s("regionName")
		}
	}); err != nil {
		return nil, err
	}
	if err := eachRow(dir, "mapConstellations.csv", func(r row) {
		if id := r.id("constellationID"); id < 21000000 {
			u.Constellations[id] = r.s("constellationName")
		}
	}); err != nil {
		return nil, err
	}
	groups := map[int64]string{}
	if err := eachRow(dir, "invGroups.csv", func(r row) {
		if r.id("categoryID") == 6 {
			groups[r.id("groupID")] = r.s("groupName")
		}
	}); err != nil {
		return nil, err
	}
	if err := eachRow(dir, "invTypes.csv", func(r row) {
		g, ok := groups[r.id("groupID")]
		if !ok || r.s("published") != "1" {
			return
		}
		// Hulls a scout reports: not the pod or a shuttle, and
		// not the long special-edition names nobody types.
		if g == "Capsule" || g == "Shuttle" || len(strings.Fields(r.s("typeName"))) > 3 {
			return
		}
		s := Ship{ID: r.id("typeID"), Name: r.s("typeName"), Group: g, Class: intelanalyze.ClassOfGroup(g)}
		u.Ships = append(u.Ships, s)
		u.ShipByName[strings.ToLower(s.Name)] = s
	}); err != nil {
		return nil, err
	}
	u.finish()
	if len(u.reportable) == 0 || len(u.Ships) == 0 {
		return nil, fmt.Errorf("intelsynth: no systems or ships read from %s", dir)
	}
	return u, nil
}

// finish orders what was read, so a seed always makes the same
// reports, and picks the reportable systems.
func (u *Universe) finish() {
	sort.Slice(u.Ships, func(i, j int) bool { return u.Ships[i].ID < u.Ships[j].ID })
	ids := make([]int64, 0, len(u.Systems))
	for id, s := range u.Systems {
		sort.Slice(s.Neighbours, func(i, j int) bool { return s.Neighbours[i] < s.Neighbours[j] })
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	u.reportable = nil
	for _, id := range ids {
		s := u.Systems[id]
		if len(s.Neighbours) == 0 {
			continue
		}
		// Intel channels live in nullsec and lowsec: weight
		// them, keep some highsec.
		w := 1
		switch {
		case s.Security <= 0:
			w = 6
		case s.Security < 0.45:
			w = 3
		}
		for k := 0; k < w; k++ {
			u.reportable = append(u.reportable, s)
		}
	}
}

// NewUniverse builds a universe from parts, for tests.
func NewUniverse(systems []*System, regions, constellations map[int64]string, ships []Ship) *Universe {
	u := &Universe{Systems: map[int64]*System{}, ByName: map[string]*System{},
		Regions: regions, Constellations: constellations, ShipByName: map[string]Ship{}}
	for _, s := range systems {
		u.Systems[s.ID] = s
		u.ByName[strings.ToLower(s.Name)] = s
	}
	for _, s := range ships {
		if s.Class == "" {
			s.Class = intelanalyze.ClassOfGroup(s.Group)
		}
		u.Ships = append(u.Ships, s)
		u.ShipByName[strings.ToLower(s.Name)] = s
	}
	u.finish()
	return u
}

type row struct {
	rec []string
	idx map[string]int
}

func (r row) s(col string) string {
	if i, ok := r.idx[col]; ok && i < len(r.rec) {
		return strings.TrimSpace(r.rec[i])
	}
	return ""
}

func (r row) id(col string) int64 {
	n, _ := strconv.ParseInt(r.s(col), 10, 64)
	return n
}

// eachRow walks name's rows, plain or bz2-compressed in dir.
func eachRow(dir, name string, fn func(row)) error {
	var body io.Reader
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		f, err = os.Open(filepath.Join(dir, name+".bz2"))
		if err != nil {
			return fmt.Errorf("intelsynth: %s: %w", name, err)
		}
		body = bzip2.NewReader(f)
	} else {
		body = f
	}
	defer f.Close()
	// The dump's files open with a byte-order mark, before the
	// first header's quote: skip it so the header reads clean.
	br := bufio.NewReader(body)
	if b, err := br.Peek(3); err == nil && string(b) == "\xef\xbb\xbf" {
		br.Discard(3)
	}
	cr := csv.NewReader(br)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	head, err := cr.Read()
	if err != nil {
		return fmt.Errorf("intelsynth: %s header: %w", name, err)
	}
	idx := map[string]int{}
	for i, h := range head {
		idx[strings.TrimPrefix(strings.TrimSpace(h), "\xef\xbb\xbf")] = i
	}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			continue
		}
		fn(row{rec: rec, idx: idx})
	}
}
