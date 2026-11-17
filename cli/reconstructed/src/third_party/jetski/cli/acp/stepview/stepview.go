// Binary VA: 0x102f7b300 | Size: 10352 B | Source: third_party/jetski/cli/acp/stepview/stepview.go

package stepview

import (
	"fmt"
	"strings"

	"google3/third_party/jetski/cli/acp/messages/messages"
	"google3/third_party/jetski/cli/steps/steps"
	"google3/third_party/jetski/cortex_pb/cortex_go_proto"
	"google3/third_party/jetski/gemini_coder_pb/gemini_coder_go_proto"
	"google3/third_party/jetski/language_server/diff_action/diff_action"
)

// Binary VA: 0x10a327eb0 | Size: 24 B | Source: third_party/jetski/cli/acp/stepview/stepview.go
type runCommandRawOutput struct {
	Output   string `json:"output"`
	ExitCode *int32 `json:"exit_code,omitempty"`
}

// Binary VA: 0x102f7b300 | Size: 1616 B | Source: third_party/jetski/cli/acp/stepview/stepview.go:L28
func ToolOutput(step /* inferred */ *gemini_coder_go_proto.Step) ([]messages.ToolCallContent, any) {
	cmdOut, cmdExitCode, cmdOK := steps.CommandResult(step)
	mcpText, mcpURI := steps.MCPToolResult(step)

	if cmdStatus := step.GetCommandStatus(); cmdStatus != nil {
		combined := cmdStatus.GetCombined()
		return []messages.ToolCallContent{
			messages.ContentToolCallContent{
				Content: messages.TextContentBlock{Text: combined},
			},
		}, combined
	}
	if cmdOK {
		return []messages.ToolCallContent{
			messages.ContentToolCallContent{
				Content: messages.TextContentBlock{Text: cmdOut},
			},
		}, runCommandRawOutput{Output: cmdOut, ExitCode: cmdExitCode}
	}
	if mcpText != "" {
		return []messages.ToolCallContent{
			messages.ContentToolCallContent{
				Content: messages.TextContentBlock{Text: mcpText},
			},
		}, mcpText
	}
	if mcpURI != "" {
		return []messages.ToolCallContent{
			messages.ContentToolCallContent{
				Content: messages.ResourceLinkContentBlock{
					URI:   mcpURI,
					Name:  "mcp_tool_output",
					Title: "Output stored at " + mcpURI,
				},
			},
		}, mcpURI
	}
	if edit := step.GetCodeAction().GetResult().GetEdit(); edit != nil && edit.GetDiff().GetUnifiedDiff() != nil {
		ud := edit.GetDiff().GetUnifiedDiff()
		path := strings.TrimPrefix(edit.GetAbsolutePathMigrateMeToUri(), "file://")
		oldText := diff_action.UnifiedDiffToOldString(ud)
		newText := diff_action.UnifiedDiffToNewString(ud)
		return []messages.ToolCallContent{
			messages.DiffToolCallContent{
				Path:    path,
				NewText: newText,
				OldText: oldText,
			},
		}, ud
	}
	return nil, nil
}

// Binary VA: 0x102f7d720 (go.shape.[]string, Size: 544 B), 0x102f7d940 (go.shape.string, Size: 208 B), 0x102f99e60 (go.shape.[]interface {}, Size: 224 B) | Source: third_party/jetski/cli/acp/stepview/stepview.go:L78
func Arg[T any](args /* inferred */ map[string]any, name /* inferred */ string) T {
	var zero T
	for k, v := range args {
		if !strings.EqualFold(k, name) {
			continue
		}
		if typed, ok := v.(T); ok {
			return typed
		}
		// Binary checks T == []string (type hash 0x1701f1b5) and converts []any elements that are string.
		if _, isStrSlice := any(zero).([]string); isStrSlice {
			if rawSlice, ok := v.([]any); ok {
				var out []string
				for _, elem := range rawSlice {
					if s, ok := elem.(string); ok {
						out = append(out, s)
					}
				}
				return any(out).(T)
			}
		}
	}
	return zero
}

