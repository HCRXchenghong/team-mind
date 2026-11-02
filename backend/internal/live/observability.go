package live

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status != 0 {
		return
	}
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

// Flush preserves streaming responses (SSE) through the wrapper.
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap lets http.NewResponseController reach the original writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

type routeMetric struct {
	Count    int
	Errors   int
	TotalMS  int64
	MaxMS    int64
	LastSeen time.Time
}

func (s *Server) observeRequest(r *http.Request, recorder *statusRecorder, requestID string, duration time.Duration) {
	status := recorder.status
	if status == 0 {
		status = http.StatusOK
	}
	route := routeKey(r.Method, r.URL.Path)
	s.metricsMu.Lock()
	if s.metrics == nil {
		s.metrics = map[string]*routeMetric{}
	}
	metric := s.metrics[route]
	if metric == nil {
		if len(s.metrics) >= 200 {
			route = "other"
			metric = s.metrics[route]
		}
		if metric == nil {
			metric = &routeMetric{}
			s.metrics[route] = metric
		}
	}
	metric.Count++
	if status >= 500 {
		metric.Errors++
	}
	// SSE connections are long-lived; counting their duration as request
	// latency would make every average meaningless.
	if !strings.HasPrefix(r.URL.Path, "/api/v1/me/events") {
		ms := duration.Milliseconds()
		metric.TotalMS += ms
		if ms > metric.MaxMS {
			metric.MaxMS = ms
		}
	}
	metric.LastSeen = time.Now().UTC()
	s.metricsMu.Unlock()
	if status >= 500 || (duration > 2*time.Second && !strings.HasPrefix(r.URL.Path, "/api/v1/me/events")) {
		logRequestSlow(requestID, r.Method, r.URL.Path, status, duration)
	}
}

func logRequestSlow(requestID, method, path string, status int, duration time.Duration) {
	// Keep the access log focused on actionable requests; per-request INFO
	// logging is left to the edge proxy.
	log.Printf("request id=%s method=%s path=%s status=%d duration=%s", requestID, method, path, status, duration.Round(time.Millisecond))
}

var routeIDPattern = regexp.MustCompile(`^[0-9A-Fa-f]{16,}$`)
var routeNumberPattern = regexp.MustCompile(`^[0-9]+$`)

func routeKey(method, path string) string {
	if strings.HasPrefix(path, "/api/v1/public/gantt/") {
		return method + " /api/v1/public/gantt/:token"
	}
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		if routeIDPattern.MatchString(segment) {
			segments[index] = ":id"
		} else if routeNumberPattern.MatchString(segment) {
			segments[index] = ":n"
		}
	}
	route := strings.Join(segments, "/")
	if route == "" {
		route = "/"
	}
	return method + " " + route
}

func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return id()
	}
	return hex.EncodeToString(buf)
}

func (s *Server) platformMetrics(w http.ResponseWriter, r *http.Request) error {
	if e := method(r, "GET"); e != nil {
		return e
	}
	s.metricsMu.Lock()
	routes := make([]map[string]any, 0, len(s.metrics))
	totalCount, totalErrors := 0, 0
	for route, metric := range s.metrics {
		average := float64(0)
		if metric.Count > 0 {
			average = float64(metric.TotalMS) / float64(metric.Count)
		}
		routes = append(routes, map[string]any{
			"route": route, "count": metric.Count, "errors": metric.Errors,
			"averageMs": average, "maxMs": metric.MaxMS, "lastSeen": metric.LastSeen,
		})
		totalCount += metric.Count
		totalErrors += metric.Errors
	}
	s.metricsMu.Unlock()
	sort.Slice(routes, func(i, j int) bool {
		return routes[i]["count"].(int) > routes[j]["count"].(int)
	})
	write(w, 200, map[string]any{
		"startedAt": s.startedAt, "uptimeSeconds": int64(time.Since(s.startedAt).Seconds()),
		"requests": totalCount, "errors": totalErrors, "routes": routes,
	})
	return nil
}
