package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRAKConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      ConfigRak
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid single SPI concentrator",
			config: ConfigRak{
				Concentrator1: ConcentratorConfig{
					ConnectionType: SPIConnection,
					Bus:           0,
					Enabled:       true,
				},
				BoardName: "test-board",
				Region:   "US915",
			},
			expectError: false,
		},
		{
			name: "valid dual concentrator (SPI + USB)",
			config: ConfigRak{
				Concentrator1: ConcentratorConfig{
					ConnectionType: SPIConnection,
					Bus:           0,
					Enabled:       true,
				},
				Concentrator2: ConcentratorConfig{
					ConnectionType: USBConnection,
					Path:          "/dev/ttyUSB0",
					Enabled:       true,
				},
				BoardName: "test-board",
				Region:   "EU868",
			},
			expectError: false,
		},
		{
			name: "no concentrators enabled",
			config: ConfigRak{
				Concentrator1: ConcentratorConfig{
					Enabled: false,
				},
				Concentrator2: ConcentratorConfig{
					Enabled: false,
				},
				BoardName: "test-board",
			},
			expectError: true,
			errorMsg:    "at least one concentrator must be enabled",
		},
		{
			name: "invalid connection type",
			config: ConfigRak{
				Concentrator1: ConcentratorConfig{
					ConnectionType: "invalid",
					Enabled:       true,
				},
				BoardName: "test-board",
			},
			expectError: true,
			errorMsg:    "connection_type must be either 'spi' or 'usb'",
		},
		{
			name: "invalid SPI bus",
			config: ConfigRak{
				Concentrator1: ConcentratorConfig{
					ConnectionType: SPIConnection,
					Bus:           2,
					Enabled:       true,
				},
				BoardName: "test-board",
			},
			expectError: true,
			errorMsg:    "spi bus can be 0 or 1",
		},
		{
			name: "missing USB path",
			config: ConfigRak{
				Concentrator1: ConcentratorConfig{
					ConnectionType: USBConnection,
					Enabled:       true,
				},
				BoardName: "test-board",
			},
			expectError: true,
			errorMsg:    "path",
		},
		{
			name: "missing board name",
			config: ConfigRak{
				Concentrator1: ConcentratorConfig{
					ConnectionType: SPIConnection,
					Bus:           0,
					Enabled:       true,
				},
			},
			expectError: true,
			errorMsg:    "board is required",
		},
		{
			name: "invalid region",
			config: ConfigRak{
				Concentrator1: ConcentratorConfig{
					ConnectionType: SPIConnection,
					Bus:           0,
					Enabled:       true,
				},
				BoardName: "test-board",
				Region:   "INVALID",
			},
			expectError: true,
			errorMsg:    "unrecognized region code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, err := tt.config.Validate("")
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				assert.Contains(t, deps, tt.config.BoardName)
			}
		})
	}
}

func TestRAKGatewayConfig(t *testing.T) {
	tests := []struct {
		name           string
		config         ConfigRak
		expectedPin    int
		expectedBus    int
		expectedRegion string
	}{
		{
			name: "single SPI concentrator",
			config: ConfigRak{
				Concentrator1: ConcentratorConfig{
					ConnectionType: SPIConnection,
					Bus:           1,
					Enabled:       true,
				},
				BoardName: "test-board",
				Region:   "US915",
			},
			expectedPin:    rak7391ResetPin0,
			expectedBus:    1,
			expectedRegion: "US915",
		},
		{
			name: "single USB concentrator",
			config: ConfigRak{
				Concentrator1: ConcentratorConfig{
					ConnectionType: USBConnection,
					Path:          "/dev/ttyUSB0",
					Enabled:       true,
				},
				BoardName: "test-board",
				Region:   "EU868",
			},
			expectedPin:    rak7391ResetPin0,
			expectedBus:    0,
			expectedRegion: "EU868",
		},
		{
			name: "dual concentrator - both enabled",
			config: ConfigRak{
				Concentrator1: ConcentratorConfig{
					ConnectionType: SPIConnection,
					Bus:           0,
					Enabled:       true,
				},
				Concentrator2: ConcentratorConfig{
					ConnectionType: USBConnection,
					Path:          "/dev/ttyUSB0",
					Enabled:       true,
				},
				BoardName: "test-board",
				Region:   "US915",
			},
			expectedPin:    rak7391ResetPin0,
			expectedBus:    0,
			expectedRegion: "US915",
		},
		{
			name: "only second concentrator enabled",
			config: ConfigRak{
				Concentrator1: ConcentratorConfig{
					Enabled: false,
				},
				Concentrator2: ConcentratorConfig{
					ConnectionType: SPIConnection,
					Bus:           1,
					Enabled:       true,
				},
				BoardName: "test-board",
				Region:   "EU868",
			},
			expectedPin:    rak7391ResetPin1,
			expectedBus:    1,
			expectedRegion: "EU868",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gwConfig := tt.config.getGatewayConfig()
			require.NotNil(t, gwConfig)
			assert.Equal(t, tt.expectedPin, *gwConfig.ResetPin)
			assert.Equal(t, tt.expectedBus, gwConfig.Bus)
			assert.Equal(t, tt.expectedRegion, gwConfig.Region)
			assert.Equal(t, tt.config.BoardName, gwConfig.BoardName)
		})
	}
}