package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caarlos0/env"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tidwall/gjson"
)

const (
	namespace = "tautulli"
	userAgent = "tautulli-prometheus-exporter"
)

var (
	streamLabelNames    = []string{"stream"}
	bandwidthLabelNames = []string{"bandwidth"}
)

func newStreamMetric(metricName string, docString string, constLabels prometheus.Labels) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        "stream_" + metricName,
			Help:        docString,
			ConstLabels: constLabels,
		},
		streamLabelNames,
	)
}

func newBandwidthMetric(metricName string, docString string, constLabels prometheus.Labels) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        "bandwidth_" + metricName,
			Help:        docString,
			ConstLabels: constLabels,
		},
		bandwidthLabelNames,
	)
}

type metrics map[int]*prometheus.GaugeVec

func (m metrics) String() string {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	s := make([]string, len(keys))
	for i, k := range keys {
		s[i] = strconv.Itoa(k)
	}
	return strings.Join(s, ",")
}

type config struct {
	TautulliApiKey    string        `env:"TAUTULLI_API_KEY"`
	TautulliScrapeUri string        `env:"TAUTULLI_URI" envDefault:"http://127.0.0.1:8181"`
	TautulliSslVerify bool          `env:"TAUTULLI_SSL_VERIFY" envDefault:"false"`
	TautulliTimeout   time.Duration `env:"TAUTULLI_TIMEOUT" envDefault:"5s"`
	ServePort         string        `env:"SERVE_PORT" envDefault:"9487"`
}

type Exporter struct {
	URI   string
	mutex sync.RWMutex
	fetch func() (io.ReadCloser, error)

	up, streamTotal, streamTranscode, streamDirectPlay, streamDirectStream, bandwidthTotal, bandwidthLan, bandwidthWan prometheus.Gauge
	totalScrapes                                                                                                       prometheus.Counter
	streamMetrics, bandwidthMetrics                                                                                    map[string]*prometheus.GaugeVec
}

var (
	version string
)

func NewExporter(uri string, sslVerify bool, timeout time.Duration) (*Exporter, error) {
	var fetch = fetchHTTP(uri, sslVerify, timeout)

	return &Exporter{
		URI:   uri,
		fetch: fetch,
		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "up",
			Help:      "Was the last scrape of Tautulli successful",
		}),
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "exporter_total_scrapes",
			Help:      "Current total Tautulli scrapes",
		}),
		streamTotal: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "stream_count",
			Help:      "Number of total streams.",
		}),
		streamTranscode: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "stream_count_transcode",
			Help:      "Number of streams that are transcoding.",
		}),
		streamDirectPlay: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "stream_direct_play",
			Help:      "Number of streams that are direct_plays.",
		}),
		streamDirectStream: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "stream_direct_stream",
			Help:      "Number of streams that are direct streams.",
		}),
		bandwidthTotal: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "bandwidth_total",
			Help:      "Total bandwidth utilized.",
		}),
		bandwidthLan: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "bandwidth_lan",
			Help:      "LAN bandwidth utilized.",
		}),
		bandwidthWan: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "bandwidth_wan",
			Help:      "WAN bandwidth utilized.",
		}),
	}, nil
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.up.Desc()
	ch <- e.totalScrapes.Desc()
	ch <- e.streamTotal.Desc()
	ch <- e.streamTranscode.Desc()
	ch <- e.streamDirectPlay.Desc()
	ch <- e.streamDirectStream.Desc()
	ch <- e.bandwidthTotal.Desc()
	ch <- e.bandwidthLan.Desc()
	ch <- e.bandwidthWan.Desc()
}

// Implements prometheus.Collector.
// Resets the metrics, fetches stats, and provides the metrics.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock() // Protects metrics from concurrent collects.
	defer e.mutex.Unlock()

	e.resetMetrics()
	e.scrape()

	ch <- e.up
	ch <- e.totalScrapes
	ch <- e.streamTotal
	ch <- e.streamTranscode
	ch <- e.streamDirectPlay
	ch <- e.streamDirectStream
	ch <- e.bandwidthTotal
	ch <- e.bandwidthLan
	ch <- e.bandwidthWan
}

