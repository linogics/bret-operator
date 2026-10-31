package controller

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

const (
	labelProvider = "provider"
	labelResult   = "result"
)

var (
	restoreRuns = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "bret_restore_runs_total",
		Help: "Finished restore attempts by provider and result.",
	}, []string{labelProvider, labelResult})

	restoreDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "bret_restore_duration_seconds",
		Help: "Duration of finished restore attempts by provider and result.",
	}, []string{labelProvider, labelResult})
)

func init() {
	metrics.Registry.MustRegister(restoreRuns, restoreDuration)
}

// recordRestoreRun counts one finished attempt and observes how long it took.
func recordRestoreRun(restore *restorev1alpha1.Restore, phase restorev1alpha1.RestorePhase) {
	labels := prometheus.Labels{
		"provider": string(restore.Spec.Provider),
		"result":   strings.ToLower(string(phase)),
	}
	restoreRuns.With(labels).Inc()

	if restore.Status.StartTime == nil {
		return
	}

	restoreDuration.With(labels).Observe(time.Since(restore.Status.StartTime.Time).Seconds())
}
