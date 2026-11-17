// Binary VA: 0x102f82eb0 | Source: third_party/jetski/cli/acp/server/elicitation.go
package server

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strings"

	"google3/base/go/log"
	"google3/third_party/jetski/cli/acp/messages/messages"
	"google3/third_party/jetski/cli/acp/schema/schema"
	"google3/third_party/jetski/cli/acp/session/session"
	"google3/third_party/jetski/proto/cortex_go_proto"
)

// Binary VA: 0x102f82eb0 | Size: 1536 B | Source: third_party/jetski/cli/acp/server/elicitation.go:L24
func buildQuestionElicitation(sessionID, toolCallID string, spec *cortex_go_proto.AskQuestionInteractionSpec) schema.CreateElicitationParams {
	props := make(map[string]schema.ElicitationProperty)
	var required []string
	questions := spec.GetQuestions()
	for i, q := range questions {
		qKey := fmt.Sprintf("q%d", i+1)
		otherKey := fmt.Sprintf("q%d_other", i+1)
		opts := q.GetOptions()
		if len(opts) == 0 {
			props[otherKey] = schema.ElicitationProperty{
				Type:  "string",
				Title: q.GetQuestion(),
			}
			required = append(required, otherKey)
			continue
		}
		var enumOpts []schema.ElicitationEnumOption
		for _, opt := range opts {
			enumOpts = append(enumOpts, schema.ElicitationEnumOption{
				Const: opt.GetLabel(), /* inferred */
				Title: opt.GetLabel(),
			})
		}
		if q.GetAllowMultiple() {
			props[qKey] = schema.ElicitationProperty{
				Type:  "array",
				Title: q.GetQuestion(),
				Items: &schema.ElicitationMultiSelectItems{AnyOf: enumOpts},
			}
		} else {
			props[qKey] = schema.ElicitationProperty{
				Type:  "string",
				Title: q.GetQuestion(),
				OneOf: enumOpts,
			}
		}
		props[otherKey] = schema.ElicitationProperty{
			Type:        "string",
			Title:       "Other",
			Description: "Optional free-form answer to: " + q.GetQuestion(),
		}
	}
	msg := "The agent has a question."
	if len(questions) != 1 {
		msg = fmt.Sprintf("The agent has %d questions.", len(questions))
	}
	return schema.CreateElicitationParams{
		SessionID:  sessionID,
		ToolCallID: toolCallID,
		Mode:       "form",
		Message:    msg,
		RequestedSchema: schema.ElicitationSchema{
			Type:       "object",
			Properties: props,
			Required:   required,
		},
	}
}

// Binary VA: 0x102f834b0 | Size: 1088 B | Source: third_party/jetski/cli/acp/server/elicitation.go:L65
func questionAnswersFromElicitation(spec *cortex_go_proto.AskQuestionInteractionSpec, res *schema.CreateElicitationResult) []*cortex_go_proto.AskQuestionEntry {
	if res == nil || res.Action != "accept" /* inferred */ {
		return nil
	}
	var entries []*cortex_go_proto.AskQuestionEntry
	for i, q := range spec.GetQuestions() {
		qKey := fmt.Sprintf("q%d", i+1)
		otherKey := fmt.Sprintf("q%d_other", i+1)
		selected := selectedOptionIDs(res.Content[qKey])
		var other string
		if rawOther, ok := res.Content[otherKey]; ok {
			_ = json.Unmarshal(rawOther, &other)
		}
		entries = append(entries, &cortex_go_proto.AskQuestionEntry{
			Question:       q.GetQuestion(),
			SelectedOption: selected, /* [unrecovered] exact proto field names */
			FreeformAnswer: other,    /* [unrecovered] exact proto field names */
		})
	}
	return entries
}

