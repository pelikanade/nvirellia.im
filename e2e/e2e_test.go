// Package e2e builds the nvirellia binary, runs it against a stub magpie, and
// checks what visitors and the feed API see over HTTP.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var (
	binPath string
	cst     = time.FixedZone("UTC+8", 8*3600)
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "nvirellia-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "nvirellia")
	build := exec.Command("go", "build", "-o", binPath, "./cmd/nvirellia")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// series is one magpie bucket on the wire.
type series struct {
	Time   time.Time `json:"time"`
	Input  int64     `json:"input"`
	Output int64     `json:"output"`
}

// magpieStub speaks magpie's web API: GET /?k=<key> sets a session cookie, and
// /api/usage needs that cookie. Setting down makes every request fail.
type magpieStub struct {
	URL   string
	key   string
	hours []series
	days  []series
	down  atomic.Bool
	hits  atomic.Int64
}

func newMagpieStub(t *testing.T, key string, hours, days []series) *magpieStub {
	t.Helper()
	s := &magpieStub{key: key, hours: hours, days: days}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		if s.down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Query().Get("k") != s.key {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	mux.HandleFunc("/api/usage", func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		if s.down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		if c, err := r.Cookie("session"); err != nil || c.Value != "ok" {
			http.Error(w, "no session", http.StatusUnauthorized)
			return
		}
		var body map[string]any
		switch r.URL.Query().Get("period") {
		case "today":
			body = map[string]any{"period": "today", "bucket": "hour", "series": s.hours}
		case "all":
			body = map[string]any{"period": "all", "bucket": "day", "series": s.days}
		default:
			http.Error(w, "unknown period", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// syncBuffer collects child output while the process is still running.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// siteEnv is the parent environment minus every variable the tests set.
func siteEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "MAGPIE_") || k == "LISTEN_ADDR" || k == "FEED_TTL" || k == "NVIRELLIA_DEV_ROOT" {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// startSite runs the binary from an empty directory (so no repo .env is read)
// and waits for /healthz. It returns the base URL.
func startSite(t *testing.T, extra ...string) string {
	t.Helper()
	addr := freeAddr(t)
	cmd := exec.Command(binPath)
	cmd.Dir = t.TempDir()
	cmd.Env = append(siteEnv(), append([]string{"LISTEN_ADDR=" + addr}, extra...)...)
	var logs syncBuffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		cmd.Process.Signal(os.Interrupt)
		<-done
	})

	base := "http://" + addr
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case <-done:
			t.Fatalf("server exited before becoming healthy:\n%s", logs.String())
		default:
		}
		if resp, err := http.Get(base + "/healthz"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return base
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("server not healthy after 10s:\n%s", logs.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

type pair struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
}

type statsResp struct {
	GeneratedAt string `json:"generatedAt"`
	TZ          string `json:"tz"`
	Tokens      *struct {
		Today  pair `json:"today"`
		Hourly struct {
			Input []int64 `json:"input"`
		} `json:"hourly"`
		Daily struct {
			Input []*int64 `json:"input"`
		} `json:"daily"`
	} `json:"tokens"`
}

func fetchStats(t *testing.T, base string) statsResp {
	t.Helper()
	code, body := get(t, base+"/api/stats.json")
	if code != http.StatusOK {
		t.Fatalf("/api/stats.json status = %d", code)
	}
	var s statsResp
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatalf("/api/stats.json is not valid JSON: %v\n%s", err, body)
	}
	return s
}

// elementText returns the text directly inside the element with the given id.
func elementText(t *testing.T, body, id string) string {
	t.Helper()
	m := regexp.MustCompile(`id="` + regexp.QuoteMeta(id) + `"[^>]*>([^<]*)<`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no element with id %q", id)
	}
	return m[1]
}

// localMidnight is 00:00 today in UTC+8, the zone the feed reports in.
func localMidnight() time.Time {
	now := time.Now().In(cst)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, cst)
}

func TestWithoutMagpieServesProfileOnly(t *testing.T) {
	base := startSite(t)

	code, page := get(t, base+"/")
	if code != http.StatusOK {
		t.Fatalf("/ status = %d", code)
	}
	for _, want := range []string{`id="profile"`, "i@nvirellia.im"} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q", want)
		}
	}
	for _, banned := range []string{`id="resources"`, `href="#resources"`, "sample data"} {
		if strings.Contains(page, banned) {
			t.Errorf("page without magpie contains %q", banned)
		}
	}

	stats := fetchStats(t, base)
	if stats.Tokens != nil {
		t.Errorf("unconfigured magpie: tokens = %+v, want null", stats.Tokens)
	}
	if stats.TZ != "Asia/Shanghai" || !strings.HasSuffix(stats.GeneratedAt, "+08:00") {
		t.Errorf("tz=%q generatedAt=%q", stats.TZ, stats.GeneratedAt)
	}
}

