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
