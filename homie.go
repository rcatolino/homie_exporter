package main

import (
	"log/slog"
	"strconv"
	"strings"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/prometheus/client_golang/prometheus"
)

func (d *Device) parseHomieProp(
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
				"device":      d.name,
				"path":        d.path,
				"property":    prop.name,
				"unit":        prop.unit,
				"source_type": "homie",
			}).Set(value)
		}
	} else if parts[0] == "$name" {
		prop.name = string(payload)
	} else if parts[0] == "$datatype" {
		if strings.HasPrefix(payload, "int") || strings.HasPrefix(payload, "bool") {
			logger.Debug("Unsupported datatype, converting to float", "datatype", payload)
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

func parseHomie(
	logger *slog.Logger,
	metric *prometheus.GaugeVec,
	parts []string,
	payload []byte,
	devices map[string]Device,
	devMutex *sync.Mutex,
) {
	// Parse main topic
	path := strings.Join(parts[1:3], "/")
	logger = logger.With("path", path)
	devMutex.Lock()
	defer devMutex.Unlock()
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
		logger.Debug("attribute is ignored", "attribute", parts[3])
	} else {
		dev.parseHomieProp(logger, metric, parts[3], parts[4:], string(payload))
	}

	devices[path] = dev
}

func onHomieMqttMsg(
	logger *slog.Logger,
	metric *prometheus.GaugeVec,
	devices map[string]Device,
	devMutex *sync.Mutex,
	_ mqtt.Client,
	msg mqtt.Message,
) {
	payload := msg.Payload()
	logger = logger.With("ID", msg.MessageID(), "topic", msg.Topic(), "payload", payload)
	logger.Debug("New mqtt message")
	parts := strings.Split(msg.Topic(), "/")
	if len(parts) < 3 {
		logger.Error("Error parsing topic, expected 4+ parts")
		return
	} else if len(parts) == 3 {
		// root device attribute, ignore
		return
	}

	if parts[0] == "homie" {
		parseHomie(logger, metric, parts, payload, devices, devMutex)
	} else {
		logger.Error("Error parsing topic, doesn't start with 'homie'")
	}
}
