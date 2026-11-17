package swarmrun

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/cortex"
	"antigravity.dev/agy-sdk/pkg/swarm"
)

// Config configures a local multi-agent SwarmRun Orchestrator.
type Config struct {
	Dir           string
	WorkspaceRoot string
	Title         string
	Model         string
	Streamer      cortex.Streamer
	StreamerFor   func(agentName, model string) cortex.Streamer
	Guard         *cortex.PermissionGuard
	SystemPrompt  string
	WorkerPrompt  string
	MaxSteps      int
	OnEvent       func(AgentEvent)
}

// AgentEvent pairs a Cortex lifecycle event with the emitting swarm member name.
type AgentEvent struct {
	Agent string
	Event cortex.Event
}

// SwarmRunResult holds the final state and summary of an Orchestrator.Run invocation.
type SwarmRunResult struct {
	Title     string
	Summary   *cortex.TurnSummary
	FinalText string
	HeadSeq   uint64
	Roster    *swarm.Roster
	Messages  []swarm.Message
}

type agentHandle struct {
	name    string
	role    string
	model   string
	addedBy string
	wakeCh  chan swarm.Doorbell
	cancel  context.CancelFunc
	done    chan struct{}
}

// Orchestrator coordinates a local Native Swarm backed by FileStore, WakeEngine, and Cortex Engines.
type Orchestrator struct {
	cfg   Config
	store *swarm.FileStore
	wake  *swarm.WakeEngine

	mu     sync.Mutex
	title  string
	ctx    context.Context
	cancel context.CancelFunc
	agents map[string]*agentHandle
	wg     sync.WaitGroup
}

// NewOrchestrator initializes a swarm directory, FileStore, and WakeEngine.
func NewOrchestrator(cfg Config) (*Orchestrator, error) {
	if strings.TrimSpace(cfg.Dir) == "" {
		return nil, fmt.Errorf("swarm dir must not be empty")
	}
	if cfg.Streamer == nil && cfg.StreamerFor == nil {
		return nil, fmt.Errorf("streamer or streamer factory must be provided")
	}
	if cfg.Title == "" {
		cfg.Title = "swarm"
	}

	store, err := swarm.Create(cfg.Dir, cfg.Title, "coordinator", cfg.Model)
	if err != nil {
		return nil, err
	}
	wake := swarm.NewWakeEngine(store, "")

	o := &Orchestrator{
		cfg:   cfg,
		store: store,
		wake:  wake,
		title: cfg.Title,
		agents: map[string]*agentHandle{
			"coordinator": {
				name:   "coordinator",
				role:   "coordinator",
				model:  cfg.Model,
				wakeCh: make(chan swarm.Doorbell, 16),
			},
		},
	}
	return o, nil
}

// Store returns the underlying swarm FileStore.
func (o *Orchestrator) Store() *swarm.FileStore {
	return o.store
}

// Run executes the coordinator turn for goal and cleanly shuts down all worker goroutines on exit.
func (o *Orchestrator) Run(ctx context.Context, goal string) (*SwarmRunResult, error) {
	orchCtx, orchCancel := context.WithCancel(ctx)
	o.mu.Lock()
	o.ctx = orchCtx
	o.cancel = orchCancel
	o.mu.Unlock()

	o.wg.Add(1)
	go o.dispatchDoorbells()

	defer func() {
		orchCancel()
		o.cancelAllWorkers()
		o.wake.Close()
		o.wg.Wait()
	}()

	eng := o.newEngine("coordinator", o.cfg.Model, o.coordinatorSystemPrompt())
	_, summary, err := eng.RunTurn(orchCtx, nil, goal)
	if err != nil {
		return nil, err
	}

	finalText := ""
	if summary != nil {
		finalText = summary.FinalText
	}

	return &SwarmRunResult{
		Title:     o.getTitle(),
		Summary:   summary,
		FinalText: finalText,
		HeadSeq:   o.store.Head(),
		Roster:    o.store.RosterSnapshot(),
		Messages:  o.store.Tail(0),
	}, nil
}

