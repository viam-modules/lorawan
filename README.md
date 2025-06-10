
# LoRaWAN module

LoRaWAN (Long Range Wide Area Network) is a low-power, long-range wireless protocol in which the **nodes** communicate over radio with **gateways**.

For more, see the [the Viam documentation page](https://docs.viam.com/data-ai/capture-data/lorawan/).

## What's in this module?

This Viam module provides models for LoRaWAN **nodes** (end devices/transmitters) as well as LoRaWAN **gateways** (receivers).

The LoRaWAN **node** models allow registering a LoRaWAN node with a LoRaWAN gateway. These models also implement Viam's Sensor GetReadings method by calling GetReadings on the gateway model and filtering the output to just the particular nodes's readings.

The LoRaWAN **gateway** models interface with a physical gateway device (such as the [Waveshare SX1302 LoRaWAN gateway HAT for Raspberry Pi](https://www.waveshare.com/wiki/SX1302_LoRaWAN_Gateway_HAT)) to pull all sensor readings from the gateway device and return them through Viam's Sensor [GetReadings](https://docs.viam.com/dev/reference/apis/components/sensor/#getreadings) method.

## Example use
A typical architecture involves:

- a gateway physically attached to a machine -- either an HAT connected to a Raspberry Pi SBC or a machine with internal LoRaWAN radios
- one or more LoRaWAN nodes physically located up to a couple miles away from the gateway

The machine runs `viam-server`. The `viam-server` configuration must include:

- a **board** model for the machine that hosts your gateway
- a **gateway** model for the gateway
- a **node** model for **each** LoRaWAN node

## Supported LoRaWAN nodes

This module provides models for the following LoRaWAN node hardware:

- `viam:lorawan:dragino-LHT65N`: [Dragino LHT65N](https://www.amazon.com/LHT65N-LoRaWAN-Temperature-Humidity-Sensor/dp/B0BL7X7X6C) temperature and humidity sensor.
- `viam:lorawan:dragino-WQSLB`: [Dragino WQS-LB](https://www.dragino.com/products/water-air-quality-sensor/item/345-wqs-lb.html) water quality sensor
- `viam:lorawan:milesight-ct101`: [Milesight CT101](https://www.milesight.com/iot/product/lorawan-sensor/ct10x) current transformer
- `viam:lorawan:milesight-em310-tilt`: [Milesight EM310-TILT sensor](https://www.milesight.com/iot/product/lorawan-sensor/em310-tilt)
- `viam:lorawan:node`: a generic model that can be used with any LoRaWAN node that meets the following criteria:
  - Class A
  - supports the `US915` or `EU868` frequency bands
  - uses LoRaWAN MAC specification version 1.0.3

See the configuration options for each of these models below.

## Supported LoRaWAN gateways

This module provides models for the following LoRaWAN gateway hardware:

- `viam:lorawan:sx1302-waveshare-hat`: [Waveshare LoRaWAN SX1302 gateway HAT](https://www.waveshare.com/wiki/SX1302_LoRaWAN_Gateway_HAT)
- `viam:lorawan:rak7391`: [RAK7391 Wisgate Connect](https://docs.rakwireless.com/product-categories/wisgate/rak7391/overview/)
- `viam:lorawan:sx1302-hat-generic`: a generic model that can be used with all other peripherals built using the SX1302 or SX1303 chips

See below for the Viam configuration for each of these models.

## Configuration for LoRaWAN nodes

### Configuration for `viam:lorawan:dragino-LHT65N` and `viam:lorawan:dragino-WQS-LB`

#### Examples

Example OTAA node configuration:

```json
{
  "join_type": "OTAA",
  "dev_eui": <string>,
  "app_key": <string>,
  "gateways": [<string>]
}
```

Example ABP node configuration:

```json
{
  "join_type": "ABP",
  "dev_addr": <string>,
  "app_s_key": <string>,
  "network_s_key": <string>,
  "gateways": [<string>]
}
```

#### General attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `gateways` | []string | yes | - | An array containing the name of the [gateway component](#add-a-gateway) in your Viam configuration. Alternatively, specify the gateway using the `Depends on` drop down. |
| `decoder_path` | string | no | (see description) | Path to a Javascript **decoder script** used to interpret data transmitted from the node. You can use a local path on your device or an HTTP(S) URL that points to a file on a remote server. If the decoder script provides multiple implementations, uses the Chirpstack version. Not compatible with The Things Network decoders. Defaults to the latest decoder published by the manufacturer on GitHub. |
| `join_type` | string | no | `OTAA` | The [activation protocol](https://docs.viam.com/data-ai/capture-data/lorawan/#activation-protocols) used to secure this network. Options: [`OTAA`, `ABP`] |
| `uplink_interval_mins` | float64 | no | 20.0 | Interval between uplink messages sent from the node, in minutes. Found in the device datasheet, but can be modified. Configured by downlink after initial connection. |
| `fport` | string | no | `01` (`0x01`) | 8-bit hexadecimal **frame port** used to send downlinks to the device. Found in the device datasheet. |


#### OTAA Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `dev_eui` | string | yes | - | The **device EUI (Extended Unique Identifier)**, a unique 64-bit identifier for the LoRaWAN device in hexadecimal format (16 characters). Found on your device or in device packaging. |
| `app_key` | string | yes | - | The 128-bit hexadecimal AES **application key** used for device authentication and session key derivation. Required for OTAA activation protocol. Found in the device datasheet. |

#### ABP attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `dev_addr` | string | yes | - | 32-bit hexadecimal **device address** used to identify this device in LoRaWAN messages. Found in the device datasheet or in device packaging. |
| `app_s_key` | string | yes | - | 128-bit hexadecimal **application session key** used to decrypt messages containing application data.  Found in the device datasheet or in device packaging. |
| `network_s_key` | string | yes | - | 128-bit hexadecimal **network session key** used to decrypt network management messages. Found in the device datasheet or in device packaging. |

NOTE: If using a WQS-LB, be sure to calibrate the sensor using the instructions below.

#### DoCommand

You can use DoCommand to send downlink requests to nodes connected to your gateway.
Connected nodes indicate communication using a blue light for uplinks and purple light for downlinks.

##### Update the uplink interval

Update the interval the node sends data at:

```json
{
  "set_interval": <float64>
}
```

Alternatively, set the interval using `uplink_interval_mins` in the node configuration.

##### Restart the device

Restart the node, triggering a new join request:


```json
{
  "restart_sensor": ""
}
```

##### Send a generic downlink

Send a generic downlink payload (in hexadecimal) to the node:

```json
{
  "send_downlink": <string>
}
```

For more information about downlink commands, see the [LHT65 temperature sensor user guide](https://www.dragino.com/downloads/downloads/LHT65/UserManual/LHT65_Temperature_Humidity_Sensor_UserManual_v1.3.pdf).

#### Calibrate your WQS-LB

Calibrate your WQS-LB water quality sensor before using it. You can calibrate your sensor using DoCommands.
For more information about calibration, consult the [user manual](https://wiki.dragino.com/xwiki/bin/view/Main/User%20Manual%20for%20LoRaWAN%20End%20Nodes/WQS-LB--LoRaWAN_Water_Quality_Sensor_Transmitter_User_Manual/).

##### Calibrate the pH probe

The pH probe uses a thre-point calibration process:

1. Wash the electrode with distilled water, then place it in a 9.18 standard buffer solution. Once the data stabilizes, send the following downlink to the node:
   ```json
   {
     "calibrate_ph": 9
   }
   ```
2. Wash the electrode with distilled water, then place it in a 6.86 standard buffer solution. Once the data stabilizes, send the following downlink to the node:
   ```json
   {
     "calibrate_ph": 6
   }
   ```
3. Wash the electrode with distilled water, then place it in a 4.01 standard buffer solution. Once the data stabilizes, send the following downlink to the node:
   ```json
   {
     "calibrate_ph": 4
   }
   ```

##### Calibrate the electrical conductivity probe

The EC probe uses a one-point calibration process. You can configure the EC probe in the following modes:

- For K=1, to measure conductivity from 0-2000 μS/cm at a resolution of 1 μS/cm, wash the electrode with distilled water and place it in a 1413 μS/cm solution. Once the data stabilizes, send the following downlink:
  ```json
  {
    "calibrate_ec": 1
  }
  ```

- For K=10, to measure conductivity from 10-20000 μS/cm at a resolution of 10 μS/cm: wash the electrode with distilled water and place it in a 12.88 mS/cm solution. Once the data stabilizes, send the following downlink:
  ```json
  {
    "calibrate_ec": 10
  }
  ```

##### Calibrate the turbidity probe

The turbidity probe uses a one-point calibration process:

1. Prepare a 0 NTU, 200 NTU, 400 NTU, 600 NTU, 800 NTU, or 1000 NTU solution.
2. Place the probe in the solution.
3. Send the downlink with NTU value to your node:
   ```json
   {
     "calibrate_t": <NTU value>
   }
   ```

##### Calibrate the ORP probe

The Oxidation-Reduction Potential (ORP) probe uses a two-point calibration process:

1. Wash the electrode with distilled water and place the probe in a 86mV standard buffer.
2. Once the data is stable, send the following downlink to the node:
   ```json
   {
     "calibrate_orp" : 86
   }
   ```

3. Wash the electrode with distilled water and place the probe in a 256mV standard buffer.
4. Once the data is stable, send the following downlink to the node:
   ```json
   {
     "calibrate_orp": 256
   }
   ```

### Configuration for `viam:lorawan:milesight-ct101` and `viam:lorawan:milesight-em310-tilt`

#### Examples

Example OTAA node configuration:

```json
{
  "join_type": "OTAA",
  "dev_eui": <string>,
  "gateways": [<string>]
}
```

Example ABP node configuration:

```json
{
  "join_type": "ABP",
  "dev_addr": <string>,
  "gateways": [<string>]
}
```

#### General Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `gateways` | []string | yes | - | An array containing the name of the [gateway component](#add-a-gateway) in your Viam configuration. Alternatively, specify the gateway using the `Depends on` drop down. |
| `decoder_path` | string | no | (see description) | Path to a Javascript **decoder script** used to interpret data transmitted from the node. You can use a local path on your device or an HTTP(S) URL that points to a file on a remote server. If the decoder script provides multiple implementations, uses the Chirpstack version. Not compatible with The Things Network decoders. Defaults to the latest decoder published by the manufacturer on GitHub. |
| `join_type` | string | no | `OTAA` | The [activation protocol](https://docs.viam.com/data-ai/capture-data/lorawan/#activation-protocols) used to secure this network. Options: [`OTAA`, `ABP`] |
| `uplink_interval_mins` | float64 | no | `10` for the CT101, `1080` for EM310-TILT | Interval between uplink messages sent from the node, in minutes. Found in the device datasheet, but can be modified. Configured by downlink after initial connection. |
| `fport` | string | no | `55` (`0x55`) | 8-bit hexadecimal **frame port** used to send downlinks to the device. Found in the device datasheet. |

#### OTAA Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `dev_eui` | string | yes | - | The **device EUI (Extended Unique Identifier)**, a unique 64-bit identifier for the LoRaWAN device in hexadecimal format (16 characters). Found on your device or in device packaging. |
| `app_key` | string | yes | `5572404C696E6B4C6F52613230313823` | The 128-bit hexadecimal AES **application key** used for device authentication and session key derivation. Required for OTAA activation protocol. Found in the device datasheet. |

#### ABP attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `dev_addr` | string | yes | - | 32-bit hexadecimal **device address** used to identify this device in LoRaWAN messages. Found in the device datasheet or in device packaging. |
| `app_s_key` | string | yes | `5572404C696E6B4C6F52613230313823` | 128-bit hexadecimal **application session key** used to decrypt messages containing application data.  Found in the device datasheet or in device packaging. |
| `network_s_key` | string | yes | `5572404C696E6B4C6F52613230313823` | 128-bit hexadecimal **network session key** used to decrypt network management messages. Found in the device datasheet or in device packaging. |

#### DoCommand

You can use DoCommand to send downlink requests to nodes connected to your gateway.

##### Update the uplink interval

Update the interval the node sends data at:

```json
{
  "set_interval": <float64>
}
```

Alternatively, set the interval using `uplink_interval_mins` in the node configuration.

##### Restart the device

Restart the node, triggering a new join request:


```json
{
  "restart_sensor": ""
}
```

##### Send a generic downlink

Send a generic downlink payload (in hexadecimal) to the node:

```json
{
  "send_downlink": <string>
}
```

For more information about downlink commands, see the [EM310-TILT user guide](https://resource.milesight.com/milesight/iot/document/em310-tilt-user-guide-en.pdf) or the [CT101 user guide](https://resource.milesight.com/milesight/iot/document/ct10x-user-guide-en.pdf).

### Configuration for `viam:lorawan:node`

#### Examples

Example OTAA node configuration:

```json
{
  "join_type": "OTAA",
  "dev_eui": <string>,
  "app_key": <string>,
  "gateways": [<string>],
}
```

Example ABP node configuration:

```json
{
  "join_type": "ABP",
  "dev_addr": <string>,
  "app_s_key": <string>,
  "network_s_key": <string>,
  "gateways": [<string>],
}
```

#### General Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `gateways` | []string | yes | - | An array containing the name of the [gateway component](#add-a-gateway) in your Viam configuration. Alternatively, specify the gateway using the `Depends on` drop down. |
| `decoder_path` | string | no | (see description) | Path to a Javascript **decoder script** used to interpret data transmitted from the node. You can use a local path on your device or an HTTP(S) URL that points to a file on a remote server. If the decoder script provides multiple implementations, uses the Chirpstack version. Not compatible with The Things Network decoders. Defaults to the latest decoder published by the manufacturer on GitHub. |
| `join_type` | string | no | `OTAA` | The [activation protocol](https://docs.viam.com/data-ai/capture-data/lorawan/#activation-protocols) used to secure this network. Options: [`OTAA`, `ABP`] |
| `uplink_interval_mins` | float64 | no | Defaults: `10` for the CT101, `1080` for EM310-TILT | Interval between uplink messages sent from the node, in minutes. Found in the device datasheet, but can be modified. Configured by downlink after initial connection. |
| `fport` | string | no | - | 8-bit hexadecimal **frame port** used to send downlinks to the device. Found in the device datasheet. |

#### OTAA Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `dev_eui` | string | yes | - | The **device EUI (Extended Unique Identifier)**, a unique 64-bit identifier for the LoRaWAN device in hexadecimal format (16 characters). Found on your device or in device packaging. |
| `app_key` | string | yes | - | The 128-bit hexadecimal AES **application key** used for device authentication and session key derivation. Required for OTAA activation protocol. Found in the device datasheet. |

#### ABP attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `dev_addr` | string | yes | - | 32-bit hexadecimal **device address** used to identify this device in LoRaWAN messages. Found in the device datasheet or in device packaging. |
| `app_s_key` | string | yes | - | 128-bit hexadecimal **application session key** used to decrypt messages containing application data.  Found in the device datasheet or in device packaging. |
| `network_s_key` | string | yes | - | 128-bit hexadecimal **network session key** used to decrypt network management messages. Found in the device datasheet or in device packaging. |


#### DoCommand

You can use DoCommand to send downlink requests to nodes connected to your gateway.

##### Send a downlink

Send a generic downlink payload (in hexadecimal) to the node:

```json
{
  "send_downlink": <string>
}
```

## Configuration for LoRaWAN gateway models

### Configuration for `viam:lorawan:sx1302-waveshare-hat`

#### Example

```json
{
    "board": <string>,
    "spi_bus": <int>,
    "region_code": <string>
}
```

#### Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `board` | string | yes | - | Name of the [board component](https://docs.viam.com/operate/reference/components/board/) that the peripheral is connected to. Used for GPIO pin control. |
| `spi_bus` | int | no | `0` | SPI bus number used to connect the gateway peripheral. Options: [`0`, `1`] |
| `region_code` | string | no | `US915` | Frequency region of your gateway. Options: [`US915`, `EU868`] |

### Configuration for `viam:lorawan:rak7391`

Before configuring the RAK7391, follow [the manual](https://docs.rakwireless.com/product-categories/software-apis-and-libraries/rakpios/quickstart) to install RAKPiOS and connect to the Internet.

You can run the following command to discover where the concentrators are connected:

`docker run --privileged --rm rakwireless/udp-packet-forwarder find_concentrator`

Use the returned SPI or serial paths in your viam configuration.


#### Quick Example

```json
{
  "pcie1": {
    "serial_path": <string>,
    "spi_bus": <int>
  },
  "pcie2": {
    "serial_path": <string>,
    "spi_bus": <int>
  },
  "board": <string>
}
```

#### Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `board` | string | yes | - | Name of the [board component](https://docs.viam.com/operate/reference/components/board/) that represents the Raspberry Pi Compute Module inside the RAK7391. Used for GPIO pin control. |
| `region_code` | string | no | `US915` | Frequency region of your gateway. Options: [`US915`, `EU868`] |
| `pcie1` | config | no | - | PCIe configuration for concentrator connected to PCIe slot 1: <br> <ul><li>`spi_bus` (integer) (Optional): SPI bus that the concentrator is connected to, if connected through SPI. </li><li>`serial_path` (string) (Optional): Serial path that the concentrator is mounted at, if connected through USB. </li></ul> |
| `pcie2` |config | no | - | PCIe configuration for concentrator connected to PCIe slot 2: <br> <ul><li>`spi_bus` (integer) (Optional): SPI bus that the concentrator is connected to, if connected through SPI. </li><li>`serial_path` (string) (Optional): Serial path that the concentrator is mounted at, if connected through USB. </li></ul> |

You must specify at least one PCIe configuration.

### Configuration for `viam:lorawan:sx1302-hat-generic`

This generic model can be used for any peripheral built using the SX1302 or SX1303 chips.

Note: To avoid a 15-minute reset loop, set the GPIO pins to the GPIO pin numbers that connect your board to the reset and power-enable pins of the HAT.

#### Example

```json
{
    "board": <string>,
    "spi_bus": <int>,
    "reset_pin": <int>,
    "power_en_pin": <int>,
    "region_code": <string>
}
```

#### Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `board` | string | yes | - | Name of the [board component](https://docs.viam.com/operate/reference/components/board/) that the peripheral is connected to. Used for GPIO pin control. |
| `reset_pin` | int | yes | - | GPIO pin used for peripheral reset. |
| `spi_bus` | int | no | `0` | SPI bus number used to connect the gateway peripheral. Options: [`0`, `1`]  |
| `power_en_pin` | int | no | - | GPIO pin used for peripheral power enable. |
| `path` | string | no | - | Serial path that the peripheral is mounted at, if connected through USB. |
| `region_code` | string | no | `US915` | Frequency region of your gateway. Options: [`US915`, `EU868`] |

## Troubleshooting

When the gateway is properly configured:

- the `pwr` LED will light up solid red
- the `rx` and `tx` LEDs will blink red

It may take several minutes after starting the module to start receiving data, especially if your node transmits on more than 8 frequency channels.

The gateway communicates through SPI, so [ensure that SPI in enabled on your machine](https://docs.viam.com/operate/reference/prepare/rpi-setup/#enable-communication-protocols).

The gateway will log `info` logs when a device sends a join request or uplink.

To avoid capturing duplicate data, set the data capture frequency longer than or equal to the expected uplink interval.

If you see the error `ERROR: Failed to set SX1250_0 in STANDBY_RC mode` in logs, unplug the machine for a few minutes, then try again.