// Binary VA: 0x102f7b950 | Size: 160 B | Source: third_party/jetski/cli/acp/stepview/stepview.go:L149
func FirstNonEmptyArg(args /* inferred */ map[string]any, names /* inferred */ ...string) string {
	for _, name := range names {
		if val := Arg[string](args, name); val != "" {
			return val
		}
	}
	return ""
}

// Binary VA: 0x102f7b9f0 | Size: 656 B | Source: third_party/jetski/cli/acp/stepview/stepview.go:L158
func codeActionFilePath(ca /* inferred */ *cortex_go_proto.CortexStepCodeAction) string {
	if uri := ca.GetResult().GetEdit().GetAbsolutePathMigrateMeToUri(); uri != "" {
		return uri
	}
	if ca.GetSpec() == nil {
		return ""
	}
	switch spec := ca.GetSpec().GetSpec().(type) {
	case *cortex_go_proto.ActionSpec_CreateFile:
		return spec.CreateFile.GetPath().GetAbsoluteUri()
	case *cortex_go_proto.ActionSpec_DeleteFile:
		return spec.DeleteFile.GetPath().GetAbsoluteUri()
	case *cortex_go_proto.ActionSpec_Command:
		switch target := spec.Command.GetTarget().(type) {
		case *cortex_go_proto.ActionSpecCommand_File:
			return target.File.GetAbsoluteUri()
		case *cortex_go_proto.ActionSpecCommand_CodeContext:
			return target.CodeContext.GetAbsoluteUri()
		case *cortex_go_proto.ActionSpecCommand_LineRange:
			return target.LineRange.GetAbsoluteUri()
		case *cortex_go_proto.ActionSpecCommand_CciWithSubrange:
			return target.CciWithSubrange.GetCci().GetAbsoluteUri()
		}
	}
	return ""
}

// Binary VA: 0x102f7bc80 | Size: 992 B | Source: third_party/jetski/cli/acp/stepview/stepview.go:L186
func stepFilePath(step /* inferred */ *gemini_coder_go_proto.Step) string {
	if step == nil {
		return ""
	}
	if perm := step.GetRequestedInteraction().GetPermission(); perm != nil {
		res := perm.GetResource()
		action := res.GetAction()
		if action == "read_file" || action == "write_file" {
			if target := res.GetTarget(); target != "" {
				return strings.TrimPrefix(target, "file://")
			}
		}
	}
	switch s := step.GetStep().(type) {
	case *gemini_coder_go_proto.Step_ViewFile:
		return strings.TrimPrefix(s.ViewFile.GetAbsolutePathUri(), "file://")
	case *gemini_coder_go_proto.Step_ListDirectory:
		return strings.TrimPrefix(s.ListDirectory.GetDirectoryPathUri(), "file://")
	case *gemini_coder_go_proto.Step_FileChange:
		return strings.TrimPrefix(s.FileChange.GetAbsolutePathUri(), "file://")
	case *gemini_coder_go_proto.Step_CodeAction:
		return strings.TrimPrefix(codeActionFilePath(s.CodeAction), "file://")
	}
	return ""
}

