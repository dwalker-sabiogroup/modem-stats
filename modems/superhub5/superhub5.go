package superhub5

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/msh100/modem-stats/utils"
)

type Modem struct {
	IPAddress string
	Stats     []byte
	FetchTime int64
}

func (sh5 *Modem) ClearStats() {
	sh5.Stats = nil
}

func (sh5 *Modem) Type() string {
	return utils.TypeDocsis
}

func (sh5 *Modem) apiAddress() string {
	if sh5.IPAddress == "" {
		sh5.IPAddress = "192.168.100.1" // TODO: Is this a reasonable default?
	}
	return fmt.Sprintf("https://%s/rest/v1/cablemodem", sh5.IPAddress)
}

type dsChannel struct {
	ID          int     `json:"channelId"`
	Frequency   int     `json:"frequency"`
	Power       float32 `json:"power"`
	Modulation  string  `json:"modulation"`
	SNR         int     `json:"snr"`
	PreRS       int     `json:"correctedErrors"`
	PostRS      int     `json:"uncorrectedErrors"`
	ChannelType string  `json:"channelType"`
	RxMer       int     `json:"rxMer"`
	LockStatus  bool    `json:"lockStatus"`
}

type usChannel struct {
	ID          int     `json:"channelId"`
	Frequency   int     `json:"frequency"`
	Power       float32 `json:"power"`
	Modulation  string  `json:"modulation"`
	ChannelType string  `json:"channelType"`
	LockStatus  bool    `json:"lockStatus"`
	SymbolRate  int     `json:"symbolRate"`
	T1Timeout   int     `json:"t1Timeout"`
	T2Timeout   int     `json:"t2Timeout"`
	T3Timeout   int     `json:"t3Timeout"`
	T4Timeout   int     `json:"t4Timeout"`
}

type serviceFlow struct {
	ServiceFlow struct {
		ID        int    `json:"serviceFlowId"`
		Direction string `json:"direction"`
		MaxRate   int    `json:"maxTrafficRate"`
		MaxBurst  int    `json:"maxTrafficBurst"`
	} `json:"serviceFlow"`
}

type eventLogEntry struct {
	Priority string `json:"priority"`
	Time     string `json:"time"`
	Message  string `json:"message"`
}

type eventLogResponse struct {
	EventLog []eventLogEntry `json:"eventlog"`
}

type resultsStruct struct {
	Downstream struct {
		Channels []dsChannel `json:"channels"`
	} `json:"downstream"`
	Upstream struct {
		Channels []usChannel `json:"channels"`
	} `json:"upstream"`
	ServiceFlows []serviceFlow `json:"serviceFlows"`
}

var modulationRegex = regexp.MustCompile("[0-9]+")

func (sh5 *Modem) ParseStats() (utils.ModemStats, error) {
	if sh5.Stats == nil {
		sh5.Stats = []byte("{}")
		queries := []string{
			sh5.apiAddress() + "/downstream",
			sh5.apiAddress() + "/upstream",
			sh5.apiAddress() + "/serviceflows",
		}

		timeStart := time.Now().UnixMilli()
		statsData := utils.BoundedParallelGet(queries, 3)
		sh5.FetchTime = time.Now().UnixMilli() - timeStart

		for _, query := range statsData {
			if query.Err != nil {
				return utils.ModemStats{}, query.Err
			}
			stats, err := io.ReadAll(query.Res.Body)
			query.Res.Body.Close()
			if err != nil {
				return utils.ModemStats{}, err
			}

			sh5.Stats, err = jsonpatch.MergeMergePatches(sh5.Stats, stats)
			if err != nil {
				return utils.ModemStats{}, err
			}
		}
	}

	var upChannels []utils.ModemChannel
	var downChannels []utils.ModemChannel
	var modemConfigs []utils.ModemConfig

	var results resultsStruct
	if err := json.Unmarshal(sh5.Stats, &results); err != nil {
		return utils.ModemStats{}, fmt.Errorf("failed to parse stats JSON: %w", err)
	}

	for index, downstream := range results.Downstream.Channels {
		qamSize := modulationRegex.FindString(downstream.Modulation)

		powerInt := int(downstream.Power * 10)
		snr := downstream.SNR * 10

		var scheme string
		if downstream.ChannelType == "sc_qam" {
			scheme = "SC-QAM"
		} else if downstream.ChannelType == "ofdm" {
			scheme = "OFDM"
			powerInt = int(downstream.Power)
			snr = downstream.RxMer
		} else {
			fmt.Println("Unknown channel scheme:", downstream.ChannelType)
			continue
		}

		downChannels = append(downChannels, utils.ModemChannel{
			ChannelID:  downstream.ID,
			Channel:    index + 1,
			Frequency:  downstream.Frequency,
			Snr:        snr,
			Power:      powerInt,
			Prerserr:   downstream.PreRS + downstream.PostRS,
			Postrserr:  downstream.PostRS,
			Modulation: "QAM" + qamSize,
			Scheme:     scheme,
			Locked:     downstream.LockStatus,
		})
	}

	for index, upstream := range results.Upstream.Channels {
		powerInt := int(upstream.Power * 10)

		var scheme string
		if upstream.ChannelType == "atdma" {
			scheme = "ATDMA"
		} else if upstream.ChannelType == "ofdma" {
			scheme = "OFDMA"
			powerInt = int(upstream.Power)
		} else {
			fmt.Println("Unknown channel scheme:", upstream.ChannelType)
			continue
		}

		upChannels = append(upChannels, utils.ModemChannel{
			ChannelID:  upstream.ID,
			Channel:    index + 1,
			Frequency:  upstream.Frequency,
			Power:      powerInt,
			Scheme:     scheme,
			Locked:     upstream.LockStatus,
			SymbolRate: upstream.SymbolRate,
			T1Timeout:  upstream.T1Timeout,
			T2Timeout:  upstream.T2Timeout,
			T3Timeout:  upstream.T3Timeout,
			T4Timeout:  upstream.T4Timeout,
		})
	}

	for _, modemConfig := range results.ServiceFlows {
		modemConfigs = append(modemConfigs, utils.ModemConfig{
			Config:        modemConfig.ServiceFlow.Direction,
			Maxrate:       modemConfig.ServiceFlow.MaxRate,
			Maxburst:      modemConfig.ServiceFlow.MaxBurst,
			ServiceFlowId: modemConfig.ServiceFlow.ID,
		})
	}

	return utils.ModemStats{
		Configs:      modemConfigs,
		UpChannels:   upChannels,
		DownChannels: downChannels,
		FetchTime:    sh5.FetchTime,
	}, nil
}

// FetchEventLog retrieves the event log from the modem
func (sh5 *Modem) FetchEventLog() ([]utils.EventLogEntry, error) {
	url := sh5.apiAddress() + "/eventlog"

	res, err := utils.InsecureHTTPClient().Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch eventlog: %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read eventlog response: %w", err)
	}

	var response eventLogResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse eventlog JSON: %w", err)
	}

	entries := make([]utils.EventLogEntry, len(response.EventLog))
	for i, e := range response.EventLog {
		entries[i] = utils.EventLogEntry{
			Priority:  e.Priority,
			Timestamp: e.Time,
			Message:   e.Message,
		}
	}

	return entries, nil
}