func (o *Orchestrator) dispatchDoorbells() {
	defer o.wg.Done()
	for db := range o.wake.Doorbells() {
		o.mu.Lock()
		h := o.agents[db.Member]
		o.mu.Unlock()
		if h == nil || h.wakeCh == nil {
			continue
		}
		select {
		case h.wakeCh <- db:
		default:
		}
	}
}

func (o *Orchestrator) cancelAllWorkers() {
	o.mu.Lock()
	handles := make([]*agentHandle, 0, len(o.agents))
	for name, h := range o.agents {
		if name == "coordinator" || h == nil {
			continue
		}
		handles = append(handles, h)
	}
	o.mu.Unlock()

	for _, h := range handles {
		if h.cancel != nil {
			h.cancel()
		}
	}
}

func (o *Orchestrator) newEngine(agentName, model, sysPrompt string) *cortex.Engine {
	if model == "" {
		model = o.cfg.Model
	}
	reg := cortex.NewDefaultRegistry()
	registerSwarmTools(reg, o, agentName)

	guard := o.guardForAgent()
	var sysContent *aicode.Content
	if sysPrompt != "" {
		sysContent = &aicode.Content{
			Role:  "system",
			Parts: []*aicode.Part{{Text: sysPrompt}},
		}
	}

	var onEvent cortex.EventCallback
	if o.cfg.OnEvent != nil {
		onEvent = func(ev cortex.Event) {
			o.cfg.OnEvent(AgentEvent{Agent: agentName, Event: ev})
		}
	}

	return &cortex.Engine{
		Streamer:     o.streamerFor(agentName, model),
		Model:        model,
		Registry:     reg,
		Guard:        guard,
		SystemPrompt: sysContent,
		MaxSteps:     o.cfg.MaxSteps,
		OnEvent:      onEvent,
	}
}

func (o *Orchestrator) streamerFor(agentName, model string) cortex.Streamer {
	if o.cfg.StreamerFor != nil {
		if s := o.cfg.StreamerFor(agentName, model); s != nil {
			return s
		}
	}
	return o.cfg.Streamer
}

func (o *Orchestrator) guardForAgent() *cortex.PermissionGuard {
	if o.cfg.Guard != nil {
		cp := *o.cfg.Guard
		cp.AdditionalDirs = append([]string(nil), o.cfg.Guard.AdditionalDirs...)
		return &cp
	}
	return cortex.NewPermissionGuard(o.cfg.WorkspaceRoot, cortex.ModeAgent)
}

func (o *Orchestrator) coordinatorSystemPrompt() string {
	if o.cfg.SystemPrompt != "" {
		return o.cfg.SystemPrompt
	}
	return "You are the swarm coordinator. Use swarm, post_message, read_board, and swarm_wait to delegate work, wait for completion, and clean up workers before returning your final summary."
}

func (o *Orchestrator) workerSystemPrompt(name string) string {
	if o.cfg.WorkerPrompt != "" {
		return o.cfg.WorkerPrompt
	}
	return fmt.Sprintf("You are swarm worker %s. Complete your assigned task in the workspace, report your result via post_message mentioning @coordinator (or your requester), and stop.", name)
}

func (o *Orchestrator) getTitle() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.title
}

func (o *Orchestrator) toolStatus() (cortex.ToolResult, error) {
	snap := o.store.RosterSnapshot()
	members := make(map[string]any, len(snap.Members))
	for _, name := range snap.SortedNames() {
		m := snap.Members[name]
		members[name] = map[string]any{
			"role":            m.Role,
			"phase":           m.Phase,
			"desired_state":   m.DesiredState,
			"effective_model": m.EffectiveModel(),
			"wake_mode":       m.WakeMode,
			"added_by":        m.AddedBy,
		}
	}
	return cortex.ToolResult{
		Output: map[string]any{
			"title":    o.getTitle(),
			"head_seq": o.store.Head(),
			"members":  members,
		},
	}, nil
}