// Binary VA: 0x102f838f0 | Size: 864 B | Source: third_party/jetski/cli/acp/server/elicitation.go:L87
func selectedOptionIDs(raw jsontext.Value) []string {
	if len(raw) == 0 {
		return nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if single == "" {
			return nil
		}
		return []string{single}
	}
	var multi []string
	if err := json.Unmarshal(raw, &multi); err == nil {
		var out []string
		for _, v := range multi {
			if v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	return nil
}

// Binary VA: 0x102f83c50 | Size: 352 B | Source: third_party/jetski/cli/acp/server/elicitation.go:L106
func (s *Server) elicitQuestionAnswers(ctx context.Context, sessionID, toolCallID string, spec *cortex_go_proto.AskQuestionInteractionSpec) ([]*cortex_go_proto.AskQuestionEntry, bool, error) {
	var res schema.CreateElicitationResult
	params := buildQuestionElicitation(sessionID, toolCallID, spec)
	if err := s.callClient(ctx, "elicitation/create", params, &res); err != nil {
		return nil, false, err
	}
	answers := questionAnswersFromElicitation(spec, &res)
	return answers, answers != nil, nil
}

// Binary VA: 0x102f83db0 | Size: 560 B | Source: third_party/jetski/cli/acp/server/elicitation.go:L116
func (s *Server) askQuestion(ctx context.Context, sessionID string, stepIdx int, toolCallID string, st *session.State, spec *cortex_go_proto.AskQuestionInteractionSpec) {
	done := st.beginInteraction()
	if s.elicitationForm {
		// Binary VA: 0x102f96cd0 | Size: 128 B | Source: third_party/jetski/cli/acp/server/elicitation.go:L127
		go func() {
			s.askQuestionWithForm(ctx, sessionID, stepIdx, toolCallID, st, spec)
			done()
		}()
		return
	}
	s.askQuestionInChat(ctx, sessionID, stepIdx, toolCallID, st, spec) /* inferred */
	done()
}

// Binary VA: 0x102f83fe0 | Size: 1248 B | Source: third_party/jetski/cli/acp/server/elicitation.go:L136
func (s *Server) askQuestionWithForm(ctx context.Context, sessionID string, stepIdx int, toolCallID string, st *session.State, spec *cortex_go_proto.AskQuestionInteractionSpec) {
	answers, accepted, err := s.elicitQuestionAnswers(ctx, sessionID, toolCallID, spec)
	if err != nil || !accepted {
		if ctx.Err() != nil || s.conn.IsClosed() {
			log.InfoContextf(ctx, "elicitation/create for session %s step %d ended without an answer: %v", sessionID, stepIdx, err) /* inferred */
			return
		}
		log.WarningContextf(ctx, "elicitation/create rejected for session %s step %d, asking in chat instead: %v", sessionID, stepIdx, err) /* inferred */
		st.EndQuestionForm()
		s.askQuestionInChat(ctx, sessionID, stepIdx, toolCallID, st, spec)
		return
	}
	st.EndQuestionForm()
	if err := s.turns().RespondToAskQuestion(ctx, sessionID, stepIdx, answers); err != nil {
		log.ErrorContextf(ctx, "Failed to respond to ask_question for session %s step %d: %v", sessionID, stepIdx, err) /* inferred */
	}
}

// Binary VA: 0x102f844c0 | Size: 1168 B | Source: third_party/jetski/cli/acp/server/elicitation.go:L167
func (s *Server) askQuestionInChat(ctx context.Context, sessionID string, stepIdx int, toolCallID string, st *session.State, spec *cortex_go_proto.AskQuestionInteractionSpec) {
	st.WaitForSteer()
	st.SetAskedQuestion(stepIdx, toolCallID) /* inferred */
	var blocks []string
	for _, q := range spec.GetQuestions() {
		var lines []string
		lines = append(lines, q.GetQuestion())
		for _, opt := range q.GetOptions() {
			lines = append(lines, fmt.Sprintf("- [%s] %s", opt.GetLabel(), opt.GetDescription())) /* inferred */
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	text := "\n" + strings.Join(blocks, "\n") + "\n"
	s.conn.SendNotification("session/update", schema.SessionNotification{
		SessionID: sessionID,
		Update: messages.AgentMessageChunkSessionUpdate{
			Content: messages.TextContentBlock{Text: text},
		},
	})
	st.YieldTurn()
}
