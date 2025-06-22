package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/prometheus/client_golang/prometheus"
)

type haconfig struct {
	DevClass          string `json:"dev_cla"`
	Unit              string `json:"unit_of_meas"`
	StatusClass       string `json:"stat_cla"`
	Name              string `json:"name"`
	StatusTopic       string `json:"stat_t"`
	AvailabilityTopic string `json:"avty_t"`
	UniqueId          string `json:"uniq_id"`
}

func parseHomeassistant(
	logger *slog.Logger,
	prefix string,
	parts []string,
	payload []byte,
	devices map[string]Device,
) {
	// The eventual path is composed of the prefix and the device name
	path := fmt.Sprintf("%s/%s", prefix, parts[2])
	logger = logger.With("path", path)
	if parts[1] != "sensor" {
		logger.Debug("Not a sensor device, ignoring")
		return
	}

	var conf haconfig
	if err := json.Unmarshal(payload, &conf); err != nil {
		logger.Warn("Failed to parse device configuration", "error", err)
		return
	}

	dev, exists := devices[path]
	if !exists {
		dev = Device{
			path: path,
			name: parts[2],
			properties: map[string]Property{},
		}
		logger.Debug("Created new homeassistant device")
	}

	prop, exists := dev.properties[conf.StatusTopic]
	if !exists {
		prop = Property{ignored: false}
		logger.Debug("Created new homeassistant property")
	} else {
		logger.Debug("Updating homeassistant property", "name", prop.name)
	}

	prop.name = conf.Name
	prop.unit = conf.Unit
	dev.properties[conf.StatusTopic] = prop
	devices[path] = dev
}

func onHaConfMsg(
	logger *slog.Logger,
	prefix string,
	devices map[string]Device,
	_ mqtt.Client,
	msg mqtt.Message,
) {
	payload := msg.Payload()
	logger = logger.With("ID", msg.MessageID(), "topic", msg.Topic(), "payload", payload)
	logger.Debug("New mqtt message")
	// homeassistant discovery messages should have a prefix like `homeassistant/sensor/devname/...`
	parts := strings.SplitN(msg.Topic(), "/", 4)
	if len(parts) != 4 {
		logger.Error("Error parsing topic, expected 4+ parts")
		return
	}

	if parts[0] == "homeassistant" {
		parseHomeassistant(logger, prefix, parts, payload, devices)
	} else {
		logger.Error("Error parsing topic, doesn't start with 'homeassistant'")
	}
}

func onHaDataMsg(
	logger *slog.Logger,
	metric *prometheus.GaugeVec,
	devices map[string]Device,
	_ mqtt.Client,
	msg mqtt.Message,
) {
	payload := msg.Payload()
	logger = logger.With("ID", msg.MessageID(), "topic", msg.Topic(), "payload", payload)
	parts := strings.Split(msg.Topic(), "/")
	if len(parts) < 3 {
		logger.Error("Error parsing topic, expected 3+ parts")
		return
	}

	if parts[1] == "discover" {
		logger.Debug("Ignoring device discovery message")
		return
	}

	if parts[2] == "debug" || parts[2] == "status" {
		logger.Debug("Ignoring debug|status message")
		return
	}

	logger.Debug("New mqtt message")
	path := strings.Join(parts[:2], "/")
	dev, exist := devices[path]
	if !exist {
		logger.Warn("received ha message for uknown device", "path", path)
		return
	}

	prop, exist := dev.properties[msg.Topic()]
	if !exist {
		logger.Warn("received ha message for unknown property", "path", path)
		return
	}

	value, err := strconv.ParseFloat(string(payload), 64)
	if err != nil {
		logger.Warn("Couldn't convert payload to float", "payload", payload, "error", err)
	} else if !prop.ignored {
		// Update metric
		metric.With(prometheus.Labels{
			"device":      dev.name,
			"path":        dev.path,
			"property":    prop.name,
			"unit":        prop.unit,
			"source_type": "ha",
		}).Set(value)
	}
}
