package tool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/health"
	"github.com/nisimpson/a2a-gateway-mcp/history"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

const (
	defaultBroadcastTimeout = 30
	minBroadcastTimeout     = 1
	maxBroadcastTimeout     = 120
)

// BroadcastMessageInput is the input schema for the broadcast_message tool.
type BroadcastMessageInput struct {
	Aliases        []string       `json:"aliases" jsonschema:"list of agent aliases to send the message to (min 1, max 20)"`
	Message        string         `json:"message,omitempty" jsonschema:"plain text message to broadcast. Mutually exclusive with 'parts'."`
	Parts          []InputPart    `json:"parts,omitempty" jsonschema:"structured message parts. Takes precedence over 'message' if both provided."`
	TimeoutSeconds *int           `json:"timeout_seconds,omitempty" jsonschema:"per-agent timeout in seconds (min 1, max 120, default 30)"`
	Metadata       map[string]any `json:"metadata,omitempty" jsonschema:"optional metadata for A2A protocol extensions"`
	Async          *bool          `json:"async,omitempty" jsonschema:"if true, return immediately and deposit responses in inbox"`
}

// BroadcastMessageTool handles broadcasting a message to multiple agents concurrently.
type BroadcastMessageTool struct {
	AgentRegistry      AgentRegistry
	A2AClientResolver  A2AClientResolver
	CallerCardInjector CallerCardInjector
	HealthTracker      HealthTracker
	HistoryRecorder    HistoryRecorder
	RateLimiter        RateLimiter
	Inbox              Inbox
}

// NewBroadcastMessageTool creates a new BroadcastMessageTool with dependencies
// resolved from the provided environment.
func NewBroadcastMessageTool(env *Env) *BroadcastMessageTool {
	return &BroadcastMessageTool{
		AgentRegistry:      env.AgentRegistry,
		A2AClientResolver:  env.A2AClientResolver,
		CallerCardInjector: env.CallerCardInjector,
		HealthTracker:      env.HealthTracker,
		HistoryRecorder:    env.HistoryRecorder,
		RateLimiter:        env.RateLimiter,
		Inbox:              env.Inbox,
	}
}

func (b *BroadcastMessageTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name: "broadcast_message",
		Description: toolDescription(
			`Send the same message to multiple agents simultaneously and collect responses.`,
			`Use 'message' for simple plain-text broadcasts.`,
			`Use 'parts' when you need to send structured data or mixed content to all agents.`,
			`Parts also support base64-encoded binary data via the 'raw' field.`,
		),
		OutputSchema: broadcastMessageOutputSchema(),
	}
}

func (b *BroadcastMessageTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *BroadcastMessageInput) (*mcp.CallToolResult, map[string]*SendMessageOutput, error) {
	if err := b.validateInput(input); err != nil {
		return nil, nil, err
	}

	timeoutSeconds, err := b.resolveTimeout(input.TimeoutSeconds)
	if err != nil {
		return nil, nil, err
	}

	if input.Async != nil && *input.Async {
		return b.broadcastAsync(ctx, input, timeoutSeconds)
	}

	outcomes := b.fanOut(ctx, input, timeoutSeconds)
	return b.buildResponse(outcomes)
}

// validateInput checks that aliases and message content are provided.
func (b *BroadcastMessageTool) validateInput(input *BroadcastMessageInput) error {
	if len(input.Aliases) == 0 {
		return fmt.Errorf("at least one alias is required")
	}
	if len(input.Aliases) > 20 {
		return fmt.Errorf("maximum 20 aliases allowed, got %d", len(input.Aliases))
	}
	if input.Message == "" && len(input.Parts) == 0 {
		return fmt.Errorf("either 'message' or 'parts' is required")
	}
	return nil
}

// resolveTimeout validates and returns the per-agent timeout.
func (b *BroadcastMessageTool) resolveTimeout(requestSeconds *int) (int, error) {
	if requestSeconds == nil {
		return defaultBroadcastTimeout, nil
	}
	v := *requestSeconds
	if v < minBroadcastTimeout || v > maxBroadcastTimeout {
		return 0, fmt.Errorf("timeout_seconds must be between %d and %d, got %d", minBroadcastTimeout, maxBroadcastTimeout, v)
	}
	return v, nil
}

