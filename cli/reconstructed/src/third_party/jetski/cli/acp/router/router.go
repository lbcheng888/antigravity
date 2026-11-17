// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102fa1ec0 | Size: 61 funcs (first func isRetiring, 240 B) | Source: third_party/jetski/cli/acp/router/router.go:L81
// Reconstructed from binary: /Users/mac/.local/bin/agy (v1.5.10)
// Source: third_party/jetski/cli/acp/router/router.go

package router

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"google3/base/go/log"
	"google3/go_utils/jexec/jexec"
	"google3/third_party/jetski/cli/acp/auth/auth"
	"google3/third_party/jetski/cli/acp/jsonrpc/jsonrpc"
	"google3/third_party/jetski/cli/acp/schema/schema"
)

// Binary VA: 0x10a55af68 | Size: 32 B
type WorkerProcess struct {
	stream     *jsonrpc.Stream
	closeInput func() error
	kill       func()
	wait       func() error
}

// Binary VA: 0x10a10a420 | Size: 8 B
type WorkerSpawner func(context.Context, int) (*WorkerProcess, error)

// Binary VA: 0x109fb2ac8 | Size: 8 B
type pendingKind int

const (
	pendingNormal     pendingKind = iota /* inferred */
	pendingNewSession                    /* inferred */
	pendingLoad                          /* inferred */
	pendingClose                         /* inferred */
)

// Binary VA: 0x10a45c680 | Size: 32 B
type pendingRequest struct {
	worker    *worker
	kind      pendingKind
	sessionID string
}

// Binary VA: 0x10a307930 | Size: 32 B
type outboundCall struct {
	worker *worker
	origID jsontext.Value
}

// Binary VA: 0x10a3078a0 | Size: 16 B
type internalCall struct {
	worker *worker
	done   chan jsonrpc.Envelope
}

// Binary VA: 0x10a3079c0 | Size: 16 B
type sessionLoad struct {
	worker *worker
	closed bool
}

// Binary VA: 0x10a8e51a0 | Size: 104 B
type worker struct {
	index     int
	proc      *WorkerProcess
	ready     chan struct{}
	readyErr  error
	exited    chan struct{}
	exitOnce  sync.Once
	mu        sync.Mutex
	sessionID string
	closing   bool
	retiring  bool
	authGen   uint64
}

// Binary VA: 0x102fa1ec0 | Size: 240 B | Source: third_party/jetski/cli/acp/router/router.go:L81
func (w *worker) isRetiring() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.retiring
}

// Binary VA: 0x102fa1fb0 | Size: 240 B | Source: third_party/jetski/cli/acp/router/router.go:L88
func (w *worker) session() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sessionID
}

// Binary VA: 0x102fa20a0 | Size: 272 B | Source: third_party/jetski/cli/acp/router/router.go:L95
func (w *worker) servesSession() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sessionID != "" && !w.closing
}

// Binary VA: 0x10a9d9958 | Size: 208 B
type Router struct {
	client              *jsonrpc.Stream
	spawn               WorkerSpawner
	exitGrace           time.Duration
	isJetski            bool
	version             string
	mu                  sync.Mutex
	initParams          jsontext.Value
	ephemeralAuthParams jsontext.Value
	authGen             uint64
	logoutGen           uint64
	workers             map[*worker]struct{}
	nextIndex           int
	sessions            map[string]*worker
	loading             map[string]*sessionLoad
	control             *worker
	pending             map[string]pendingRequest
	outbound            map[string]outboundCall
	internal            map[string]internalCall
	nextOutID           uint64
	shuttingDown        bool
	controlMu           sync.Mutex
}

func (r *Router) productLabel() string { /* pruned by linker; inferred */
	if r.isJetski {
		return "Jetski"
	}
	return "Antigravity"
}

// Binary VA: 0x102fa21b0 | Size: 176 B | Source: third_party/jetski/cli/acp/router/router.go:L192
func (r *Router) Run(ctx context.Context) error {
	for {
		raw, err := r.client.ReadMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		r.handleClientMessage(ctx, raw)
	}
}

// Binary VA: 0x102fa2260 | Size: 976 B | Source: third_party/jetski/cli/acp/router/router.go:L207
func (r *Router) Shutdown() {
	r.mu.Lock()
	r.shuttingDown = true
	workers := make([]*worker, 0, len(r.workers))
	for w := range r.workers {
		workers = append(workers, w)
	}
	r.mu.Unlock()

	for _, w := range workers {
		r.retire(w)
	}

	timer := time.NewTimer(r.exitGrace) /* inferred */
	defer timer.Stop()
	for _, w := range workers {
		select {
		case <-w.exited:
		case <-timer.C:
			for _, rem := range workers { /* inferred */
				rem.proc.kill()
			}
			for _, rem := range workers {
				<-rem.exited
			}
			return
		}
	}
}

