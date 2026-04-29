package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry holds the metrics Claude services expose.
type Registry struct {
	HTTPRequests  *prometheus.CounterVec
	HTTPDuration  *prometheus.HistogramVec
	BusinessOps   *prometheus.CounterVec
	ServiceUp     prometheus.Gauge
	ExternalCalls *prometheus.CounterVec
}

// New creates and registers metrics for the given service name.
func New(service string) *Registry {
	labels := prometheus.Labels{"service": service}

	r := &Registry{
		HTTPRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name:        "http_requests_total",
				Help:        "Total HTTP requests received",
				ConstLabels: labels,
			},
			[]string{"method", "path", "status"},
		),
		HTTPDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:        "http_request_duration_seconds",
				Help:        "HTTP request duration in seconds",
				Buckets:     prometheus.DefBuckets,
				ConstLabels: labels,
			},
			[]string{"method", "path"},
		),
		BusinessOps: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name:        "business_operations_total",
				Help:        "Domain-level operations (orders created, logins, etc.)",
				ConstLabels: labels,
			},
			[]string{"operation", "result"},
		),
		ServiceUp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "service_up",
			Help:        "1 if the service has finished initialising successfully, 0 otherwise",
			ConstLabels: labels,
		}),
		ExternalCalls: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name:        "external_calls_total",
				Help:        "Calls made to other services (gRPC/AMQP/etc.)",
				ConstLabels: labels,
			},
			[]string{"target", "result"},
		),
	}
	prometheus.MustRegister(r.HTTPRequests, r.HTTPDuration, r.BusinessOps, r.ServiceUp, r.ExternalCalls)
	return r
}

// Handler returns the /metrics HTTP handler.
func Handler() http.Handler {
	return promhttp.Handler()
}

// statusRecorder lets us capture HTTP status for metrics.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Middleware wraps an http.Handler and records request metrics.
func (r *Registry) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, req)
		dur := time.Since(start).Seconds()
		path := req.URL.Path
		r.HTTPRequests.WithLabelValues(req.Method, path, strconv.Itoa(rec.status)).Inc()
		r.HTTPDuration.WithLabelValues(req.Method, path).Observe(dur)
	})
}
