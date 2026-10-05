package node

import (
	"errors"
	"net"
	"time"

	"github.com/agentic-substrate/substrate/internal/artifacts"
)

type maintenanceRequest struct {
	request Request
	reply   chan maintenanceResult
}
type maintenanceResult struct {
	artifacts.Maintenance
	Processed int `json:"processed"`
	err       error
}

func (n *Node) release(conn net.Conn) {
	conn.Close()
	n.connectionMu.Lock()
	delete(n.connections, conn)
	n.connectionMu.Unlock()
}

func (n *Node) notify() {
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

func (n *Node) control(req Request) (maintenanceResult, error) {
	if req.Control != "" && req.Pause != nil {
		return maintenanceResult{}, errors.New("index controls are mutually exclusive")
	}
	switch req.Control {
	case "", "status", "rebuild", "run", "retry":
	default:
		return maintenanceResult{}, errors.New("unsupported index control")
	}
	command := maintenanceRequest{request: req, reply: make(chan maintenanceResult, 1)}
	select {
	case n.controls <- command:
	case <-n.ctx.Done():
		return maintenanceResult{}, artifacts.ErrUnavailable
	}
	select {
	case result := <-command.reply:
		return result, result.err
	case <-n.ctx.Done():
		return maintenanceResult{}, artifacts.ErrUnavailable
	}
}

func (n *Node) perform(command maintenanceRequest) {
	req := command.request
	var result maintenanceResult
	if req.Pause != nil {
		result.err = n.store.SetIndexPaused(*req.Pause)
	}
	if result.err == nil {
		switch req.Control {
		case "rebuild":
			result.err = n.store.RebuildIndex()
		case "retry":
			result.err = n.store.RetryIndex()
		}
	}
	if result.err == nil {
		result.Maintenance, result.err = n.store.Maintenance()
	}
	if result.err == nil && !result.Paused && (req.Control == "" || req.Control == "run") {
		for range 100 {
			attempted, err := n.store.IndexNext(n.ctx, req.Control == "run")
			if err == nil && attempted {
				result.Processed++
			}
			if !attempted || n.ctx.Err() != nil {
				result.err = err
				break
			}
		}
		if result.err == nil {
			result.Maintenance, result.err = n.store.Maintenance()
		}
	}
	command.reply <- result
	if req.Control != "status" {
		n.notify()
	}
}

func (n *Node) maintain() {
	defer n.wg.Done()
	for {
		select {
		case <-n.ctx.Done():
			return
		case command := <-n.controls:
			n.perform(command)
		case <-n.wake:
			timer := time.NewTimer(25 * time.Millisecond)
		debounce:
			for {
				select {
				case <-n.ctx.Done():
					timer.Stop()
					return
				case command := <-n.controls:
					n.perform(command)
				case <-timer.C:
					break debounce
				}
			}
			for {
				select {
				case <-n.ctx.Done():
					return
				case command := <-n.controls:
					n.perform(command)
				default:
				}
				paused, err := n.store.IndexPaused()
				if err != nil || paused {
					break
				}
				attempted, _ := n.store.IndexNext(n.ctx, false)
				if !attempted {
					break
				}
			}
		}
	}
}
