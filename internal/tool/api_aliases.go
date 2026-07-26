package tool

import "github.com/nisimpson/a2a-gateway-mcp/gateway/api"

// Type aliases re-export the shared API types so that existing internal code
// continues to reference them as tool.XxxInput / tool.XxxOutput without
// modification. Embedders import from gateway/api directly.

// InputPart is an alias for [api.InputPart].
type InputPart = api.InputPart

// ConnectAgentInput is an alias for [api.ConnectAgentInput].
type ConnectAgentInput = api.ConnectAgentInput

// ConnectAgentOutput is an alias for [api.ConnectAgentOutput].
type ConnectAgentOutput = api.ConnectAgentOutput

// DisconnectAgentInput is an alias for [api.DisconnectAgentInput].
type DisconnectAgentInput = api.DisconnectAgentInput

// DisconnectAgentOutput is an alias for [api.DisconnectAgentOutput].
type DisconnectAgentOutput = api.DisconnectAgentOutput

// SendMessageInput is an alias for [api.SendMessageInput].
type SendMessageInput = api.SendMessageInput

// SendMessageOutput is an alias for [api.SendMessageOutput].
type SendMessageOutput = api.SendMessageOutput

// AsyncSendOutput is an alias for [api.AsyncSendOutput].
type AsyncSendOutput = api.AsyncSendOutput

// BroadcastMessageInput is an alias for [api.BroadcastMessageInput].
type BroadcastMessageInput = api.BroadcastMessageInput

// GetTaskInput is an alias for [api.GetTaskInput].
type GetTaskInput = api.GetTaskInput

// CancelTaskInput is an alias for [api.CancelTaskInput].
type CancelTaskInput = api.CancelTaskInput

// CancelTaskOutput is an alias for [api.CancelTaskOutput].
type CancelTaskOutput = api.CancelTaskOutput

// DiscoverAgentsInput is an alias for [api.DiscoverAgentsInput].
type DiscoverAgentsInput = api.DiscoverAgentsInput

// DiscoverAgentsOutput is an alias for [api.DiscoverAgentsOutput].
type DiscoverAgentsOutput = api.DiscoverAgentsOutput

// GetAgentCardInput is an alias for [api.GetAgentCardInput].
type GetAgentCardInput = api.GetAgentCardInput

// GetAgentCardOutput is an alias for [api.GetAgentCardOutput].
type GetAgentCardOutput = api.GetAgentCardOutput

// PingAgentInput is an alias for [api.PingAgentInput].
type PingAgentInput = api.PingAgentInput

// PingAgentOutput is an alias for [api.PingAgentOutput].
type PingAgentOutput = api.PingAgentOutput

// ListAgentsInput is an alias for [api.ListAgentsInput].
type ListAgentsInput = api.ListAgentsInput

// ListAgentsOutput is an alias for [api.ListAgentsOutput].
type ListAgentsOutput = api.ListAgentsOutput

// ListAgentEntry is an alias for [api.ListAgentEntry].
type ListAgentEntry = api.ListAgentEntry

// GetHistoryInput is an alias for [api.GetHistoryInput].
type GetHistoryInput = api.GetHistoryInput

// GetHistoryOutput is an alias for [api.GetHistoryOutput].
type GetHistoryOutput = api.GetHistoryOutput

// ClearHistoryInput is an alias for [api.ClearHistoryInput].
type ClearHistoryInput = api.ClearHistoryInput

// ClearHistoryOutput is an alias for [api.ClearHistoryOutput].
type ClearHistoryOutput = api.ClearHistoryOutput

// CreateCallerCardInput is an alias for [api.CreateCallerCardInput].
type CreateCallerCardInput = api.CreateCallerCardInput

// CallerSkill is an alias for [api.CallerSkill].
type CallerSkill = api.CallerSkill

// CallerCapabilities is an alias for [api.CallerCapabilities].
type CallerCapabilities = api.CallerCapabilities

// CreateCallerCardOutput is an alias for [api.CreateCallerCardOutput].
type CreateCallerCardOutput = api.CreateCallerCardOutput

// RemoveCallerCardOutput is an alias for [api.RemoveCallerCardOutput].
type RemoveCallerCardOutput = api.RemoveCallerCardOutput

// RemoveCallerCardInput is an alias for [api.RemoveCallerCardInput].
type RemoveCallerCardInput = api.RemoveCallerCardInput

// ViewCallerCardInput is an alias for [api.ViewCallerCardInput].
type ViewCallerCardInput = api.ViewCallerCardInput

// CheckInboxInput is an alias for [api.CheckInboxInput].
type CheckInboxInput = api.CheckInboxInput

// CheckInboxOutput is an alias for [api.CheckInboxOutput].
type CheckInboxOutput = api.CheckInboxOutput

// InboxSummary is an alias for [api.InboxSummary].
type InboxSummary = api.InboxSummary

// ReadInboxInput is an alias for [api.ReadInboxInput].
type ReadInboxInput = api.ReadInboxInput

// ReadInboxOutput is an alias for [api.ReadInboxOutput].
type ReadInboxOutput = api.ReadInboxOutput