// agentOutcome holds the per-agent result from a broadcast send.
type agentOutcome struct {
	result   *broadcastResult
	response *SendMessageOutput
}

// fanOut sends the message to all aliases concurrently and collects outcomes.
func (b *BroadcastMessageTool) fanOut(ctx context.Context, input *BroadcastMessageInput, timeoutSeconds int) map[string]*agentOutcome {
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		outcomes = make(map[string]*agentOutcome, len(input.Aliases))
	)

	for _, alias := range input.Aliases {
		wg.Add(1)
		go func(alias string) {
			defer wg.Done()
			result, resp := b.broadcastToAgent(ctx, alias, input, timeoutSeconds)
			mu.Lock()
			outcomes[alias] = &agentOutcome{result: result, response: resp}
			mu.Unlock()
		}(alias)
	}

	wg.Wait()
	return outcomes
}

// buildResponse constructs the MCP result from collected outcomes.
func (b *BroadcastMessageTool) buildResponse(outcomes map[string]*agentOutcome) (*mcp.CallToolResult, map[string]*SendMessageOutput, error) {
	structured := make(map[string]*SendMessageOutput, len(outcomes))

	for alias, outcome := range outcomes {
		if outcome.response != nil {
			structured[alias] = outcome.response
		}
	}

	return nil, structured, nil
}

// broadcastToAgent sends a message to a single agent and returns the result.
func (b *BroadcastMessageTool) broadcastToAgent(ctx context.Context, alias string, input *BroadcastMessageInput, timeoutSeconds int) (*broadcastResult, *SendMessageOutput) {
	// Resolve alias.
	entry := b.AgentRegistry.Lookup(alias)
	if entry == nil {
		result := &broadcastResult{Status: "error", Error: fmt.Sprintf("alias %q is not registered", alias)}
		b.recordBroadcastHistory(ctx, alias, input, result)
		return result, nil
	}

	// Health check BEFORE rate limit (HLTH-6.1, HLTH-6.4).
	if b.HealthTracker.IsEnabled() && !b.HealthTracker.IsHealthy(alias) {
		result := &broadcastResult{Status: "skipped", Error: "agent is unhealthy"}
		b.recordBroadcastHistory(ctx, alias, input, result)
		return result, nil
	}

	// Rate limit check.
	if !b.RateLimiter.Allow(alias) {
		result := &broadcastResult{Status: "error", Error: fmt.Sprintf("rate limited: agent %q has exceeded its rate limit", alias)}
		b.recordBroadcastHistory(ctx, alias, input, result)
		return result, nil
	}

	// Per-agent timeout context.
	agentCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Resolve SDK client.
	resolved := &registry.ResolveResult{URL: entry.URL, IsAlias: true, Headers: entry.Headers, Alias: alias}
	a2aClient, err := b.A2AClientResolver.Resolve(agentCtx, resolved)
	if err != nil {
		result := &broadcastResult{Status: "error", Error: fmt.Sprintf("failed to resolve client: %v", err)}
		b.recordBroadcastHistory(ctx, alias, input, result)
		return result, nil
	}

	// Build message.
	contentParts, err := buildMessageParts(input.Message, input.Parts)
	if err != nil {
		result := &broadcastResult{Status: "error", Error: err.Error()}
		b.recordBroadcastHistory(ctx, alias, input, result)
		return result, nil
	}

	msg := a2a.NewMessage(a2a.MessageRoleUser, contentParts...)
	sendReq := &a2a.SendMessageRequest{Message: msg}
	if len(input.Metadata) > 0 {
		sendReq.Metadata = input.Metadata
	}
	sendReq.Metadata = b.CallerCardInjector.InjectCallerCard(sendReq.Metadata)

	// Route: streaming or direct.
	if b.AgentRegistry.SupportsStreaming(resolved) {
		return b.broadcastViaStreaming(agentCtx, a2aClient, sendReq, alias, input)
	}
	return b.broadcastDirect(agentCtx, a2aClient, sendReq, alias, input)
}