// Binary VA: 0x102fa2630 | Size: 320 B | Source: third_party/jetski/cli/acp/router/router.go:L238
func idKey(id jsontext.Value) string {
	dec := json.NewDecoder(bytes.NewReader(id))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return strings.TrimSpace(string(id))
	}
	b, err := jsonv2.Marshal(v)
	if err != nil {
		return strings.TrimSpace(string(id))
	}
	return string(b)
}

// Binary VA: 0x102fa2770 | Size: 272 B | Source: third_party/jetski/cli/acp/router/router.go:L250
func isFrontID(id jsontext.Value) bool {
	var s string
	if err := jsonv2.Unmarshal(id, &s); err != nil {
		return false
	}
	return strings.HasPrefix(s, "front-") /* inferred */
}

// Binary VA: 0x102fa2880 | Size: 224 B | Source: third_party/jetski/cli/acp/router/router.go:L255
func sessionIDOf(params jsontext.Value) string {
	var p struct {
		SessionID      string `json:"sessionId"`
		SessionIDSnake string `json:"session_id"`
	}
	if err := jsonv2.Unmarshal(params, &p); err != nil {
		return ""
	}
	if p.SessionID != "" {
		return p.SessionID
	}
	return p.SessionIDSnake
}

// Binary VA: 0x102fa2960 | Size: 336 B | Source: third_party/jetski/cli/acp/router/router.go:L270
func withID(raw []byte, id jsontext.Value) ([]byte, error) {
	var m map[string]jsontext.Value
	if err := jsonv2.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	m["id"] = id
	return jsonv2.Marshal(m)
}

// Binary VA: 0x102fa2ab0 | Size: 624 B | Source: third_party/jetski/cli/acp/router/router.go:L279
func (r *Router) reply(ctx context.Context, id jsontext.Value, result any) {
	if len(id) == 0 || string(id) == "null" {
		return
	}
	idCopy := append(jsontext.Value(nil), id...) /* inferred */
	resp := jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      &idCopy,
		Result:  result,
	}
	data, err := jsonv2.Marshal(resp)
	if err != nil {
		log.ctxlogf(ctx, "front: failed to encode response: %v", err) /* inferred */
		return
	}
	if err := r.client.WriteMessage(data); err != nil {
		log.ctxlogf(ctx, "front: failed to write response: %v", err)
	}
}

// Binary VA: 0x102fa2d20 | Size: 656 B | Source: third_party/jetski/cli/acp/router/router.go:L298
func (r *Router) replyRPCError(ctx context.Context, id jsontext.Value, rpcErr *jsonrpc.Error) {
	if len(id) == 0 || string(id) == "null" || rpcErr == nil {
		return
	}
	idCopy := append(jsontext.Value(nil), id...)
	resp := jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      &idCopy,
		Error:   rpcErr,
	}
	data, err := jsonv2.Marshal(resp)
	if err != nil {
		log.ctxlogf(ctx, "front: failed to encode error response: %v", err)
		return
	}
	if err := r.client.WriteMessage(data); err != nil {
		log.ctxlogf(ctx, "front: failed to write error response: %v", err)
	}
}

// Binary VA: 0x102fa2fb0 | Size: 240 B | Source: third_party/jetski/cli/acp/router/router.go:L313
func (r *Router) replyRawResult(ctx context.Context, id jsontext.Value, raw jsontext.Value) {
	if len(raw) == 0 {
		raw = jsontext.Value("{}") /* inferred: 2-byte literal */
	}
	r.reply(ctx, id, raw)
}

// Binary VA: 0x102fa30a0 | Size: 832 B | Source: third_party/jetski/cli/acp/router/router.go:L323
func isEphemeralAuthParams(raw jsontext.Value) bool {
	if len(raw) == 0 {
		return false
	}
	var p schema.AuthenticateParams
	if err := jsonv2.Unmarshal(raw, &p); err != nil {
		return false
	}
	// [unrecovered] checks p.MethodID against auth method constants (e.g. "vertex-ai", "gemini-api-key", "terminal-login") and p.Meta keys
	_ = p.Meta
	return false
}

