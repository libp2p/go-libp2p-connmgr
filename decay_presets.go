package connmgr

import (
	"math"
	"time"

	"github.com/libp2p/go-libp2p-core/connmgr"
)

// NoDecay applies no decay.
func NoDecay() connmgr.DecayFn {
	return func(value connmgr.DecayingValue) (_ int, rm bool) {
		return value.Value, false
	}
}

// FixedDecay subtracts from by the provided minuend, and deletes the tag when
// first reaching 0 or negative.
func FixedDecay(minuend int) connmgr.DecayFn {
	return func(value connmgr.DecayingValue) (_ int, rm bool) {
		v := value.Value - minuend
		return v, v <= 0
	}
}

// LinearDecay applies a fractional coefficient to the value of the current tag,
// rounding down via math.Floor. It erases the tag when the result is zero.
func LinearDecay(coef float64) connmgr.DecayFn {
	return func(value connmgr.DecayingValue) (after int, rm bool) {
		v := math.Floor(float64(value.Value) * coef)
		return int(v), v <= 0
	}
}

// ExpireWhenInactive expires a tag after a certain period of no bumps.
func ExpireWhenInactive(after time.Duration) connmgr.DecayFn {
	return func(value connmgr.DecayingValue) (_ int, rm bool) {
		rm = value.LastVisit.Sub(time.Now()) >= after
		return 0, rm
	}
}

// SumUnbounded adds the incoming value to the peer's score.
func SumUnbounded() connmgr.BumpFn {
	return func(value connmgr.DecayingValue, delta int) (after int) {
		return value.Value + delta
	}
}

// SumBounded keeps summing the incoming score, keeping it within a [min, max]
// range.
func SumBounded(min, max int) connmgr.BumpFn {
	return func(value connmgr.DecayingValue, delta int) (after int) {
		v := value.Value + delta
		if v >= max {
			return max
		} else if v <= min {
			return min
		}
		return v
	}
}

// Overwrite replaces the current value of the tag with the incoming one.
func Overwrite() connmgr.BumpFn {
	return func(value connmgr.DecayingValue, delta int) (after int) {
		return delta
	}
}
