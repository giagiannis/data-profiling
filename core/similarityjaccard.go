package core

import (
	"bytes"
	"errors"
	"log"
	"strconv"
	"time"
)

// JaccardEstimator estimates the Jaccard coefficients between the different
// datasets. The Jaccard coefficient between two datasets is defined as
// the cardinality of the intersection divided by the cardinality of the
// union of the two datasets.
type JaccardEstimator struct {
	AbstractDatasetSimilarityEstimator
}

// Compute runs the similarity evaluation and constructs the Similarity Matrix
func (e *JaccardEstimator) Compute() error {
	e.similarities = NewDatasetSimilarities(len(e.datasets))

	log.Println("Fetching datasets in memory")
	if e.datasets == nil || len(e.datasets) == 0 {
		log.Println("No datasets were given")
		return errors.New("Empty dataset slice")
	}
	for _, d := range e.datasets {
		d.ReadFromFile()
	}

	start := time.Now()
	datasetSimilarityEstimatorCompute(e)
	e.duration = time.Since(start).Seconds()

	return nil
}

// Similarity returns the similarity between two datasets
func (e *JaccardEstimator) Similarity(a, b *Dataset) float64 {
	inter := len(DatasetsIntersection(a, b))
	union := len(DatasetsUnion(a, b))
	value := float64(inter) / float64(union)
	return value
}

// Configure sets the necessary parameters before the similarity execution
func (e *JaccardEstimator) Configure(conf map[string]string) {
	if val, ok := conf["concurrency"]; ok {
		conv, err := strconv.ParseInt(val, 10, 32)
		if err != nil {
			log.Println(err)
		} else {
			e.concurrency = int(conv)
		}
	}
}

// Options returns a list of applicable parameters
func (e *JaccardEstimator) Options() map[string]string {
	return map[string]string{
		"concurrency": "max num of threads used (int)",
	}
}

// Serialize returns a byte array containing the estimator.
func (e *JaccardEstimator) Serialize() []byte {
	buffer := new(bytes.Buffer)
	buffer.Write(getBytesInt(int(SIMILARITY_TYPE_JACCARD)))
	buffer.Write(
		datasetSimilarityEstimatorSerialize(e.AbstractDatasetSimilarityEstimator))
	return buffer.Bytes()
}

// Deserialize instantiates the estimator based on a byte array
func (e *JaccardEstimator) Deserialize(b []byte) {
	buffer := bytes.NewBuffer(b)
	tempInt := make([]byte, 4)
	buffer.Read(tempInt) // consume estimator type

	buffer.Read(tempInt)
	absEstBytes := make([]byte, getIntBytes(tempInt))
	buffer.Read(absEstBytes)
	e.AbstractDatasetSimilarityEstimator =
		*datasetSimilarityEstimatorDeserialize(absEstBytes)

}
