package connmgr

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/libp2p/go-libp2p-core/connmgr"
	tu "github.com/libp2p/go-libp2p-core/test"

	"github.com/benbjohnson/clock"
)

func TestDecayExpire(t *testing.T) {
	var (
		id                    = tu.RandPeerIDFatal(t)
		mgr, decay, mockClock = testDecayTracker(t)
	)

	tag, err := decay.RegisterDecayingTag("pop", 250*time.Millisecond, ExpireWhenInactive(1*time.Second), SumUnbounded())
	if err != nil {
		t.Fatal(err)
	}

	err = tag.Bump(id, 10)
	if err != nil {
		t.Fatal(err)
	}

	// give time for the bump command to process.
	<-time.After(100 * time.Millisecond)

	if v := mgr.GetTagInfo(id).Value; v != 10 {
		t.Fatalf("wrong value; expected = %d; got = %d", 10, v)
	}

	mockClock.Add(250 * time.Millisecond)
	mockClock.Add(250 * time.Millisecond)
	mockClock.Add(250 * time.Millisecond)
	mockClock.Add(250 * time.Millisecond)

	if v := mgr.GetTagInfo(id).Value; v != 0 {
		t.Fatalf("wrong value; expected = %d; got = %d", 0, v)
	}
}

func TestMultipleBumps(t *testing.T) {
	var (
		id            = tu.RandPeerIDFatal(t)
		mgr, decay, _ = testDecayTracker(t)
	)

	tag, err := decay.RegisterDecayingTag("pop", 250*time.Millisecond, ExpireWhenInactive(1*time.Second), SumBounded(10, 20))
	if err != nil {
		t.Fatal(err)
	}

	err = tag.Bump(id, 5)
	if err != nil {
		t.Fatal(err)
	}

	<-time.After(100 * time.Millisecond)

	if v := mgr.GetTagInfo(id).Value; v != 10 {
		t.Fatalf("wrong value; expected = %d; got = %d", 10, v)
	}

	err = tag.Bump(id, 100)
	if err != nil {
		t.Fatal(err)
	}

	<-time.After(100 * time.Millisecond)

	if v := mgr.GetTagInfo(id).Value; v != 20 {
		t.Fatalf("wrong value; expected = %d; got = %d", 20, v)
	}
}

func TestMultipleTagsNoDecay(t *testing.T) {
	var (
		id            = tu.RandPeerIDFatal(t)
		mgr, decay, _ = testDecayTracker(t)
	)

	tag1, err := decay.RegisterDecayingTag("beep", 250*time.Millisecond, NoDecay(), SumBounded(0, 100))
	if err != nil {
		t.Fatal(err)
	}

	tag2, err := decay.RegisterDecayingTag("bop", 250*time.Millisecond, NoDecay(), SumBounded(0, 100))
	if err != nil {
		t.Fatal(err)
	}

	tag3, err := decay.RegisterDecayingTag("foo", 250*time.Millisecond, NoDecay(), SumBounded(0, 100))
	if err != nil {
		t.Fatal(err)
	}

	_ = tag1.Bump(id, 100)
	_ = tag2.Bump(id, 100)
	_ = tag3.Bump(id, 100)
	_ = tag1.Bump(id, 100)
	_ = tag2.Bump(id, 100)
	_ = tag3.Bump(id, 100)

	<-time.After(500 * time.Millisecond)

	// all tags are upper-bounded, so the score must be 300
	ti := mgr.GetTagInfo(id)
	if v := ti.Value; v != 300 {
		t.Fatalf("wrong value; expected = %d; got = %d", 300, v)
	}

	for _, s := range []string{"beep", "bop", "foo"} {
		if v, ok := ti.Tags[s]; !ok || v != 100 {
			t.Fatalf("expected tag %s to be 100; was = %d", s, v)
		}
	}
}