// Binary VA: 0x102fa33e0 | Size: 4544 B | Source: third_party/jetski/cli/acp/router/router.go:L342
func (r *Router) handleClientMessage(ctx context.Context, raw []byte) {
	var env jsonrpc.Envelope
	if err := jsonv2.Unmarshal(raw, &env); err != nil {
		log.ctxlogf(ctx, "front: dropping unparsable client message: %v", err)
		return
	}
	if env.Method == "" {
		r.routeClientResponse(ctx, env, raw)
		return
	}
	log.ctxlogf(ctx, "front: %s", env.Method)
	switch env.Method {
	case "initialize":
		r.mu.Lock()
		r.initParams = append([]byte(nil), env.Params...)
		r.mu.Unlock()
		var params schema.InitializeParams
		if err := jsonv2.Unmarshal(env.Params, &params); err != nil { /* inferred */
			r.replyRPCError(ctx, env.ID, &jsonrpc.Error{Code: -32602, Message: err.Error()}) /* inferred */
			return
		}
		support := auth.ClientSupportFrom(params)
		methods := auth.AdvertisedMethods(support)
		res := schema.BuildInitializeResult(r.isJetski, r.version, true, methods) /* inferred: httpMCP=true */
		r.reply(ctx, env.ID, res)
	case "session/new":
		// Binary VA: 0x102faaa90 | Size: 128 B | Source: third_party/jetski/cli/acp/router/router.go:L365
		go r.relayToNewWorker(ctx, env, raw, "")
	case "session/load", "session/resume":
		sid := sessionIDOf(env.Params)
		if w := r.ownerOf(sid); w != nil {
			r.relay(ctx, w, pendingLoad, sid, env, raw)
			return
		}
		if !r.beginLoad(sid) {
			r.replyRPCError(ctx, env.ID, &jsonrpc.Error{Code: -32000, Message: fmt.Sprintf("Session %s is already being loaded", sid)}) /* inferred code */
			return
		}
		// Binary VA: 0x102faaa10 | Size: 128 B | Source: third_party/jetski/cli/acp/router/router.go:L380
		go r.relayToNewWorker(ctx, env, raw, sid)
	case "session/prompt", "session/cancel", "session/set_mode", "session/set_model", "session/set_config_option":
		sid := sessionIDOf(env.Params)
		w := r.ownerOf(sid)
		if w == nil {
			r.replyRPCError(ctx, env.ID, &jsonrpc.Error{Code: -32000, Message: fmt.Sprintf("Session not found: %s", sid)}) /* inferred code */
			return
		}
		r.relay(ctx, w, pendingNormal, sid, env, raw)
	case "session/close":
		sid := sessionIDOf(env.Params)
		if r.closeLoading(sid) {
			r.reply(ctx, env.ID, struct{}{})
			return
		}
		w := r.ownerOf(sid)
		if w == nil {
			r.reply(ctx, env.ID, struct{}{})
			return
		}
		r.beginClose(sid, w)
		r.relay(ctx, w, pendingClose, sid, env, raw)
		if len(env.ID) == 0 || string(env.ID) == "null" {
			r.retire(w)
		}
	case "session/list":
		// Binary VA: 0x102faa9a0 | Size: 112 B | Source: third_party/jetski/cli/acp/router/router.go:L406
		go r.relayToAnyWorker(ctx, env, raw)
	case "authenticate":
		// Binary VA: 0x102faa930 | Size: 112 B | Source: third_party/jetski/cli/acp/router/router.go:L408
		go r.handleAuthenticate(ctx, env)
	case "logout":
		// Binary VA: 0x102faa8c0 | Size: 112 B | Source: third_party/jetski/cli/acp/router/router.go:L410
		go r.handleLogout(ctx, env, raw)
	default:
		r.replyRPCError(ctx, env.ID, &jsonrpc.Error{Code: -32601, Message: fmt.Sprintf("Method not found: %s", env.Method)}) /* inferred code */
	}
}

// Binary VA: 0x102fa45a0 | Size: 352 B | Source: third_party/jetski/cli/acp/router/router.go:L416
func (r *Router) ownerOf(sessionID string) *worker {
	r.mu.Lock()
	w := r.sessions[sessionID]
	r.mu.Unlock()
	if w != nil && w.servesSession() {
		return w
	}
	return nil
}

