// Binary VA: 0x102f87340 | Source: third_party/jetski/cli/acp/server/permissions.go
package server

import (
	"context"
	"strings"

	"google3/base/go/log"
	"google3/third_party/jetski/cli/acp/messages/messages"
	"google3/third_party/jetski/cli/acp/schema/schema"
	"google3/third_party/jetski/cli/acp/stepview/stepview"
	"google3/third_party/jetski/proto/codeium_common_go_proto"
)

// Binary VA: 0x102f87340 | Size: 2592 B | Source: third_party/jetski/cli/acp/server/permissions.go:L16
func (s *Server) requestPermissionFromClient(
	ctx context.Context,
	sessionID string,
	toolCallID string,
	stepIdx int, /* inferred */
	toolCall *codeium_common_go_proto.ToolCall,
	args map[string]interface{},
) bool {
	kind, title, locs := stepview.KindTitleAndLocations(s.params.WorkingDir, toolCall.GetName(), args)
	var content []messages.ToolCallContent
	switch strings.TrimPrefix(toolCall.GetName(), "default_api:") {
	case "write_to_file":
		path := stepview.FirstNonEmptyArg(args, "targetFile", "Path")
		newText := stepview.FirstNonEmptyArg(args, "codeContent", "content")
		if path != "" {
			content = append(content, messages.DiffToolCallContent{
				Path:    path,
				NewText: newText,
			})
		}
	case "edit_file", "replace_file_content":
		path := stepview.FirstNonEmptyArg(args, "targetFile", "Path")
		oldText := stepview.FirstNonEmptyArg(args, "targetContent", "target")
		newText := stepview.FirstNonEmptyArg(args, "replacementContent", "replacement")
		if path != "" {
			content = append(content, messages.DiffToolCallContent{
				Path:    path,
				NewText: newText,
				OldText: oldText,
			})
		}
	case "multi_replace_file_content":
		path, _ := stepview.Arg[string](args, "targetFile")
		chunks, _ := stepview.Arg[[]interface{}](args, "replacementChunks")
		if path != "" && len(chunks) > 0 {
			for _, rawChunk := range chunks {
				chunk, ok := rawChunk.(map[string]interface{})
				if !ok {
					continue
				}
				oldText, _ := stepview.Arg[string](chunk, "targetContent")
				newText, _ := stepview.Arg[string](chunk, "replacementContent")
				content = append(content, messages.DiffToolCallContent{
					Path:    path,
					NewText: newText,
					OldText: oldText,
				})
			}
		}
	}

	params := schema.RequestPermissionParams{
		SessionID: sessionID,
		ToolCall: messages.ToolCallSessionUpdate{
			ToolCallID: toolCall.GetId(),
			Name:       toolCall.GetName(),
			Args:       args,
			Status:     "pending",
			Kind:       kind,
			Title:      title,
			Locations:  locs,
			RawInput:   args,
			Content:    content,
		},
		Options: []schema.PermissionOption{
			{OptionID: "allow", Name: "Allow", Kind: "allow_once"},
			{OptionID: "deny", Name: "Deny", Kind: "reject_once"},
		},
	}
	var res schema.RequestPermissionResult
	if err := s.callClient(ctx, "session/request_permission", params, &res); err != nil {
		log.ErrorContextf(ctx, "session/request_permission RPC failed: %v", err) /* inferred */
		return false
	}
	return res.Outcome.OptionID == "allow"
}
