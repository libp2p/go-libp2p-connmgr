package connmgr

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p-core/connmgr"
	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/benbjohnson/clock"
)

// DefaultResolution is the default resolution of the decay tracker.
var DefaultResolution = 1 * time.Minute

// bumpCmd represents a bump command.
type bumpCmd struct {
	peer  peer.ID
	tag   *decayingTag
	delta int
}

// decayer tracks and manages all decaying tags and their values.
type decayer struct {
	cfg   *DecayerCfg
	mgr   *BasicConnMgr
	clock clock.Clock // for testing.

	tagsMu    sync.Mutex
	knownTags map[string]*decayingTag

	// currRound is the current round we're in; guarded by atomic.
	currRound uint64

	// bumpCh queues bump commands to be processed by the loop.
	bumpCh chan bumpCmd

	// closure thingies.
	closeCh chan struct{}
	doneCh  chan struct{}
	err     error
}

var _ connmgr.Decayer = (*decayer)(nil)

// DecayerCfg is the configuration object for the Decayer.
type DecayerCfg struct {
	Resolution time.Duration
	Clock      clock.Clock
}

// WithDefaults writes the default values on this DecayerConfig instance,
// and returns itself for chainability.
//
//  cfg := (&DecayerCfg{}).WithDefaults()
//  cfg.Resolution = 30 * time.Second
//  t := NewDecayer(cfg, cm)
func (cfg *DecayerCfg) WithDefaults() *DecayerCfg {
	cfg.Resolution = DefaultResolution
	return cfg
}

// NewDecayer creates a new decaying tag registry.
func NewDecayer(cfg *DecayerCfg, mgr *BasicConnMgr) (*decayer, error) {
	// use real time if the Clock in the config is nil.
	if cfg.Clock == nil {
		cfg.Clock = clock.New()
	}

	d := &decayer{
		cfg:       cfg,
		mgr:       mgr,
		clock:     cfg.Clock,
		knownTags: make(map[string]*decayingTag),
		bumpCh:    make(chan bumpCmd, 128),
		closeCh:   make(chan struct{}),
		doneCh:    make(chan struct{}),
	}

	// kick things off.
	go d.process()

	return d, nil
}

// decayingTag represents a decaying tag, with an associated decay interval, a
// decay function, and a bump function.
type decayingTag struct {
	trkr      *decayer
	name      string
	interval  time.Duration
	nextRound uint64
	decayFn   connmgr.DecayFn
	bumpFn    connmgr.BumpFn
}

func (d *decayer) RegisterDecayingTag(name string, interval time.Duration, decayFn connmgr.DecayFn, bumpFn connmgr.BumpFn) (connmgr.DecayingTag, error) {
	d.tagsMu.Lock()
	defer d.tagsMu.Unlock()

	tag, ok := d.knownTags[name]
	if ok {
		return nil, fmt.Errorf("decaying tag with name %s already exists", name)
	}

	if interval < d.cfg.Resolution {
		err := fmt.Errorf("decay interval for %s (%s) is lower than tracker's resolution (%s)",
			name,
			interval,
			d.cfg.Resolution)
		return nil, err
	}

	if interval%d.cfg.Resolution != 0 {
		err := fmt.Errorf("decay interval for tag %s (%s) is not a multiple of tracker's resolution (%s); "+
			"preventing undesired loss of precision",
			name,
			interval,
			d.cfg.Resolution)
		return nil, err
	}

	initRound := atomic.LoadUint64(&d.currRound) + uint64(interval/d.cfg.Resolution)
	tag = &decayingTag{
		trkr:      d,
		name:      name,
		interval:  interval,
		nextRound: initRound,
		decayFn:   decayFn,
		bumpFn:    bumpFn,
	}

	d.knownTags[name] = tag
	return tag, nil
}

// Close closes the Decayer. It is idempotent.
func (d *decayer) Close() error {
	select {
	case <-d.doneCh:
		return d.err
	default:
	}

	close(d.closeCh)
	<-d.doneCh
	return d.err
}

// process is the heart of the tracker. It performs the following duties:
//
//  1. Manages decay.
//  2. Applies score bumps.
//  3. Yields when closed.
func (d *decayer) process() {
	defer close(d.doneCh)

	ticker := d.clock.Ticker(d.cfg.Resolution)
	defer ticker.Stop()

	var (
		bmp   bumpCmd
		now   time.Time
		visit = make(map[*decayingTag]struct{})
	)

	for {
		select {
		case now = <-ticker.C:
			round := atomic.AddUint64(&d.currRound, 1)

			for _, tag := range d.knownTags {
				if tag.nextRound <= round {
					// Mark the tag to be updated in this round.
					visit[tag] = struct{}{}
				}
			}

			// Visit each peer, and decay tags that need to be decayed.
			for _, s := range d.mgr.segments {
				s.Lock()

				// Entered a segment that contains peers. Process each peer.
				for _, p := range s.peers {
					for tag, v := range p.decaying {
						if _, ok := visit[tag]; !ok {
							// skip this tag.
							continue
						}

						// ~ this value needs to be visited. ~
						var delta int
						if after, rm := tag.decayFn(*v); rm {
							// delete the value and move on to the next tag.
							delta -= v.Value
							delete(p.decaying, tag)
						} else {
							// accumulate the delta, and apply the changes.
							delta += (after - v.Value)
							v.Value, v.LastVisit = after, now
						}
						p.value += delta
					}
				}

				s.Unlock()
			}

			// Reset each tag's next visit round, and clear the visited set.
			for tag := range visit {
				tag.nextRound = round + uint64(tag.interval/d.cfg.Resolution)
				delete(visit, tag)
			}

		case bmp = <-d.bumpCh:
			var (
				now       = d.clock.Now()
				peer, tag = bmp.peer, bmp.tag
			)

			s := d.mgr.segments.get(peer)
			s.Lock()

			p := s.tagInfoFor(peer)
			v, ok := p.decaying[tag]
			if !ok {
				v = &connmgr.DecayingValue{
					Tag:       tag,
					Peer:      peer,
					LastVisit: now,
					Added:     now,
					Value:     0,
				}
				p.decaying[tag] = v
			}

			prev := v.Value
			v.Value, v.LastVisit = v.Tag.(*decayingTag).bumpFn(*v, bmp.delta), now
			p.value += v.Value - prev

			s.Unlock()

		case <-d.closeCh:
			return

		}
	}
}

// Bump bumps a tag for this peer.
func (t *decayingTag) Bump(p peer.ID, delta int) error {
	bmp := bumpCmd{peer: p, tag: t, delta: delta}

	select {
	case t.trkr.bumpCh <- bmp:
		return nil

	default:
		return fmt.Errorf(
			"unable to bump decaying tag for peer %s, tag %s, delta %d; queue full (len=%d)",
			p.Pretty(),
			t.name,
			delta,
			len(t.trkr.bumpCh))
	}
}
