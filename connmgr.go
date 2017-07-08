package connmgr

import (
	"context"
	"sort"
	"sync"
	"time"

	inet "gx/ipfs/QmRscs8KxrSmSv4iuevHv8JfuUzHBMoqiaHzxfDRiksd6e/go-libp2p-net"
	logging "gx/ipfs/QmSpJByNKFX1sCsHBEp3R73FL4NF6FnQTEGyNAXHm2GS52/go-log"
	ma "gx/ipfs/QmcyqRMCAXVtYPS4DiBrA7sezL9rRGfW8Ctx7cywL4TXJj/go-multiaddr"
)

var log = logging.Logger("connmgr")

type ConnManager struct {
	HighWater int
	LowWater  int

	GracePeriod time.Duration

	conns map[inet.Conn]connInfo

	lk sync.Mutex
}

func NewConnManager(low, hi int) *ConnManager {
	return &ConnManager{
		HighWater:   hi,
		LowWater:    low,
		GracePeriod: time.Second * 10,
		conns:       make(map[inet.Conn]connInfo),
	}
}

type connInfo struct {
	tags   map[string]int
	value  int
	c      inet.Conn
	closed bool

	firstSeen time.Time
}

func (cm *ConnManager) TrimOpenConns(ctx context.Context) {
	defer log.EventBegin(ctx, "connCleanup").Done()
	cm.lk.Lock()
	defer cm.lk.Unlock()

	if len(cm.conns) < cm.LowWater {
		log.Info("open connection count below limit")
		return
	}

	var infos []connInfo

	for _, inf := range cm.conns {
		infos = append(infos, inf)
	}

	sort.Slice(infos, func(i, j int) bool {
		if infos[i].closed {
			return true
		}
		if infos[j].closed {
			return false
		}

		return infos[i].value < infos[j].value
	})

	toclose := infos[:len(infos)-cm.LowWater]

	for _, inf := range toclose {
		if time.Since(inf.firstSeen) < cm.GracePeriod {
			continue
		}

		log.Info("closing conn: ", inf.c.RemotePeer())
		log.Event(ctx, "closeConn", inf.c.RemotePeer())
		inf.c.Close()
		inf.closed = true
	}

	if len(cm.conns) > cm.HighWater {
		log.Error("still over high water mark after trimming connections")
	}
}

func (cm *ConnManager) TagConn(c inet.Conn, tag string, val int) {
	cm.lk.Lock()
	defer cm.lk.Unlock()

	ci, ok := cm.conns[c]
	if !ok {
		log.Error("tried to tag untracked conn: ", c.RemotePeer())
		return
	}

	ci.value += (val - ci.tags[tag])
	ci.tags[tag] = val
}

func (cm *ConnManager) UntagConn(c inet.Conn, tag string) {
	cm.lk.Lock()
	defer cm.lk.Unlock()

	ci, ok := cm.conns[c]
	if !ok {
		log.Error("tried to remove tag on untracked conn: ", c.RemotePeer())
		return
	}

	ci.value -= ci.tags[tag]
	delete(ci.tags, tag)
}

func (cm *ConnManager) Notifee() *cmNotifee {
	return (*cmNotifee)(cm)
}

type cmNotifee ConnManager

func (nn *cmNotifee) cm() *ConnManager {
	return (*ConnManager)(nn)
}

func (nn *cmNotifee) Connected(n inet.Network, c inet.Conn) {
	cm := nn.cm()

	cm.lk.Lock()
	defer cm.lk.Unlock()

	cinfo, ok := cm.conns[c]
	if ok {
		log.Error("received connected notification for conn we are already tracking: ", c.RemotePeer())
		return
	}

	cinfo = connInfo{
		firstSeen: time.Now(),
		tags:      make(map[string]int),
		c:         c,
	}
	cm.conns[c] = cinfo

	if len(cm.conns) > nn.HighWater {
		go cm.TrimOpenConns(context.Background())
	}
}

func (nn *cmNotifee) Disconnected(n inet.Network, c inet.Conn) {
	cm := nn.cm()

	cm.lk.Lock()
	defer cm.lk.Unlock()

	_, ok := cm.conns[c]
	if !ok {
		log.Error("received disconnected notification for conn we are not tracking: ", c.RemotePeer())
		return
	}

	delete(cm.conns, c)
}

func (nn *cmNotifee) Listen(n inet.Network, addr ma.Multiaddr)      {}
func (nn *cmNotifee) ListenClose(n inet.Network, addr ma.Multiaddr) {}
func (nn *cmNotifee) OpenedStream(inet.Network, inet.Stream)        {}
func (nn *cmNotifee) ClosedStream(inet.Network, inet.Stream)        {}
