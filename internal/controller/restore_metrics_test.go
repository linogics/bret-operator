package controller

import (
	"math"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

func TestRestoreDurationBuckets(t *testing.T) {
	obs := restoreDuration.WithLabelValues("bucket-test", "succeeded")
	obs.Observe(600)

	var m dto.Metric
	if err := obs.(interface{ Write(*dto.Metric) error }).Write(&m); err != nil {
		t.Fatal(err)
	}

	for _, b := range m.GetHistogram().GetBucket() {
		if b.GetUpperBound() >= 600 && !math.IsInf(b.GetUpperBound(), 1) {
			if b.GetCumulativeCount() != 1 {
				t.Fatalf("bucket le=%v count = %d, want 1", b.GetUpperBound(), b.GetCumulativeCount())
			}

			return
		}
	}

	t.Fatal("no finite bucket >= 600s")
}
