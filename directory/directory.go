package directory

import (
	"context"
	"net/http"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// QueryResult holds the result of a directory query.
type QueryResult struct {
	Cards     []a2a.AgentCard     `json:"cards"`
	NextToken string              `json:"next_token,omitempty"`
	HelpResp  *FilterHelpResponse `json:"help,omitempty"` // non-nil only when Help was requested
}

// Compile-time interface check.
var _ http.Handler = (*Directory)(nil)

// Directory is an A2A agent directory service that stores agent cards
// and serves them over HTTP.
type Directory struct {
	registry Registry
	resolver FilterResolver
}

// New creates a new Directory with a MemoryRegistry and DefaultResolver
// unless overridden via options.
func New(opts ...Option) *Directory {
	d := &Directory{
		registry: NewMemoryRegistry(),
		resolver: DefaultResolver{},
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Register adds or replaces an agent card in the directory.
func (d *Directory) Register(ctx context.Context, card a2a.AgentCard) error {
	return d.registry.Register(ctx, card)
}

// Unregister removes an agent card by name.
func (d *Directory) Unregister(ctx context.Context, name string) (bool, error) {
	return d.registry.Unregister(ctx, name)
}
