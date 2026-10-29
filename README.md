# intel-lab

Local tools for the intel classifier. Not part of EveSynapse: this
folder sits inside an EveSynapse checkout (so it can use the
reader's internal packages) and is ignored there by
`.git/info/exclude`. Only the trained model goes live, inside
`internal/inteltag`.

Everything here uses public data only: the SDE's map and ships (the
fuzzwork dump the site imports), made-up pilot names, and known intel
phrasing. No player's report is read.

Run every command from this folder.

## 1. Get the SDE (once)

```sh
go run ./cmd/intel-synth -sde sde -fetch
```

Downloads six CSV files (about 23 MB) into `sde/` and prints ten
sample reports.

## 2. Make training and test data

Use different seeds so the test lines are never trained on.

```sh
go run ./cmd/intel-synth -sde sde -n 100000 -seed 1 -out data/train.jsonl
go run ./cmd/intel-synth -sde sde -n 20000 -seed 2 -out data/test.jsonl
```

## 3. Score the deterministic reader (optional)

```sh
go run ./cmd/intel-synth -sde sde -n 20000 -seed 2 -eval
go run ./cmd/intel-synth -sde sde -n 20000 -seed 2 -eval -worst -misses 50
```

`-worst` makes every string some character, as EVE's name lookup
nearly does.

## 4. Train the tagger

```sh
go run ./cmd/intel-train -sde sde -train data/train.jsonl -test data/test.jsonl -out data/tagger.gob.gz
```

Takes a few minutes. Prints each epoch, the model's size, and its
precision and recall per kind on the test set. `-known 0.6` is the
share of pilot names the site's name cache is taken to know;
`-epochs` sets the passes (default 5).

## 5. Ship it

Not wired yet: the next step embeds the model in
`internal/inteltag` and has the site read with it. Shipping then is
copying `data/tagger.gob.gz` over the embedded file, committing, and
releasing as usual.

## Layout

| Path | What |
| --- | --- |
| `synth/` | the generator: universe from the SDE, report frames, phrasings, scoring of the deterministic reader |
| `train/` | training and scoring of the tagger |
| `cmd/intel-synth` | make examples, score the reader |
| `cmd/intel-train` | train and score the tagger |
| `sde/`, `data/` | downloads and generated files; not tracked |
