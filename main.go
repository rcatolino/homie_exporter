package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"log/slog"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Property struct {
	name    string
	unit    string
	ignored bool
}

// Actually this should be a device Node, but I'm not parsing root device properties,
// so let's simplify this a bit
type Device struct {
	path       string
	name       string
	properties map[string]Property
}

func (d *Device) parseMqttProp(
	logger *slog.Logger,
	metric *prometheus.GaugeVec,
	prop_name string,
	parts []string,
	payload string,
) {
	logger = logger.With("property", prop_name)
	prop, exists := d.properties[prop_name]

	if !exists {
		prop = Property{
			name:    "",
			unit:    "",
			ignored: false,
		}
		logger.Debug("Created new homie property")
	}

	if len(parts) == 0 {
		value, err := strconv.ParseFloat(payload, 64)
		if err != nil {
			logger.Warn("Couldn't convert payload to float", "payload", payload, "error", err)
		} else if !prop.ignored {
			metric.With(prometheus.Labels{
				"device":   d.name,
				"path":     d.path,
				"property": prop.name,
				"unit":     prop.unit,
			}).Set(value)
		}
	} else if parts[0] == "$name" {
		prop.name = string(payload)
	} else if parts[0] == "$datatype" {
		if strings.HasPrefix(payload, "int") || strings.HasPrefix(payload, "bool") {
			logger.Info("Unsupported datatype, converting to float", "datatype", payload)
		} else if !strings.HasPrefix(payload, "float") {
			logger.Warn("Unsupported datatype, ignoring property", "datatype", payload)
			prop.ignored = true
		}
	} else if parts[0] == "$unit" {
		prop.unit = payload
	} else if parts[0][0] == '$' {
		logger.Debug("Attribute is ignored", "attribute", parts[0])
	} else {
		logger.Error("Unexpected property attributes", "attributes", parts)
	}

	d.properties[prop_name] = prop
}

func NewMetrics(reg prometheus.Registerer) *prometheus.GaugeVec {
	m := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "homie_sensor",
		Help: "Homie metric.",
	}, []string{"device", "path", "property", "unit"})

	err := reg.Register(m)
	if err != nil {
		fmt.Printf("Error registering metric : %s\n", err)
	}
	return m
}

func onMqttMsg(
	logger *slog.Logger,
	metric *prometheus.GaugeVec,
	devices map[string]Device,
	_ mqtt.Client,
	msg mqtt.Message,
) {
	payload := msg.Payload()
	logger = logger.With("ID", msg.MessageID(), "topic", msg.Topic(), "payload", payload)
	logger.Debug("New mqtt message")
	parts := strings.Split(msg.Topic(), "/")
	if len(parts) < 3 {
		logger.Error("Error parsing homie topic, expected 4+ parts")
		return
	} else if len(parts) == 3 {
		// root device attribute, ignore
		return
	}

	if parts[0] != "homie" {
		logger.Error("Error parsing homie topic, doesn't start with 'homie'")
		return
	}

	// Parse main topic
	path := strings.Join(parts[1:3], "/")
	logger = logger.With("path", path)
	dev, exists := devices[path]
	if !exists {
		dev = Device{
			path:       path,
			name:       "",
			properties: map[string]Property{},
		}
		logger.Debug("Created new homie device")
	}

	if parts[3] == "$name" {
		dev.name = string(payload)
	} else if parts[3] == "$properties" {
		props := strings.Split(string(payload), ",")
		dev.properties = make(map[string]Property, len(props))
	} else if parts[3][0] == '$' {
		logger.Info("attribute is ignored", "attribute", parts[3])
	} else {
		dev.parseMqttProp(logger, metric, parts[3], parts[4:], string(payload))
	}

	devices[path] = dev
}

type Args struct {
	broker_url     string
	listen_address string
	debug          bool
}

func parseArgs() Args {
	var args Args
	flag.StringVar(&(args.broker_url), "b", "broker", "MQTT broker url. With the format tcp://<host>:<port>")
	flag.StringVar(&(args.listen_address), "l", "list", "Address to listen on. With the format <ip>:<port>")
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
	logger.Info("homie exporter start")
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
	topic := "homie/#"
	mqtt_logger = mqtt_logger.With("subtopic", topic)
	sub_token := mqtt_client.Subscribe(
		topic,
		0, // At least once, it doesn't matter if we lose one event
		func(c mqtt.Client, m mqtt.Message) {
			onMqttMsg(mqtt_logger, metric, devices, c, m)
		},
	)

	wait_result = sub_token.WaitTimeout(5 * time.Second)
	err = sub_token.Error()
	if err != nil {
		mqtt_logger.Error("error subscribing to topic", "error", err)
		return
	}

	logger.Debug("mqtt client subscribed", "subtoken", sub_token)
	err = http.ListenAndServe(args.listen_address, nil)
	if err != nil {
		logger.Error("Error starting server", "error", err)
		return
	}
}
