package connmgr

import (
	"time"
)

type BasicConnManagerConfig struct {
	highWater     int
	lowWater      int
	gracePeriod   time.Duration
	silencePeriod time.Duration
	decayer       *DecayerCfg
}

type Option func(*BasicConnManagerConfig) error

func DecayerConfig(opts *DecayerCfg) Option {
	return func(cfg *BasicConnManagerConfig) error {
		cfg.decayer = opts
		return nil
	}
}