func TestMagpieServesTokensAndResources(t *testing.T) {
	mid := localMidnight()
	stub := newMagpieStub(t, "k-test",
		[]series{
			{Time: mid, Input: 100, Output: 10},
			{Time: mid.Add(time.Hour), Input: 200, Output: 20},
		},
		[]series{{Time: mid, Input: 300, Output: 30}},
	)
	base := startSite(t, "MAGPIE_URL="+stub.URL, "MAGPIE_WEB_KEY=k-test")

	stats := fetchStats(t, base)
	if stats.Tokens == nil {
		t.Fatal("magpie answering: tokens must be present")
	}
	if got := stats.Tokens.Today; got.Input != 300 || got.Output != 30 {
		t.Errorf("today = %+v, want input 300 output 30", got)
	}
	if n := len(stats.Tokens.Hourly.Input); n != 24 {
		t.Errorf("hourly window = %d, want 24", n)
	}
	if n := len(stats.Tokens.Daily.Input); n != 112 {
		t.Errorf("daily window = %d, want 112", n)
	}

	_, page := get(t, base+"/")
	for _, want := range []string{`id="resources"`, `href="#resources"`} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q", want)
		}
	}
	_, frag := get(t, base+"/fragments/readouts")
	// r-today is the day's total: input 300 + output 30.
	if got := elementText(t, frag, "r-today"); got != "330" {
		t.Errorf("r-today = %q, want 330", got)
	}
}

func TestRejectedMagpieKeyDegradesToNoTokens(t *testing.T) {
	stub := newMagpieStub(t, "right-key", nil, nil)
	base := startSite(t, "MAGPIE_URL="+stub.URL, "MAGPIE_WEB_KEY=wrong-key")

	if stats := fetchStats(t, base); stats.Tokens != nil {
		t.Errorf("rejected key: tokens = %+v, want null", stats.Tokens)
	}
	code, page := get(t, base+"/")
	if code != http.StatusOK {
		t.Fatalf("/ status = %d", code)
	}
	if strings.Contains(page, `id="resources"`) {
		t.Error("rejected key: page must not render the Resources panel")
	}
}

func TestLastGoodTokensServedDuringOutage(t *testing.T) {
	mid := localMidnight()
	stub := newMagpieStub(t, "k-test",
		[]series{{Time: mid, Input: 100, Output: 10}},
		[]series{{Time: mid, Input: 300, Output: 30}},
	)
	base := startSite(t, "MAGPIE_URL="+stub.URL, "MAGPIE_WEB_KEY=k-test", "FEED_TTL=1s")

	before := fetchStats(t, base)
	if before.Tokens == nil {
		t.Fatal("first fetch: tokens must be present")
	}

	stub.down.Store(true)
	time.Sleep(1500 * time.Millisecond)

	after := fetchStats(t, base)
	if after.Tokens == nil {
		t.Fatal("magpie down within 10 min: last good tokens must still be served")
	}
	if after.Tokens.Today != before.Tokens.Today {
		t.Errorf("stale today = %+v, want %+v", after.Tokens.Today, before.Tokens.Today)
	}
}

func TestHealthzDoesNotCallMagpie(t *testing.T) {
	stub := newMagpieStub(t, "k-test", nil, nil)
	base := startSite(t, "MAGPIE_URL="+stub.URL, "MAGPIE_WEB_KEY=k-test")

	before := stub.hits.Load()
	code, body := get(t, base+"/healthz")
	if code != http.StatusOK || body != "ok" {
		t.Errorf("healthz = %d %q", code, body)
	}
	if after := stub.hits.Load(); after != before {
		t.Errorf("healthz reached magpie %d times", after-before)
	}
}

func TestMagpieURLWithoutKeyRefusesToStart(t *testing.T) {
	cmd := exec.Command(binPath)
	cmd.Dir = t.TempDir()
	cmd.Env = append(siteEnv(), "LISTEN_ADDR="+freeAddr(t), "MAGPIE_URL=http://127.0.0.1:1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected a non-zero exit; output:\n%s", out)
	}
	if !strings.Contains(string(out), "MAGPIE_WEB_KEY") {
		t.Errorf("error should name MAGPIE_WEB_KEY; output:\n%s", out)
	}
}
