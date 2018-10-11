package cfg

import (
	"io/ioutil"
	"time"
	"path"
	"path/filepath"
	"github.com/BurntSushi/toml"
)

const (
	// FuzzModeDrop is a mode in which we randomly drop reads/writes, connections or sleep
	FuzzModeDrop = iota
	// FuzzModeDelay is a mode in which we randomly sleep
	FuzzModeDelay
)

const _DEFAULT_DATA_DIR = "data"

type Config struct {
	BaseConfig

	P2p *P2pConfig
	Rpc *RpcConfig
	TrxPool *TrxPoolConfig
}

type BaseConfig struct {
        chainId string

        RootDir string

        GenesisFile string

        NodeKeyFile string

        DbPath string

	LogLevel string

        FastSync bool
}

type P2pConfig struct {
        ListenAddr string
        ExternalAddr string
	Upnp bool
	SeedMode bool

	HandshakeTimeout time.Duration
	DialTimeout time.Duration

	AddrBook string
	AddrBookStrict bool

	MaxNumPeers int
	MinNumOutgoing int

	AllowDuplicateIp bool

	FlushThrottleTimeout int
	MaxPacketPayloadSize int

	SendRate int64
	RecvRate int64

	Seeds []string
	PersistentPeers []string
	PrivatePeerIds []string

	PexReactor bool

	TestDialFail bool
	TestFuzz bool
	TestFuzzConfig *FuzzConnConfig
}

type RpcConfig struct {
	ListenAddrs []string
}

type TrxPoolConfig struct {
	RootDir      string
	Recheck      bool
	RecheckEmpty bool
	Broadcast    bool
	Size         int
	CacheSize    int
}

// FuzzConnConfig is a FuzzedConnection configuration.
type FuzzConnConfig struct {
	Mode         int
	MaxDelay     time.Duration
	ProbDropRW   float64
	ProbDropConn float64
	ProbSleep    float64
}

// DefaultFuzzConnConfig returns the default config.
func DefaultFuzzConnConfig() *FuzzConnConfig {
	return &FuzzConnConfig{
		Mode:         FuzzModeDrop,
		MaxDelay:     3 * time.Second,
		ProbDropRW:   0.2,
		ProbDropConn: 0.00,
		ProbSleep:    0.00,
	}
}

func DefaultP2pConfig() *P2pConfig {
	config := &P2pConfig{
		ListenAddr:           "tcp://0.0.0.0:26656",
		ExternalAddr:         "",
		Upnp:                 false,
		AddrBook:             "addrbook.json",
		AddrBookStrict:       true,
		MaxNumPeers:          50,
		FlushThrottleTimeout: 100,
		MaxPacketPayloadSize: 1024,
		SendRate:             1024*1024*5,
		RecvRate:             1024*1024*5,
		PexReactor:           true,
		SeedMode:             false,
		AllowDuplicateIp:     true,
		HandshakeTimeout:     20 * time.Second,
		DialTimeout:          3 * time.Second,
		TestDialFail:         false,
		TestFuzz:             false,
		TestFuzzConfig:       DefaultFuzzConnConfig(),
	}
	return config
}

func DefaultRpcConfig() *RpcConfig {
	config := &RpcConfig {
		ListenAddrs: []string{"tcp://0.0.0.0:8765", "tcp://0.0.0.0:5678"},
	}
	return config
}

func DefaultTrxPoolConfig() *TrxPoolConfig {
	config := &TrxPoolConfig{
		Recheck:      true,
		RecheckEmpty: true,
		Broadcast:    true,
		Size:         100000,
		CacheSize:    100000,
	}
	return config
}

func defaultConfig() *Config {
	config := &Config{}
	config.P2p = DefaultP2pConfig()
	config.Rpc = DefaultRpcConfig()
	config.TrxPool = DefaultTrxPoolConfig()
	return config
}

func adjustPath(dir string, path *string) bool {
	if len(*path) == 0 {
		return false
	}

	if filepath.IsAbs(*path) {
		return false
	}

	*path = filepath.Join(dir, *path)
	return true
}

func LoadConfig(pathname string) (*Config, error) {
	bz, err := ioutil.ReadFile(pathname)
	if err != nil {
		return nil, err
	}

	config := Config{}
	_, err = toml.Decode(string(bz), &config)
	if err != nil {
		return nil, err
	}

	configDir := path.Dir(pathname)
	if configDir != "." {
		adjustPath(configDir, &config.GenesisFile)
		adjustPath(configDir, &config.NodeKeyFile)
		adjustPath(configDir, &config.DbPath)
		adjustPath(configDir, &config.P2p.AddrBook)
	}
	return &config, nil
}

func ResetTestRoot(root string) *Config {
	// TODO
	return defaultConfig()
}

