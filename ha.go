package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/prometheus/client_golang/prometheus"
)

type HAEntity struct {
	AvailabilityTopic string    `json:"avty_t"`
	DevClass          string    `json:"dev_cla"`
	EntityCategory    string    `json:"ent_cat"`
	Name              string    `json:"name"`
	StateClass        string    `json:"stat_cla"`
	StatusTopic       string    `json:"stat_t"`
	UniqueId          string    `json:"uniq_id"`
	Unit              string    `json:"unit_of_meas"`
	Device            *HADevice `json:"dev"`
}

type HADevice struct {
	ID      string `json:"ids"`
	Name    string `json:"name"`
	Version string `json:"sw"`
	Model   string `json:"mdl"`
	Vendor  string `json:"mf"`
}

type HAListener struct {
	Done    chan error
	client  mqtt.Client
	devices map[string]Device
	logger  *slog.Logger
	prefix  string
	metric  *prometheus.GaugeVec
}

func NewHAListener(logger *slog.Logger, client mqtt.Client, prefix string, metric *prometheus.GaugeVec) (*HAListener, error) {
	l := HAListener{
		Done:    make(chan error),
		client:  client,
		devices: map[string]Device{},
		logger:  logger,
		prefix:  prefix,
		metric:  metric,
	}

	// Register to the configuration/discovery topic for homeassistant/sensors messages
	token := l.client.Subscribe(
		"homeassistant/sensor/#",
		1, // At least once
		func(c mqtt.Client, m mqtt.Message) {
			l.onHaConfMsg(m.Topic(), m.Payload())
		},
	)

	waitResult := token.WaitTimeout(5 * time.Second)
	if !waitResult {
		return nil, fmt.Errorf("homeassistant topic subscription timeout")
	} else if err := token.Error(); err != nil {
		return nil, err
	}

	return &l, nil
}

func (h *HAListener) onHaConfMsg(topic string, payload []byte) {
	h.logger.Debug("New mqtt message", "topic", topic)
	// homeassistant discovery messages should have a look like `homeassistant/sensor/<dev path>/config`
	// we only need to check the topic ends with 'config'
	if !strings.HasSuffix(topic, "/config") {
		h.logger.Debug("received ha message, but it's not a config topic", "topic", topic)
		return
	}

	var conf HAEntity
	if err := json.Unmarshal(payload, &conf); err != nil {
		h.logger.Warn("failed to parse device configuration", "topic", topic, "payload", payload, "error", err)
		return
	}

	if conf.UniqueId == "" {
		h.logger.Warn("ha entity configuration is missing 'uniq_id' field", "topic", topic, "payload", payload)
		return
	}

	if conf.Device.ID == "" {
		h.logger.Warn("ha device configuration is missing 'ids' field", "topic", topic, "payload", payload)
		return
	}

	dev, exists := h.devices[conf.Device.ID]
	// Create dev node if it doesn't exist yet
	if !exists {
		dev = Device{
			properties: map[string]Property{},
		}
	}

	// Always update the device attribute in case they change
	topicParts := strings.SplitN(conf.StatusTopic, "/", 3)
	dev.path = strings.Join(topicParts[:2], "/")
	dev.name = conf.Device.Name
	if !exists {
		h.logger.Debug("Created new homeassistant device", "name", dev.name, "path", dev.path)
	}

	prop, propExists := dev.properties[conf.UniqueId]
	// Create prop node if it doesn't exist yet
	if !propExists {
		prop = Property{ignored: false}
		h.logger.Debug("Created new homeassistant property")
		// Subscribe to the prop state topic
		token := h.client.Subscribe(
			conf.StatusTopic,
			0,
			func(c mqtt.Client, m mqtt.Message) {
				h.onHaDataMsg(&dev, &prop, m.Payload())
			},
		)

		waitResult := token.WaitTimeout(1 * time.Second)
		if !waitResult {
			h.Done <- fmt.Errorf("homeassistant topic subscription timeout")
			return
		} else if err := token.Error(); err != nil {
			h.Done <- err
			return
		}
	} else {
		h.logger.Debug("Updating homeassistant property", "name", prop.name)
	}

	// Always update the prop attribute in case they change
	prop.name = conf.Name
	prop.unit = conf.Unit
	dev.properties[conf.UniqueId] = prop
	// Update the device
	h.devices[conf.Device.ID] = dev

	// parse config message
	/*
		// Register to the data topic for homeassistant messages
		topic = fmt.Sprintf("%s/#", args.hatopicPrefix)
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
	*/
}

func (h *HAListener) onHaDataMsg(dev *Device, prop *Property, payload []byte) {
	h.logger.Debug("New mqtt message")
	value, err := strconv.ParseFloat(string(payload), 64)
	if err != nil {
		h.logger.Warn("Couldn't convert payload to float", "payload", payload, "error", err)
	} else if !prop.ignored {
		// Update metric
		h.metric.With(prometheus.Labels{
			"device":      dev.name,
			"path":        dev.path,
			"property":    prop.name,
			"unit":        prop.unit,
			"source_type": "ha",
		}).Set(value)
	}
}
