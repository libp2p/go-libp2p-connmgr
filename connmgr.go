package connmgr

import (
	"context"
	"sort"
	"sync"
	"time"

	logging "github.com/ipfs/go-log"
	inet "github.com/libp2p/go-libp2p-net"
	peer "github.com/libp2p/go-libp2p-peer"
	ma "github.com/multiformats/go-multiaddr"
)

var log = logging.Logger("connmgr")

type ConnManager struct {
	HighWater int
	LowWater  int

	GracePeriod time.Duration

	peers     map[peer.ID]peerInfo
	connCount int

	lk sync.Mutex

	lastTrim time.Time
}

func NewConnManager(low, hi int) *ConnManager {
	return &ConnManager{
		HighWater:   hi,
		LowWater:    low,
		GracePeriod: time.Second * 10,
		peers:       make(map[peer.ID]peerInfo),
	}
}

type peerInfo struct {
	tags  map[string]int
	value int

	conns map[inet.Conn]time.Time

	firstSeen time.Time
}

func (cm *ConnManager) TrimOpenConns(ctx context.Context) {
	cm.lk.Lock()
	defer cm.lk.Unlock()
	defer log.EventBegin(ctx, "connCleanup").Done()
	cm.lastTrim = time.Now()

	if len(cm.peers) < cm.LowWater {
		log.Info("open connection count below limit")
		return
	}

	var infos []peerInfo

	for _, inf := range cm.peers {
		infos = append(infos, inf)
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].value < infos[j].value
	})

	toclose := infos[:len(infos)-cm.LowWater]

	for _, inf := range toclose {
		if time.Since(inf.firstSeen) < cm.GracePeriod {
			continue
		}

		// TODO: if a peer has more than one connection, maybe only close one?
		for c, _ := range inf.conns {
			log.Info("closing conn: ", c.RemotePeer())
			log.Event(ctx, "closeConn", c.RemotePeer())
			c.Close()
		}
	}

	if len(cm.peers) > cm.HighWater {
		log.Error("still over high water mark after trimming connections")
	}
}

func (cm *ConnManager) TagConn(p peer.ID, tag string, val int) {
	cm.lk.Lock()
	defer cm.lk.Unlock()

	pi, ok := cm.peers[p]
	if !ok {
		log.Error("tried to tag conn from untracked peer: ", p)
		return
	}

	pi.value += (val - pi.tags[tag])
	pi.tags[tag] = val
}

func (cm *ConnManager) UntagConn(p peer.ID, tag string) {
	cm.lk.Lock()
	defer cm.lk.Unlock()

	pi, ok := cm.peers[p]
	if !ok {
		log.Error("tried to remove tag from untracked peer: ", p)
		return
	}

	pi.value -= pi.tags[tag]
	delete(pi.tags, tag)
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

	pinfo, ok := cm.peers[c.RemotePeer()]
	if !ok {
		pinfo = peerInfo{
			firstSeen: time.Now(),
			tags:      make(map[string]int),
			conns:     make(map[inet.Conn]time.Time),
		}
		cm.peers[c.RemotePeer()] = pinfo
	}

	_, ok = pinfo.conns[c]
	if ok {
		log.Error("received connected notification for conn we are already tracking: ", c.RemotePeer())
		return
	}

	pinfo.conns[c] = time.Now()
	cm.connCount++

	if cm.connCount > nn.HighWater {
		if cm.lastTrim.IsZero() || time.Since(cm.lastTrim) > time.Second*10 {
			go cm.TrimOpenConns(context.Background())
		}
	}
}

func (nn *cmNotifee) Disconnected(n inet.Network, c inet.Conn) {
	cm := nn.cm()

	cm.lk.Lock()
	defer cm.lk.Unlock()

	cinf, ok := cm.peers[c.RemotePeer()]
	if !ok {
		log.Error("received disconnected notification for peer we are not tracking: ", c.RemotePeer())
		return
	}

	_, ok = cinf.conns[c]
	if !ok {
		log.Error("received disconnected notification for conn we are not tracking: ", c.RemotePeer())
		return
	}

	delete(cinf.conns, c)
	cm.connCount--
	if len(cinf.conns) == 0 {
		delete(cm.peers, c.RemotePeer())
	}
}

func (nn *cmNotifee) Listen(n inet.Network, addr ma.Multiaddr)      {}
func (nn *cmNotifee) ListenClose(n inet.Network, addr ma.Multiaddr) {}
func (nn *cmNotifee) OpenedStream(inet.Network, inet.Stream)        {}
func (nn *cmNotifee) ClosedStream(inet.Network, inet.Stream)        {}
