# [`lorawan module`](<https://github.com/oliviamiller/lorawan-gateway>)

LoRaWAN (Long Range Wide Area Network) is a low-power, long-range wireless protocol, enabling efficient communication over large distances with minimal energy usage.
For more on why this is useful, see the [article on the Viam blog](https://www.viam.com/post/launch-lorawan-support-viam).

This module combines the functionality of a LoRaWAN gateway and network server, enabling communication between LoRaWAN sensors and the Viam app.
It handles packet forwarding, device management, and message routing to allow LoRaWAN sensor data to be viewed and managed directly in Viam.
This module provides two models:

- `sx1302-gateway`: Sensor model for a SX1302 LoRaWAN concentrator hat connected to a Raspberry Pi.
- `node`: Sensor model for the end nodes sending data to the gateway.

You'll configure the `sx1302-gateway` model, and one or more `node`s depending on how many sensors you have.

Compatible with:
- US915 frequency band
- Class A Devices
- LoraWAN MAC version 1.0.3

## Requirements

Hardware Required:
- Raspberry Pi (any model with GPIO pins)
- SX1302 Gateway HAT/concentrator board
- US915 LoRaWAN sensors

See [Hardware Tested Section](https://github.com/viam-modules/lorawan?tab=readme-ov-file#hardware-tested) for hardware we have used this module with successfully.

## Configure the `viam:sensor:sx1302-gateway`

Navigate to the **CONFIGURE** tab of your machine in the [Viam app](https://app.viam.com/) and click the **+** button.
[Add sx1302-gateway to your machine](https://docs.viam.com/operate/get-started/supported-hardware/#configure-hardware-on-your-machine).

### Attributes

The following attributes are available for `viam:sensor:sx1302-gateway` sensors:

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| board | string | yes | - | Name of the board connected to the HAT. The board communicates with the gateway through SPI |
| reset_pin | int | yes | - | GPIO pin number for sx1302 reset pin |
| spi_bus | int | no | 0 | SPI bus number (0 or 1) |
| power_en_pin | int | no | - | GPIO pin number for the power enable pin |

Example gateway configuration:
```json
{
  "components": [
    {
      "name": "lora-gateway",
      "model": "viam:sensor:sx1302-gateway",
      "type": "sensor",
      "attributes": {
        "board": "rpi",
        "spi_bus": 0,
        "reset_pin": 17,
        "power_en_pin": 27
      }
    }
  ]
}
```

## Configure the `viam:sensor:node`

As when configuring the gateway, use the **+** button on your machine's **CONFIGURE** tab to add the `viam:sensor:node` model to your machine.

The node model supports any US915 class A V1.0.3 device.
The node component supports two types of activation: OTAA (Over-the-Air Activation) and ABP (Activation by Personalization).

### Common Attributes

| Name | Type | Required | Description |
|------|------|----------|-------------|
| decoder_path | string | yes | Path to the payload decoder script. This must be a .js file. If the device provides multiple decoder files, use the chirpstack version. |
| join_type | string | no | Join type ("OTAA" or "ABP"). Defaults to "OTAA" |
| uplink_interval_mins | float64 | yes | Expected interval between uplink messages sent by the node. The default can be found on the datasheet and can be modified using device specific software.

The gateway component must be added as a dependency in the depends on drop down.
The node registers itself with the gateway so the gateway will recognize messages from the node.

### OTAA Attributes

| Name | Type | Required | Description |
|------|------|----------|-------------|
| dev_eui | string | yes | Device EUI (8 bytes in hex). Unique indentifer for the node. Can be found printed on your device or on the box.|
| app_key | string | yes | Application Key (16 bytes in hex). Used to securely join the network. The default can normally be found in the node's datasheet. |

### ABP Attributes

| Name | Type | Required | Description |
|------|------|----------|-------------|
| dev_addr | string | yes | Device Address (4 bytes in hex). Used to identify uplink messages. Can normally be found on datasheet or box. |
| app_s_key | string | yes | Application Session Key (16 bytes in hex) Used to decrypt uplink messages. Default can normally be found on the node's datasheet or box. |
| network_s_key | string | yes | Network Session Key (16 bytes in hex) Used to decypt uplink messages. Default can normally be found on the node's datasheet or box. |

Example OTAA node configuration:
```json
{
  "components": [
    {
      "name": "temperature-sensor",
      "model": "viam:sensor:node",
      "type": "sensor",
      "depends_on": ["lora-gateway"],
      "attributes": {
        "join_type": "OTAA",
        "decoder_path": "/path/to/decoder.js",
        "dev_eui": "0123456789ABCDEF",
        "app_key": "0123456789ABCDEF0123456789ABCDEF"
      }
    }
  ]
}
```

Example ABP node configuration:
```json
{
  "components": [
    {
      "name": "humidity-sensor",
      "model": "viam:sensor:node",
      "type": "sensor",
      "depends_on": ["lora-gateway"],
      "attributes": {
        "join_type": "ABP",
        "decoder_path": "/path/to/decoder.js",
        "dev_addr": "01234567",
        "app_s_key": "0123456789ABCDEF0123456789ABCDEF",
        "network_s_key": "0123456789ABCDEF0123456789ABCDEF"
      }
    }
  ]
}
```

## Troubleshooting Notes
When the gateway is properly configured, the pwr LED will be solid red and the rx and tx LEDs will be blinking red.

It may take several minutes after starting the module to start receiving data, especially if your node transmits on more than 8 frequency channels.
The gateway will log info logs when it has received a join request or data uplink.

The gateway communicates through SPI, so [ensure that SPI in enabled on the Pi](https://docs.viam.com/operate/reference/prepare/rpi-setup/#enable-communication-protocols).

To avoid capturing duplicate data, set the data capture frequency equal to or less than the expected uplink interval.

If you see the error `ERROR: Failed to set SX1250_0 in STANDBY_RC mode` in logs, unplug the Raspberry Pi for a few minutes and then try again.

## Hardware Tested
The sx1302-gateway model has been tested with:\
[Waveshare Gateway HAT](https://www.waveshare.com/wiki/SX1302_LoRaWAN_Gateway_HAT)

The node model has been tested with:\
[Milesight CT101 Smart Current Transformer](https://www.milesight.com/iot/product/lorawan-sensor/ct10x)\
[Milesight EM310 Tilt Sensor](https://www.milesight.com/iot/product/lorawan-sensor/em310-tilt)\
[Dragino LHT65 Temperature & Humidity Sensor](https://www.dragino.com/products/temperature-humidity-sensor/item/151-lht65.html)

