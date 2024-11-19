# LoRaWAN Gateway Module

This module provides a complete implementation for handling LoRaWAN communication between the gateway and end nodes.
Provides a sensor model for a sx1302 lorawan concentrator hat connected to a raspberry pi.
Provides a sensor model for the end nodes communicating with the gateway.

## Overview

This module implements a LoRaWAN system compatible with:
- US915 frequency band
- Class A Devices
- LoraWAN MAC version 1.0.3
- Supports device activation (OTAA and ABP)

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

The node component supports two types of activation: OTAA (Over-the-Air Activation) and ABP (Activation by Personalization).

#### Common Attributes

| Name | Type | Required | Description |
|------|------|----------|-------------|
| decoder_path | string | yes | Path to the payload decoder script |
| join_type | string | no | Join type ("OTAA" or "ABP"). Defaults to "OTAA" |
| uplink_interval_mins | flo

#### OTAA Attributes

| Name | Type | Required | Description |
|------|------|----------|-------------|
| dev_eui | string | yes | Device EUI (8 bytes in hex). Unique indentifer for the node. Found on your physical device or the box.|
| app_key | string | yes | Application Key (16 bytes in hex). Used to securely join the network. Default can normally be found on the node's datasheet. |

#### ABP Attributes

| Name | Type | Required | Description |
|------|------|----------|-------------|
| dev_addr | string | yes | Device Address (4 bytes in hex). Used to identify uplink messages. Default can normally be found on the node's datasheet or box. |
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

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## Support

For questions and support:
- Open an issue on GitHub
- Contact the maintainers

## Acknowledgments

- Semtech for the SX1302 reference implementation
- The Things Network for the LoRaWAN stack
- VIAM Robotics for the RDK framework
