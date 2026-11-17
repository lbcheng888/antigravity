// Binary VA: 0x101feb090 | Size: 288 B | Source: third_party/jetski/cortex/core/native_swarm/post_message.go:L73
package nativeswarm

import (
	"context"
	"encoding/json/v2"
	"fmt"

	"google3/third_party/golang/googleuuid/uuid"
	"google3/third_party/jetski/cortex/core/core"
	"google3/third_party/jetski/cortex/core/native_swarm/board/board"
	"google3/third_party/jetski/cortex/cortex_pb/cortex_go_proto"
)

// Binary VA: 0x10a3417a0 | Size: 32 B
type postMessageArgs struct {
	Text    string `json:"text"`
	Channel string `json:"channel,omitempty"`
}

// Binary VA: 0x10a5b23e8 | Size: 64 B
type postMessageResponse struct {
	Status   string   `json:"status"`
	Seq      uint64   `json:"seq"`
	Channel  string   `json:"channel,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// Binary VA: 0x10a341830 | Size: 32 B
type postMessageTool struct {
	member   *swarmMember
	channels channelFlags
}

// Binary VA: 0x101feb090 | Size: 288 B | Source: third_party/jetski/cortex/core/native_swarm/post_message.go:L73
func NewPostMessageTool(member *swarmMember /* inferred */) core.Tool {
	// [unrecovered] openSwarm(context.Background(), ...)
	_ = openSwarm
	return &postMessageTool{
		member:   member,
		channels: channelFlagsOf(member /* inferred */),
	}
}

// Binary VA: 0x101feb1b0 | Size: 16 B | Source: third_party/jetski/cortex/core/native_swarm/post_message.go:L82
func (t *postMessageTool) Name() string {
	return "post_message"
}

// Binary VA: 0x101feb1c0 | Size: 16 B | Source: third_party/jetski/cortex/core/native_swarm/post_message.go:L87
func (t *postMessageTool) Description() string {
	return "Post a message to the swarm message board, where every agent can read it. Messages are tagged with your agent id."
}

// Binary VA: 0x101feb1d0 | Size: 208 B | Source: third_party/jetski/cortex/core/native_swarm/post_message.go:L93
func (t *postMessageTool) ParameterSchema() []byte /* jsontext.Value */ {
	if t.channels.allow() {
		// [unrecovered] 157-byte JSON schema with channel property
		return nil
	}
	// [unrecovered] base JSON schema without channel property
	return nil
}

// Binary VA: 0x101feb2a0 | Size: 1616 B | Source: third_party/jetski/cortex/core/native_swarm/post_message.go:L103
func (t *postMessageTool) Execute(ctx context.Context, rawArgs []byte /* jsontext.Value */, _ core.ToolContext) (core.ToolReturn, error) {
	if err := t.member.reachable(ctx); err != nil {
		return nil, err
	}
	var args postMessageArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("failed to unmarshal args: %w", err)
	}
	if args.Text == "" {
		return nil, fmt.Errorf("text is required")
	}
	ch, err := t.channel(ctx, channelArg(args.Channel))
	if err != nil {
		return nil, err
	}
	var warnings []string
	if len(board.ChannelMentions(args.Text)) > 0 {
		roster, err := t.member.Store.Roster(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to read the roster: %w", err)
		}
		if t.channels.allow() {
			warnings = board.CheckChannelMentions(roster, args.Text)
		}
	}
	msg, err := t.member.Store.Post(ctx, board.PostRequest{
		Author:    t.member.Name,
		Text:      args.Text,
		Channel:   ch,
		RequestID: uuid.NewString(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to post to the board: %w", err)
	}
	t.member.Engine.Flush(ctx)
	resp := postMessageResponse{
		Status:   "posted",
		Seq:      msg.Seq,
		Channel:  msg.Channel,
		Warnings: warnings,
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{
		Output:     string(out),
		RenderInfo: postMessageRenderInfo(msg.Seq, msg.Channel, args.Text),
	}, nil
}

// Binary VA: 0x101feb8f0 | Size: 384 B | Source: third_party/jetski/cortex/core/native_swarm/post_message.go:L161
func (t *postMessageTool) channel(ctx context.Context, ch string) (string, error) {
	if ch != "" && ch != "main" {
		return ch, nil
	}
	roster, err := t.member.Store.Roster(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to read the roster: %w", err)
	}
	m := roster.Members[t.member.Name]
	return m.HomeChannelIn(roster), nil
}

// Binary VA: 0x101feba70 | Size: 1040 B | Source: third_party/jetski/cortex/core/native_swarm/post_message.go:L175
func postMessageRenderInfo(seq uint64, ch, text string) *cortex_go_proto.StepRenderInfo {
	target := "board"
	if ch != "" {
		target = "#" + ch
	}
	return &cortex_go_proto.StepRenderInfo{
		Verb: "Posted",
		Title: []*cortex_go_proto.StepRenderInfo_TitlePart{
			{Part: &cortex_go_proto.StepRenderInfo_TitlePart_Text{Text: fmt.Sprintf("#%d to %s", seq, target)}},
		},
		Content: []*cortex_go_proto.StepRenderInfo_ContentBlock{
			{Block: &cortex_go_proto.StepRenderInfo_ContentBlock_Markdown{Markdown: blockquote(text)}},
		},
		Icon:     "send",
		Category: "actions",
	}
}

// Binary VA: 0x101febe80 | Size: 192 B | Source: third_party/jetski/cortex/core/native_swarm/post_message.go:L199
func memberRun(m *board.Member, alive bool /* inferred */) board.RunState {
	if m == nil {
		return board.RunWorking
	}
	if !alive {
		return board.RunStopped
	}
	if m.WakeMode != "" /* inferred */ {
		return board.RunWaiting
	}
	return board.RunWorking
}
