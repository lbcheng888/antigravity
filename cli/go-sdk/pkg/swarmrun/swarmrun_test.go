package swarmrun

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/cortex"
)

type scriptedStreamer struct {
	mu    sync.Mutex
	turns [][]*aicode.SSEChunk
	reqs  []*aicode.GeminiRequest
}

func (s *scriptedStreamer) Stream(_ context.Context, _ string, req *aicode.GeminiRequest, onChunk func(*aicode.SSEChunk) error) error {
	s.mu.Lock()
	idx := len(s.reqs)
	s.reqs = append(s.reqs, req)
	if idx >= len(s.turns) {
		s.mu.Unlock()
		return fmt.Errorf("unexpected stream call #%d", idx+1)
	}
	chunks := s.turns[idx]
	s.mu.Unlock()

	for _, ch := range chunks {
		if err := onChunk(ch); err != nil {
			return err
		}
	}
	return nil
}

func fcChunk(id, name string, args map[string]any) *aicode.SSEChunk {
	return &aicode.SSEChunk{
		Response: &aicode.InnerGenerateContentResponse{
			Candidates: []*aicode.Candidate{{
				Content: &aicode.Content{
					Role: "model",
					Parts: []*aicode.Part{{
						FunctionCall: &aicode.FunctionCall{
							ID:   id,
							Name: name,
							Args: args,
						},
					}},
				},
			}},
		},
	}
}

func textChunk(text string) *aicode.SSEChunk {
	return &aicode.SSEChunk{
		Response: &aicode.InnerGenerateContentResponse{
			Candidates: []*aicode.Candidate{{
				FinishReason: "STOP",
				Content: &aicode.Content{
					Role:  "model",
					Parts: []*aicode.Part{{Text: text}},
				},
			}},
		},
	}
}

func TestOrchestratorMultiAgentWorkflow(t *testing.T) {
	ws := t.TempDir()
	swarmDir := filepath.Join(t.TempDir(), "swarm-board")

	coordStreamer := &scriptedStreamer{
		turns: [][]*aicode.SSEChunk{
			// 1. Set title and spawn worker-a
			{
				fcChunk("c1", "swarm", map[string]any{
					"action": "title",
					"title":  "File Writer Swarm",
				}),
				fcChunk("c2", "swarm", map[string]any{
					"action": "add",
					"name":   "worker-a",
					"brief":  "Write hello.txt with 'hello from worker-a' and report @coordinator [done].",
					"model":  "mock-worker-model",
				}),
			},
			// 2. Wait on the board via WakeEngine
			{
				fcChunk("c3", "swarm_wait", map[string]any{
					"since":         1,
					"mentions_only": true,
				}),
			},
			// 3. Check status and kill worker-a
			{
				fcChunk("c4", "swarm", map[string]any{"action": "status"}),
				fcChunk("c5", "swarm", map[string]any{
					"action": "kill",
					"name":   "worker-a",
				}),
			},
			// 4. Final summary
			{
				textChunk("All done: worker-a wrote hello.txt and was cleaned up."),
			},
		},
	}

	workerStreamer := &scriptedStreamer{
		turns: [][]*aicode.SSEChunk{
			// 1. Write file in workspace
			{
				fcChunk("w1", "write_file", map[string]any{
					"Path":    "hello.txt",
					"Content": "hello from worker-a\n",
				}),
			},
			// 2. Post @coordinator [done]
			{
				fcChunk("w2", "post_message", map[string]any{
					"text": "@coordinator [done] wrote hello.txt",
				}),
			},
			// 3. End worker turn
			{
				textChunk("Finished writing hello.txt."),
			},
		},
	}

	orch, err := NewOrchestrator(Config{
		Dir:           swarmDir,
		WorkspaceRoot: ws,
		Model:         "mock-coord-model",
		StreamerFor: func(agentName, _ string) cortex.Streamer {
			if agentName == "coordinator" {
				return coordStreamer
			}
			return workerStreamer
		},
	})
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := orch.Run(ctx, "Delegate writing hello.txt to worker-a.")
	if err != nil {
		t.Fatalf("Orchestrator.Run: %v", err)
	}

	if res.Title != "File Writer Swarm" {
		t.Fatalf("unexpected title: %q", res.Title)
	}
	if !strings.Contains(res.FinalText, "worker-a wrote hello.txt") {
		t.Fatalf("unexpected final text: %q", res.FinalText)
	}

	gotBytes, err := os.ReadFile(filepath.Join(ws, "hello.txt"))
	if err != nil {
		t.Fatalf("read hello.txt: %v", err)
	}
	if string(gotBytes) != "hello from worker-a\n" {
		t.Fatalf("hello.txt = %q, want %q", string(gotBytes), "hello from worker-a\n")
	}

	worker := res.Roster.Members["worker-a"]
	if worker == nil || !worker.IsKilled() {
		t.Fatalf("expected worker-a to be killed, got %+v", worker)
	}
	if len(res.Messages) < 2 {
		t.Fatalf("expected at least 2 board messages (brief + done), got %d", len(res.Messages))
	}
}

func TestOrchestratorContextCancelCleanup(t *testing.T) {
	ws := t.TempDir()
	swarmDir := filepath.Join(t.TempDir(), "swarm-cancel")

	coordStreamer := &scriptedStreamer{
		turns: [][]*aicode.SSEChunk{
			{
				fcChunk("c1", "swarm", map[string]any{
					"action": "add",
					"name":   "worker-b",
					"brief":  "Wait forever.",
				}),
			},
			{
				fcChunk("c2", "swarm_wait", map[string]any{
					"since": 1,
				}),
			},
		},
	}

	workerStreamer := &scriptedStreamer{
		turns: [][]*aicode.SSEChunk{
			{
				fcChunk("w1", "read_board", map[string]any{"limit": 5}),
			},
			{
				textChunk("Parking now."),
			},
		},
	}

	orch, err := NewOrchestrator(Config{
		Dir:           swarmDir,
		WorkspaceRoot: ws,
		Model:         "mock-model",
		StreamerFor: func(agentName, _ string) cortex.Streamer {
			if agentName == "coordinator" {
				return coordStreamer
			}
			return workerStreamer
		},
	})
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, err = orch.Run(ctx, "Start worker-b and wait.")
	if err == nil {
		t.Fatalf("expected context deadline error, got nil")
	}
}
