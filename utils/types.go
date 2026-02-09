package utils

type ModemChannel struct {
	ChannelID  int
	Channel    int
	Frequency  int
	Snr        int
	Power      int
	Prerserr   int
	Postrserr  int
	Modulation string
	Scheme     string

	Noise       int
	Attenuation int

	// DOCSIS timeout counters (upstream only)
	T1Timeout int
	T2Timeout int
	T3Timeout int
	T4Timeout int

	// Additional channel info
	Locked     bool
	SymbolRate int
}

type ModemConfig struct {
	Config        string
	Maxrate       int
	Maxburst      int
	ServiceFlowId int
}

type ModemStats struct {
	Configs      []ModemConfig
	UpChannels   []ModemChannel
	DownChannels []ModemChannel
	FetchTime    int64
	ModemType    string
}

type DocsisModem interface {
	ParseStats() (ModemStats, error)
	ClearStats()
	Type() string
}

const (
	TypeDocsis = "DOCSIS"
	TypeVDSL   = "VDSL"
)
