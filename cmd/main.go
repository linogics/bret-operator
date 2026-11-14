package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins. For example, Azure, GCP and OIDC.
	// This lets exec-entrypoint and run use them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
	"github.com/linogics/bret-operator/internal/controller"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(restorev1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// pollIntervalEnv reads POLL_INTERVAL, for example 30s. Empty means the controller default.
// A value that cannot be parsed, or is under 5s, stops the operator at startup.
func pollIntervalEnv() time.Duration {
	raw := os.Getenv("POLL_INTERVAL")
	if raw == "" {
		return 0
	}

	d, err := time.ParseDuration(raw)
	if err != nil || d < 5*time.Second {
		setupLog.Error(err, "POLL_INTERVAL must be a duration of at least 5s", "value", raw)
		os.Exit(1)
	}

	return d
}

// csvEnv splits a comma-separated env var. It drops empty items.
func csvEnv(name string) []string {
	var out []string

	for _, v := range strings.Split(os.Getenv(name), ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}

	return out
}

// parseProviders reads the PROVIDERS env var. Unset serves every provider, as outside the chart.
// Set but empty, or "none", serves no provider. The chart renders that when every provider is off.
func parseProviders(raw string, set bool) (map[restorev1alpha1.Provider]bool, error) {
	if !set {
		return nil, nil
	}

	enabled := map[restorev1alpha1.Provider]bool{}

	for _, n := range strings.Split(raw, ",") {
		n = strings.TrimSpace(n)

		switch restorev1alpha1.Provider(n) {
		case "", "none":
		case restorev1alpha1.ProviderKopiur, restorev1alpha1.ProviderVelero, restorev1alpha1.ProviderVolSync:
			enabled[restorev1alpha1.Provider(n)] = true
		default:
			return nil, fmt.Errorf("unknown provider %q in PROVIDERS", n)
		}
	}

	return enabled, nil
}

// nolint:gocyclo
func main() {
	var (
		metricsAddr                                      string
		metricsCertPath, metricsCertName, metricsCertKey string
		webhookCertPath, webhookCertName, webhookCertKey string
		enableLeaderElection                             bool
		probeAddr                                        string
		secureMetrics                                    bool
		enableHTTP2                                      bool
		tlsOpts                                          []func(*tls.Config)
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// If the enable-http2 flag is false (the default), disable HTTP/2.
	// HTTP/2 has known vulnerabilities. Disabling it protects against the
	// HTTP/2 Stream Cancellation and Rapid Reset CVEs. For more information, see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")

		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for the metrics and webhook certificates.
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		var err error

		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(webhookCertPath, webhookCertName),
			filepath.Join(webhookCertPath, webhookCertKey),
		)
		if err != nil {
			setupLog.Error(err, "Failed to initialize webhook certificate watcher")
			os.Exit(1)
		}

		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: webhookTLSOpts,
	})

	// The metrics endpoint is enabled in 'config/default/kustomization.yaml'. The metrics options configure the server.
	// For more information:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider protects the metrics endpoint with authn and authz.
		// These configurations allow only authorized users and service accounts to access the metrics endpoint.
		// The RBAC rules are configured in 'config/rbac/kustomization.yaml'. For more information:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime generates self-signed certificates for the metrics server.
	// This is convenient for development and testing. This setup is not recommended for production.
	//
	// To use cert-manager certificates, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		var err error

		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(metricsCertPath, metricsCertName),
			filepath.Join(metricsCertPath, metricsCertKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
			os.Exit(1)
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	enabledProviders, err := parseProviders(os.LookupEnv("PROVIDERS"))
	if err != nil {
		setupLog.Error(err, "invalid PROVIDERS")
		os.Exit(1)
	}

	veleroOn := enabledProviders == nil || enabledProviders[restorev1alpha1.ProviderVelero]

	// Empty WATCH_NAMESPACES is cluster-wide. Otherwise the cache is limited to those namespaces. With Velero on,
	// only the bret objects are: the check pods and PVCs of the scratch namespaces stay cluster-wide.
	var cacheOpts cache.Options

	if watch := csvEnv("WATCH_NAMESPACES"); len(watch) > 0 {
		nsConfig := map[string]cache.Config{}
		for _, ns := range watch {
			nsConfig[ns] = cache.Config{}
		}

		if veleroOn {
			cacheOpts.ByObject = map[client.Object]cache.ByObject{}
			for _, o := range []client.Object{
				&restorev1alpha1.Restore{}, &restorev1alpha1.RestoreTest{}, &restorev1alpha1.RestoreCheck{},
			} {
				cacheOpts.ByObject[o] = cache.ByObject{Namespaces: nsConfig}
			}
		} else {
			cacheOpts.DefaultNamespaces = nsConfig
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Cache:                  cacheOpts,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "51003adf.bret.linogics.io",
		// LeaderElectionReleaseOnCancel sets whether the leader steps down voluntarily when the Manager ends.
		// The binary must end immediately when the Manager stops. Otherwise, this setting is unsafe.
		// It speeds up voluntary leader transitions, because the new leader does not wait for LeaseDuration.
		//
		// The scaffold program ends immediately after the Manager stops, so enabling this option is safe.
		// Enabling it might be unsafe if you run cleanup after the Manager stops.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := (&controller.RestoreReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor("restore"),
		APIReader: mgr.GetAPIReader(),

		EnabledProviders: enabledProviders,
		VeleroNamespace:  os.Getenv("VELERO_NAMESPACE"),
		PollInterval:     pollIntervalEnv(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Restore")
		os.Exit(1)
	}

	if err := (&controller.RestoreTestReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor("restoretest"),
		APIReader: mgr.GetAPIReader(),

		EnabledProviders: enabledProviders,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RestoreTest")
		os.Exit(1)
	}

	if err := (&controller.RestoreCheckReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RestoreCheck")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if metricsCertWatcher != nil {
		setupLog.Info("Adding metrics certificate watcher to manager")

		if err := mgr.Add(metricsCertWatcher); err != nil {
			setupLog.Error(err, "unable to add metrics certificate watcher to manager")
			os.Exit(1)
		}
	}

	if webhookCertWatcher != nil {
		setupLog.Info("Adding webhook certificate watcher to manager")

		if err := mgr.Add(webhookCertWatcher); err != nil {
			setupLog.Error(err, "unable to add webhook certificate watcher to manager")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
