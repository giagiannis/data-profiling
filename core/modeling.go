package core

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Modeler is the interface for the objects that model the dataset space.
type Modeler interface {
	//  Configure is responsible to provide the necessary configuration
	// options to the Modeler struct. Call it before Run.
	Configure(map[string]string) error
	// Run initiates the modeling process.
	Run() error

	// Datasets returns the datasets slice
	Datasets() []*Dataset
	// Samples returns the indices of the chosen datasets.
	Samples() map[int]float64
	// AppxValues returns a slice of the approximated values
	AppxValues() []float64

	// ErrorMetrics returns a list of error metrics for the specified modeler
	ErrorMetrics() map[string]float64
}

// NewModeler is the factory method for the modeler object
func NewModeler(
	datasets []*Dataset,
	sr float64,
	coordinates []DatasetCoordinates,
	evaluator DatasetEvaluator) Modeler {
	modeler := new(ScriptBasedModeler)
	modeler.datasets = datasets
	modeler.samplingRate = sr
	modeler.coordinates = coordinates
	modeler.evaluator = evaluator
	return modeler
}

// AbstractModeler implements the common methods of the Modeler structs
type AbstractModeler struct {
	datasets    []*Dataset           // the datasets the modeler refers to
	evaluator   DatasetEvaluator     // the evaluator struct that gets the values
	coordinates []DatasetCoordinates // the dataset coordinates

	samplingRate float64 // the portion of the datasets to examine
	// the dataset indices chosen for samples
	samples    map[int]float64
	appxValues []float64 // the appx values of ALL the datasets
}

// Datasets returns the datasets slice
func (a *AbstractModeler) Datasets() []*Dataset {
	return a.datasets
}

// Samples return the indices of the chosen datasets
func (a *AbstractModeler) Samples() map[int]float64 {
	return a.samples
}

// AppxValues returns the values of all the datasets
func (a *AbstractModeler) AppxValues() []float64 {
	return a.appxValues
}

// ErrorMetrics returns a list of error metrics for the specified model
func (a *AbstractModeler) ErrorMetrics() map[string]float64 {
	if a.appxValues == nil || len(a.appxValues) == 0 {
		return nil
	}
	// evaluation for entire dataset
	var actual []float64
	for _, d := range a.datasets {
		val, err := a.evaluator.Evaluate(d.Path())
		if err != nil {
			log.Println(err)
			actual = append(actual, math.NaN())
		} else {
			actual = append(actual, val)
		}
	}
	errors := make(map[string]float64)
	errors["MSE-all"] = MeanSquaredError(actual, a.appxValues)
	errors["MAPE-all"] = MeanAbsolutePercentageError(actual, a.appxValues)
	errors["R^2-all"] = RSquared(actual, a.appxValues)
	return errors
}

// ScriptBasedModeler utilizes a script to train an ML model and obtain is values
type ScriptBasedModeler struct {
	AbstractModeler
	script string // the script to use for modeling
}

// Configure expects the necessary conf options for the specified struct.
// Specifically, the following parameters are necessary:
// - script: the path of the script to use
func (m *ScriptBasedModeler) Configure(conf map[string]string) error {
	if val, ok := conf["script"]; ok {
		m.script = val
	} else {
		log.Println("script parameter is missing")
		return errors.New("script parameter is missing")
	}
	return nil
}

// Run executes the modeling process and populates the samples, realValues and
// appxValues slices.
func (m *ScriptBasedModeler) Run() error {
	// sample the datasets
	permutation := rand.Perm(len(m.datasets))
	s := int(math.Floor(m.samplingRate * float64(len(m.datasets))))
	m.samples = make(map[int]float64)

	// deploy samples
	var trainingSet, testSet [][]float64
	for i := 0; i < len(permutation) && (len(m.samples) < s); i++ {
		idx := permutation[i]
		val, err := m.evaluator.Evaluate(m.datasets[idx].Path())
		if err != nil {
			log.Printf("%s: %s\n", m.datasets[idx].Path(), err.Error())
		} else {
			m.samples[idx] = val
			trainingSet = append(trainingSet, append(m.coordinates[idx], val))
		}

	}
	log.Println("Picked", len(m.samples), "out of the requested", s, "samples")
	trainFile := createCSVFile(trainingSet, true)
	for _, v := range m.coordinates {
		testSet = append(testSet, v)
	}
	testFile := createCSVFile(testSet, false)
	appx, err := m.executeMLScript(trainFile, testFile)
	if err != nil {
		return err
	}
	m.appxValues = appx
	os.Remove(trainFile)
	os.Remove(testFile)
	return nil
}

