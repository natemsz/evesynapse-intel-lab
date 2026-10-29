package synth

import (
	"math/rand"
	"strconv"
	"strings"
)

// Pilot names are made up, never taken from players: EVE-shaped
// first-and-last names, one-word handles with digits or inner
// capitals, and on purpose the hard ones, characters named for an
// everyday word, a place or a thing ("Help", "Moon", "Local"),
// which the reader must read as a pilot only where the text puts
// a pilot.

var (
	nameStarts = []string{"Ka", "Vel", "Tor", "Ash", "Mir", "Dra", "Sol", "Ren", "Bor", "Zan",
		"Ny", "Kel", "Ar", "Ul", "Ves", "Mor", "Tal", "Ix", "Gra", "Sae", "Ro", "Lu", "Ha", "Qu",
		"Ste", "Fen", "Jor", "Ely", "Cas", "Oro"}
	nameMids  = []string{"", "", "", "ra", "en", "ka", "li", "do", "va", "rin", "th", "ma", "zu", "ne"}
	nameEnds  = []string{"n", "r", "s", "x", "th", "a", "o", "ek", "is", "us", "ar", "ia", "on", "ius", "ec"}
	handleBit = []string{"Dark", "Iron", "Void", "Red", "Star", "Ghost", "Cold", "Wolf", "Hex",
		"Blade", "Frost", "Null", "Echo", "Raven", "Storm", "Ash", "Zero", "Rust"}
	// trickyNames are characters named after words, places and
	// things that also appear in reports.
	trickyNames = []string{"Help", "Moon", "Station", "Local", "Clear", "Still", "Gate", "Camp",
		"Wreck", "Red", "Neut", "Tackle", "Sun", "Belt", "Dock", "Warp", "Gone", "Ess",
		"Sort Dragon", "Good Coffee", "Up", "Again", "Nope"}
)

func syllableName(r *rand.Rand) string {
	return nameStarts[r.Intn(len(nameStarts))] + nameMids[r.Intn(len(nameMids))] + nameEnds[r.Intn(len(nameEnds))]
}

// PilotName makes one pilot's name. tricky allows the names that
// are also words; a caller puts those only where a pilot stands.
func PilotName(r *rand.Rand, tricky bool) string {
	switch x := r.Float64(); {
	case tricky && x < 0.08:
		return trickyNames[r.Intn(len(trickyNames))]
	case x < 0.55:
		return syllableName(r) + " " + syllableName(r)
	case x < 0.70:
		return syllableName(r) + " " + syllableName(r) + " " + syllableName(r)
	case x < 0.85:
		return handleBit[r.Intn(len(handleBit))] + handleBit[r.Intn(len(handleBit))] + strconv.Itoa(r.Intn(990)+10)
	default:
		n := syllableName(r)
		if r.Intn(2) == 0 {
			n = strings.ToLower(n)
		}
		return n + strconv.Itoa(r.Intn(99)+1)
	}
}

func isTricky(n string) bool {
	for _, t := range trickyNames {
		if t == n {
			return true
		}
	}
	return false
}

// TrickyNames are the made-up characters named for words, places
// and things: all of them are characters EVE would confirm.
func TrickyNames() []string { return append([]string(nil), trickyNames...) }
