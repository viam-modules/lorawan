# LoRaWAN Module

Viam module for receiving data from LoRaWAN sensors.\
`sx1302-gateway`: sensor model for a sx1302 lorawan concentrator hat connected to a raspberry pi.\
`node`: sensor model for the end nodes sending data to the gateway.

Compatible with:
- US915 frequency band
- Class A Devices
- LoraWAN MAC version 1.0.3

## Hardware Requirements

- Raspberry Pi (any model with GPIO pins)
- SX1302 Gateway HAT/concentrator board
- US915 LoRaWAN sensors

## Configuration

### Gateway Configuration (viam:sensor:sx1302-gateway)

configuration attributes:

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| spi_bus | int | no | 0 | SPI bus number (0 or 1) |
| power_en_pin | int | no | - | GPIO pin number for the power enable pin |
| reset_pin | int | yes | - | GPIO pin number for sx1302 reset pin |

Example gateway configuration:
```json
{
  "components": [
    {
      "name": "lora-gateway",
      "model": "viam:sensor:sx1302-gateway",
      "type": "sensor",
      "attributes": {
        "spi_bus": 0,
        "reset_pin": 17,
        "power_en_pin": 27
      }
    }
  ]
}
```

### Node Configuration (viam:sensor:node)

The node model supports any US915 class A V1.0.3 device.
The node component supports two types of activation: OTAA (Over-the-Air Activation) and ABP (Activation by Personalization).

#### Common Attributes

| Name | Type | Required | Description |
|------|------|----------|-------------|
| decoder_path | string | yes | Path to the payload decoder script. This must be a .js file. If the device provides multiple decoder files, use the chirpstack version. |
| join_type | string | no | Join type ("OTAA" or "ABP"). Defaults to "OTAA" |
| uplink_interval_mins | float64 | yes | Expected interval between uplink messages sent by the node. The default can be found on the datasheet and can be modified using device specific software.

#### OTAA Attributes

| Name | Type | Required | Description |
|------|------|----------|-------------|
| dev_eui | string | yes | Device EUI (8 bytes in hex). Unique indentifer for the node. Can be found printed on your device or on the box.|
| app_key | string | yes | Application Key (16 bytes in hex). Used to securely join the network. The default can normally be found in the node's datasheet. |

#### ABP Attributes

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

### Notes
It may take several minutes after starting the module to start receiving data.
