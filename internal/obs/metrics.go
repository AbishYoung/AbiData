package obs

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "abidata"

// latencyBuckets span sub-millisecond to multi-second durations.
var latencyBuckets = []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Metrics holds all Prometheus collectors for the server, registered on a
// private registry.
type Metrics struct {
	reg *prometheus.Registry

	HTTPRequests   *prometheus.HistogramVec // http_request_duration_seconds{method,route,status}
	OpenWorkspaces prometheus.Gauge         // open_workspaces
	WriterGateWait *prometheus.HistogramVec // writer_gate_wait_seconds{priority}
	WriterGateHold prometheus.Histogram     // writer_gate_hold_seconds
	ReaderPoolWait prometheus.Histogram     // reader_pool_wait_seconds
	ScriptRuns     *prometheus.CounterVec   // script_runs_total{context,outcome}
	ScriptDuration *prometheus.HistogramVec // script_duration_seconds{context}
	JobRuns        *prometheus.CounterVec   // job_runs_total{status}
	EgressRequests *prometheus.CounterVec   // egress_requests_total{outcome}
	BackupDuration prometheus.Histogram     // backup_duration_seconds
	BackupBytes    prometheus.Histogram     // backup_size_bytes
	WALBytes       *prometheus.GaugeVec     // wal_size_bytes{workspace}
	FilesUploaded  prometheus.Counter       // files_uploaded_total
	FileBytes      prometheus.Counter       // file_upload_bytes_total
	FilesPurged    prometheus.Counter       // files_purged_total
	FilePurgeBytes prometheus.Counter       // file_purged_bytes_total
}

func hist(name, help string, buckets []float64) prometheus.HistogramOpts {
	return prometheus.HistogramOpts{Namespace: namespace, Name: name, Help: help, Buckets: buckets}
}

// NewMetrics creates a Metrics set on a fresh registry that also includes the
// Go runtime and process collectors.
func NewMetrics() *Metrics {
	m := &Metrics{reg: prometheus.NewRegistry()}

	m.HTTPRequests = prometheus.NewHistogramVec(
		hist("http_request_duration_seconds", "HTTP request latency.", latencyBuckets),
		[]string{"method", "route", "status"})
	m.OpenWorkspaces = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace, Name: "open_workspaces", Help: "Number of currently open workspaces."})
	m.WriterGateWait = prometheus.NewHistogramVec(
		hist("writer_gate_wait_seconds", "Time spent waiting for the workspace writer gate.", latencyBuckets),
		[]string{"priority"})
	m.WriterGateHold = prometheus.NewHistogram(
		hist("writer_gate_hold_seconds", "Time the workspace writer gate is held.", latencyBuckets))
	m.ReaderPoolWait = prometheus.NewHistogram(
		hist("reader_pool_wait_seconds", "Time spent waiting for a pooled reader connection.", latencyBuckets))
	m.ScriptRuns = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "script_runs_total", Help: "Script executions."},
		[]string{"context", "outcome"})
	m.ScriptDuration = prometheus.NewHistogramVec(
		hist("script_duration_seconds", "Script execution time.", prometheus.DefBuckets),
		[]string{"context"})
	m.JobRuns = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "job_runs_total", Help: "Background job runs."},
		[]string{"status"})
	m.EgressRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "egress_requests_total", Help: "Outbound requests made on behalf of scripts."},
		[]string{"outcome"})
	m.BackupDuration = prometheus.NewHistogram(
		hist("backup_duration_seconds", "Backup duration.", prometheus.ExponentialBuckets(0.01, 4, 10)))
	m.BackupBytes = prometheus.NewHistogram(
		hist("backup_size_bytes", "Backup size in bytes.", prometheus.ExponentialBuckets(1<<20, 4, 10)))
	m.WALBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace, Name: "wal_size_bytes", Help: "SQLite WAL file size per workspace."},
		[]string{"workspace"})

	counter := func(name, help string) prometheus.Counter {
		return prometheus.NewCounter(prometheus.CounterOpts{Namespace: namespace, Name: name, Help: help})
	}
	m.FilesUploaded = counter("files_uploaded_total", "File attachments uploaded.")
	m.FileBytes = counter("file_upload_bytes_total", "Bytes of file attachments uploaded.")
	m.FilesPurged = counter("files_purged_total", "File attachments removed by the janitor.")
	m.FilePurgeBytes = counter("file_purged_bytes_total", "Bytes freed by removing file attachments.")

	m.reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.HTTPRequests, m.OpenWorkspaces, m.WriterGateWait, m.WriterGateHold, m.ReaderPoolWait,
		m.ScriptRuns, m.ScriptDuration, m.JobRuns, m.EgressRequests,
		m.BackupDuration, m.BackupBytes, m.WALBytes,
		m.FilesUploaded, m.FileBytes, m.FilesPurged, m.FilePurgeBytes,
	)
	return m
}

// Handler serves the metrics in Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}