// Binary VA: 0x102fa4700 | Size: 1088 B | Source: third_party/jetski/cli/acp/router/router.go:L429
func (r *Router) relay(ctx context.Context, w *worker, kind pendingKind, sessionID string, env jsonrpc.Envelope, raw []byte) {
	if len(env.ID) > 0 && string(env.ID) != "null" {
		key := idKey(env.ID)
		r.mu.Lock()
		r.pending[key] = pendingRequest{worker: w, kind: kind, sessionID: sessionID}
		r.mu.Unlock()
	}
	if err := w.proc.stream.WriteMessage(raw); err != nil {
		log.ctxlogf(ctx, "front: worker %d: failed to relay %s: %v", w.index, env.Method, err)
		if len(env.ID) > 0 && string(env.ID) != "null" {
			r.takePending(idKey(env.ID))
		}
		r.replyRPCError(ctx, env.ID, &jsonrpc.Error{
			Code:    -32000, /* inferred */
			Message: fmt.Sprintf("Failed to reach the %s process of this session: %v", r.productLabel(), err),
		})
	}
}

// Binary VA: 0x102fa4b40 | Size: 464 B | Source: third_party/jetski/cli/acp/router/router.go:L445
func (r *Router) takePending(key string) (pendingRequest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	req, ok := r.pending[key]
	if ok {
		delete(r.pending, key)
	}
	return req, ok
}

// Binary VA: 0x102fa4d10 | Size: 784 B | Source: third_party/jetski/cli/acp/router/router.go:L456
func (r *Router) relayToNewWorker(ctx context.Context, env jsonrpc.Envelope, raw []byte, loadSessionID string) {
	w, err := r.startWorker(ctx)
	if err != nil {
		if loadSessionID != "" {
			r.endLoad(loadSessionID)
		}
		r.replyRPCError(ctx, env.ID, &jsonrpc.Error{
			Code:    -32000, /* inferred */
			Message: fmt.Sprintf("Failed to start a %s process for the session: %v", r.productLabel(), err),
		})
		return
	}
	kind := pendingNewSession /* inferred */
	if loadSessionID != "" {
		kind = pendingLoad
		r.mu.Lock()
		sl := r.loading[loadSessionID]
		r.mu.Unlock()
		if sl != nil && sl.closed { /* inferred */
			r.endLoad(loadSessionID)
			r.retire(w)
			r.reply(ctx, env.ID, struct{}{}) /* inferred */
			return
		}
	}
	r.relay(ctx, w, kind, loadSessionID, env, raw)
}

// Binary VA: 0x102fa5020 | Size: 432 B | Source: third_party/jetski/cli/acp/router/router.go:L475
func (r *Router) relayToAnyWorker(ctx context.Context, env jsonrpc.Envelope, raw []byte) {
	w, err := r.anyWorker(ctx)
	if err != nil {
		r.replyRPCError(ctx, env.ID, &jsonrpc.Error{
			Code:    -32000, /* inferred */
			Message: fmt.Sprintf("Failed to start a %s process: %v", r.productLabel(), err),
		})
		return
	}
	r.relay(ctx, w, pendingNormal, "", env, raw)
}

// Binary VA: 0x102fa51d0 | Size: 2464 B | Source: third_party/jetski/cli/acp/router/router.go:L484
func (r *Router) handleAuthenticate(ctx context.Context, env jsonrpc.Envelope) {
	w, err := r.anyWorker(ctx)
	if err != nil {
		r.replyRPCError(ctx, env.ID, &jsonrpc.Error{
			Code:    -32000, /* inferred */
			Message: fmt.Sprintf("Failed to start a %s process: %v", r.productLabel(), err),
		})
		return
	}
	paramsCopy := append([]byte(nil), env.Params...)
	respEnv, err := r.callWorkerEnvelope(ctx, w, "authenticate", paramsCopy)
	if err != nil {
		r.replyRPCError(ctx, env.ID, &jsonrpc.Error{
			Code:    -32000, /* inferred */
			Message: fmt.Sprintf("Failed to reach a %s process: %v", r.productLabel(), err),
		})
		return
	}
	if respEnv.Error != nil {
		r.replyRPCError(ctx, env.ID, respEnv.Error)
		return
	}

	r.mu.Lock()
	r.authGen++
	gen := r.authGen
	if isEphemeralAuthParams(paramsCopy) {
		r.ephemeralAuthParams = paramsCopy
	} else {
		r.ephemeralAuthParams = nil
	}
	var toSync []*worker
	for wk := range r.workers {
		if wk != w && !wk.isRetiring() {
			toSync = append(toSync, wk)
		}
	}
	r.mu.Unlock()

	for _, wk := range toSync {
		<-wk.ready
		if wk.isRetiring() {
			continue
		}
		wk.mu.Lock()
		wkGen := wk.authGen
		wk.mu.Unlock()
		if wkGen < gen { /* inferred */
			if _, err := r.callWorker(ctx, wk, "authenticate", paramsCopy); err != nil {
				log.ctxlogf(ctx, "front: worker %d failed to sync authenticate state: %v", wk.index, err)
			}
		}
	}
	r.replyRawResult(ctx, env.ID, respEnv.Result)
}