func (o *Orchestrator) toolAdd(actor, name, brief, model string) (cortex.ToolResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return cortex.ToolResult{}, fmt.Errorf("worker name must not be empty")
	}
	if name == "coordinator" {
		return cortex.ToolResult{}, fmt.Errorf("cannot add reserved member name %q", name)
	}
	if strings.TrimSpace(brief) == "" {
		return cortex.ToolResult{}, fmt.Errorf("brief must not be empty")
	}
	if model == "" {
		model = o.cfg.Model
	}

	o.mu.Lock()
	if existing := o.agents[name]; existing != nil {
		o.mu.Unlock()
		return cortex.ToolResult{}, fmt.Errorf("member %q already exists", name)
	}
	parentCtx := o.ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	workerCtx, workerCancel := context.WithCancel(parentCtx)
	h := &agentHandle{
		name:    name,
		role:    "worker",
		model:   model,
		addedBy: actor,
		wakeCh:  make(chan swarm.Doorbell, 16),
		cancel:  workerCancel,
		done:    make(chan struct{}),
	}
	o.agents[name] = h
	o.mu.Unlock()

	briefText := brief
	if !swarm.Mentions(briefText, name) {
		briefText = "@" + name + " " + brief
	}

	msg, err := o.store.AddMembers(swarm.AddMembersRequest{
		Actor: actor,
		Members: []swarm.NewMember{{
			Name:          name,
			Role:          "worker",
			DesiredModel:  model,
			WorkspaceMode: "inherit",
			Workspace:     o.cfg.WorkspaceRoot,
		}},
		BriefText: briefText,
	})
	if err != nil {
		o.mu.Lock()
		delete(o.agents, name)
		o.mu.Unlock()
		workerCancel()
		close(h.done)
		return cortex.ToolResult{}, err
	}

	o.wg.Add(1)
	go o.runWorker(workerCtx, h, briefText)

	return cortex.ToolResult{
		Output: map[string]any{
			"status":    "added",
			"name":      name,
			"model":     model,
			"brief_seq": msg.Seq,
		},
	}, nil
}

func (o *Orchestrator) runWorker(ctx context.Context, h *agentHandle, initialBrief string) {
	defer o.wg.Done()
	defer close(h.done)

	eng := o.newEngine(h.name, h.model, o.workerSystemPrompt(h.name))
	history, _, err := eng.RunTurn(ctx, nil, initialBrief)
	if err != nil || ctx.Err() != nil {
		return
	}

	for ctx.Err() == nil {
		snap := o.store.RosterSnapshot()
		if m := snap.Members[h.name]; m == nil || m.IsKilled() {
			return
		}

		since := o.store.Head()
		if m := snap.Members[h.name]; m != nil && m.LastReadSeq > 0 {
			since = m.LastReadSeq
		}

		msgs, err := o.waitForMessages(ctx, h.name, since, true)
		if err != nil || ctx.Err() != nil {
			return
		}
		if len(msgs) == 0 {
			continue
		}

		var sb strings.Builder
		sb.WriteString("New board messages:\n")
		for _, m := range msgs {
			fmt.Fprintf(&sb, "#%d %s: %s\n", m.Seq, m.Agent, m.Text)
		}
		history, _, err = eng.RunTurn(ctx, history, sb.String())
		if err != nil || ctx.Err() != nil {
			return
		}
	}
}

func (o *Orchestrator) toolModel(target, model string) (cortex.ToolResult, error) {
	if strings.TrimSpace(model) == "" {
		return cortex.ToolResult{}, fmt.Errorf("model must not be empty")
	}
	o.mu.Lock()
	if h := o.agents[target]; h != nil {
		h.model = model
	}
	o.mu.Unlock()

	return cortex.ToolResult{
		Output: map[string]any{
			"status": "switched",
			"name":   target,
			"model":  model,
		},
	}, nil
}