// broadcastViaStreaming sends via streaming transport.
func (b *BroadcastMessageTool) broadcastViaStreaming(ctx context.Context, client *a2aclient.Client, sendReq *a2a.SendMessageRequest, alias string, input *BroadcastMessageInput) (*broadcastResult, *SendMessageOutput) {
	events := client.SendStreamingMessage(ctx, sendReq)

	state := &streamState{}
	result, err := consumeStream(ctx, events, state)
	if err != nil {
		b.recordBroadcastHealthOutcome(alias, fmt.Errorf("%s", err.Error()))
		br := &broadcastResult{Status: "error", Error: err.Error()}
		b.recordBroadcastHistory(ctx, alias, input, br)
		return br, nil
	}

	b.HealthTracker.RecordSuccess(alias)

	var br *broadcastResult
	var resp *SendMessageOutput

	switch {
	case result.task != nil:
		br, resp = b.handleBroadcastTaskResult(result.task)
	case result.message != nil:
		text := extractContentFromMessageParts(result.message.Parts)
		br = &broadcastResult{Status: "success", Response: text, Message: result.message}
		resp = &SendMessageOutput{Message: result.message}
	case result.terminatedByStatus:
		task := buildTaskFromState(result.state)
		br, resp = b.handleBroadcastTaskResult(task)
	default:
		br = &broadcastResult{Status: "error", Error: "no terminal event received"}
	}

	b.recordBroadcastHistory(ctx, alias, input, br)
	return br, resp
}

// broadcastDirect sends via non-streaming transport.
func (b *BroadcastMessageTool) broadcastDirect(ctx context.Context, client *a2aclient.Client, sendReq *a2a.SendMessageRequest, alias string, input *BroadcastMessageInput) (*broadcastResult, *SendMessageOutput) {
	sendResult, err := client.SendMessage(ctx, sendReq)
	if err != nil {
		b.recordBroadcastHealthOutcome(alias, err)
		br := &broadcastResult{Status: "error", Error: err.Error()}
		b.recordBroadcastHistory(ctx, alias, input, br)
		return br, nil
	}

	b.HealthTracker.RecordSuccess(alias)

	var br *broadcastResult
	var resp *SendMessageOutput

	switch v := sendResult.(type) {
	case *a2a.Message:
		// Requirement: SRES-4.2 — broadcast includes raw message
		text := extractContentFromMessageParts(v.Parts)
		br = &broadcastResult{Status: "success", Response: text, Message: v}
		resp = &SendMessageOutput{Message: v}
	case *a2a.Task:
		br, resp = b.handleBroadcastTaskResult(v)
	default:
		br = &broadcastResult{Status: "error", Error: "unrecognized response format"}
	}

	b.recordBroadcastHistory(ctx, alias, input, br)
	return br, resp
}

// handleBroadcastTaskResult processes a task result in the broadcast context.
func (b *BroadcastMessageTool) handleBroadcastTaskResult(task *a2a.Task) (*broadcastResult, *SendMessageOutput) {
	// Requirement: SRES-4.1, SRES-4.3 — broadcast includes raw task in successful results
	switch task.Status.State {
	case a2a.TaskStateCompleted:
		text := extractContentFromArtifacts(task.Artifacts)
		return &broadcastResult{Status: "success", Response: text, Task: task}, &SendMessageOutput{Task: task}

	case a2a.TaskStateInputRequired:
		text := extractTaskResponseText(task)
		return &broadcastResult{Status: "input-required", Response: text, Task: task}, &SendMessageOutput{Task: task}

	case a2a.TaskStateAuthRequired:
		text := extractTaskResponseText(task)
		return &broadcastResult{Status: "auth-required", Response: text, Task: task}, &SendMessageOutput{Task: task}

	case a2a.TaskStateFailed:
		// Requirement: SRES-4.4 — error/skipped results omit task and message
		failMsg := "task failed"
		if task.Status.Message != nil {
			if text := extractStatusMessageText(task.Status.Message); text != "" {
				failMsg = text
			}
		}
		return &broadcastResult{Status: "error", Error: failMsg}, nil

	case a2a.TaskStateCanceled:
		// Requirement: SRES-4.4 — error/skipped results omit task and message
		return &broadcastResult{Status: "error", Error: "task was canceled by the agent"}, nil

	default:
		return &broadcastResult{Status: "error", Error: fmt.Sprintf("timeout waiting for task completion (state: %s)", task.Status.State)}, nil
	}
}