// Binary VA: 0x102fa5b70 | Size: 1296 B | Source: third_party/jetski/cli/acp/router/router.go:L548
func (r *Router) handleLogout(ctx context.Context, env jsonrpc.Envelope, raw []byte) {
	r.mu.Lock()
	r.logoutGen++
	r.ephemeralAuthParams = nil
	r.mu.Unlock()

	w, err := r.anyWorker(ctx)
	if err != nil {
		r.replyRPCError(ctx, env.ID, &jsonrpc.Error{
			Code:    -32000, /* inferred */
			Message: fmt.Sprintf("Failed to start a %s process: %v", r.productLabel(), err),
		})
		return
	}

	r.mu.Lock()
	var toRetire []*worker
	for wk := range r.workers {
		if wk != w {
			toRetire = append(toRetire, wk)
		}
	}
	r.mu.Unlock()

	for _, wk := range toRetire {
		r.retire(wk)
	}
	r.relay(ctx, w, pendingNormal, "", env, raw)
}

// Binary VA: 0x102fa6080 | Size: 1136 B | Source: third_party/jetski/cli/acp/router/router.go:L575
func (r *Router) anyWorker(ctx context.Context) (*worker, error) {
	r.mu.Lock()
	for wk := range r.workers {
		if !wk.servesSession() && !wk.isRetiring() { /* inferred */
			r.mu.Unlock()
			return wk, nil
		}
	}
	r.mu.Unlock()

	r.controlMu.Lock()
	defer r.controlMu.Unlock()

	r.mu.Lock()
	ctrl := r.control
	r.mu.Unlock()
	if ctrl != nil && !ctrl.isRetiring() {
		select {
		case <-ctrl.exited:
		default:
			return ctrl, nil
		}
	}
	w, err := r.startWorker(ctx)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.control = w
	r.mu.Unlock()
	return w, nil
}

// Binary VA: 0x102fa64f0 | Size: 3024 B | Source: third_party/jetski/cli/acp/router/router.go:L611
func (r *Router) startWorker(ctx context.Context) (*worker, error) {
	r.mu.Lock()
	if r.shuttingDown {
		r.mu.Unlock()
		return nil, errors.New("Server is shutting down")
	}
	idx := r.nextIndex
	r.nextIndex++
	r.mu.Unlock()

	proc, err := r.spawn(ctx, idx)
	if err != nil {
		return nil, err
	}
	w := &worker{
		index:  idx,
		proc:   proc,
		ready:  make(chan struct{}),
		exited: make(chan struct{}),
	}

	r.mu.Lock()
	if r.shuttingDown {
		r.mu.Unlock()
		close(w.ready)
		// Binary VA: 0x102faac30 | Size: 64 B | Source: third_party/jetski/cli/acp/router/router.go:L638
		go r.readWorker(ctx, w)
		r.retire(w)
		proc.kill()
		return nil, errors.New("Server is shutting down")
	}
	if r.logoutGen > 0 { /* inferred: logout check */
		// [unrecovered] checks logoutGen delta vs initial
	}
	r.workers[w] = struct{}{}
	initParams := append([]byte(nil), r.initParams...)
	r.mu.Unlock()

	// Binary VA: 0x102faabf0 | Size: 64 B | Source: third_party/jetski/cli/acp/router/router.go:L647
	go r.readWorker(ctx, w)

	if len(initParams) == 0 {
		initParams = []byte("{}") /* inferred */
	}
	if _, err := r.callWorker(ctx, w, "initialize", initParams); err != nil {
		w.readyErr = err
		close(w.ready)
		log.ctxlogf(ctx, "front: worker %d failed to initialize: %v", w.index, err)
		r.retire(w)
		return nil, err
	}

	r.mu.Lock()
	ephemeral := append([]byte(nil), r.ephemeralAuthParams...)
	gen := r.authGen
	r.mu.Unlock()

	if len(ephemeral) > 0 {
		if _, err := r.callWorker(ctx, w, "authenticate", ephemeral); err != nil {
			w.readyErr = err
			close(w.ready)
			log.ctxlogf(ctx, "front: worker %d failed to initialize: %v", w.index, err)
			r.retire(w)
			return nil, err
		}
		w.mu.Lock()
		w.authGen = gen
		w.mu.Unlock()
	}

	close(w.ready)
	log.ctxlogf(ctx, "front: worker %d ready", w.index)
	return w, nil
}

