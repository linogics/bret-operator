package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	labelRestoreTest = "restoretest"
	labelNamespace   = "namespace"

	// legacyMetricsFinalizer was added by older releases. The controller only removes it.
	legacyMetricsFinalizer = "bret.linogics.io/metrics"
)

// restoreTestLastSuccess holds the Unix time of the last run of a RestoreTest that passed.
var restoreTestLastSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "bret_restoretest_last_success_timestamp_seconds",
	Help: "Unix time of the last successful run of a RestoreTest.",
}, []string{labelRestoreTest, labelNamespace})

func init() {
	metrics.Registry.MustRegister(restoreTestLastSuccess)
}
