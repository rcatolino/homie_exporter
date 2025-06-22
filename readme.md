# [homie](https://homieiot.github.io/) exporter

Example with a mqtt broker at `broker.mqtt.example` :

```
$ go build
$ ./homieexporter -b tcp://broker.mqtt.example:1883 -l 0.0.0.0:4309
```

```
$ curl localhost:4309/metrics
# HELP homie_sensor Homie metric.
# TYPE homie_sensor gauge
homie_sensor{device="sensors",path="airqualilty/sensors",property="CO2",unit="ppm"} 9495
homie_sensor{device="sensors",path="airqualilty/sensors",property="Humidity",unit="%"} 92 
homie_sensor{device="sensors",path="airqualilty/sensors",property="NOx",unit="ppb"} 219
homie_sensor{device="sensors",path="airqualilty/sensors",property="PM01",unit="μg/m³"} 52
homie_sensor{device="sensors",path="airqualilty/sensors",property="PM10",unit="μg/m³"} 122
homie_sensor{device="sensors",path="airqualilty/sensors",property="PM2.5",unit="μg/m³"} 3902
homie_sensor{device="sensors",path="airqualilty/sensors",property="Temperature",unit="°C"} 38.6
```

# [homeassistant](https://www.home-assistant.io/integrations/mqtt#mqtt-discovery) mqtt exporter

Example with a mqtt broker at `broker.mqtt.example` :

```
$ go build
$ ./homieexporter -p esphome -b tcp://broker.mqtt.example:1883 -l 0.0.0.0:4309
```

```
$ curl localhost:4309/metrics
...
```
