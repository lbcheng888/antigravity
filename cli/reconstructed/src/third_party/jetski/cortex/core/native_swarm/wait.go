// Binary VA: 0x101ff3790 | Size: 288 B | Source: third_party/jetski/cortex/core/native_swarm/wait.go:L60
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

// Binary VA: 0x10a3419e0 | Size: 16 B
type swarmWaitArgs struct {
	Since        uint64 `json:"since"`
	MentionsOnly *bool  `json:"mentions_only,omitempty"`
}

// Binary VA: 0x10a341a70 | Size: 32 B
type swarmWaitTool struct {
	member   *swarmMember
	channels channelFlags
}

// Binary VA: 0x101ff3790 | Size: 288 B | Source: third_party/jetski/cortex/core/native_swarm/wait.go:L60
func NewSwarmWaitTool(member *swarmMember /* inferred */) core.Tool {
	// [unrecovered] openSwarm(context.Background(), ...)
	_ = openSwarm
	return &swarmWaitTool{
		member:   member,
		channels: channelFlagsOf(member /* inferred */),
	}
}

// Binary VA: 0x101ff38b0 | Size: 16 B | Source: third_party/jetski/cortex/core/native_swarm/wait.go:L69
func (t *swarmWaitTool) Name() string {
	return "swarm_wait"
}

// Binary VA: 0x101ff38c0 | Size: 16 B | Source: third_party/jetski/cortex/core/native_swarm/wait.go:L74
func (t *swarmWaitTool) Description() string {
	return "Sleep until another member of the swarm posts to the board. Waiting costs nothing: you consume no tokens while waiting, and you wake as soon as a message that concerns you arrives. It ends your turn immediately, so put any `post_message` in the same response. When you are answering the user, you don't need swarm_wait: ending your turn already parks you. Use it instead of polling the board, and pass the highest sequence number you have seen as `since`."
}

// Binary VA: 0x101ff38d0 | Size: 208 B | Source: third_party/jetski/cortex/core/native_swarm/wait.go:L85
func (t *swarmWaitTool) ParameterSchema() []byte /* jsontext.Value */ {
	if t.channels.allow() {
		// [unrecovered] 734-byte JSON schema
		return nil
	}
	// [unrecovered] base JSON schema
	return nil
}

// Binary VA: 0x101ff39a0 | Size: 832 B | Source: third_party/jetski/cortex/core/native_swarm/wait.go:L93
func (t *swarmWaitTool) Execute(ctx context.Context, rawArgs []byte /* jsontext.Value */, _ core.ToolContext) (core.ToolReturn, error) {
	if err := t.member.reachable(ctx); err != nil {
		return nil, err
	}
	var args swarmWaitArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("failed to unmarshal args: %w", err)
	}
	mode := board.WakeMode("mentions") /* inferred */
	if args.MentionsOnly != nil && !*args.MentionsOnly {
		mode = board.WakeMode("anything") /* inferred */
	}
	if err := t.member.Engine.Park(ctx, t.member.Name, args.Since, mode); err != nil {
		return nil, fmt.Errorf("failed to record the wait: %w", err)
	}
	what := "a message that calls on you by name, or on @all"
	if mode == "anything" /* inferred */ {
		what = t.anythingIn(ctx)
	}
	return &core.ToolResult{
		Output:     fmt.Sprintf("Waiting for %s posted after #%d.", what, args.Since),
		RenderInfo: swarmWaitRenderInfo(what, args.Since),
	}, nil
}

// Binary VA: 0x101ff3ce0 | Size: 480 B | Source: third_party/jetski/cortex/core/native_swarm/wait.go:L133
func (t *swarmWaitTool) anythingIn(ctx context.Context) string {
	roster, err := t.member.Store.Roster(ctx)
	if err != nil || roster == nil {
		return "any message"
	}
	m := roster.Members[t.member.Name]
	if m == nil {
		return "any message"
	}
	chs := board.EffectiveChannels(m.Role, m.Channels)
	if len(chs) == 0 || slices.Contains(chs, "*") {
		return "any message"
	}
	var prefixed []string
	for _, ch := range chs {
		prefixed = append(prefixed, "#"+ch)
	}
	return fmt.Sprintf("any message in %s, or that names you", strings.Join(prefixed, ", "))
}

// Binary VA: 0x101ff3ec0 | Size: 672 B | Source: third_party/jetski/cortex/core/native_swarm/wait.go:L153
func swarmWaitRenderInfo(what string, since uint64) *cortex_go_proto.StepRenderInfo {
	// [unrecovered] short label selection when what == "anything" / len == 8
	return &cortex_go_proto.StepRenderInfo{
		Verb: "Waiting",
		Title: []*cortex_go_proto.StepRenderInfo_TitlePart{
			{Part: &cortex_go_proto.StepRenderInfo_TitlePart_Text{Text: fmt.Sprintf("for %s after #%d", what, since)}},
		},
		Icon:     "actions", /* inferred */
		Category: "actions",
	}
}