// recordBroadcastHistory records a broadcast interaction for a single agent.
func (b *BroadcastMessageTool) recordBroadcastHistory(ctx context.Context, alias string, input *BroadcastMessageInput, result *broadcastResult) {
	sent := summarizeMessage(input.Message, input.Parts)
	response := result.Response
	if response == "" && result.Error != "" {
		response = result.Error
	}
	isError := result.Status == "error" || result.Status == "skipped"
	b.HistoryRecorder.Record(ctx, history.RecordInput{
		Alias:    alias,
		Sent:     sent,
		Response: response,
		IsError:  isError,
	})
}

// recordBroadcastHealthOutcome classifies an error and records health state.
func (b *BroadcastMessageTool) recordBroadcastHealthOutcome(alias string, err error) {
	outcome := health.ClassifyError(err)
	switch outcome {
	case health.OutcomeConnectionError:
		b.HealthTracker.RecordFailure(alias)
	case health.OutcomeSuccess:
		b.HealthTracker.RecordSuccess(alias)
	}
}

// broadcastResult holds the outcome of sending a message to a single agent.
type broadcastResult struct {
	Status   string       `json:"status"`
	Response string       `json:"response,omitempty"`
	Error    string       `json:"error,omitempty"`
	Task     *a2a.Task    `json:"task,omitempty"`
	Message  *a2a.Message `json:"message,omitempty"`
}

// broadcastAsync validates all aliases synchronously, spawns background goroutines
// for valid agents, and returns a per-agent dispatch summary immediately.
// Each alias maps to a SendMessageOutput with the Async field populated.
// Requirement: AINB-2.2, AINB-2.4
func (b *BroadcastMessageTool) broadcastAsync(_ context.Context, input *BroadcastMessageInput, timeoutSeconds int) (*mcp.CallToolResult, map[string]*SendMessageOutput, error) {
	results := make(map[string]*SendMessageOutput, len(input.Aliases))

	for _, alias := range input.Aliases {
		// Check alias exists.
		entry := b.AgentRegistry.Lookup(alias)
		if entry == nil {
			results[alias] = &SendMessageOutput{
				Async: &AsyncSendOutput{
					Alias:  alias,
					Status: "error",
					Error:  fmt.Sprintf("alias %q is not registered", alias),
				},
			}
			continue
		}

		// Check rate limit.
		if err := b.RateLimiter.CheckRateLimit(alias); err != nil {
			results[alias] = &SendMessageOutput{
				Async: &AsyncSendOutput{
					Alias:  alias,
					Status: "error",
					Error:  fmt.Sprintf("rate limited: %s", err.Error()),
				},
			}
			continue
		}

		// Dispatch background goroutine.
		go b.backgroundBroadcastToAgent(alias, input, timeoutSeconds)

		results[alias] = &SendMessageOutput{
			Async: &AsyncSendOutput{
				Alias:  alias,
				Status: "dispatched",
			},
		}
	}

	return nil, results, nil
}

// backgroundBroadcastToAgent performs the actual agent communication in the background
// and deposits the result into the inbox.
// Requirement: AINB-2.3
func (b *BroadcastMessageTool) backgroundBroadcastToAgent(alias string, input *BroadcastMessageInput, timeoutSeconds int) {
	ctx := context.Background()
	result, resp := b.broadcastToAgent(ctx, alias, input, timeoutSeconds)

	entry := registry.InboxEntry{
		Alias: alias,
	}

	if result != nil && result.Status == "error" {
		entry.State = "error"
		entry.Error = result.Error
	} else if resp != nil {
		if resp.Task != nil {
			entry.TaskID = string(resp.Task.ID)
			entry.ContextID = resp.Task.ContextID
			entry.State = string(resp.Task.Status.State)
			entry.Task = resp.Task
		} else if resp.Message != nil {
			entry.ContextID = resp.Message.ContextID
			entry.State = "completed"
			entry.Message = resp.Message
		}
	}

	b.Inbox.Deposit(entry)
}
