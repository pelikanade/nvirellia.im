package feed

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Prometheus reads host probes from a Prometheus instance's HTTP API. The
// scrape configuration is the source of truth: targets are node_exporter jobs
// labelled with host, and this client only queries:
//
//	up{job=<job>}                                      -> target state
//	time() - node_boot_time_seconds{job=<job>}         -> host uptime
//	100 * (1 - rate(node_cpu_seconds_total{mode="idle"}[5m])) -> CPU %
//	100 * (1 - MemAvailable / MemTotal)                -> memory %
//	100 * (1 - filesystem avail / size)                -> root disk %
//
// The API is read-only and unauthenticated; the deployment binds it to loopback.
type Prometheus struct {
	baseURL string
	job     string
	client  *http.Client
}

// NewPrometheus returns a client for baseURL. An empty job defaults to "node".
func NewPrometheus(baseURL, job string) (*Prometheus, error) {
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("PROMETHEUS_URL: %w", err)
	}
	if job == "" {
		job = "node"
	}
	return &Prometheus{
		baseURL: strings.TrimRight(baseURL, "/"),
		job:     job,
		client:  &http.Client{},
	}, nil
}

// Probes implements ProbeSource.
func (p *Prometheus) Probes(ctx context.Context) (Probes, error) {
	queries := []struct{ key, expr string }{
		{"up", fmt.Sprintf(`up{job=%q}`, p.job)},
		{"uptime", fmt.Sprintf(`time() - node_boot_time_seconds{job=%q}`, p.job)},
		{"cpu", fmt.Sprintf(`100 * (1 - avg by (host, instance) (rate(node_cpu_seconds_total{job=%q,mode="idle"}[5m])))`, p.job)},
		{"mem", fmt.Sprintf(`100 * (1 - node_memory_MemAvailable_bytes{job=%q} / node_memory_MemTotal_bytes{job=%q})`, p.job, p.job)},
		{"disk", fmt.Sprintf(`100 * (1 - node_filesystem_avail_bytes{job=%q,mountpoint="/",fstype!~"tmpfs|overlay"} / node_filesystem_size_bytes{job=%q,mountpoint="/",fstype!~"tmpfs|overlay"})`, p.job, p.job)},
	}

	results := make(map[string][]promSample, len(queries))
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	for _, q := range queries {
		wg.Add(1)
		go func(key, expr string) {
			defer wg.Done()
			samples, err := p.query(ctx, expr)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				return
			}
			results[key] = samples
		}(q.key, q.expr)
	}
	wg.Wait()
	if first != nil {
		return Probes{}, first
	}

	type builder struct {
		probe   Probe
		updated time.Time
	}
	builders := make(map[string]*builder)
	for _, s := range results["up"] {
		name := sampleHost(s)
		if name == "" {
			continue
		}
		b := &builder{probe: Probe{Name: name, Host: s.metric["instance"], Status: "unknown"}}
		switch s.value {
		case 1:
			b.probe.Status = "up"
		case 0:
			b.probe.Status = "down"
		}
		if !s.at.IsZero() {
			b.probe.Updated = s.at.UTC().Format(time.RFC3339)
			b.updated = s.at
		}
		builders[name] = b
	}

	for _, key := range []string{"uptime", "cpu", "mem", "disk"} {
		for _, s := range results[key] {
			b := builders[sampleHost(s)]
			if b == nil || b.probe.Status != "up" {
				continue // metrics for a down target are stale; leave them blank
			}
			if s.at.After(b.updated) {
				b.updated = s.at
				b.probe.Updated = s.at.UTC().Format(time.RFC3339)
			}
			switch key {
			case "uptime":
				if v, ok := nonNegative(s.value); ok {
					sec := uint64(math.Round(v))
					b.probe.UptimeSeconds = &sec
				}
			case "cpu":
				b.probe.CPU = percent(s.value)
			case "mem":
				b.probe.Mem = percent(s.value)
			case "disk":
				b.probe.Disk = percent(s.value)
			}
		}
	}

	out := make([]Probe, 0, len(builders))
	for _, b := range builders {
		out = append(out, b.probe)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return Probes{Systems: out}, nil
}

// promResponse is the part of /api/v1/query the feed reads.
type promResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// promSample is one vector element, with its timestamp decoded.
type promSample struct {
	metric map[string]string
	value  float64
	at     time.Time
}

// query runs one PromQL expression and returns its vector.
func (p *Prometheus) query(ctx context.Context, expr string) ([]promSample, error) {
	var body promResponse
	target := joinURL(p.baseURL, "/api/v1/query", url.Values{"query": {expr}})
	if err := getJSON(ctx, p.client, target, "prometheus", &body); err != nil {
		return nil, err
	}
	if body.Status != "success" {
		if body.Error != "" {
			return nil, fmt.Errorf("prometheus: %s", body.Error)
		}
		return nil, fmt.Errorf("prometheus: status %q", body.Status)
	}
	if body.Data.ResultType != "vector" {
		return nil, fmt.Errorf("prometheus: result type %q", body.Data.ResultType)
	}
	out := make([]promSample, 0, len(body.Data.Result))
	for _, r := range body.Data.Result {
		ts, ok := r.Value[0].(float64)
		if !ok {
			continue
		}
		text, ok := r.Value[1].(string)
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(text, 64)
		if err != nil {
			continue
		}
		sec, frac := math.Modf(ts)
		out = append(out, promSample{
			metric: r.Metric,
			value:  v,
			at:     time.Unix(int64(sec), int64(frac*1e9)),
		})
	}
	return out, nil
}

// sampleHost prefers the host label the deployment adds, then falls back to
// the scrape instance.
func sampleHost(s promSample) string {
	if h := s.metric["host"]; h != "" {
		return h
	}
	return s.metric["instance"]
}

// nonNegative reports v when it is a finite, non-negative number.
func nonNegative(v float64) (float64, bool) {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, false
	}
	return v, true
}

// percent clamps a Prometheus percentage to the display range.
func percent(v float64) *float64 {
	v, ok := nonNegative(v)
	if !ok {
		return nil
	}
	v = min(v, 100)
	return &v
}
