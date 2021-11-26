package connmgr

import "time"

// config is the configuration struct for the basic connection manager.
type config struct {
	highWater     int
	lowWater      int
	gracePeriod   time.Duration
	silencePeriod time.Duration
	decayer       *DecayerCfg
}

// Option represents an option for the basic connection manager.
type Option func(*config) error

// DecayerConfig applies a configuration for the decayer.
func DecayerConfig(opts *DecayerCfg) Option {
	return func(cfg *config) error {
		cfg.decayer = opts
		return nil
	}
}
