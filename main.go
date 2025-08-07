package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"log/slog"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func NewMetrics(reg prometheus.Registerer) *prometheus.GaugeVec {
	m := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mqtt_sensor",
		Help: "HA|Homie metric.",
	}, []string{"device", "path", "property", "unit", "source_type"})

	err := reg.Register(m)
	if err != nil {
		fmt.Printf("Error registering metric : %s\n", err)
	}

	return m
}

type Args struct {
	brokerUrl     string
	listenAddress string
	hatopicPrefix string
	debug         bool
}

func parseArgs() Args {
	var args Args
	flag.StringVar(&(args.brokerUrl), "b", "[::1]:1883", "MQTT broker url. With the format tcp://<host>:<port>")
	flag.StringVar(&(args.listenAddress), "l", "[::1]:8080", "Address to listen on. With the format <ip>:<port>")
	flag.BoolVar(&(args.debug), "d", false, "Set debug mode")
	flag.Parse()

	return args
}

func startMetricServer(listenAddress string) chan error {
	c := make(chan error)
	go func() {
		err := http.ListenAndServe(listenAddress, nil)
		c <- err
	}()
	return c
}

func main() {
	args := parseArgs()
	lvl := slog.LevelInfo
	if args.debug {
		lvl = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: lvl,
	}))
	logger.Info("mqtt exporter start")
	reg := prometheus.NewRegistry()
	logger.Debug("prom exporter initialized")
	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))

	metric := NewMetrics(reg)

	mqttClient := mqtt.NewClient(
		mqtt.NewClientOptions().AddBroker(args.brokerUrl).SetOrderMatters(false).SetClientID("homiexporter"),
	)

	err := reg.Register(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name:        "mqtt_client_status",
			ConstLabels: prometheus.Labels{"broker": args.brokerUrl},
		},
		func() float64 {
			if mqttClient.IsConnected() {
				return 1
			} else {
				return 0
			}
		},
	))

	if err != nil {
		fmt.Printf("Error registering mqtt_client_status metric : %s\n", err)
		os.Exit(1)
	}

	mqtt_logger := logger.With("broker", args.brokerUrl)
	token := mqttClient.Connect()
	wait_result := token.WaitTimeout(5 * time.Second)
	err = token.Error()
	if err != nil {
		mqtt_logger.Error("error connecting to mqtt", "broker", args.brokerUrl, "error", err)
		return
	}

	mqtt_logger.Debug("mqtt client started and connected", "wait", wait_result)
	// Register to the homie topic
	topic := "homie/#"
	homie_mqtt_logger := mqtt_logger.With("subtopic", topic)
	devices := make(map[string]Device, 10)
	var devMutex sync.Mutex
	sub_token := mqttClient.Subscribe(
		topic,
		0, // At least once, it doesn't matter if we lose one event
		func(c mqtt.Client, m mqtt.Message) {
			onHomieMqttMsg(homie_mqtt_logger, metric, devices, &devMutex, c, m)
		},
	)

	wait_result = sub_token.WaitTimeout(5 * time.Second)
	err = sub_token.Error()
	if err != nil {
		homie_mqtt_logger.Error("error subscribing to topic", "error", err)
		return
	}

	haListener, err := NewHAListener(logger, mqttClient, metric)
	if err != nil {
		logger.Error("halistener creation error", "error", err)
		return
	}

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	httpChan := startMetricServer(args.listenAddress)
	logger.Debug("http started")

out:
	for {
		select {
		case <-signalChan:
			logger.Debug("interrupt received")
			break out
		case err := <-httpChan:
			logger.Error("http server", "error", err)
			break out
		case err := <-haListener.Done:
			logger.Warn("ha listener error", "error", err)
			break out
		}
	}
}
