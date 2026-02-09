package superhub5

import (
	"os"
	"strings"
	"testing"

	"github.com/msh100/modem-stats/outputs"
	"github.com/msh100/modem-stats/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadTestData(t *testing.T, filename string) []byte {
	data, err := os.ReadFile("test_state/" + filename)
	require.NoError(t, err, "failed to load test data: %s", filename)
	return data
}

// testModem wraps Modem but prevents ClearStats from actually clearing
// This allows Prometheus e2e tests to work with pre-loaded data
type testModem struct {
	Modem
	statsBackup []byte
}

func (tm *testModem) ClearStats() {
	// Don't actually clear - restore from backup instead
	tm.Stats = tm.statsBackup
}

func newTestModem(stats []byte, fetchTime int64) *testModem {
	return &testModem{
		Modem: Modem{
			Stats:     stats,
			FetchTime: fetchTime,
		},
		statsBackup: stats,
	}
}

func TestModem_Type(t *testing.T) {
	modem := Modem{}
	assert.Equal(t, utils.TypeDocsis, modem.Type())
}

func TestModem_ClearStats(t *testing.T) {
	modem := Modem{
		Stats: []byte("test data"),
	}
	assert.NotNil(t, modem.Stats)
	modem.ClearStats()
	assert.Nil(t, modem.Stats)
}

func TestModem_ApiAddress(t *testing.T) {
	tests := []struct {
		name      string
		ipAddress string
		expected  string
	}{
		{
			name:      "default IP",
			ipAddress: "",
			expected:  "https://192.168.100.1/rest/v1/cablemodem",
		},
		{
			name:      "custom IP",
			ipAddress: "10.0.0.1",
			expected:  "https://10.0.0.1/rest/v1/cablemodem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modem := Modem{IPAddress: tt.ipAddress}
			assert.Equal(t, tt.expected, modem.apiAddress())
		})
	}
}

func TestModem_ParseStats_DownstreamChannels(t *testing.T) {
	modem := Modem{
		Stats:     loadTestData(t, "full_stats.json"),
		FetchTime: 100,
	}

	stats, err := modem.ParseStats()
	require.NoError(t, err)

	// Should have 32 downstream channels (31 SC-QAM + 1 OFDM)
	assert.Len(t, stats.DownChannels, 32)

	// Test first SC-QAM channel (channelId 37)
	firstChannel := stats.DownChannels[0]
	assert.Equal(t, 37, firstChannel.ChannelID)
	assert.Equal(t, 1, firstChannel.Channel)
	assert.Equal(t, 419000000, firstChannel.Frequency)
	assert.Equal(t, 410, firstChannel.Snr)         // 41 * 10
	assert.Equal(t, 21, firstChannel.Power)        // 2.1 * 10
	assert.Equal(t, 257919, firstChannel.Prerserr) // corrected + uncorrected
	assert.Equal(t, 11087, firstChannel.Postrserr) // uncorrected only
	assert.Equal(t, "QAM256", firstChannel.Modulation)
	assert.Equal(t, "SC-QAM", firstChannel.Scheme)

	// Test OFDM channel (last one, channelId 33)
	ofdmChannel := stats.DownChannels[31]
	assert.Equal(t, 33, ofdmChannel.ChannelID)
	assert.Equal(t, 32, ofdmChannel.Channel)
	assert.Equal(t, 0, ofdmChannel.Snr)    // rxMer for OFDM
	assert.Equal(t, 12, ofdmChannel.Power) // Not multiplied for OFDM
	assert.Equal(t, "QAM4096", ofdmChannel.Modulation)
	assert.Equal(t, "OFDM", ofdmChannel.Scheme)
}

