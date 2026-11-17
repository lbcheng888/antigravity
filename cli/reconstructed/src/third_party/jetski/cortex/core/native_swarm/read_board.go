// Binary VA: 0x101fecd80 | Size: 288 B | Source: third_party/jetski/cortex/core/native_swarm/read_board.go:L91
package nativeswarm

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"

	"google3/third_party/jetski/cortex/core/core"
	"google3/third_party/jetski/cortex/core/native_swarm/board/board"
	"google3/third_party/jetski/cortex/cortex_pb/cortex_go_proto"
)

// Binary VA: 0x10a676208 | Size: 64 B
type readBoardArgs struct {
	Limit   *int     `json:"limit,omitempty"`
	Seqs    []uint64 `json:"seqs,omitempty"`
	Since   *uint64  `json:"since,omitempty"`
	Status  bool     `json:"status,omitempty"`
	Channel string   `json:"channel,omitempty"`
}

// Binary VA: 0x10a3418c0 | Size: 32 B
type readBoardOutput struct {
	Messages []board.Message `json:"messages"`
	Head     uint64          `json:"head"`
}

// Binary VA: 0x10a341950 | Size: 32 B
type readBoardTool struct {
	member   *swarmMember
	channels channelFlags
}

// Binary VA: 0x10a6762e0 | Size: 64 B
type statusChannel struct {
	Name      string `json:"name"`
	Topic     string `json:"topic,omitempty"`
	CreatedBy string `json:"created_by"`
	Archived  bool   `json:"archived,omitempty"`
	Members   int    `json:"members"`
}

// Binary VA: 0x10a95a128 | Size: 224 B
type statusMember struct {
	Name             string         `json:"name"`
	Role             string         `json:"role"`
	Model            string         `json:"model"`
	ModelDisplayName string         `json:"model_display_name,omitempty"`
	State            string         `json:"state"`
	Killed           bool           `json:"killed"`
	WakeMode         board.WakeMode `json:"wake_mode,omitempty"`
	WaitSince        *uint64        `json:"wait_since,omitempty"`
	LastReadSeq      uint64         `json:"last_read_seq"`
	LastPostSeq      uint64         `json:"last_post_seq,omitempty"`
	LastPostTime     string         `json:"last_post_time,omitempty"`
	Workspace        string         `json:"workspace,omitempty"`
	Node             string         `json:"node,omitempty"`
	LastSeen         string         `json:"last_seen,omitempty"`
	Channels         []string       `json:"channels,omitempty"`
	Unread           *int           `json:"unread,omitempty"`
}

// Binary VA: 0x10a860eb8 | Size: 128 B
type statusOutput struct {
	Title       string          `json:"title"`
	Board       string          `json:"board"`
	Swarm       string          `json:"swarm,omitempty"`
	OpenBoard   string          `json:"open_board,omitempty"`
	Head        uint64          `json:"head"`
	TailFromSeq uint64          `json:"tail_from_seq"`
	Members     []statusMember  `json:"members"`
	Channels    []statusChannel `json:"channels,omitempty"`
}

// Binary VA: 0x101fecd80 | Size: 288 B | Source: third_party/jetski/cortex/core/native_swarm/read_board.go:L91
func NewReadBoardTool(member *swarmMember /* inferred */) core.Tool {
	// [unrecovered] openSwarm(context.Background(), ...)
	_ = openSwarm
	return &readBoardTool{
		member:   member,
		channels: channelFlagsOf(member /* inferred */),
	}
}

// Binary VA: 0x101fecea0 | Size: 16 B | Source: third_party/jetski/cortex/core/native_swarm/read_board.go:L100
func (t *readBoardTool) Name() string {
	return "read_board"
}

// Binary VA: 0x101feceb0 | Size: 16 B | Source: third_party/jetski/cortex/core/native_swarm/read_board.go:L105
func (t *readBoardTool) Description() string {
	return "Read messages from the swarm message board, oldest first. Each message carries a sequence number; pass the last one you have seen as `since` to read only what came after it, or `seqs` to read named messages and nothing else. Without `since` you get the most recent messages."
}

