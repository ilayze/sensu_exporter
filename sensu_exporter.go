package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"crypto/tls"
	"sync"
	"time"
	"golang.org/x/net/proxy"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)



var (

	httpClient = &http.Client{
		Timeout: 40 * time.Second,

	}
	listenAddress = flag.String(
		// exporter port list:
		// https://github.com/prometheus/prometheus/wiki/Default-port-allocations
		"listen", ":9251",
		"Address to listen on for serving Prometheus Metrics.",
	)
	sensuAPI = flag.String(
		"api", "http://localhost:4567",
		"Address to Sensu API.",
	)
)

type SensuCheckResult struct {
	Client string
	Check  SensuCheck
}

type SensuCheck struct {
	Name        string
	Duration    float64
	Executed    int64
	Subscribers []string
	Output      string
	Status      int
	Issued      int64
	Interval    int
}

// BEGIN: Class SensuCollector
type SensuCollector struct {
	apiUrl      string
	mutex       sync.RWMutex
	CheckStatus *prometheus.Desc
}

func (c *SensuCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.CheckStatus
}

func (c *SensuCollector) Collect(ch chan<- prometheus.Metric) {
	c.mutex.Lock() // To protect metrics from concurrent collects.
	defer c.mutex.Unlock()

	results := c.getCheckResults()
	for i, result := range results {
		log.Debugln("...", fmt.Sprintf("%d, %v, %v", i, result.Check.Name, result.Check.Status))
		// in Sensu, 0 means OK
		// in Prometheus, 1 means OK
		status := 0.0
		if result.Check.Status == 0 {
			status = 0.0
		} else if result.Check.Status == 1 {
			status = 1.0

		} else if result.Check.Status == 2{
			status = 2.0
		} else {
			status = 3.0

		}
		ch <- prometheus.MustNewConstMetric(
			c.CheckStatus,
			prometheus.GaugeValue,
			status,
			result.Client,
			result.Check.Name,
		)
	}
}

func (c *SensuCollector) getCheckResults() []SensuCheckResult {
	log.Debugln("Sensu API URL", c.apiUrl)
	results := []SensuCheckResult{}
	err := c.GetJson(c.apiUrl+"/results", &results)
	if err != nil {
		log.Errorln("Query Sensu failed.", fmt.Sprintf("%v", err))
	}
	return results
}

func (c *SensuCollector) GetJson(url string, obj interface{}) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(obj)
}

// END: Class SensuCollector

func NewSensuCollector(url string) *SensuCollector {
	return &SensuCollector{
		apiUrl: url,
		CheckStatus: prometheus.NewDesc(
			"sensu_check_status",
			"Sensu Check Status(1:Up, 0:Down)",
			[]string{"client", "check_name"},
			nil,
		),
	}
}

func main() {


	socks5_proxy := flag.String("socks5", "", "a string")

	flag.Parse()

	if *socks5_proxy != ""{
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		dialSocksProxy, err := proxy.SOCKS5("tcp", *socks5_proxy, nil, proxy.Direct)
		if err != nil {
			fmt.Fprintln(os.Stderr, "can't connect to the proxy:", err)
			os.Exit(1)
		}
		tr := &http.Transport{Dial: dialSocksProxy.Dial}
		httpClient = &http.Client{
			Timeout: 40 * time.Second,
			Transport: tr,
		}
		tr.Dial = dialSocksProxy.Dial
		log.Infoln("Using socks5 proxy:", *socks5_proxy)
	}

	serveMetrics()

}

func serveMetrics() {
	collector := NewSensuCollector(*sensuAPI)
	prometheus.MustRegister(collector)
	metricPath := "/metrics"
	http.Handle(metricPath, prometheus.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(metricPath))
	})
	log.Infoln("Listening on", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