func TestCustomFunctions(t *testing.T) {
	var (
		id                    = tu.RandPeerIDFatal(t)
		mgr, decay, mockClock = testDecayTracker(t)
	)

	tag1, err := decay.RegisterDecayingTag("beep", 250*time.Millisecond, FixedDecay(10), SumUnbounded())
	if err != nil {
		t.Fatal(err)
	}

	tag2, err := decay.RegisterDecayingTag("bop", 100*time.Millisecond, FixedDecay(5), SumUnbounded())
	if err != nil {
		t.Fatal(err)
	}

	tag3, err := decay.RegisterDecayingTag("foo", 50*time.Millisecond, FixedDecay(1), SumUnbounded())
	if err != nil {
		t.Fatal(err)
	}

	_ = tag1.Bump(id, 1000)
	_ = tag2.Bump(id, 1000)
	_ = tag3.Bump(id, 1000)

	<-time.After(500 * time.Millisecond)

	// no decay has occurred yet, so score must be 3000.
	if v := mgr.GetTagInfo(id).Value; v != 3000 {
		t.Fatalf("wrong value; expected = %d; got = %d", 3000, v)
	}

	// only tag3 should tick.
	mockClock.Add(50 * time.Millisecond)
	if v := mgr.GetTagInfo(id).Value; v != 2999 {
		t.Fatalf("wrong value; expected = %d; got = %d", 2999, v)
	}

	// tag3 will tick thrice, tag2 will tick twice.
	mockClock.Add(150 * time.Millisecond)
	if v := mgr.GetTagInfo(id).Value; v != 2986 {
		t.Fatalf("wrong value; expected = %d; got = %d", 2986, v)
	}

	// tag3 will tick once, tag1 will tick once.
	mockClock.Add(50 * time.Millisecond)
	if v := mgr.GetTagInfo(id).Value; v != 2975 {
		t.Fatalf("wrong value; expected = %d; got = %d", 2975, v)
	}
}

func TestMultiplePeers(t *testing.T) {
	var (
		ids                   = []peer.ID{tu.RandPeerIDFatal(t), tu.RandPeerIDFatal(t), tu.RandPeerIDFatal(t)}
		mgr, decay, mockClock = testDecayTracker(t)
	)

	tag1, err := decay.RegisterDecayingTag("beep", 250*time.Millisecond, FixedDecay(10), SumUnbounded())
	if err != nil {
		t.Fatal(err)
	}

	tag2, err := decay.RegisterDecayingTag("bop", 100*time.Millisecond, FixedDecay(5), SumUnbounded())
	if err != nil {
		t.Fatal(err)
	}

	tag3, err := decay.RegisterDecayingTag("foo", 50*time.Millisecond, FixedDecay(1), SumUnbounded())
	if err != nil {
		t.Fatal(err)
	}

	_ = tag1.Bump(ids[0], 1000)
	_ = tag2.Bump(ids[0], 1000)
	_ = tag3.Bump(ids[0], 1000)

	_ = tag1.Bump(ids[1], 500)
	_ = tag2.Bump(ids[1], 500)
	_ = tag3.Bump(ids[1], 500)

	_ = tag1.Bump(ids[2], 100)
	_ = tag2.Bump(ids[2], 100)
	_ = tag3.Bump(ids[2], 100)

	// allow the background goroutine to process bumps.
	<-time.After(500 * time.Millisecond)

	mockClock.Add(3 * time.Second)

	// allow the background goroutine to process ticks.
	<-time.After(500 * time.Millisecond)

	if v := mgr.GetTagInfo(ids[0]).Value; v != 2670 {
		t.Fatalf("wrong value; expected = %d; got = %d", 2670, v)
	}

	if v := mgr.GetTagInfo(ids[1]).Value; v != 1170 {
		t.Fatalf("wrong value; expected = %d; got = %d", 1170, v)
	}

	if v := mgr.GetTagInfo(ids[2]).Value; v != 40 {
		t.Fatalf("wrong value; expected = %d; got = %d", 40, v)
	}
}

func TestLinearDecayOverwrite(t *testing.T) {
	var (
		id                    = tu.RandPeerIDFatal(t)
		mgr, decay, mockClock = testDecayTracker(t)
	)

	tag1, err := decay.RegisterDecayingTag("beep", 250*time.Millisecond, LinearDecay(0.5), Overwrite())
	if err != nil {
		t.Fatal(err)
	}

	_ = tag1.Bump(id, 1000)
	// allow the background goroutine to process bumps.
	<-time.After(500 * time.Millisecond)

	mockClock.Add(250 * time.Millisecond)

	if v := mgr.GetTagInfo(id).Value; v != 500 {
		t.Fatalf("value should be half; got = %d", v)
	}

	mockClock.Add(250 * time.Millisecond)

	if v := mgr.GetTagInfo(id).Value; v != 250 {
		t.Fatalf("value should be half; got = %d", v)
	}

	_ = tag1.Bump(id, 1000)
	// allow the background goroutine to process bumps.
	<-time.After(500 * time.Millisecond)

	if v := mgr.GetTagInfo(id).Value; v != 1000 {
		t.Fatalf("value should 1000; got = %d", v)
	}
}

func testDecayTracker(tb testing.TB) (*BasicConnMgr, connmgr.Decayer, *clock.Mock) {
	mockClock := clock.NewMock()
	cfg := &DecayerCfg{
		Resolution: 50 * time.Millisecond,
		Clock:      mockClock,
	}

	mgr := NewConnManager(10, 10, 1*time.Second, DecayerConfig(cfg))
	decay, ok := connmgr.SupportsDecay(mgr)
	if !ok {
		tb.Fatalf("connmgr does not support decay")
	}

	return mgr, decay, mockClock
}