// executeMLScript executes the ML script, utilizing the selected samples (indices)
// and populates the real and appx values slices
func (m *ScriptBasedModeler) executeMLScript(trainFile, testFile string) ([]float64, error) {
	var result []float64
	cmd := exec.Command(m.script, trainFile, testFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, errors.New(err.Error() + string(out))
	}
	outputString := string(out)
	array := strings.Split(outputString, "\n")
	result = make([]float64, len(m.datasets))
	for i := 0; i < len(m.datasets); i++ {
		val, err := strconv.ParseFloat(array[i], 64)
		if err != nil {
			log.Println(err)
		} else {
			result[i] = val
		}
	}
	return result, nil
}

// createCSVFile serializes a double float slice to a CSV file and returns
// the filename
func createCSVFile(matrix [][]float64, output bool) string {
	f, err := ioutil.TempFile("/tmp", "csv")
	if err != nil {
		log.Println(err)
	}
	cols := 0
	if len(matrix) > 0 {
		cols = len(matrix[0])
	}
	if output {
		cols--
	}

	for i := 1; i < cols+1; i++ {
		fmt.Fprintf(f, "x%d", i)
		if i < cols {
			fmt.Fprintf(f, ",")
		}
	}
	if output {
		fmt.Fprintf(f, ",class")
	}
	fmt.Fprintf(f, "\n")

	for i := range matrix {
		for j := range matrix[i] {
			fmt.Fprintf(f, "%.5f", matrix[i][j])
			if j < len(matrix[i])-1 {
				fmt.Fprintf(f, ",")
			}
		}
		fmt.Fprintf(f, "\n")
	}
	f.Close()
	return f.Name()
}

// MeanSquaredError returns the MSE of the actual vs the predicted values
func MeanSquaredError(actual, predicted []float64) float64 {
	if len(actual) != len(predicted) || len(actual) == 0 {
		log.Println("actual and predicted values are of different size!!")
		return math.NaN()
	}
	sum := 0.0
	count := 0.0
	for i := range actual {
		if !math.IsNaN(actual[i]) {
			diff := actual[i] - predicted[i]
			sum += diff * diff
			count += 1
		}
	}
	if count > 0 {
		return sum / count
	}
	return math.NaN()
}

// MeanAbsolutePercentageError returns the MAPE of the actual vs the predicted values
func MeanAbsolutePercentageError(actual, predicted []float64) float64 {
	if len(actual) != len(predicted) || len(actual) == 0 {
		log.Println("actual and predicted values are of different size!!")
		return math.NaN()
	}
	sum := 0.0
	count := 0.0
	for i := range actual {
		if actual[i] != 0.0 && !math.IsNaN(actual[i]) {
			count += 1.0
			sum += math.Abs((actual[i] - predicted[i]) / actual[i])
		}
	}
	if count > 0 {
		return sum / count
	}
	return math.NaN()
}

// RSquared returns the coeff. of determination of the actual vs the predicted values
func RSquared(actual, predicted []float64) float64 {
	if len(predicted) != len(actual) || len(predicted) == 0 {
		log.Println("actual and predicted values are of different size!!")
		return math.NaN()
	}
	mean := Mean(actual)
	ssRes, ssTot := 0.0, 0.0
	for i := range actual {
		if !math.IsNaN(actual[i]) {
			ssTot += (actual[i] - mean) * (actual[i] - mean)
			ssRes += (actual[i] - predicted[i]) * (actual[i] - predicted[i])
		}
	}
	if ssTot > 0 {
		return 1.0 - (ssRes / ssTot)
	}
	return math.NaN()
}
