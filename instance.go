package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Single instance: only one Keep the Light window runs at a time. Starting it
// again (or dropping files on the exe) hands the files to the window that is
// already open and brings it to the front, instead of opening a second window
// that would share, and fight over, the same remembered queue and settings.
//
// The first instance holds a lock (a named mutex on Windows) and listens on a
// private local connection; its port and a random token are in instance.json
// in the settings folder, which only this Windows user can read.

type instanceInfo struct {
	PID   int    `json:"pid"`
	Port  int    `json:"port"`
	Token string `json:"token"`
}

type openRequest struct {
	Token string   `json:"token"`
	Files []string `json:"files"`
}

var (
	instanceMu  sync.Mutex
	instanceLn  net.Listener
	instanceRel func() // releases the lock
)

func instanceFile() string { return filepath.Join(appDataDir(), "instance.json") }

// claimInstance decides whether this process is the one window. It returns
// true if another instance took the files (this process should exit).
func claimInstance(g *guiServer) (handedOff bool) {
	if rel, ok := acquireInstanceLock(); ok {
		instanceRel = rel
		g.serveInstance()
		return false
	}
	// Another instance holds the lock: give it the files. It may still be
	// starting up (writing instance.json), so try for a few seconds.
	for end := time.Now().Add(6 * time.Second); time.Now().Before(end); time.Sleep(250 * time.Millisecond) {
		if sendToInstance(g.initial) == nil {
			return true
		}
	}
	// The other instance does not answer (hung, or crashed without releasing
	// the lock): open normally rather than leaving the user with nothing.
	if hasConsole() {
		fmt.Println("Another Keep the Light seems to be running but does not answer; opening a new window.")
	}
	g.serveInstance()
	return false
}

// sendToInstance passes files to the running instance and lets it come to the front.
func sendToInstance(files []string) error {
	b, err := os.ReadFile(instanceFile())
	if err != nil {
		return err
	}
	var info instanceInfo
	if err := json.Unmarshal(b, &info); err != nil || info.Port == 0 {
		return fmt.Errorf("bad instance file")
	}
	allowForeground(info.PID) // Windows only lets the program the user just started hand over the front
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", info.Port), 2*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if files == nil {
		files = []string{}
	}
	if err := json.NewEncoder(c).Encode(openRequest{Token: info.Token, Files: files}); err != nil {
		return err
	}
	reply, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return err
	}
	if reply != "ok\n" {
		return fmt.Errorf("refused: %q", reply)
	}
	return nil
}

// serveInstance listens for later starts of Keep the Light.
func (g *guiServer) serveInstance() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return
	}
	info := instanceInfo{PID: os.Getpid(), Port: ln.Addr().(*net.TCPAddr).Port, Token: g.token}
	b, _ := json.Marshal(info)
	_ = os.MkdirAll(appDataDir(), 0o700)
	if err := os.WriteFile(instanceFile(), b, 0o600); err != nil {
		ln.Close()
		return
	}
	instanceMu.Lock()
	instanceLn = ln
	instanceMu.Unlock()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go g.handleOpen(c)
		}
	}()
}

func (g *guiServer) handleOpen(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	var req openRequest
	if err := json.NewDecoder(bufio.NewReader(c)).Decode(&req); err != nil || req.Token != g.token {
		_, _ = c.Write([]byte("no\n"))
		return
	}
	g.mu.Lock()
	g.inbox = append(g.inbox, req.Files...)
	g.opened++
	raise := g.raise
	g.mu.Unlock()
	if raise != nil {
		raise()
	}
	_, _ = c.Write([]byte("ok\n"))
}

// takeInbox returns files sent by a later start, for the page to open.
func (g *guiServer) takeInboxLocked() []string {
	files := g.inbox
	g.inbox = nil
	return files
}

// releaseInstance gives up being the one window (before handing over to a
// replacement process, or when exiting).
func releaseInstance() {
	instanceMu.Lock()
	defer instanceMu.Unlock()
	if instanceLn != nil {
		instanceLn.Close()
		instanceLn = nil
		_ = os.Remove(instanceFile())
	}
	if instanceRel != nil {
		instanceRel()
		instanceRel = nil
	}
}
