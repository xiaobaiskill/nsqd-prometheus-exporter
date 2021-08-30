package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus" // Prometheus client library
	"github.com/prometheus/client_golang/prometheus/promhttp"
	logger "github.com/sirupsen/logrus" // Logging library
	"github.com/urfave/cli/v2"          // CLI helper
)

var (
	// Version is defined at build time - see VERSION file
	Version string

	scrapeInterval  int
	nsqds           []string
	knownTopics     []string
	knownChannels   []string
	buildInfoMetric *prometheus.GaugeVec
	nsqMetrics      = make(map[string]*prometheus.GaugeVec)
)

const (
	PrometheusNamespace = "nsqd"
	DepthMetric         = "depth"
	BackendDepthMetric  = "backend_depth"
	InFlightMetric      = "in_flight_count"
	TimeoutCountMetric  = "timeout_count_total"
	RequeueCountMetric  = "requeue_count_total"
	DeferredCountMetric = "deferred_count_total"
	MessageCountMetric  = "message_count_total"
	ClientCountMetric   = "client_count"
	ChannelCountMetric  = "channel_count"
	InfoMetric          = "info"
)

func main() {
	app := cli.NewApp()
	app.Version = Version
	app.Name = "nsqd-prometheus-exporter"
	app.Usage = "Scrapes nsqd stats and serves them up as Prometheus metrics"
	app.Flags = []cli.Flag{
		&cli.StringSliceFlag{
			Name:     "nsqd",
			Aliases:  []string{"n"},
			Usage:    "URL of nsqd to export stats from",
			Required: true,
			EnvVars:  []string{"NSQD_URL"},
		},
		&cli.StringFlag{
			Name:    "listenPort",
			Aliases: []string{"p"},
			Value:   ":2112",
			Usage:   "Port on which prometheus will expose metrics",
			EnvVars: []string{"LISTEN_PORT"},
		},
		&cli.IntFlag{
			Name:    "scrapeInterval",
			Value:   30,
			Aliases: []string{"s"},
			Usage:   "How often (in seconds) nsqd stats should be scraped",
			EnvVars: []string{"SCRAPE_INTERVAL"},
		},
	}
	app.Action = func(c *cli.Context) error {
		// Set and validate configuration
		nsqds = c.StringSlice("nsqd")

		scrapeInterval = c.Int("scrapeInterval")
		if scrapeInterval < 1 {
			logger.Warn("Invalid scrape interval set, continuing with default (30s)")
			scrapeInterval = 30
		}

		// Initialize Prometheus metrics
		var emptyMap map[string]string
		commonLabels := []string{"url", "type", "topic", "paused", "channel"}
		buildInfoMetric = createGaugeVector("nsqd_prometheus_exporter_build_info", "", "",
			"nsqd-prometheus-exporter build info", emptyMap, []string{"version"})
		buildInfoMetric.WithLabelValues(app.Version).Set(1)
		// # HELP nsqd_info nsqd info
		// # TYPE nsqd_info gauge
		nsqMetrics[InfoMetric] = createGaugeVector(InfoMetric, PrometheusNamespace,
			"", "nsqd info", emptyMap, []string{"url", "health", "start_time", "version"})
		// # HELP nsqd_depth Queue depth
		// # TYPE nsqd_depth gauge
		nsqMetrics[DepthMetric] = createGaugeVector(DepthMetric, PrometheusNamespace,
			"", "Queue depth", emptyMap, commonLabels)
		// # HELP nsqd_backend_depth Queue backend depth
		// # TYPE nsqd_backend_depth gauge
		nsqMetrics[BackendDepthMetric] = createGaugeVector(BackendDepthMetric, PrometheusNamespace,
			"", "Queue backend depth", emptyMap, commonLabels)
		// # HELP nsqd_in_flight_count In flight count
		// # TYPE nsqd_in_flight_count gauge
		nsqMetrics[InFlightMetric] = createGaugeVector(InFlightMetric, PrometheusNamespace,
			"", "In flight count", emptyMap, commonLabels)
		// # HELP nsqd_timeout_count_total Timeout count
		// # TYPE nsqd_timeout_count_total gauge
		nsqMetrics[TimeoutCountMetric] = createGaugeVector(TimeoutCountMetric, PrometheusNamespace,
			"", "Timeout count", emptyMap, commonLabels)
		// # HELP nsqd_requeue_count_total Requeue count
		// # TYPE nsqd_requeue_count_total gauge
		nsqMetrics[RequeueCountMetric] = createGaugeVector(RequeueCountMetric, PrometheusNamespace,
			"", "Requeue count", emptyMap, commonLabels)
		// # HELP nsqd_deferred_count_total Deferred count
		// # TYPE nsqd_deferred_count_total gauge
		nsqMetrics[DeferredCountMetric] = createGaugeVector(DeferredCountMetric, PrometheusNamespace,
			"", "Deferred count", emptyMap, commonLabels)
		// # HELP nsqd_message_count_total Total message count
		// # TYPE nsqd_message_count_total gauge
		nsqMetrics[MessageCountMetric] = createGaugeVector(MessageCountMetric, PrometheusNamespace,
			"", "Total message count", emptyMap, commonLabels)
		// # HELP nsqd_client_count Number of clients
		// # TYPE nsqd_client_count gauge
		nsqMetrics[ClientCountMetric] = createGaugeVector(ClientCountMetric, PrometheusNamespace,
			"", "Number of clients", emptyMap, commonLabels)
		// # HELP nsqd_channel_count Number of channels
		// # TYPE nsqd_channel_count gauge
		nsqMetrics[ChannelCountMetric] = createGaugeVector(ChannelCountMetric, PrometheusNamespace,
			"", "Number of channels", emptyMap, commonLabels[:4])

		go fetchAndSetStats()

		logger.Info("service up")
		// Start HTTP server
		http.Handle("/metrics", promhttp.Handler())
		err := http.ListenAndServe(c.String("listenPort"), nil)
		if err != nil {
			logger.Fatal("Error starting Prometheus metrics server: " + err.Error())
		}
		return nil
	}

	err := app.Run(os.Args)
	if err != nil {
		logger.Info(err.Error())
	}
}