// Binary VA: 0x102fa70c0 | Size: 448 B | Source: third_party/jetski/cli/acp/router/router.go:L694
func (r *Router) callWorker(ctx context.Context, w *worker, method string, params jsontext.Value) (jsontext.Value, error) {
	env, err := r.callWorkerEnvelope(ctx, w, method, params)
	if err != nil {
		return nil, err
	}
	if env.Error != nil {
		return nil, fmt.Errorf("%s: %s (code %d)", method, env.Error.Message, env.Error.Code)
	}
	return env.Result, nil
}

// Binary VA: 0x102fa7280 | Size: 3360 B | Source: third_party/jetski/cli/acp/router/router.go:L705
func (r *Router) callWorkerEnvelope(ctx context.Context, w *worker, method string, params jsontext.Value) (jsonrpc.Envelope, error) {
	r.mu.Lock()
	r.nextOutID++
	idNum := r.nextOutID
	id := jsontext.Value(strconv.AppendQuote(nil, "front-"+strconv.FormatUint(idNum, 10))) /* inferred */
	ch := make(chan jsonrpc.Envelope, 1)
	key := idKey(id)
	r.internal[key] = internalCall{worker: w, done: ch}
	r.mu.Unlock()

	req := jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	data, err := jsonv2.Marshal(req)
	if err != nil {
		r.mu.Lock()
		delete(r.internal, key)
		r.mu.Unlock()
		return jsonrpc.Envelope{}, err
	}
	if err := w.proc.stream.WriteMessage(data); err != nil {
		r.mu.Lock()
		delete(r.internal, key)
		r.mu.Unlock()
		return jsonrpc.Envelope{}, err
	}

	const workerCallTimeout = 30 * time.Second /* inferred */
	timer := time.NewTimer(workerCallTimeout)
	defer timer.Stop()
	select {
	case resp := <-ch:
		return resp, nil
	case <-w.exited:
		select {
		case resp := <-ch:
			return resp, nil
		default:
		}
		r.mu.Lock()
		delete(r.internal, key)
		r.mu.Unlock()
		return jsonrpc.Envelope{}, errors.New("worker exited")
	case <-timer.C:
		r.mu.Lock()
		delete(r.internal, key)
		r.mu.Unlock()
		return jsonrpc.Envelope{}, fmt.Errorf("%s: no response from the worker within %s", method, workerCallTimeout)
	case <-ctx.Done():
		r.mu.Lock()
		delete(r.internal, key)
		r.mu.Unlock()
		return jsonrpc.Envelope{}, ctx.Err()
	}
}

// Binary VA: 0x102fa7fa0 | Size: 736 B | Source: third_party/jetski/cli/acp/router/router.go:L758
func (r *Router) readWorker(ctx context.Context, w *worker) {
	for {
		raw, err := w.proc.stream.ReadMessage()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.ctxlogf(ctx, "front: worker %d: read failed: %v", w.index, err)
			}
			break
		}
		r.handleWorkerMessage(ctx, w, raw)
	}
	if err := w.proc.wait(); err != nil {
		log.ctxlogf(ctx, "front: worker %d exited: %v", w.index, err)
	} else {
		log.ctxlogf(ctx, "front: worker %d exited", w.index)
	}
	r.onWorkerExit(ctx, w)
}

// Binary VA: 0x102fa8280 | Size: 1856 B | Source: third_party/jetski/cli/acp/router/router.go:L778
func (r *Router) handleWorkerMessage(ctx context.Context, w *worker, raw []byte) {
	var env jsonrpc.Envelope
	if err := jsonv2.Unmarshal(raw, &env); err != nil {
		log.ctxlogf(ctx, "front: worker %d: dropping unparsable message: %v", w.index, err)
		return
	}
	if env.Method == "" {
		if isFrontID(env.ID) {
			r.resolveInternal(env)
			return
		}
		key := idKey(env.ID)
		req, ok := r.takePending(key)
		if !ok {
			log.ctxlogf(ctx, "front: worker %d: dropping response to unknown request %s", w.index, key)
			return
		}
		switch req.kind {
		case pendingNewSession:
			sid := sessionIDOf(env.Result)
			r.settleNewSession(ctx, w, sid)
		case pendingLoad:
			r.settleLoad(ctx, w, req.sessionID, env.Error != nil)
		case pendingClose:
			if w.session() != "" { /* inferred */
				r.retire(w)
			} else {
				r.retire(w)
			}
		}
		r.writeToClient(ctx, raw)
		return
	}
	if len(env.ID) == 0 || string(env.ID) == "null" {
		r.writeToClient(ctx, raw)
		return
	}
	r.mu.Lock()
	r.nextOutID++
	newID := jsontext.Value(strconv.AppendQuote(nil, "front-"+strconv.FormatUint(r.nextOutID, 10))) /* inferred */
	r.outbound[idKey(newID)] = outboundCall{worker: w, origID: env.ID}
	r.mu.Unlock()
	rewritten, err := withID(raw, newID)
	if err != nil {
		log.ctxlogf(ctx, "front: worker %d: failed to rewrite request id: %v", w.index, err)
		return
	}
	r.writeToClient(ctx, rewritten)
}

