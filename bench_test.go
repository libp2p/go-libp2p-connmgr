package connmgr

import (
	"math/rand"
	"sync"
	"testing"

	inet "github.com/libp2p/go-libp2p-net"
)

func randomConns(tb testing.TB) (c [5000]inet.Conn) {
	for i, _ := range c {
		c[i] = randConn(tb, nil)
	}
	return c
}

func BenchmarkLockContention(b *testing.B) {
	conns := randomConns(b)
	cm := NewConnManager(1000, 1000, 0)
	not := cm.Notifee()

	kill := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-kill:
					return
				default:
					_ = cm.GetTagInfo(conns[rand.Intn(3000)].RemotePeer())
				}
			}
		}()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rc := conns[rand.Intn(3000)]
		not.Connected(nil, rc)
		cm.TagPeer(rc.RemotePeer(), "tag", 100)
		cm.UntagPeer(rc.RemotePeer(), "tag")
		not.Disconnected(nil, rc)
	}
	close(kill)
	wg.Wait()
}
