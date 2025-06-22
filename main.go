package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
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
	broker_url     string
	listen_address string
	hatopic_prefix string
	debug          bool
}

func parseArgs() Args {
	var args Args
	flag.StringVar(&(args.broker_url), "b", "[::1]:1883", "MQTT broker url. With the format tcp://<host>:<port>")
	flag.StringVar(&(args.listen_address), "l", "[::1]:8080", "Address to listen on. With the format <ip>:<port>")
	flag.StringVar(&(args.hatopic_prefix), "p", "", "Mqtt topic prefix for homeassistant sensors messages")
	flag.BoolVar(&(args.debug), "d", false, "Set debug mode")
	flag.Parse()

	return args
}

func main() {
	args := parseArgs()
	devices := make(map[string]Device, 10)
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

	mqtt_client := mqtt.NewClient(
		mqtt.NewClientOptions().AddBroker(args.broker_url),
	)

	mqtt_logger := logger.With("broker", args.broker_url)
	token := mqtt_client.Connect()
	wait_result := token.WaitTimeout(5 * time.Second)
	err := token.Error()
	if err != nil {
		mqtt_logger.Error("error connecting to mqtt", "broker", args.broker_url, "error", err)
		return
	}

	mqtt_logger.Debug("mqtt client started and connected", "wait", wait_result)
	// Register to the homie topic
	topic := "homie/#"
	homie_mqtt_logger := mqtt_logger.With("subtopic", topic)
	sub_token := mqtt_client.Subscribe(
		topic,
		0, // At least once, it doesn't matter if we lose one event
		func(c mqtt.Client, m mqtt.Message) {
			onHomieMqttMsg(homie_mqtt_logger, metric, devices, c, m)
		},
	)

	wait_result = sub_token.WaitTimeout(5 * time.Second)
	err = sub_token.Error()
	if err != nil {
		homie_mqtt_logger.Error("error subscribing to topic", "error", err)
		return
	}

	if args.hatopic_prefix != "" {
		logger.Info("hatopic_prefix is set, subscribing to messages for homeassistant", "prefix", args.hatopic_prefix)
		// Register to the configuration/discovery topic for homeassistant messages
		topic = "homeassistant/#"
		haconf_mqtt_logger := mqtt_logger.With("subtopic", topic)
		sub_token = mqtt_client.Subscribe(
			topic,
			0, // At least once, it doesn't matter if we lose one event
			func(c mqtt.Client, m mqtt.Message) {
				onHaConfMsg(haconf_mqtt_logger, args.hatopic_prefix, devices, c, m)
			},
		)

		wait_result = sub_token.WaitTimeout(5 * time.Second)
		err = sub_token.Error()
		if err != nil {
			haconf_mqtt_logger.Error("error subscribing to topic", "error", err)
			return
		}

		// Register to the data topic for homeassistant messages
		topic = fmt.Sprintf("%s/#", args.hatopic_prefix)
		ha_mqtt_logger := mqtt_logger.With("subtopic", topic)
		sub_token = mqtt_client.Subscribe(
			topic,
			0,
			func(c mqtt.Client, m mqtt.Message) {
				onHaDataMsg(ha_mqtt_logger, metric, devices, c, m)
			},
		)

		wait_result = sub_token.WaitTimeout(5 * time.Second)
		err = sub_token.Error()
		if err != nil {
			ha_mqtt_logger.Error("error subscribing to topic", "error", err)
			return
		}

		logger.Debug("mqtt client subscribed", "subtoken", sub_token)
	} else {
		logger.Info("hatopic_prefix is not set, ignoring messages for homeassistant")
	}

	err = http.ListenAndServe(args.listen_address, nil)
	if err != nil {
		logger.Error("Error starting server", "error", err)
		return
	}
}