type know struct {
	topics   []string
	channels []string
}

// fetchAndSetStats scrapes stats from nsqd and updates all the Prometheus metrics
// above on the provided interval. If a dead topic or channel is detected, the
// application exits.
func fetchAndSetStats() {
	knowMap := make(map[string]*know, len(nsqds))
	for _, v := range nsqds {
		knowMap[v] = &know{}
	}
	for {
		for _, v := range nsqds {
			nsqd := v
			// Fetch stats
			stats, err := getNsqdStats(nsqd)
			if err != nil {
				logger.Error("Error scraping stats from nsqd: " + err.Error())
				continue
			}

			// Build list of detected topics and channels - the list of channels is built
			// including the topic name that each belongs to, as it is possible to have
			// multiple channels with the same name on different topics.
			var detectedChannels []string
			var detectedTopics []string
			for _, topic := range stats.Topics {
				detectedTopics = append(detectedTopics, topic.Name)
				for _, channel := range topic.Channels {
					detectedChannels = append(detectedChannels, topic.Name+channel.Name)
				}
			}

			// Exit if a dead topic or channel is detected
			if deadTopicOrChannelExists(knowMap[nsqd].topics, detectedTopics) {
				logger.Warning("At least one old topic no longer included in nsqd stats - rebuilding metrics")
				for _, metric := range nsqMetrics {
					metric.Reset()
				}
			}
			if deadTopicOrChannelExists(knowMap[nsqd].channels, detectedChannels) {
				logger.Warning("At least one old channel no longer included in nsqd stats - rebuilding metrics")
				for _, metric := range nsqMetrics {
					metric.Reset()
				}
			}
			// Update list of known topics and channels
			knowMap[nsqd].topics = detectedTopics
			knowMap[nsqd].channels = detectedChannels

			// Update info metric with health, start time, and nsqd version
			nsqMetrics[InfoMetric].
				WithLabelValues(nsqd, stats.Health, fmt.Sprintf("%d", stats.StartTime), stats.Version).Set(1)

			// Loop through topics and set metrics
			for _, topic := range stats.Topics {
				paused := "false"
				if topic.Paused {
					paused = "true"
				}
				nsqMetrics[DepthMetric].WithLabelValues(nsqd, "topic", topic.Name, paused, "").
					Set(float64(topic.Depth))
				nsqMetrics[BackendDepthMetric].WithLabelValues(nsqd, "topic", topic.Name, paused, "").
					Set(float64(topic.BackendDepth))
				nsqMetrics[ChannelCountMetric].WithLabelValues(nsqd, "topic", topic.Name, paused).
					Set(float64(len(topic.Channels)))

				// Loop through a topic's channels and set metrics
				for _, channel := range topic.Channels {
					paused = "false"
					if channel.Paused {
						paused = "true"
					}
					nsqMetrics[DepthMetric].WithLabelValues(nsqd, "channel", topic.Name, paused, channel.Name).
						Set(float64(channel.Depth))
					nsqMetrics[BackendDepthMetric].WithLabelValues(nsqd, "channel", topic.Name, paused, channel.Name).
						Set(float64(channel.BackendDepth))
					nsqMetrics[InFlightMetric].WithLabelValues(nsqd, "channel", topic.Name, paused, channel.Name).
						Set(float64(channel.InFlightCount))
					nsqMetrics[TimeoutCountMetric].WithLabelValues(nsqd, "channel", topic.Name, paused, channel.Name).
						Set(float64(channel.TimeoutCount))
					nsqMetrics[RequeueCountMetric].WithLabelValues(nsqd, "channel", topic.Name, paused, channel.Name).
						Set(float64(channel.RequeueCount))
					nsqMetrics[DeferredCountMetric].WithLabelValues(nsqd, "channel", topic.Name, paused, channel.Name).
						Set(float64(channel.DeferredCount))
					nsqMetrics[MessageCountMetric].WithLabelValues(nsqd, "channel", topic.Name, paused, channel.Name).
						Set(float64(channel.MessageCount))
					nsqMetrics[ClientCountMetric].WithLabelValues(nsqd, "channel", topic.Name, paused, channel.Name).
						Set(float64(len(channel.Clients)))
				}
			}
		}
		// Scrape every scrapeInterval
		time.Sleep(time.Duration(scrapeInterval) * time.Second)
	}
}

// deadTopicOrChannelExists loops through a list of known topic or channel names
// and compares them to a list of detected names. If a known name no longer exists,
// it is deemed dead, and the function returns true.
func deadTopicOrChannelExists(known []string, detected []string) bool {
	// Loop through all known names and check against detected names
	for _, knownName := range known {
		found := false
		for _, detectedName := range detected {
			if knownName == detectedName {
				found = true
				break
			}
		}
		// If a topic/channel isn't found, it's dead
		if !found {
			return true
		}
	}
	return false
}

// createGaugeVector creates a GaugeVec and registers it with Prometheus.
func createGaugeVector(name string, namespace string, subsystem string, help string,
	labels map[string]string, labelNames []string) *prometheus.GaugeVec {
	gaugeVec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        name,
		Help:        help,
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: labels,
	}, labelNames)
	if err := prometheus.Register(gaugeVec); err != nil {
		logger.Fatal("Failed to register prometheus metric: " + err.Error())
	}
	return gaugeVec
}