// Binary VA: 0x102fa89c0 | Size: 224 B | Source: third_party/jetski/cli/acp/router/router.go:L835
func (r *Router) writeToClient(ctx context.Context, raw []byte) {
	if err := r.client.WriteMessage(raw); err != nil {
		log.ctxlogf(ctx, "front: failed to write to the client: %v", err)
	}
}

// Binary VA: 0x102fa8aa0 | Size: 448 B | Source: third_party/jetski/cli/acp/router/router.go:L841
func (r *Router) resolveInternal(env jsonrpc.Envelope) {
	key := idKey(env.ID)
	r.mu.Lock()
	ic, ok := r.internal[key]
	if ok {
		delete(r.internal, key)
	}
	r.mu.Unlock()
	if ok {
		ic.done <- env
	}
}

// Binary VA: 0x102fa8c60 | Size: 1056 B | Source: third_party/jetski/cli/acp/router/router.go:L852
func (r *Router) routeClientResponse(ctx context.Context, env jsonrpc.Envelope, raw []byte) {
	key := idKey(env.ID)
	r.mu.Lock()
	oc, ok := r.outbound[key]
	if ok {
		delete(r.outbound, key)
	}
	r.mu.Unlock()
	if !ok {
		log.ctxlogf(ctx, "front: dropping client response to unknown request %s", key)
		return
	}
	rewritten, err := withID(raw, oc.origID)
	if err != nil {
		log.ctxlogf(ctx, "front: failed to restore request id: %v", err)
		return
	}
	if err := oc.worker.proc.stream.WriteMessage(rewritten); err != nil {
		log.ctxlogf(ctx, "front: worker %d: failed to relay response: %v", oc.worker.index, err)
	}
}

// Binary VA: 0x102fa9080 | Size: 96 B | Source: third_party/jetski/cli/acp/router/router.go:L875
func (r *Router) settleNewSession(ctx context.Context, w *worker, sessionID string) {
	if sessionID == "" {
		r.retire(w)
		return
	}
	r.bind(ctx, sessionID, w)
}

// Binary VA: 0x102fa90e0 | Size: 432 B | Source: third_party/jetski/cli/acp/router/router.go:L883
func (r *Router) settleLoad(ctx context.Context, w *worker, sessionID string, failed bool) {
	r.mu.Lock()
	sl := r.loading[sessionID]
	delete(r.loading, sessionID)
	r.mu.Unlock()
	if failed || (sl != nil && sl.closed) {
		r.retire(w)
		return
	}
	r.bind(ctx, sessionID, w)
}

// Binary VA: 0x102fa9290 | Size: 416 B | Source: third_party/jetski/cli/acp/router/router.go:L902
func (r *Router) beginLoad(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.loading[sessionID]; exists {
		return false
	}
	r.loading[sessionID] = &sessionLoad{}
	return true
}

// Binary VA: 0x102fa9430 | Size: 336 B | Source: third_party/jetski/cli/acp/router/router.go:L912
func (r *Router) closeLoading(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	sl := r.loading[sessionID]
	if sl == nil {
		return false
	}
	sl.closed = true
	return true
}

// Binary VA: 0x102fa9580 | Size: 288 B | Source: third_party/jetski/cli/acp/router/router.go:L923
func (r *Router) endLoad(sessionID string) {
	r.mu.Lock()
	delete(r.loading, sessionID)
	r.mu.Unlock()
}

// Binary VA: 0x102fa96a0 | Size: 1024 B | Source: third_party/jetski/cli/acp/router/router.go:L929
func (r *Router) bind(ctx context.Context, sessionID string, w *worker) {
	r.mu.Lock()
	prev := r.sessions[sessionID]
	r.sessions[sessionID] = w
	r.mu.Unlock()

	w.mu.Lock()
	w.sessionID = sessionID
	w.mu.Unlock()

	if prev == nil {
		log.ctxlogf(ctx, "front: session %s served by worker %d", sessionID, w.index)
	} else if prev != w {
		log.ctxlogf(ctx, "front: session %s moves from worker %d to worker %d", sessionID, prev.index, w.index)
		r.retire(prev)
	}
}

