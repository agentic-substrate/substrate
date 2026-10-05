package node

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/agentic-substrate/substrate/internal/strictjson"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
)

const MaxFrame = 8 * 1024 * 1024

type Request struct {
	Action              string                        `json:"action"`
	Token               string                        `json:"token"`
	Checkout            string                        `json:"checkout"`
	Contribution        artifacts.Contribution        `json:"contribution"`
	Search              artifacts.SearchRequest       `json:"search"`
	Read                artifacts.ReadRequest         `json:"read"`
	Resolution          artifacts.Resolution          `json:"resolution"`
	ID                  string                        `json:"id"`
	Expected            string                        `json:"expected"`
	Operation           string                        `json:"operation"`
	Pause               *bool                         `json:"pause,omitempty"`
	Publication         artifacts.PublicationInput    `json:"publication"`
	PublicationApproval artifacts.PublicationApproval `json:"publication_approval"`
}
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	Code   string          `json:"code,omitempty"`
}
type Node struct {
	auth     *authority.Store
	store    *artifacts.Store
	listener *net.UnixListener
	lock     *os.File
	socket   os.FileInfo
	stop     chan struct{}
	once     sync.Once
	wg       sync.WaitGroup
	slots    chan struct{}
	paused   atomic.Bool
}

func Start(auth *authority.Store, paused bool) (*Node, error) {
	lock, err := Lock(auth)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Node, error) { lock.Close(); return nil, err }
	path := filepath.Join(auth.Dir, "node.sock")
	if len(path) > 100 {
		return fail(errors.New("node socket path too long: use a shorter private state directory"))
	}
	if _, err := privateSocket(path); err == nil {
		if os.Remove(path) != nil {
			return fail(ErrUnavailable)
		}
	} else if !os.IsNotExist(err) {
		return fail(ErrUnavailable)
	}
	store, err := artifacts.Open(auth)
	if err != nil {
		return fail(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		store.Close()
		return fail(ErrUnavailable)
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		os.Remove(path)
		store.Close()
		return fail(ErrUnavailable)
	}
	socket, err := privateSocket(path)
	if err != nil {
		listener.Close()
		os.Remove(path)
		store.Close()
		return fail(err)
	}
	n := &Node{auth: auth, store: store, listener: listener, lock: lock, socket: socket, stop: make(chan struct{}), slots: make(chan struct{}, 32)}
	n.paused.Store(paused)
	n.wg.Add(2)
	go n.accept()
	go n.maintain()
	return n, nil
}
func (n *Node) Close() error {
	n.once.Do(func() {
		close(n.stop)
		n.listener.Close()
		n.wg.Wait()
		n.store.Close()
		path := filepath.Join(n.auth.Dir, "node.sock")
		if info, err := os.Lstat(path); err == nil && os.SameFile(info, n.socket) {
			os.Remove(path)
		}
		n.lock.Close()
	})
	return nil
}
func (n *Node) maintain() {
	defer n.wg.Done()
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-n.stop:
			return
		case <-timer.C:
			if !n.paused.Load() {
				n.store.IndexBatch(100)
			}
		}
	}
}
func (n *Node) accept() {
	defer n.wg.Done()
	for {
		conn, err := n.listener.Accept()
		if err != nil {
			return
		}
		select {
		case n.slots <- struct{}{}:
			n.wg.Add(1)
			go func() {
				defer n.wg.Done()
				defer func() { <-n.slots }()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(30 * time.Second))
				var req Request
				if decodeFrame(conn, &req) != nil {
					writeFrame(conn, Response{Error: "invalid or oversized local request", Code: "invalid"})
					return
				}
				writeFrame(conn, n.dispatch(req))
			}()
		default:
			conn.Close()
		}
	}
}
func (n *Node) dispatch(req Request) Response {
	var result any
	var err error
	if req.Action == "publication-review" {
		result, err = n.store.ReviewPublication(req.Token)
	} else if req.Action == "publication-publish" {
		result, err = n.store.Publish(req.Token, req.PublicationApproval)
	} else if req.Action == "index" {
		if req.Pause != nil {
			n.paused.Store(*req.Pause)
		}
		var count int
		if !n.paused.Load() {
			count, err = n.store.IndexBatch(100)
		}
		result = struct {
			Indexed int  `json:"processed"`
			Paused  bool `json:"paused"`
		}{count, n.paused.Load()}
	} else {
		var session *artifacts.Session
		session, err = n.store.Session(req.Token, req.Checkout)
		if err == nil {
			switch req.Action {
			case "browser-inventory":
				result, err = session.BrowserInventory()
			case "browser-inspect":
				result, err = session.InspectBrowser(req.ID)
			case "publication-propose":
				result, err = session.ProposePublication(req.Publication)
			case "publication-get":
				result, err = session.Publication(req.ID)
			case "capture":
				result, err = session.Contribute(req.Contribution)
			case "search", "discovery":
				result, err = session.Search(req.Search)
			case "read":
				result, err = session.Read(req.Read)
			case "artifact":
				result, err = session.Inspect(req.ID)
			case "pending":
				result, err = session.Pending()
			case "choices":
				result, err = session.Choices(req.Read.Selector)
			case "retire":
				result, err = session.Retire(req.ID, req.Expected, req.Operation)
			case "resolve-conflict":
				result, err = session.ResolveConflict(req.Resolution)
			default:
				err = errors.New("unsupported local operation")
			}
		}
	}
	if err != nil {
		code := "invalid"
		if errors.Is(err, authority.ErrDenied) {
			code = "denied"
		} else if errors.Is(err, artifacts.ErrUnavailable) || errors.Is(err, authority.ErrUnavailable) {
			code = "unavailable"
		} else if errors.Is(err, artifacts.ErrConflict) || errors.Is(err, artifacts.ErrOperation) {
			code = "conflict"
		}
		return Response{Error: err.Error(), Code: code}
	}
	data, err := json.Marshal(result)
	if err != nil || len(data) > MaxFrame-1024 {
		return Response{Error: "local result exceeds the bounded response size", Code: "unavailable"}
	}
	return Response{Result: data}
}
func decodeFrame(r io.Reader, v any) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), MaxFrame)
	if !scanner.Scan() {
		return errors.New("missing bounded frame")
	}
	data := scanner.Bytes()
	if !strictjson.ValidText(data) {
		return errors.New("invalid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}
func writeFrame(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) >= MaxFrame {
		return errors.New("frame too large")
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}
func Call(ctx context.Context, dir string, req Request) Response {
	auth := &authority.Store{Dir: dir}
	if _, err := auth.Inventory(); err != nil {
		return Response{Error: err.Error(), Code: "denied"}
	}
	path := filepath.Join(dir, "node.sock")
	if _, err := privateSocket(path); err != nil {
		return Response{Error: ErrUnavailable.Error(), Code: "unavailable"}
	}
	conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", path)
	if err != nil {
		return Response{Error: ErrUnavailable.Error(), Code: "unavailable"}
	}
	defer conn.Close()
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	if writeFrame(conn, req) != nil {
		return Response{Error: "invalid or oversized local request", Code: "invalid"}
	}
	var response Response
	if decodeFrame(conn, &response) != nil {
		return Response{Error: ErrUnavailable.Error(), Code: "unavailable"}
	}
	return response
}
