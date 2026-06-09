package metrics

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	mu               sync.Mutex
	configured       bool
	registry         *prometheus.Registry
	httpRequests     *prometheus.CounterVec
	httpDuration     *prometheus.HistogramVec
	businessEvents   *prometheus.CounterVec
	platformCalls    *prometheus.CounterVec
	platformLatency  *prometheus.HistogramVec
	imageJobs        *prometheus.CounterVec
	workflowRuns     *prometheus.CounterVec
	chargeSessions   *prometheus.CounterVec
	runtimeCallbacks *prometheus.CounterVec
	providerCalls    *prometheus.CounterVec
)

func Configure(namespace, subsystem string, buckets []float64) {
	mu.Lock()
	defer mu.Unlock()
	if configured {
		return
	}
	if namespace == "" {
		namespace = "ecommerce"
	}
	if subsystem == "" {
		subsystem = "service"
	}
	if len(buckets) == 0 {
		buckets = prometheus.DefBuckets
	}
	registry = prometheus.NewRegistry()
	httpRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: namespace, Subsystem: subsystem, Name: "http_requests_total", Help: "Total HTTP requests."},
		[]string{"method", "path", "status"},
	)
	httpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Namespace: namespace, Subsystem: subsystem, Name: "http_request_duration_seconds", Help: "HTTP request duration in seconds.", Buckets: buckets},
		[]string{"method", "path", "status"},
	)
	businessEvents = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: namespace, Subsystem: subsystem, Name: "business_events_total", Help: "Total business events."},
		[]string{"name"},
	)
	platformCalls = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: namespace, Subsystem: subsystem, Name: "platform_calls_total", Help: "Total outbound Platform internal API calls."},
		[]string{"endpoint", "status", "error_code"},
	)
	platformLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Namespace: namespace, Subsystem: subsystem, Name: "platform_call_duration_seconds", Help: "Outbound Platform internal API call duration in seconds.", Buckets: buckets},
		[]string{"endpoint", "status"},
	)
	imageJobs = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: namespace, Subsystem: subsystem, Name: "image_jobs_total", Help: "Total ecommerce image jobs."},
		[]string{"status", "provider", "scene_type"},
	)
	workflowRuns = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: namespace, Subsystem: subsystem, Name: "visual_workflow_runs_total", Help: "Total visual workflow runs."},
		[]string{"status"},
	)
	chargeSessions = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: namespace, Subsystem: subsystem, Name: "billing_charge_sessions_total", Help: "Total billing charge sessions observed by ecommerce."},
		[]string{"status"},
	)
	runtimeCallbacks = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: namespace, Subsystem: subsystem, Name: "runtime_callbacks_total", Help: "Total runtime callbacks received by ecommerce."},
		[]string{"status"},
	)
	providerCalls = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: namespace, Subsystem: subsystem, Name: "provider_calls_total", Help: "Total provider calls initiated by ecommerce."},
		[]string{"provider", "task_type", "status"},
	)
	registry.MustRegister(httpRequests, httpDuration, businessEvents, platformCalls, platformLatency, imageJobs, workflowRuns, chargeSessions, runtimeCallbacks, providerCalls)
	configured = true
}

func RecordHTTPRequest(method, path string, status int, duration time.Duration) {
	ensureConfigured()
	statusLabel := strconv.Itoa(status)
	httpRequests.WithLabelValues(method, path, statusLabel).Inc()
	httpDuration.WithLabelValues(method, path, statusLabel).Observe(duration.Seconds())
}

func IncBusinessCounter(name string) {
	ensureConfigured()
	businessEvents.WithLabelValues(name).Inc()
}

func RecordPlatformCall(endpoint, status, errorCode string, duration time.Duration) {
	ensureConfigured()
	if endpoint == "" {
		endpoint = "unknown"
	}
	if status == "" {
		status = "unknown"
	}
	platformCalls.WithLabelValues(endpoint, status, errorCode).Inc()
	platformLatency.WithLabelValues(endpoint, status).Observe(duration.Seconds())
}

func IncImageJob(status, provider, sceneType string) {
	ensureConfigured()
	imageJobs.WithLabelValues(status, provider, sceneType).Inc()
}

func IncVisualWorkflowRun(status string) {
	ensureConfigured()
	workflowRuns.WithLabelValues(status).Inc()
}

func IncBillingChargeSession(status string) {
	ensureConfigured()
	chargeSessions.WithLabelValues(status).Inc()
}

func IncRuntimeCallback(status string) {
	ensureConfigured()
	runtimeCallbacks.WithLabelValues(status).Inc()
}

func IncProviderCall(provider, taskType, status string) {
	ensureConfigured()
	providerCalls.WithLabelValues(provider, taskType, status).Inc()
}

func Handler() http.Handler {
	ensureConfigured()
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

func ensureConfigured() {
	Configure("ecommerce", "service", nil)
}
