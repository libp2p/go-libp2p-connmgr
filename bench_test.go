package connmgr

import (
	"context"
	"math/rand"
	"sync"
	"testing"

	"github.com/libp2p/go-libp2p-core/network"
)

func randomConns(tb testing.TB) (c [5000]network.Conn) {
	for i, _ := range c {
		c[i] = randConn(tb, nil)
	}
	return c
}

func BenchmarkLockContention(b *testing.B) {
	wg := &sync.WaitGroup{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conns := randomConns(b)
	cm := NewConnManager(ctx, wg, 1000, 1000, 0)
	not := cm.Notifee()

	kill := make(chan struct{})
	var wg1 sync.WaitGroup

	for i := 0; i < 16; i++ {
		wg1.Add(1)
		go func() {
			defer wg1.Done()
			for {
				select {
				case <-kill:
					return
				default:
					cm.TagPeer(conns[rand.Intn(len(conns))].RemotePeer(), "another-tag", 1)
				}
			}
		}()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rc := conns[rand.Intn(len(conns))]
		not.Connected(nil, rc)
		cm.TagPeer(rc.RemotePeer(), "tag", 100)
		cm.UntagPeer(rc.RemotePeer(), "tag")
		not.Disconnected(nil, rc)
	}
	close(kill)
	wg.Wait()
}
