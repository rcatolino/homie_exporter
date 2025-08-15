# mqtt exporter for topic following the [Home Assistant format](https://www.home-assistant.io/integrations/mqtt#mqtt-discovery)

Example with a mqtt broker at `broker.mqtt.example` :

```
$ go build
$ ./ha_mqtt_exporter -b tcp://broker.mqtt.example:1883 -l 0.0.0.0:4309
```

```
$ curl localhost:4309/metrics
# HELP mqtt_sensor Homie metric.
# TYPE mqtt_sensor gauge
mqtt_sensor{device="sensors",path="airqualilty/sensors",property="CO2",unit="ppm"} 9495
mqtt_sensor{device="sensors",path="airqualilty/sensors",property="Humidity",unit="%"} 92 
mqtt_sensor{device="sensors",path="airqualilty/sensors",property="NOx",unit="ppb"} 219
mqtt_sensor{device="sensors",path="airqualilty/sensors",property="PM01",unit="μg/m³"} 52
mqtt_sensor{device="sensors",path="airqualilty/sensors",property="PM10",unit="μg/m³"} 122
mqtt_sensor{device="sensors",path="airqualilty/sensors",property="PM2.5",unit="μg/m³"} 3902
mqtt_sensor{device="sensors",path="airqualilty/sensors",property="Temperature",unit="°C"} 38.6
```