// Binary VA: 0x102fa9aa0 | Size: 256 B | Source: third_party/jetski/cli/acp/router/router.go:L944
func (r *Router) beginClose(sessionID string, w *worker) {
	r.unbind(sessionID, w)
	w.mu.Lock()
	w.closing = true
	w.mu.Unlock()
}

// Binary VA: 0x102fa9ba0 | Size: 304 B | Source: third_party/jetski/cli/acp/router/router.go:L951
func (r *Router) unbind(sessionID string, w *worker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessions[sessionID] == w {
		delete(r.sessions, sessionID)
	}
}

// Binary VA: 0x102fa9cd0 | Size: 768 B | Source: third_party/jetski/cli/acp/router/router.go:L959
func (r *Router) retire(w *worker) {
	w.mu.Lock()
	if w.retiring {
		w.mu.Unlock()
		return
	}
	w.retiring = true
	w.mu.Unlock()

	log.ctxlogf(context.Background(), "front: retiring worker %d", w.index) /* inferred ctx */
	if err := w.proc.closeInput(); err != nil {
		log.ctxlogf(context.Background(), "front: worker %d: failed to close input: %v", w.index, err)
	}
	// Binary VA: 0x102faae00 | Size: 352 B | Source: third_party/jetski/cli/acp/router/router.go:L971
	go func() {
		timer := time.NewTimer(r.exitGrace)
		defer timer.Stop()
		select {
		case <-w.exited:
		case <-timer.C:
			log.ctxlogf(context.Background(), "front: worker %d did not exit in %s, killing it", w.index, r.exitGrace)
			w.proc.kill()
		}
	}()
}

// Binary VA: 0x102fa9fd0 | Size: 1888 B | Source: third_party/jetski/cli/acp/router/router.go:L981
func (r *Router) onWorkerExit(ctx context.Context, w *worker) {
	// Binary VA: 0x102faaf60 | Size: 64 B | Source: third_party/jetski/cli/acp/router/router.go:L982
	w.exitOnce.Do(func() {
		close(w.exited)
	})

	r.mu.Lock()
	delete(r.workers, w)
	if r.control == w {
		r.control = nil
	}
	for sid, wk := range r.sessions {
		if wk == w {
			delete(r.sessions, sid)
		}
	}
	for sid, sl := range r.loading {
		if sl.worker == w {
			delete(r.loading, sid)
		}
	}
	var failedPending []jsontext.Value
	for key, req := range r.pending {
		if req.worker == w {
			delete(r.pending, key)
			failedPending = append(failedPending, jsontext.Value(key))
		}
	}
	var failedInternal []internalCall
	for key, ic := range r.internal {
		if ic.worker == w {
			delete(r.internal, key)
			failedInternal = append(failedInternal, ic)
		}
	}
	for key, oc := range r.outbound {
		if oc.worker == w {
			delete(r.outbound, key)
		}
	}
	r.mu.Unlock()

	msg := fmt.Sprintf("The %s process serving this session exited unexpectedly", r.productLabel())
	rpcErr := &jsonrpc.Error{Code: -32000, Message: msg} /* inferred code */
	for _, id := range failedPending {
		r.replyRPCError(ctx, id, rpcErr)
	}
	for _, ic := range failedInternal {
		ic.done <- jsonrpc.Envelope{Error: rpcErr}
	}
}

// Binary VA: 0x102faf190 | Size: 1184 B | Source: third_party/jetski/cli/acp/router/router.go:L1030
func SpawnWorkerProcesses(extraArgs ...string) WorkerSpawner { /* inferred signature */
	return func(ctx context.Context, index int) (*WorkerProcess, error) {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("locating the executable: %w", err)
		}
		cmd := jexec.HostCommand(ctx, exe, extraArgs...) /* inferred args */
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			_ = stdin.Close()
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("starting worker: %w", err)
		}
		log.InfoContextf(ctx, "front: started worker %d (pid %d)", index, cmd.Process.Pid)
		return &WorkerProcess{
			stream:     jsonrpc.NewStream(stdout, stdin),
			closeInput: stdin.Close,
			kill: func() {
				// Binary VA: 0x102faf630 | Size: 64 B | Source: third_party/jetski/cli/acp/router/router.go:L1053
				_ = cmd.Process.Kill()
			},
			wait: cmd.Wait,
		}, nil
	}
}
