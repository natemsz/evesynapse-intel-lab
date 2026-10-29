// Command intel-train trains the intel tagger (internal/inteltag) on
// examples intel-synth wrote, scores it on a held-out set, and saves
// the model. Local only.
//
//	intel-train -sde ../sde -train train.jsonl -test test.jsonl -out tagger.gob.gz
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"evesynapse/intel-lab/synth"
	"evesynapse/intel-lab/train"
)

func main() {
	sde := flag.String("sde", "", "directory of SDE CSV files: the tagger's name lists")
	trainPath := flag.String("train", "", "training examples (JSON lines from intel-synth)")
	testPath := flag.String("test", "", "held-out examples to score on (another seed)")
	out := flag.String("out", "tagger.gob.gz", "where to save the trained model")
	epochs := flag.Int("epochs", 5, "passes over the training examples")
	seed := flag.Int64("seed", 1, "shuffle seed")
	known := flag.Float64("known", 0.6, "share of pilot names the site has confirmed (its name cache)")
	flag.Parse()
	if *sde == "" || *trainPath == "" {
		fail(fmt.Errorf("-sde and -train are required"))
	}
	u, err := synth.LoadSDE(*sde)
	if err != nil {
		fail(err)
	}
	lex := train.NewLexicon(u)
	examples, err := read(*trainPath)
	if err != nil {
		fail(err)
	}
	lex.KnowPilots(examples, *known)
	start := time.Now()
	fmt.Printf("training on %d examples, %d epochs\n", len(examples), *epochs)
	m, skipped := train.Train(examples, lex, train.Options{Epochs: *epochs, Seed: *seed, Log: os.Stdout})
	fmt.Printf("trained in %s (%d examples left out: spans inside a glued paste), %d features kept\n\n",
		time.Since(start).Round(time.Second), skipped, len(m.W))
	f, err := os.Create(*out)
	if err != nil {
		fail(err)
	}
	if err := m.Save(f); err != nil {
		fail(err)
	}
	f.Close()
	if st, err := os.Stat(*out); err == nil {
		fmt.Printf("saved %s (%.1f MB)\n\n", *out, float64(st.Size())/1e6)
	}
	if *testPath != "" {
		test, err := read(*testPath)
		if err != nil {
			fail(err)
		}
		lex.KnowPilots(test, *known)
		m.Lex = lex
		train.Evaluate(m, test).Write(os.Stdout)
	}
}

func read(path string) ([]synth.Example, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []synth.Example
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var ex synth.Example
		if err := json.Unmarshal(sc.Bytes(), &ex); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, ex)
	}
	return out, sc.Err()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "intel-train:", err)
	os.Exit(1)
}
