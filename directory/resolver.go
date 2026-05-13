package directory

import (
	"context"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// FilterResolver filters a slice of agent cards based on a filter string.
// Used as the fallback when the Registry does not implement Filterer.
type FilterResolver interface {
	Resolve(ctx context.Context, filter string, cards []a2a.AgentCard) []a2a.AgentCard
}

// FilterResolverFunc is an adapter to allow use of ordinary functions as FilterResolvers.
// This follows the same pattern as http.HandlerFunc.
type FilterResolverFunc func(ctx context.Context, filter string, cards []a2a.AgentCard) []a2a.AgentCard

// Resolve calls f(ctx, filter, cards).
func (f FilterResolverFunc) Resolve(ctx context.Context, filter string, cards []a2a.AgentCard) []a2a.AgentCard {
	return f(ctx, filter, cards)
}

// DefaultResolver performs case-insensitive substring matching against
// agent name, description, and skill tags.
type DefaultResolver struct{}

// Resolve returns only those cards whose name, description, or any skill tag
// contains the filter string as a case-insensitive substring.
func (DefaultResolver) Resolve(_ context.Context, filter string, cards []a2a.AgentCard) []a2a.AgentCard {
	q := strings.ToLower(filter)
	var results []a2a.AgentCard
	for _, card := range cards {
		if matchesCard(q, card) {
			results = append(results, card)
		}
	}
	if results == nil {
		return []a2a.AgentCard{}
	}
	return results
}

// matchesCard reports whether the lowercased filter is a substring of the card's
// name, description, or any of its skill tags.
func matchesCard(filter string, card a2a.AgentCard) bool {
	if strings.Contains(strings.ToLower(card.Name), filter) {
		return true
	}
	if strings.Contains(strings.ToLower(card.Description), filter) {
		return true
	}
	for _, skill := range card.Skills {
		for _, tag := range skill.Tags {
			if strings.Contains(strings.ToLower(tag), filter) {
				return true
			}
		}
	}
	return false
}
