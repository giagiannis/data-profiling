package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/giagiannis/data-profiler/core"
)

type expOrderingParams struct {
	mlScript    *string // script used for approximation
	output      *string // output file path
	repetitions *int    // number of times to repeat experiment
	threads     *int    // number of threads to utilize

	coords []core.DatasetCoordinates // coords of datasets
	scores []float64                 // scores of datasets

	samplingRates []float64 // samplings rates to run
}

func expOrderingParseParams() *expOrderingParams {
	params := new(expOrderingParams)
	params.mlScript =
		flag.String("ml", "", "ML script to use for approximation")
	params.output =
		flag.String("o", "", "output path")
	params.repetitions =
		flag.Int("r", 1, "number of repetitions")
	params.threads =
		flag.Int("t", 1, "number of threads")
	loger :=
		flag.String("l", "", "log file")

	coordsFile :=
		flag.String("c", "", "coordinates file")
	scoresFile :=
		flag.String("s", "", "scores file")
	idxFile :=
		flag.String("i", "", "index file")
	srString :=
		flag.String("sr", "", "comma separated sampling rates")

	flag.Parse()
	setLogger(*loger)
	if *params.mlScript == "" || *params.output == "" || *coordsFile == "" ||
		*scoresFile == "" || *idxFile == "" || *srString == "" {
		fmt.Println("Options:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// sampling rates parsing
	a := strings.Split(*srString, ",")
	params.samplingRates = make([]float64, 0)
	for i := range a {
		v, err := strconv.ParseFloat(a[i], 64)
		if err == nil {
			params.samplingRates = append(params.samplingRates, v)
		}
	}

	// idx file parsing
	f, err := os.Open(*idxFile)
	if err != nil {
		log.Fatalln(err)
	}
	buf, err := ioutil.ReadAll(f)
	if err != nil {
		log.Fatalln(err)
	}
	idx := make([]string, 0)
	for i, line := range strings.Split(string(buf), "\n") {
		a := strings.Split(line, "\t")
		if len(a) == 2 {
			j, err := strconv.ParseInt(a[0], 10, 32)
			if err != nil || int(j) != i {
				log.Fatalln(err)
			}
			idx = append(idx, a[1])
		}
	}
	f.Close()

	// coordinates file parsing
	f, err = os.Open(*coordsFile)
	if err != nil {
		log.Fatalln(err)
	}
	buf, err = ioutil.ReadAll(f)
	if err != nil {
		log.Fatalln(err)
	}
	params.coords = make([]core.DatasetCoordinates, 0)
	for i, line := range strings.Split(string(buf), "\n") {
		a := strings.Split(line, " ")
		res := make(core.DatasetCoordinates, 0)
		if i > 0 && len(a) > 0 {
			for _, s := range a {
				if s != "" {
					v, err := strconv.ParseFloat(s, 64)
					if err != nil {
						log.Fatalln(err)
					}
					res = append(res, v)
				}
			}
			if len(res) > 0 {
				params.coords = append(params.coords, res)
			}
		}
	}
	f.Close()

	// scores
	f, err = os.Open(*scoresFile)
	if err != nil {
		log.Fatalln(err)
	}
	buf, err = ioutil.ReadAll(f)
	if err != nil {
		log.Fatalln(err)
	}
	scores := core.NewDatasetScores()
	scores.Deserialize(buf)
	params.scores = make([]float64, len(scores.Scores))
	for i, path := range idx {
		params.scores[i] = scores.Scores[path]
	}
	f.Close()

	return params
}

type evalResults struct {
	tau                         float64
	top10, top25, top50, top100 float64
}

func expOrderingRun() {
	// inititializing steps
	params := expOrderingParseParams()
	rand.Seed(int64(time.Now().Nanosecond()))
	output := setOutput(*params.output)
	fmt.Fprintf(output, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		"sr", "tau-avg", "tau-perc-0", "tau-perc-25", "tau-perc-50", "tau-perc-75", "tau-perc-100",
		"top10-avg", "top10-perc-0", "top10-perc-25", "top10-perc-50", "top10-perc-75", "top10-perc-100",
		"top25-avg", "top25-perc-0", "top25-perc-25", "top25-perc-50", "top25-perc-75", "top25-perc-100",
		"top50-avg", "top50-perc-0", "top50-perc-25", "top50-perc-50", "top50-perc-75", "top50-perc-100",
	)

	slice := make([]int, len(params.coords))
	for i := 0; i < len(slice); i++ {
		slice[i] = i
	}

	testset := generateSet(slice[0:int(float64(len(slice))*1.0)], params.coords, params.scores)

	executeScript := func(script, trainset, testset string) []float64 {
		cmd := exec.Command(script, trainset, testset)
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Println(err)
		}
		result := make([]float64, 0)
		for _, line := range strings.Split(string(out), "\n") {
			val, err := strconv.ParseFloat(line, 64)
			if err == nil {
				result = append(result, val)
			}

		}
		return result
	}

	topKCommon := func(x, y []int, k int) float64 {
		set := make(map[int]bool)
		for i, rank := range x {
			if rank < k {
				set[i] = true
			}
		}
		count := 0
		for j, rank := range y {
			if rank < k && set[j] {
				count += 1
			}
		}
		return float64(count) / float64(k)
	}

	eval := func(sr float64) *evalResults {
		perm := rand.Perm(len(params.coords))
		trainsetIndexes := perm[0:int(float64(len(perm))*sr)]
		trainset := generateSet(trainsetIndexes, params.coords, params.scores)
		appxScores := executeScript(*params.mlScript, trainset, testset)
		ranksAppx, ranksScores := getRanks(appxScores), getRanks(params.scores)
		res := new(evalResults)
		res.top10 = topKCommon(ranksAppx, ranksScores, int(float64(len(ranksAppx))*0.1))
		res.top25 = topKCommon(ranksAppx, ranksScores, int(float64(len(ranksAppx))*0.25))
		res.top50 = topKCommon(ranksAppx, ranksScores, int(float64(len(ranksAppx))*0.5))
		res.tau = getKendalTau(ranksAppx, ranksScores)
		return res
	}

	// execute
	for _, sr := range params.samplingRates {
		resultsTau, resultsTop10, resultsTop25, resultsTop50 :=
			make([]float64, 0), make([]float64, 0), make([]float64, 0), make([]float64, 0)
		done := make(chan *evalResults)
		slots := make(chan bool, *params.threads)
		for i := 0; i < *params.threads; i++ {
			slots <- true
		}

		for i := 0; i < *params.repetitions; i++ {
			go func(done chan *evalResults, slots chan bool, repetition int) {
				log.Printf("[thread-%d] Starting calculation for SR %.2f\n", repetition, sr)
				<-slots
				done <- eval(sr)
				slots <- true
				log.Printf("[thread-%d] Done calculation for SR %.2f\n", repetition, sr)
			}(done, slots, i)
		}
		for i := 0; i < *params.repetitions; i++ {
			v := <-done
			resultsTau = append(resultsTau, v.tau)
			resultsTop10 = append(resultsTop10, v.top10)
			resultsTop25 = append(resultsTop25, v.top25)
			resultsTop50 = append(resultsTop50, v.top50)
		}
		fmt.Fprintf(output,
			"%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\t%.5f\n",
			sr,
			getAverage(resultsTau),
			getPercentile(resultsTau, 0),
			getPercentile(resultsTau, 25),
			getPercentile(resultsTau, 50),
			getPercentile(resultsTau, 75),
			getPercentile(resultsTau, 100),
			getAverage(resultsTop10),
			getPercentile(resultsTop10, 0),
			getPercentile(resultsTop10, 25),
			getPercentile(resultsTop10, 50),
			getPercentile(resultsTop10, 75),
			getPercentile(resultsTop10, 100),
			getAverage(resultsTop25),
			getPercentile(resultsTop25, 0),
			getPercentile(resultsTop25, 25),
			getPercentile(resultsTop25, 50),
			getPercentile(resultsTop25, 75),
			getPercentile(resultsTop25, 100),
			getAverage(resultsTop50),
			getPercentile(resultsTop50, 0),
			getPercentile(resultsTop50, 25),
			getPercentile(resultsTop50, 50),
			getPercentile(resultsTop50, 75),
			getPercentile(resultsTop50, 100),
		)
	}
	os.Remove(testset)
}
