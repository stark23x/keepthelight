package main

import "sync"

// ---------- keeping the PC awake while the queue runs ----------
//
// Each job already keeps the PC from sleeping while it converts, but a queue
// run is more than that: copying a network file here first, the quality
// search, the gaps between files, copying the result back and telling Plex.
// While a run is going (and not paused) the window holds one request with
// Windows for all of it. On a PC with Modern Standby (most newer laptops)
// Windows freezes programs when the screen turns off, so there the screen is
// kept on too, unless that is switched off in Settings.

var holdAwake = holdAwakeOS // tests replace it

// awakeState is held by the window: the request now held, if any.
type awakeState struct {
	awakeMu     sync.Mutex // order: awakeMu, then g.mu
	awakeRel    func()
	awakeScreen bool
	awakeExtra  int // other work that wants the PC awake (under g.mu)
}

// awakeWantedLocked: should the request be held now? Caller holds g.mu.
func (g *guiServer) awakeWantedLocked() (want, screen bool) {
	if !g.keepAwake || g.closing {
		return false, false
	}
	busy := g.q.running || g.awakeExtra > 0
	if j := g.job; j != nil && j.running && j.paused {
		busy = false // paused: the PC may sleep as usual
	}
	return busy, g.keepScreen
}

// syncAwake takes or gives back the request to match the queue's state. It
// is called whenever a run starts or ends, a job is paused or resumed, and the
// setting changes.
func (g *guiServer) syncAwake() {
	g.awakeMu.Lock()
	defer g.awakeMu.Unlock()
	g.mu.Lock()
	want, screen := g.awakeWantedLocked()
	g.mu.Unlock()
	if g.awakeRel != nil && (!want || screen != g.awakeScreen) {
		g.awakeRel()
		g.awakeRel = nil
	}
	if want && g.awakeRel == nil {
		rel, err := holdAwake(screen)
		if err != nil {
			g.mu.Lock() // a quiet note: the conversions go on either way
			g.q.notice, g.q.noticeSeq = "Could not ask Windows to keep the PC awake: "+err.Error(), g.q.noticeSeq+1
			g.mu.Unlock()
			return
		}
		g.awakeRel, g.awakeScreen = rel, screen
	}
}