func (o *Orchestrator) toolKill(ctx context.Context, actor, name string) (cortex.ToolResult, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "coordinator" {
		return cortex.ToolResult{}, fmt.Errorf("cannot kill %q", name)
	}

	o.mu.Lock()
	h := o.agents[name]
	if h != nil && actor != "coordinator" && h.addedBy != actor {
		o.mu.Unlock()
		return cortex.ToolResult{}, fmt.Errorf("%s cannot kill %q (added by %s)", actor, name, h.addedBy)
	}
	delete(o.agents, name)
	o.mu.Unlock()

	if h != nil && h.cancel != nil {
		h.cancel()
	}
	if err := o.store.Kill([]string{name}); err != nil {
		return cortex.ToolResult{}, err
	}
	if h != nil && h.done != nil {
		select {
		case <-h.done:
		case <-ctx.Done():
			return cortex.ToolResult{}, ctx.Err()
		}
	}

	return cortex.ToolResult{
		Output: map[string]any{
			"status": "killed",
			"name":   name,
		},
	}, nil
}

func (o *Orchestrator) toolTitle(actor, title string) (cortex.ToolResult, error) {
	if actor != "coordinator" {
		return cortex.ToolResult{}, fmt.Errorf("only coordinator can set swarm title")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return cortex.ToolResult{}, fmt.Errorf("title must not be empty")
	}
	o.mu.Lock()
	o.title = title
	o.mu.Unlock()
	return cortex.ToolResult{
		Output: map[string]any{
			"status": "updated",
			"title":  title,
		},
	}, nil
}

func (o *Orchestrator) waitForMessages(ctx context.Context, agentName string, since uint64, mentionsOnly bool) ([]swarm.Message, error) {
	o.mu.Lock()
	h := o.agents[agentName]
	o.mu.Unlock()
	if h == nil {
		return nil, fmt.Errorf("unknown agent %q", agentName)
	}

	mode := swarm.WakeModeMentions
	if !mentionsOnly {
		mode = swarm.WakeModeAnything
	}

	if err := o.store.UpdateMemberLocked(agentName, func(st *swarm.MemberState) {
		st.WakeMode = mode
		st.WaitSince = since
	}); err != nil {
		return nil, err
	}

	if msgs := o.matchingMessagesSince(agentName, since, mentionsOnly); len(msgs) > 0 {
		o.finishWait(agentName, msgs)
		return msgs, nil
	}

	for {
		select {
		case <-ctx.Done():
			_ = o.store.UpdateMemberLocked(agentName, func(st *swarm.MemberState) {
				st.WakeMode = swarm.WakeModeWorking
			})
			return nil, ctx.Err()
		case db, ok := <-h.wakeCh:
			if !ok {
				return nil, context.Canceled
			}
			if db.Seq <= since {
				continue
			}
			msgs := o.store.ReadSince(since, 0)
			if len(msgs) == 0 {
				continue
			}
			o.finishWait(agentName, msgs)
			return msgs, nil
		}
	}
}

func (o *Orchestrator) finishWait(agentName string, msgs []swarm.Message) {
	lastSeq := msgs[len(msgs)-1].Seq
	_ = o.store.UpdateMemberLocked(agentName, func(st *swarm.MemberState) {
		st.WakeMode = swarm.WakeModeWorking
		st.CatchUp(lastSeq)
	})
}

func (o *Orchestrator) matchingMessagesSince(agentName string, since uint64, mentionsOnly bool) []swarm.Message {
	msgs := o.store.ReadSince(since, 0)
	if len(msgs) == 0 {
		return nil
	}
	snap := o.store.RosterSnapshot()
	m := snap.Members[agentName]
	var chans []string
	if m != nil {
		chans = m.EffectiveChannels(agentName)
	}
	channelActive := func(ch string) bool {
		if snap == nil || snap.Channels == nil {
			return false
		}
		c := snap.Channels[ch]
		return c != nil && !c.Archived
	}

	for _, msg := range msgs {
		if msg.Agent == agentName || msg.Kind == swarm.MessageKindBrief {
			continue
		}
		if m != nil && !m.Sees(agentName, msg, channelActive) {
			continue
		}
		if !mentionsOnly {
			return msgs
		}
		if swarm.MentionsIn(msg.Text, agentName, msg.Channel, chans, channelActive) {
			return msgs
		}
	}
	return nil
}