// Fetches stats from Tautulli for later processing
func fetchHTTP(uri string, sslVerify bool, timeout time.Duration) func() (io.ReadCloser, error) {

	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: !sslVerify}}
	client := http.Client{
		Timeout:   timeout,
		Transport: tr,
	}

	return func() (io.ReadCloser, error) {
		resp, err := client.Get(uri)
		if err != nil {
			return nil, err
		}
		if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
		}
		return resp.Body, nil
	}
}

// Scrapes stats using the previous fetch
func (e *Exporter) scrape() {
	e.totalScrapes.Inc()

	body, err := e.fetch()
	if err != nil {
		e.up.Set(0)
		fmt.Errorf("Can't scrape Tautulli: %v", err)
		return
	}
	defer body.Close()

	// If we got data, we're up
	e.up.Set(1)

	// Read in the bytes from our body for use in our json parser
	buf := new(bytes.Buffer)
	buf.ReadFrom(body)

	data := gjson.GetBytes(buf.Bytes(), "response.data")

	e.streamTotal.Set(data.Get("stream_count").Float())
	e.streamTranscode.Set(data.Get("stream_count_transcode").Float())
	e.streamDirectPlay.Set(data.Get("stream_count_direct_play").Float())
	e.streamDirectStream.Set(data.Get("stream_count_direct_stream").Float())

	e.bandwidthTotal.Set(data.Get("total_bandwidth").Float())
	e.bandwidthLan.Set(data.Get("lan_bandwidth").Float())
	e.bandwidthWan.Set(data.Get("wan_bandwidth").Float())

}

// Resets metrics to 0
func (e *Exporter) resetMetrics() {
	e.streamTotal.Set(0)
	e.streamTranscode.Set(0)
	e.streamDirectPlay.Set(0)
	e.streamDirectStream.Set(0)
	e.bandwidthTotal.Set(0)
	e.bandwidthLan.Set(0)
	e.bandwidthWan.Set(0)
}

func main() {
	if len(version) == 0 {
		version = "dev"
	}

	log.Println("Tautulli exporter version:", version)

	cfg := config{}
	err := env.Parse(&cfg)
	if err != nil {
		fmt.Printf("%+v\n", err)
	}

	if len(cfg.TautulliApiKey) == 0 {
		log.Fatal("No API key set")
	}

	log.Println("Tautulli Scrape URI:", cfg.TautulliScrapeUri)
	log.Println("Tautulli SSL verify:", strconv.FormatBool(cfg.TautulliSslVerify))
	log.Println("Tautulli Timeout:", cfg.TautulliTimeout)
	log.Println("Tautulli API key:", cfg.TautulliApiKey)

	u, err := url.Parse(cfg.TautulliScrapeUri + "/api/v2")
	if err != nil {
		log.Fatal(err)
	}

	q := u.Query()
	q.Set("apikey", cfg.TautulliApiKey)
	q.Set("cmd", "get_activity")
	u.RawQuery = q.Encode()

	exporter, err := NewExporter(u.String(), cfg.TautulliSslVerify, cfg.TautulliTimeout)
	if err != nil {
		log.Fatal(err)
	}
	prometheus.MustRegister(exporter)

	// Expose the registered metrics via HTTP.
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>Tautulli Exporter</title></head>
			<body>
			<h1>Tautulli Exporter</h1>
			<p><a href="/metrics">Metrics</a></p>
			<p>Version: ` + version + `</p>
			</body>
			</html>`))
	})
	log.Println("Serving /metrics on port", cfg.ServePort)
	log.Fatal(http.ListenAndServe(":"+cfg.ServePort, nil))
}