// Binary VA: 0x101fecec0 | Size: 208 B | Source: third_party/jetski/cortex/core/native_swarm/read_board.go:L113
func (t *readBoardTool) ParameterSchema() []byte /* jsontext.Value */ {
	if t.channels.allow() {
		// [unrecovered] 870-byte JSON schema
		return nil
	}
	// [unrecovered] base JSON schema
	return nil
}

// Binary VA: 0x101fecf90 | Size: 2352 B | Source: third_party/jetski/cortex/core/native_swarm/read_board.go:L121
func (t *readBoardTool) Execute(ctx context.Context, rawArgs []byte /* jsontext.Value */, _ core.ToolContext) (core.ToolReturn, error) {
	if err := t.member.reachable(ctx); err != nil {
		return nil, err
	}
	var args readBoardArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("failed to unmarshal args: %w", err)
	}
	if args.Status {
		return t.status(ctx)
	}
	ch := channelArg(args.Channel)
	limit := 20 // 0x14
	if args.Limit != nil && *args.Limit > 0 {
		limit = *args.Limit
	}
	if ch != "" {
		return t.readView(ctx, ch, args.Since, limit)
	}
	var msgs []board.Message
	var err error
	if len(args.Seqs) > 0 {
		msgs, err = t.member.Store.Get(ctx, args.Seqs)
	} else if args.Since != nil {
		msgs, err = t.member.Store.Since(ctx, *args.Since, limit)
	} else {
		msgs, err = t.member.Store.Latest(ctx, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the board: %w", err)
	}
	head, err := t.member.Store.Head(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read the board head: %w", err)
	}
	var maxSeq uint64
	for _, m := range msgs {
		if m.Seq > maxSeq {
			maxSeq = m.Seq
		}
	}
	if len(args.Seqs) > 0 {
		if err := t.member.Engine.MarkSeen(ctx, t.member.Name, maxSeq); err != nil {
			return nil, fmt.Errorf("failed to advance the read cursor: %w", err)
		}
	} else if maxSeq > 0 {
		if err := t.member.Engine.MarkRead(ctx, t.member.Name, maxSeq); err != nil {
			return nil, fmt.Errorf("failed to advance the read cursor: %w", err)
		}
	}
	if msgs == nil {
		msgs = make([]board.Message, 0)
	}
	out, err := json.Marshal(readBoardOutput{
		Messages: msgs,
		Head:     head,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to render the board: %w", err)
	}
	return &core.ToolResult{
		Output:     string(out),
		RenderInfo: readBoardRenderInfo("", msgs),
	}, nil
}

// Binary VA: 0x101fed8c0 | Size: 1840 B | Source: third_party/jetski/cortex/core/native_swarm/read_board.go:L217
func (t *readBoardTool) readView(ctx context.Context, ch string, since *uint64, limit int) (core.ToolReturn, error) {
	view := board.View{
		Member: t.member.Name,
	}
	if ch == "*" {
		view.All = true
	} else {
		view.Channels = []string{ch}
	}
	roster, err := t.member.Store.Roster(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read the roster: %w", err)
	}
	var msgs []board.Message
	var head uint64
	if since != nil {
		msgs, head, err = t.member.Store.SinceIn(ctx, view, *since, limit)
	} else {
		msgs, head, err = t.member.Store.LatestIn(ctx, view, limit)
	}
	if err != nil {
		if ch != "*" && ch != "main" {
			return nil, fmt.Errorf("%w: #%s (channels: %s)", err, ch, channelNames(roster))
		}
		return nil, fmt.Errorf("failed to read the board: %w", err)
	}
	var maxSeq uint64
	for _, m := range msgs {
		if m.Seq > maxSeq {
			maxSeq = m.Seq
		}
	}
	if maxSeq > 0 {
		if err := t.member.Engine.MarkRead(ctx, t.member.Name, maxSeq); err != nil {
			return nil, fmt.Errorf("failed to advance the read cursor: %w", err)
		}
	}
	if msgs == nil {
		msgs = make([]board.Message, 0)
	}
	out, err := json.Marshal(readBoardOutput{
		Messages: msgs,
		Head:     head,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to render the board: %w", err)
	}
	return &core.ToolResult{
		Output:     string(out),
		RenderInfo: readBoardRenderInfo(ch, msgs),
	}, nil
}

// Binary VA: 0x101fedff0 | Size: 528 B | Source: third_party/jetski/cortex/core/native_swarm/read_board.go:L288
func channelNames(roster *board.Roster) string {
	names := []string{"#main"}
	if roster != nil && len(roster.Channels) > 0 {
		var keys []string
		for k := range roster.Channels {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			label := "#" + k
			if c := roster.Channels[k]; c != nil && c.Archived {
				label += " (archived)"
			}
			names = append(names, label)
		}
	}
	return strings.Join(names, ", ")
}

// Binary VA: 0x101fee200 | Size: 1936 B | Source: third_party/jetski/cortex/core/native_swarm/read_board.go:L302
func readBoardRenderInfo(ch string, msgs []board.Message) *cortex_go_proto.StepRenderInfo {
	scope := "board"
	if ch == "*" {
		scope = "*" /* inferred */
	} else if ch != "" {
		scope = "#" + ch
	}
	var title string
	if len(msgs) == 0 {
		title = fmt.Sprintf("%s (0 messages)", scope)
	} else if len(msgs) == 1 {
		title = fmt.Sprintf("1 message from %s (#%d)", scope, msgs[0].Seq)
	} else {
		title = fmt.Sprintf("%d messages from %s (#%d–#%d)", len(msgs), scope, msgs[0].Seq, msgs[len(msgs)-1].Seq)
	}
	var sb strings.Builder
	for i, m := range msgs {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		if m.Channel != "" {
			fmt.Fprintf(&sb, "**#%d @%s** #%s: %s", m.Seq, m.Agent, m.Channel, m.Text)
		} else {
			fmt.Fprintf(&sb, "**#%d @%s**: %s", m.Seq, m.Agent, m.Text)
		}
	}
	return &cortex_go_proto.StepRenderInfo{
		Verb: "Read",
		Title: []*cortex_go_proto.StepRenderInfo_TitlePart{
			{Part: &cortex_go_proto.StepRenderInfo_TitlePart_Text{Text: title}},
		},
		Content: []*cortex_go_proto.StepRenderInfo_ContentBlock{
			{Block: &cortex_go_proto.StepRenderInfo_ContentBlock_Markdown{Markdown: sb.String()}},
		},
		Icon:     "forum",
		Category: "actions",
	}
}

// Binary VA: 0x101fee990 | Size: 1488 B | Source: third_party/jetski/cortex/core/native_swarm/read_board.go:L336
func (t *readBoardTool) status(ctx context.Context) (core.ToolReturn, error) {
	st, err := buildStatus(ctx, t.member.Store, t.member.Engine /* inferred */)
	if err != nil {
		return nil, fmt.Errorf("failed to read the swarm status: %w", err)
	}
	out, err := json.Marshal(st)
	if err != nil {
		return nil, fmt.Errorf("failed to render the swarm status: %w", err)
	}
	return &core.ToolResult{
		Output: string(out),
		RenderInfo: &cortex_go_proto.StepRenderInfo{
			Verb: "Read",
			Title: []*cortex_go_proto.StepRenderInfo_TitlePart{
				{Part: &cortex_go_proto.StepRenderInfo_TitlePart_Text{Text: fmt.Sprintf("swarm status (board #%d)", st.Head)}},
			},
			Content: []*cortex_go_proto.StepRenderInfo_ContentBlock{
				{Block: &cortex_go_proto.StepRenderInfo_ContentBlock_Markdown{Markdown: statusTable(st)}},
			},
			Icon:     "forum",
			Category: "actions",
		},
	}, nil
}

// Binary VA: 0x101feef60 | Size: 1056 B | Source: third_party/jetski/cortex/core/native_swarm/read_board.go:L358
func statusTable(st statusOutput) string {
	var sb strings.Builder
	sb.WriteString("| member | state | parked on | last post |\n| --- | --- | --- | --- |\n") /* inferred separator */
	for _, m := range st.Members {
		parked := ""
		if m.State == "waiting" || m.State == "stopped" {
			if m.WaitSince != nil {
				parked = fmt.Sprintf("%s after #%d", m.WakeMode, *m.WaitSince)
			}
		}
		lastPost := "—"
		if m.LastPostSeq > 0 {
			lastPost = fmt.Sprintf("#%d %s", m.LastPostSeq, m.LastPostTime)
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", m.Name, m.State, parked, lastPost)
	}
	return sb.String()
}
