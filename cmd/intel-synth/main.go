// Command intel-synth makes synthetic intel reports, labelled span
// by span, from public data only (intel-lab/synth): the SDE's
// map and ships from the same dump the site imports, made-up pilot
// names, and the phrasing intel channels are known to use. It reads
// no player's report.
//
//	intel-synth -sde ./sde -fetch -n 20000 -out synth.jsonl
//	intel-synth -sde ./sde -n 5000 -eval          # score the reader
//	intel-synth -sde ./sde -n 5000 -eval -worst   # every name a character
//
// -fetch downloads the SDE files the generator needs into -sde
// when they are missing. Each output line is one example: the
// text, how it arrived, the frame it was made from, and its spans.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"evesynapse/intel-lab/synth"
)

const sdeBase = "https://www.fuzzwork.co.uk/dump/latest/csv/"

func main() {
	dir := flag.String("sde", "", "directory of SDE CSV files (plain or .bz2)")
	fetch := flag.Bool("fetch", false, "download missing SDE files into -sde first")
	n := flag.Int("n", 1000, "how many examples")
	seed := flag.Int64("seed", 20261029, "random seed: the same seed makes the same examples")
	out := flag.String("out", "", "write the examples here as JSON lines (- for stdout)")
	eval := flag.Bool("eval", false, "score the deterministic reader on the examples")
	worst := flag.Bool("worst", false, "with -eval: every name is some character")
	misses := flag.Int("misses", 6, "with -eval: how many misses to show per field")
	flag.Parse()
	if *dir == "" {
		fail(fmt.Errorf("-sde is required"))
	}
	if *fetch {
		if err := fetchSDE(*dir); err != nil {
			fail(err)
		}
	}
	u, err := synth.LoadSDE(*dir)
	if err != nil {
		fail(err)
	}
	g := synth.NewGenerator(u, *seed)
	examples := make([]synth.Example, *n)
	for i := range examples {
		examples[i] = g.Next()
	}
	if *out != "" {
		if err := writeJSONL(*out, examples); err != nil {
			fail(err)
		}
	}
	if *eval {
		synth.Evaluate(u, examples, *worst, *misses).Write(os.Stdout)
	}
	if *out == "" && !*eval {
		for _, ex := range examples[:min(10, len(examples))] {
			fmt.Printf("%-10s %q\n", ex.Delivery, ex.Text)
		}
	}
}

func writeJSONL(path string, examples []synth.Example) error {
	var w io.Writer = os.Stdout
	if path != "-" {
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	for _, ex := range examples {
		if err := enc.Encode(ex); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// fetchSDE downloads the files the generator reads when neither
// form is in dir yet: the plain file, or the .bz2 a mirror may
// serve instead.
func fetchSDE(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	for _, name := range synth.SDEFiles {
		if exists(filepath.Join(dir, name)) || exists(filepath.Join(dir, name+".bz2")) {
			continue
		}
		var resp *http.Response
		saveAs := name
		for _, cand := range []string{name, name + ".bz2"} {
			fmt.Fprintf(os.Stderr, "fetching %s\n", cand)
			r, err := client.Get(sdeBase + cand)
			if err != nil {
				return err
			}
			if r.StatusCode == http.StatusOK {
				resp, saveAs = r, cand
				break
			}
			r.Body.Close()
		}
		if resp == nil {
			return fmt.Errorf("%s: not found at %s", name, sdeBase)
		}
		tmp := filepath.Join(dir, saveAs+".part")
		f, err := os.Create(tmp)
		if err != nil {
			resp.Body.Close()
			return err
		}
		_, err = io.Copy(f, resp.Body)
		resp.Body.Close()
		f.Close()
		if err != nil {
			return err
		}
		if err := os.Rename(tmp, filepath.Join(dir, saveAs)); err != nil {
			return err
		}
	}
	return nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "intel-synth:", err)
	os.Exit(1)
}