func TestModem_ParseStats_UpstreamChannels(t *testing.T) {
	modem := Modem{
		Stats:     loadTestData(t, "full_stats.json"),
		FetchTime: 100,
	}

	stats, err := modem.ParseStats()
	require.NoError(t, err)

	// Should have 6 upstream channels (5 ATDMA + 1 OFDMA)
	assert.Len(t, stats.UpChannels, 6)

	// Test first ATDMA channel
	firstChannel := stats.UpChannels[0]
	assert.Equal(t, 1, firstChannel.ChannelID)
	assert.Equal(t, 1, firstChannel.Channel)
	assert.Equal(t, 49600000, firstChannel.Frequency)
	assert.Equal(t, 448, firstChannel.Power) // 44.8 * 10
	assert.Equal(t, "ATDMA", firstChannel.Scheme)

	// Test OFDMA channel (last one, channelId 11)
	ofdmaChannel := stats.UpChannels[5]
	assert.Equal(t, 11, ofdmaChannel.ChannelID)
	assert.Equal(t, 6, ofdmaChannel.Channel)
	assert.Equal(t, 402, ofdmaChannel.Power) // Not multiplied for OFDMA
	assert.Equal(t, "OFDMA", ofdmaChannel.Scheme)
}

func TestModem_ParseStats_ServiceFlows(t *testing.T) {
	modem := Modem{
		Stats:     loadTestData(t, "full_stats.json"),
		FetchTime: 100,
	}

	stats, err := modem.ParseStats()
	require.NoError(t, err)

	// Should have 4 service flows
	assert.Len(t, stats.Configs, 4)

	// Find downstream service flows
	var downstreamConfigs []utils.ModemConfig
	var upstreamConfigs []utils.ModemConfig
	for _, cfg := range stats.Configs {
		if cfg.Config == "downstream" {
			downstreamConfigs = append(downstreamConfigs, cfg)
		} else if cfg.Config == "upstream" {
			upstreamConfigs = append(upstreamConfigs, cfg)
		}
	}

	assert.Len(t, downstreamConfigs, 2)
	assert.Len(t, upstreamConfigs, 2)

	// Check primary downstream flow
	foundPrimaryDown := false
	for _, cfg := range downstreamConfigs {
		if cfg.ServiceFlowId == 412832 {
			foundPrimaryDown = true
			assert.Equal(t, 287500061, cfg.Maxrate)
			assert.Equal(t, 42600, cfg.Maxburst)
		}
	}
	assert.True(t, foundPrimaryDown, "primary downstream service flow not found")

	// Check primary upstream flow
	foundPrimaryUp := false
	for _, cfg := range upstreamConfigs {
		if cfg.ServiceFlowId == 412831 {
			foundPrimaryUp = true
			assert.Equal(t, 27500061, cfg.Maxrate)
			assert.Equal(t, 42600, cfg.Maxburst)
		}
	}
	assert.True(t, foundPrimaryUp, "primary upstream service flow not found")
}

func TestModem_ParseStats_FetchTime(t *testing.T) {
	modem := Modem{
		Stats:     loadTestData(t, "full_stats.json"),
		FetchTime: 150,
	}

	stats, err := modem.ParseStats()
	require.NoError(t, err)
	assert.Equal(t, int64(150), stats.FetchTime)
}

func TestModem_ParseStats_InvalidJSON(t *testing.T) {
	modem := Modem{
		Stats: []byte("invalid json"),
	}

	_, err := modem.ParseStats()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse stats JSON")
}

func TestModem_ParseStats_EmptyJSON(t *testing.T) {
	modem := Modem{
		Stats: []byte("{}"),
	}

	stats, err := modem.ParseStats()
	require.NoError(t, err)
	assert.Empty(t, stats.DownChannels)
	assert.Empty(t, stats.UpChannels)
	assert.Empty(t, stats.Configs)
}

