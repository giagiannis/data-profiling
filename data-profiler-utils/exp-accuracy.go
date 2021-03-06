package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/giagiannis/data-profiler/core"
)

type expAccuracyParams struct {
	output      *string          // output file path
	appxOutput  *string          // output approximation files path
	repetitions *int             // number of times to repeat experiment
	threads     *int             // number of threads to utilize
	datasets    []*core.Dataset  //datasets to use
	modelerType core.ModelerType // type of modeler

	smpath   *string // similarity matrix
	k        *int    // k of knn
	mlScript *string // script used for approximation
	coords   *string // coords of datasets

	evaluator core.DatasetEvaluator // evaluator of the datasets

	samplingRates []float64 // samplings rates to run

	writeAppxScores bool // write approximations to file
}

func expAccuracyParseParams() *expAccuracyParams {
	params := new(expAccuracyParams)
	modelerTypeStr :=
		flag.String("mt", "script", "modeler type [knn | script]")
	params.mlScript =
		flag.String("ml", "", "ML script to use for approximation (from script ML)")
	params.output =
		flag.String("o", "", "output path")
	params.repetitions =
		flag.Int("r", 1, "number of repetitions")
	params.threads =
		flag.Int("t", 1, "number of threads")
	params.coords =
		flag.String("c", "", "coordinates file (from script ml)")
	params.smpath =
		flag.String("sm", "", "similarity matrix (from knn ml)")
	params.k =
		flag.Int("k", 5, "k (from knn ml)")
	params.appxOutput =
		flag.String("a", "", "approximations output file")
	loger :=
		flag.String("l", "", "log file")
	scoresFile :=
		flag.String("s", "", "scores file")
	inputPath :=
		flag.String("i", "", "input path")
	srString :=
		flag.String("sr", "", "comma separated sampling rates")

	flag.Parse()
	setLogger(*loger)
	if *params.output == "" || *scoresFile == "" ||
		*inputPath == "" || *srString == "" {
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

	// datasets parsing
	params.datasets = core.DiscoverDatasets(*inputPath)

	// evaluator allocation
	var err error
	params.evaluator, err = core.NewDatasetEvaluator(core.FileBasedEval, map[string]string{"scores": *scoresFile})
	if err != nil {
		log.Fatalln(err)
	}

	// modeler type parsing
	if *modelerTypeStr == "string" {
		params.modelerType = core.ScriptBasedModelerType
	} else if *modelerTypeStr == "knn" {
		params.modelerType = core.KNNModelerType
	}

	// write approximations to file
	params.writeAppxScores = *params.appxOutput != ""

	return params
}

func expAccuracyRun() {
	// inititializing steps
	params := expAccuracyParseParams()
	rand.Seed(int64(time.Now().Nanosecond()))
	output := setOutput(*params.output)
	defer output.Close()

	results := make(map[float64][]map[string]float64)

	// threads configuration
	sync := make(chan bool, *params.threads)
	resChannel := make(chan resChannelResult)
	for i := 0; i < *params.threads; i++ {
		sync <- true
	}

	go func() {
		for r := 0; r < *params.repetitions; r++ {
			for _, sr := range params.samplingRates {
				modeler := core.NewModeler(params.modelerType, params.datasets, sr, params.evaluator)
				if params.modelerType == core.ScriptBasedModelerType {
					modeler.Configure(map[string]string{"script": *params.mlScript, "coordinates": *params.coords})
				} else {
					modeler.Configure(map[string]string{"k": fmt.Sprintf("%d", *params.k), "smatrix": *params.smpath})
				}
				go runModeler(sr, params.writeAppxScores, modeler, sync, resChannel)
			}
		}
	}()

	noResults := *params.repetitions * len(params.samplingRates)
	for i := 0; i < noResults; i++ {
		v := <-resChannel
		if _, ok := results[v.sr]; !ok {
			results[v.sr] = make([]map[string]float64, 0)
		}
		results[v.sr] = append(results[v.sr], v.res)

		if params.writeAppxScores {
			err := writeAppxScores(i, params, &v)
			if err != nil {
				log.Println(err)
			}
		}
	}
	log.Println(results)

	keys := writeResults(output, results, params.samplingRates)
	fmt.Println("Column names/indices:")
	fmt.Printf("%d - %s\n", 1, "sr")
	for i, k := range keys {
		fmt.Printf("%d - %s\n", i+2, k)
	}

}

// writeResults writes the results to the output file and returns a string slice
// containing the names of the CSV's columns
func writeResults(output *os.File, results map[float64][]map[string]float64, samplingRates []float64) []string {
	keys, keysFinal := make([]string, 0), make([]string, 0)
	getValue := func(key string, results []map[string]float64) []float64 {
		res := make([]float64, 0)
		for _, v := range results {
			res = append(res, v[key])
		}
		return res
	}
	for _, sr := range samplingRates {
		rLine := results[sr]
		if len(keys) == 0 { // get and print header
			for k := range rLine[0] {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			fmt.Fprintf(output, "sr")
			for _, k := range keys {
				for _, k2 := range []string{"mean", "stddev", "median"} {
					fmt.Fprintf(output, "\t%s", k+"-"+k2)
					keysFinal = append(keysFinal, k+"-"+k2)
				}
			}
			fmt.Fprintf(output, "\n")
		}

		fmt.Fprintf(output, "%.2f", sr)
		for _, k := range keys {
			values := getValue(k, rLine)
			mean, stddev, median := core.Mean(values), core.StdDev(values), core.Percentile(values, 50)
			fmt.Fprintf(output, "\t%.5f\t%.5f\t%.5f", mean, stddev, median)
		}
		fmt.Fprintf(output, "\n")
	}
	return keysFinal
}

// writeAppxScores writes approximation scores to a file stored at a given path.
func writeAppxScores(repetition int, params *expAccuracyParams, res *resChannelResult) error {
	appxOutFilepath := fmt.Sprintf("%s_%f_%d", *params.appxOutput, res.sr, repetition)

	appxOutput := setOutput(appxOutFilepath)
	defer appxOutput.Close()

	b, err := res.appx.Serialize()
	if err != nil {
		log.Println(err)
		return err
	}

	appxOutput.Write(b)

	return nil
}

type resChannelResult struct {
	sr  float64
	res map[string]float64
	appx *core.DatasetScores
}

func runModeler(sr float64, writeAppxScores bool, modeler core.Modeler, sync chan bool, resChannel chan resChannelResult) {
	<-sync
	err := modeler.Run()
	if err != nil {
		log.Println(err)
		sync <- true
		return
	}
	res := modeler.ErrorMetrics()
	res["TimeExec"] = modeler.ExecTime()
	res["TimeEval"] = modeler.EvalTime()

	var appxScores *core.DatasetScores = nil
	if writeAppxScores {
		appxScores = core.AppxScores(modeler)
	}

	resChannel <- resChannelResult{sr, res, appxScores}
	sync <- true
}
