// Package faultproxy injects worker-to-database transport failures, not imaginary disk faults.
package faultproxy

import (
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type Proxy struct {
	listener    net.Listener
	target      string
	mu          sync.Mutex
	connections map[net.Conn]struct{}
	blocked     atomic.Bool
	delay       atomic.Int64
	closed      atomic.Bool
}

func Start(target string) (*Proxy, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &Proxy{listener: l, target: target, connections: map[net.Conn]struct{}{}}
	go p.accept()
	return p, nil
}
func (p *Proxy) Addr() string { return p.listener.Addr().String() }
func (p *Proxy) Set(blocked bool, delay time.Duration) {
	p.delay.Store(int64(delay))
	p.blocked.Store(blocked)
	if blocked {
		p.mu.Lock()
		for c := range p.connections {
			c.Close()
		}
		p.mu.Unlock()
	}
}
func (p *Proxy) Close() { p.closed.Store(true); p.listener.Close(); p.Set(true, 0) }
func (p *Proxy) accept() {
	for {
		client, err := p.listener.Accept()
		if err != nil {
			return
		}
		if p.blocked.Load() {
			client.Close()
			continue
		}
		go p.serve(client)
	}
}
func (p *Proxy) serve(client net.Conn) {
	backend, err := net.DialTimeout("tcp", p.target, time.Second)
	if err != nil {
		client.Close()
		return
	}
	p.mu.Lock()
	if p.blocked.Load() || p.closed.Load() {
		p.mu.Unlock()
		client.Close()
		backend.Close()
		return
	}
	p.connections[client] = struct{}{}
	p.connections[backend] = struct{}{}
	p.mu.Unlock()
	defer func() {
		client.Close()
		backend.Close()
		p.mu.Lock()
		delete(p.connections, client)
		delete(p.connections, backend)
		p.mu.Unlock()
	}()
	done := make(chan struct{}, 2)
	copy := func(to, from net.Conn) {
		defer func() { to.Close(); done <- struct{}{} }()
		buf := make([]byte, 32768)
		for {
			n, err := from.Read(buf)
			if n > 0 {
				if d := time.Duration(p.delay.Load()); d > 0 {
					time.Sleep(d)
				}
				if p.blocked.Load() {
					return
				}
				if _, writeErr := to.Write(buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
	go copy(client, backend)
	go copy(backend, client)
	<-done
	<-done
}