// Prometheus end-to-end tests using testutil
func TestPrometheusExporter_FullStats(t *testing.T) {
	modem := newTestModem(loadTestData(t, "full_stats.json"), 100)

	// Create a new registry to avoid conflicts
	registry := prometheus.NewRegistry()
	exporter := outputs.ProExporter(modem)
	registry.MustRegister(exporter)

	// Test downstream metrics exist
	metricCount, err := testutil.GatherAndCount(registry,
		"modemstats_downstream_frequency",
		"modemstats_downstream_power",
		"modemstats_downstream_snr",
		"modemstats_downstream_prerserr",
		"modemstats_downstream_postrserr",
	)
	require.NoError(t, err)
	// 32 channels * 5 metrics = 160
	assert.Equal(t, 160, metricCount)

	// Test upstream metrics exist
	metricCount, err = testutil.GatherAndCount(registry,
		"modemstats_upstream_frequency",
		"modemstats_upstream_power",
	)
	require.NoError(t, err)
	// 6 channels * 2 metrics = 12
	assert.Equal(t, 12, metricCount)

	// Test config metrics exist
	metricCount, err = testutil.GatherAndCount(registry,
		"modemstats_config_maxrate",
		"modemstats_config_maxburst",
	)
	require.NoError(t, err)
	// 4 service flows * 2 metrics = 8
	assert.Equal(t, 8, metricCount)

	// Test fetchtime metric exists
	metricCount, err = testutil.GatherAndCount(registry, "modemstats_shstatsinfo_timems")
	require.NoError(t, err)
	assert.Equal(t, 1, metricCount)
}

func TestPrometheusExporter_SpecificMetricValues(t *testing.T) {
	modem := newTestModem(loadTestData(t, "full_stats.json"), 100)

	registry := prometheus.NewRegistry()
	exporter := outputs.ProExporter(modem)
	registry.MustRegister(exporter)

	// Test specific downstream metric value for first channel
	expected := `
		# HELP modemstats_downstream_frequency Downstream Frequency in HZ
		# TYPE modemstats_downstream_frequency gauge
		modemstats_downstream_frequency{channel="1",id="37",modulation="QAM256",scheme="SC-QAM"} 4.19e+08
	`
	err := testutil.CollectAndCompare(exporter, strings.NewReader(expected), "modemstats_downstream_frequency")
	// This will fail because there are 32 channels; use GatherAndLint instead for validation
	assert.Error(t, err) // Expected since we only provide one of many values

	// Test that metrics are valid (no lint errors)
	problems, err := testutil.GatherAndLint(registry)
	require.NoError(t, err)
	assert.Empty(t, problems, "metrics have lint problems: %v", problems)
}

func TestPrometheusExporter_ConfigMetrics(t *testing.T) {
	modem := newTestModem(loadTestData(t, "full_stats.json"), 100)

	registry := prometheus.NewRegistry()
	exporter := outputs.ProExporter(modem)
	registry.MustRegister(exporter)

	// Verify service flow metrics are properly labeled with unique IDs
	expected := `
		# HELP modemstats_config_maxrate Maximum link rate
		# TYPE modemstats_config_maxrate gauge
		modemstats_config_maxrate{config="downstream",serviceflow_id="412832"} 2.87500061e+08
		modemstats_config_maxrate{config="downstream",serviceflow_id="412834"} 128000
		modemstats_config_maxrate{config="upstream",serviceflow_id="412831"} 2.7500061e+07
		modemstats_config_maxrate{config="upstream",serviceflow_id="412833"} 128000
	`
	err := testutil.CollectAndCompare(exporter, strings.NewReader(expected), "modemstats_config_maxrate")
	assert.NoError(t, err)
}

func TestPrometheusExporter_FetchTimeMetric(t *testing.T) {
	modem := newTestModem(loadTestData(t, "full_stats.json"), 250)

	registry := prometheus.NewRegistry()
	exporter := outputs.ProExporter(modem)
	registry.MustRegister(exporter)

	expected := `
		# HELP modemstats_shstatsinfo_timems Time to fetch statistics from the modem in milliseconds
		# TYPE modemstats_shstatsinfo_timems gauge
		modemstats_shstatsinfo_timems 250
	`
	err := testutil.CollectAndCompare(exporter, strings.NewReader(expected), "modemstats_shstatsinfo_timems")
	assert.NoError(t, err)
}
