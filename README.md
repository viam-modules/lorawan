
# [`lorawan module`](<https://github.com/oliviamiller/lorawan-gateway>)

## LoRaWAN Background
LoRaWAN (Long Range Wide Area Network) is a low-power, long-range wireless protocol in which the **sensors** communicate over radio to **gateways** that receive the messages.


For more, see the [article on the Viam blog](https://www.viam.com/post/launch-lorawan-support-viam).

## What's in this module
This Viam module provides models for LoRaWAN **sensors** (end devices/transmitters) as well as LoRaWAN **gateways** (receivers).

The LoRaWAN **sensor** models allow registering a LoRaWAN sensor with a LoRaWAN gateway model. These models also implement Viam's Sensor GetReadings method by calling GetReadings on the gateway model and filtering the output to just the particular sensor's readings.

The LoRaWAN **gateway** models interface with a physical gateway device (such as the [Waveshare sx1302 LoRaWAN gateway HAT for Raspberry Pi](https://www.waveshare.com/wiki/SX1302_LoRaWAN_Gateway_HAT)) to pull all sensor readings from the gateway device and return them through Viam's Sensor [GetReadings](https://docs.viam.com/dev/reference/apis/components/sensor/#getreadings) method.

## Example use
A typical architecture will involve:

- a Raspberry Pi (any model with GPIO pins)
- an sx1302 gateway HAT physically attached to the Raspberry Pi
- one or more LoRaWAN sensors physically located up to a couple miles away from the Raspberry Pi/gateway

The Raspberry Pi will be running viam-server, and the **Viam config** for this viam-server will include:

- a **gateway** model for the gateway HAT
- a **sensor** model for **each** LoRaWAN sensor

## LoRaWAN sensor models provided
This module provides the following models for **specific** LoRaWAN sensors:

- `dragino-LHT65N`: [Dragino LHT65N](https://www.amazon.com/LHT65N-LoRaWAN-Temperature-Humidity-Sensor/dp/B0BL7X7X6C) temperature and humidity sensor.
- `dragino-WQSLB`: [Dragino WQS-LB](https://www.dragino.com/products/water-air-quality-sensor/item/345-wqs-lb.html) water quality sensor
- `milesight-ct101`: [Milesight ct101](https://www.milesight.com/iot/product/lorawan-sensor/ct10x) current transformer
- `milesight-em310-tilt`: [Milesight em310 tilt sensor](https://www.milesight.com/iot/product/lorawan-sensor/em310-tilt)

This module also provides a **generic** model that can be used with any LoRaWAN sensor that meets the criteria, but requires inputting slightly more via the config attributes:

- `node`: Any LoRaWAN sensor that is Class A, supports either the US915 or EU868 frequency band, and uses LoRaWAN MAC specification version 1.0.3

See below for the Viam configuration for each of these models.

## LoRaWAN gateway models provided
This module provides the following models for **specific** LoRaWAN gateways:

- `sx1302-waveshare-hat`: [Waveshare LoRaWAN sx1302 gateway HAT](https://www.waveshare.com/wiki/SX1302_LoRaWAN_Gateway_HAT)

This module also provides a **sx1302 generic** model that can be used with other sx1302 HATs:

- `sx1302-hat-generic`: Other sx1302 LoRaWAN gateway HATs

See below for the Viam configuration for each of these models.

## Configuration for LoRaWAN sensor models

### Configuration for `dragino-LHT65N` and `dragino-WQSLB`

If using a WSQ-LB, be sure to calibrate the sensor using the instructions below.

#### Quick examples

Example OTAA node configuration:

```json
{
  "join_type": "OTAA",
  "dev_eui": "0123456789ABCDEF",
  "app_key": "0123456789ABCDEF0123456789ABCDEF",
  "gateways": ["gateway-1"]
}
```

Example ABP node configuration:
```json
{
  "join_type": "ABP",
  "dev_addr": "01234567",
  "app_s_key": "0123456789ABCDEF0123456789ABCDEF",
  "network_s_key": "0123456789ABCDEF0123456789ABCDEF",
  "gateways": ["gateway-1"]
}
```
#### General Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| join_type | string | no | "OTAA" | Join type ("OTAA" or "ABP") |
| uplink_interval_mins | float64 | no | 20 | Desired interval in minutes between uplink messages sent by the node. The sensor will send a set interval downlink if configured. |
| gateways | []string | yes | - | gateways the node can send data to. Can also be in the `Depends on` drop down. |

#### OTAA Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| dev_eui | string | yes | - | Device EUI (8 bytes in hex). Unique indentifer for the node. Can be found printed on your device or on the box.|
| app_key | string | yes | - | Application Key (16 bytes in hex). Used to securely join the network. Unique to the specific device, can be found on the box. |

#### ABP Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| dev_addr | string | yes | - | Device Address (4 bytes in hex). Used to identify uplink messages. Can normally be found on datasheet or box. |
| app_s_key | string | yes | - | Application Session Key (16 bytes in hex) Used to decrypt uplink messages. Default can normally be found on the node's datasheet or box. |
| network_s_key | string | yes | - | Network Session Key (16 bytes in hex) Used to decypt uplink messages. Default can normally be found on the node's datasheet or box. |

#### DoCommand

The dragino model uses DoCommand to send downlinks to sensors from the gateway. A purple light will flash after the sensor sends an uplink once a downlink has been successfully sent.
**The sensor must already be connected to Viam (i.e., "Live" on app.viam.com) for downlink requests to work.**

##### Update the uplink interval

This command will update the interval the dragino sends data at. This can also be set via the 'uplink_interval_mins' field in the config.

```json
{
  "set_interval": 1.0
}
```

##### Restart the device

This command will restart the dragino sensor, triggering a new join request.


```json
{
  "restart_sensor": ""
}
```

##### Send a generic downlink

This command will send a generic downlink payload to the gateway. the string is expected to be a set of bytes in hex. See the [LHT64 temperature sensor user guide](https://www.dragino.com/downloads/downloads/LHT65/UserManual/LHT65_Temperature_Humidity_Sensor_UserManual_v1.3.pdf) for other downlink commands.

```json
{
  "send_downlink": "<BYTESINHEX>"
}
```

#### Calibrate your `dragino-WQS-LB`
The WQS-LB water quality sensor should be calibrated upon first use. The calibration can be completed using do commands on the UI.
For more information about calibrating the devices, consult the [user manual.](https://wiki.dragino.com/xwiki/bin/view/Main/User%20Manual%20for%20LoRaWAN%20End%20Nodes/WQS-LB--LoRaWAN_Water_Quality_Sensor_Transmitter_User_Manual/)

##### Calibrate the PH Probe
The PH probe uses a 3 point calibration, follow the below steps to calibrate:
1. Wash the electrode with distilled water and place it in a 9.18 standard buffer solution. Once the data stabilizes, send the following downlink to the node:
```json
{
  "calibrate_ph": 9
}
```
2. Wash the electrode with distilled water and place it in a 6.86 standard buffer solution. Once the data stabilizes, send the following downlink to the node:
```json
{
  "calibrate_ph": 6
}
```
3. Wash the electrode with distilled water and place it in a 4.01 standard buffer solution. Once the data stabilizes, send the following downlink to the node:
```json
{
  "calibrate_ph": 4
}
```

##### Calibrate the electrical conductivity probe
The EC probe uses one-point calibration.

If K=1 (1-2000 uS/cm) use the following steps to calibrate:
1. Wash the electrode with distilled water and place it in a 1413 uS/cm solution
2. When data is stable, send the following downlink:
```json
{
  "calibrate_ec": 1
}
```

If K=10 (10-20000 mS/cm), use the following steps to calibrate:
1. Wash the electrode with distilled water and place it in a 12.88 mS/cm solution
2. When data is stable, send the following downlink:
```json
{
  "calibrate_ec_10": 10
}
```

##### Calibrate the turbidity probe
The turbidity probe uses a one-point calibration, use the following steps to calibrate:

1. Prepare a 0 NTU, 200 NTU, 400 NTU, 600 NTU, 800 NTU, or 1000 NTU solution
2. Place the probe in the solution
3. Send the downlink with NTU value to your node:
```json
{
  "calibrate_t": <NTU value>
}
```

##### Calibrate the orp probe
The orp probe use 2-point calibration. To calibrate, follow these steps:
1. Wash the electrode with distilled water and place it in a 86mV standard buffer.
2. Once the data is stable, send the following downlink to the node:
```json
{
  "calibrate_orp" : 86
}
```

3. Wash with distilled water again, and place probe in a 256mV standard buffer.
4. Once the data is stable, send the following downlink to the node:
```json
{
  "calibrate_orp": 256
}
```

### Configuration for `milesight-ct101` and `milesight-em310-tilt`

#### Quick examples
Example OTAA node configuration:

```json
{
  "join_type": "OTAA",
  "dev_eui": "0123456789ABCDEF",
  "gateways": ["gateway-1"]
}
```

Example ABP node configuration:

```json
{
  "join_type": "ABP",
  "dev_addr": "01234567",
  "gateways": ["gateway-1"]
}
```

#### General Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| join_type | string | no | "OTAA" | Join type ("OTAA" or "ABP") |
| uplink_interval_mins | float64 | no | **10** for the ct101 and **1080** for the em310-tilt | Desired interval in minutes between uplink messages sent by the node. The sensor will send a set interval downlink if configured. |
| gateways | []string | yes | - | gateways the node can send data to. Can also be in the `Depends on` drop down. |

#### OTAA Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| dev_eui | string | yes | - | Device EUI (8 bytes in hex). Unique indentifer for the node. Can be found printed on your device or on the box.|
| app_key | string | no | 5572404C696E6B4C6F52613230313823 | Application Key (16 bytes in hex). Used to securely join the network. |

#### ABP Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| dev_addr | string | yes | - | Device Address (4 bytes in hex). Used to identify uplink messages. Can normally be found on datasheet or box. |
| app_s_key | string | no | 5572404C696E6B4C6F52613230313823 | Application Session Key (16 bytes in hex) Used to decrypt uplink messages. |
| network_s_key | string | no | 5572404C696E6B4C6F52613230313823 | Network Session Key (16 bytes in hex) Used to decypt uplink messages. |

#### DoCommand

The Milesight models use DoCommands to send downlinks to sensors from the gateway. A purple light will flash after the sensor sends an uplink once a downlink has been successfully sent.
**The sensor must already be connected to Viam (i.e., "Live" on app.viam.com) for downlink requests to work.**

##### Update the uplink interval

This command will update the interval the milesight sends data at. This can also be set via the 'uplink_interval_mins' field in the config.

```json
{
  "set_interval": 1.0
}
```

##### Restart the device

This command will restart the milesight sensor, triggering a new join request.

```json
{
  "restart_sensor": ""
}
```

##### Send a generic downlink

This command will send a generic downlink payload to the gateway. The string is expected to be a set of bytes in hex. See the [em310 tilt sensor user guide](https://resource.milesight.com/milesight/iot/document/em310-tilt-user-guide-en.pdf) or the [ct101 user guide](https://resource.milesight.com/milesight/iot/document/ct10x-user-guide-en.pdf) for their respective downlink commands.

```json
{
  "send_downlink": "<BYTESINHEX>"
}
```

### Configuration for `viam:sensor:node`

#### Quick examples

Example OTAA node configuration:

```json
{
  "join_type": "OTAA",
  "decoder_path": "/path/to/decoder.js",
  "dev_eui": "0123456789ABCDEF",
  "app_key": "0123456789ABCDEF0123456789ABCDEF",
  "uplink_interval_mins": 10,
  "gateways": ["gateway-1"]
}
```

Example ABP node configuration:

```json
{
  "join_type": "ABP",
  "decoder_path": "/path/to/decoder.js",
  "dev_addr": "01234567",
  "app_s_key": "0123456789ABCDEF0123456789ABCDEF",
  "network_s_key": "0123456789ABCDEF0123456789ABCDEF",
  "uplink_interval_mins": 10,
  "gateways": ["gateway-1"],
  "fport": "55"
}
```

#### General Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| decoder_path | string | yes | - | Path to the payload decoder script. This must be a .js file. If the device provides multiple decoder files, use the chirpstack version. |
| join_type | string | no | "OTAA" | Join type ("OTAA" or "ABP"). |
| uplink_interval_mins | float64 | yes | - | Expected interval between uplink messages sent by the node. The default can be found on the datasheet and can be modified using device specific software. |
| gateways | []string | yes | - | gateways the node can send data to. Can also be in the `Depends on` drop down. |
| fport | string | no | - | port (in hex) to send downlinks to the device. |

The node registers itself with the gateway so the gateway will recognize messages from the node.

#### OTAA Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| dev_eui | string | yes | - | Device EUI (8 bytes in hex). Unique indentifer for the node. Can be found printed on your device or on the box.|
| app_key | string | yes | - | Application Key (16 bytes in hex). Used to securely join the network. The default can normally be found in the node's datasheet. |

#### ABP Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| dev_addr | string | yes | - | Device Address (4 bytes in hex). Used to identify uplink messages. Can normally be found on datasheet or box. |
| app_s_key | string | yes | - | Application Session Key (16 bytes in hex) Used to decrypt uplink messages. Default can normally be found on the node's datasheet or box. |
| network_s_key | string | yes | - | Network Session Key (16 bytes in hex) Used to decypt uplink messages. Default can normally be found on the node's datasheet or box. |

#### DoCommand

The sensor node model uses DoCommands to send downlinks to sensors from the gateway.

##### Send a downlink

This command will send a generic downlink payload to the gateway. The string is expected to be a set of bytes in hex.

```json
{
  "send_downlink": "<BYTESINHEX>"
}
```


## Configuration for LoRaWAN gateway models

### Configuration for `viam:sensor:sx1302-gateway`

#### Quick example

```json
{
    "board": "rpi",
    "spi_bus": 0,
    "reset_pin": int,
    "power_en_pin": int,
    "region_code": "US915"
}
```

#### Attributes

The following attributes are available for `viam:sensor:sx1302-gateway` sensors:

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| board | string | yes | - | Name of the board connected to the HAT. The board communicates with the gateway through SPI |
| reset_pin | int | yes | - | GPIO pin number for sx1302 reset pin |
| spi_bus | int | no | 0 | SPI bus number (0 or 1) |
| power_en_pin | int | no | - | GPIO pin number for the power enable pin |
| region_code | string | no | US915 | frequency region of your gateway (US915 or EU868) |

### Configuration for `viam:sensor:sx1302-hat-generic`

Note: The gpio pins MUST be set to the gpio pin numbers on your board connected to the reset and power-enable pins of the sx1302 hat to avoid a 15-minute reset loop.

#### Quick example

```json
{
    "board": "rpi",
    "spi_bus": 0,
    "reset_pin": int,
    "power_en_pin": int,
    "region_code": "US915"
}
```

#### Attributes

The following attributes are available for `viam:sensor:sx1302-hat-generic` sensors:

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| board | string | yes | - | Name of the board connected to the HAT. The board communicates with the gateway through SPI |
| reset_pin | int | yes | - | GPIO pin number for sx1302 reset pin |
| spi_bus | int | no | 0 | SPI bus number  |
| power_en_pin | int | no | - | GPIO pin number for the power enable pin |
| region_code | string | no | US915 | frequency region of your gateway (US915 or EU868) |

### Configuration for `viam:sensor:sx1302-waveshare-hat`

#### Quick example

```json
{
    "board": "rpi",
    "spi_bus": 0,
    "region_code": "US915"
}
```

### Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| board | string | yes | - | Name of the board connected to the HAT. The board communicates with the gateway through SPI |
| spi_bus | int | no | 0 | SPI bus number (0 or 1 on a raspberry pi) |
| region_code | string | no | US915 | frequency region of your gateway (US915 or EU868) |

## Troubleshooting
When the gateway is properly configured, the pwr LED will be solid red and the rx and tx LEDs will be blinking red.

It may take several minutes after starting the module to start receiving data, especially if your node transmits on more than 8 frequency channels.
The gateway will log info logs when it has received a join request or data uplink.

The gateway communicates through SPI, so [ensure that SPI in enabled on the Pi](https://docs.viam.com/operate/reference/prepare/rpi-setup/#enable-communication-protocols).

To avoid capturing duplicate data, set the data capture frequency equal to or less than the expected uplink interval.

If you see the error `ERROR: Failed to set SX1250_0 in STANDBY_RC mode` in logs, unplug the Raspberry Pi for a few minutes and then try again.