// Binary VA: 0x102f7c060 | Size: 5824 B | Source: third_party/jetski/cli/acp/stepview/stepview.go:L214
func KindTitleAndLocations(step /* inferred */ *gemini_coder_go_proto.Step, toolName /* inferred */ string, args /* inferred */ map[string]any, cwd /* inferred */ string) (string, string, []messages.ToolCallLocation) {
	title := "Run " + toolName
	filePath := stepFilePath(step)
	shortName := strings.TrimPrefix(toolName, "default_api:")

	var kind string
	switch shortName {
	case "read_file", "view_file":
		if filePath == "" {
			filePath = FirstNonEmptyArg(args, "absolutePath", "Path")
		}
		title = "File " + relPath(cwd, filePath, false)
		kind = "read"
	case "list_dir":
		if filePath == "" {
			filePath = Arg[string](args, "directoryPath")
		}
		title = "Directory " + relPath(cwd, filePath, true)
		kind = "read"
	case "read_notebook":
		if filePath == "" {
			filePath = FirstNonEmptyArg(args, "notebookPath", "path")
		}
		title = "Notebook " + relPath(cwd, filePath, false)
		kind = "read"
	case "read_url_content":
		title = "URL " + Arg[string](args, "url")
		kind = "read"
	case "read_resource":
		title = "Resource " + Arg[string](args, "uri")
		kind = "read"
	case "sed_file", "edit_file", "write_file", "write_to_file", "replace_file_content", "multi_replace_file_content":
		if filePath == "" {
			filePath = FirstNonEmptyArg(args, "targetFile", "target_file", "Path")
		}
		title = "File " + relPath(cwd, filePath, false)
		kind = "edit"
	case "notebook_edit":
		if filePath == "" {
			filePath = FirstNonEmptyArg(args, "notebookPath", "path")
		}
		title = "Notebook " + relPath(cwd, filePath, false)
		kind = "edit"
	case "delete_knowledge":
		title = "Knowledge ID " + Arg[string](args, "id")
		kind = "delete"
	case "grep_search":
		if q := Arg[string](args, "query"); q != "" {
			title = fmt.Sprintf("Grep %q in %s", q, relPath(cwd, Arg[string](args, "searchPath"), false))
		}
		kind = "search"
	case "code_search":
		title = fmt.Sprintf("%q on Code Search", Arg[string](args, "query"))
		kind = "search"
	case "moma_search":
		title = fmt.Sprintf("%q on Moma", Arg[string](args, "query"))
		kind = "search"
	case "find_by_name":
		if pat := Arg[string](args, "pattern"); pat != "" {
			if dir := Arg[string](args, "searchDirectory"); dir == "" {
				title = fmt.Sprintf("Files named %q", pat)
			} else {
				title = fmt.Sprintf("Files named %q in %s", pat, relPath(cwd, dir, true))
			}
		}
		kind = "search"
	case "search_web":
		title = fmt.Sprintf("%q on Web", Arg[string](args, "query"))
		kind = "search"
	case "run_command":
		title = Arg[string](args, "commandLine")
		kind = "execute"
	case "send_command_input":
		if inp := Arg[string](args, "input"); inp != "" {
			title = fmt.Sprintf("Send %q to command", inp)
		}
		kind = "execute"
	case "command_status":
		title = "Waiting for command completion"
		kind = "execute"
	case "execute_notebook", "notebook_execution":
		if filePath == "" {
			filePath = FirstNonEmptyArg(args, "notebookPath", "path")
		}
		title = "Notebook " + relPath(cwd, filePath, false)
		kind = "execute"
	case "build_targets":
		if targets := Arg[[]string](args, "targets"); len(targets) > 0 {
			title = fmt.Sprintf("Build %s", strings.Join(targets, ", "))
		}
		kind = "execute"
	case "test_targets":
		if targets := Arg[[]string](args, "targets"); len(targets) > 0 {
			title = fmt.Sprintf("Test %s", strings.Join(targets, ", "))
		}
		kind = "execute"
	case "call_mcp_tool":
		mcpName := Arg[string](args, "name")
		if mcpName == "" {
			mcpName = Arg[string](args, "toolName")
		}
		if mcpName == "" {
			mcpName = Arg[string](args, "tool_name")
		}
		title = "MCP Tool " + mcpName
		kind = "execute"
	case "wait", "wait_5_seconds":
		title = "Waiting..."
		kind = "think"
	case "ask_permission":
		action := Arg[string](args, "action")
		target := Arg[string](args, "target")
		switch action {
		case "read_url", "read_file":
			kind = "read"
		case "write_file":
			kind = "edit"
		case "mcp", "command", "execute_url", "unsandboxed", "download_url":
			kind = "execute"
		default:
			kind = "other"
		}
		if filePath == "" && (action == "read_file" || action == "write_file") {
			filePath = target
		}
		title = fmt.Sprintf("Permit %s on %s", action, target)
	default:
		kind = "other"
	}

	if filePath == "" {
		filePath = FirstNonEmptyArg(args, "targetFile", "target_file", "absolutePath", "notebookPath", "directoryPath")
	}
	var locations []messages.ToolCallLocation
	if filePath != "" {
		locations = []messages.ToolCallLocation{{Path: filePath}}
	}
	return kind, title, locations
}
