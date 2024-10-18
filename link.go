package hub

import "sync"

type linker interface {
	Send(pkt *Packet) error
	Close()
}

var uplinks = make(map[linker]bool) // keyed by linker
var uplinksMu sync.RWMutex

func uplinksAdd(l linker) {
	uplinksMu.Lock()
	defer uplinksMu.Unlock()
	uplinks[l] = true
}

func uplinksRemove(l linker) {
	uplinksMu.Lock()
	defer uplinksMu.Unlock()
	delete(uplinks, l)
}

func uplinksRoute(pkt *Packet) {
	uplinksMu.RLock()
	defer uplinksMu.RUnlock()
	for ul := range uplinks {
		ul.Send(pkt)
	}
}

var downlinks = make(map[string]linker) // keyed by device id
var downlinksMu sync.RWMutex

func downlinksAdd(id string, l linker) {
	downlinksMu.Lock()
	defer downlinksMu.Unlock()
	downlinks[id] = l
}

func downlinksRemove(id string) {
	downlinksMu.Lock()
	defer downlinksMu.Unlock()
	delete(downlinks, id)
}

func downlinkRoute(pkt *Packet) {
	downlinksMu.RLock()
	defer downlinksMu.RUnlock()
	if dl, ok := downlinks[pkt.Dst]; ok {
		dl.Send(pkt)
	}
}

func downlinkClose(id string) {
	downlinksMu.RLock()
	defer downlinksMu.RUnlock()
	if dl, ok := downlinks[id]; ok {
		dl.Close()
	}
}
