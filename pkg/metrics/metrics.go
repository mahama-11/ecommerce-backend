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
	mu             sync.Mutex
	configured     bool
	registry       *prometheus.Registry
	httpRequests   *prometheus.CounterVec
	httpDuration   *prometheus.HistogramVec
	businessEvents *prometheus.CounterVec
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
	registry.MustRegister(httpRequests, httpDuration, businessEvents)
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

func Handler() http.Handler {
	ensureConfigured()
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

func ensureConfigured() {
	Configure("ecommerce", "service", nil)
}
