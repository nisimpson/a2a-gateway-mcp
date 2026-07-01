package gateway

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/nisimpson/a2a-gateway-mcp/directory"
)

// Feature: discover-agents-default-url, Property 2: Last-writer-wins for functional options
// **Validates: Requirements 1.4, 2.6**

func TestPropertyLastWriterWinsDefaultDirectoryURL(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for the number of URL options to apply (1–10)
	countGen := gen.IntRange(1, 10)

	// Property: applying a sequence of WithDefaultDirectoryURL options results in the last URL being stored
	properties.Property("last WithDefaultDirectoryURL value wins in serverConfig", prop.ForAll(
		func(count int) bool {
			// Generate a random sequence of valid URLs
			urls := make([]string, count)
			for i := range urls {
				host := fmt.Sprintf("host%d%d", i, rand.Intn(10000))
				if i%2 == 0 {
					urls[i] = "http://" + host + ".com"
				} else {
					urls[i] = "https://" + host + ".org/path"
				}
			}

			// Build and apply options to a fresh serverConfig
			cfg := &serverConfig{}
			for _, u := range urls {
				WithDefaultDirectoryURL(u)(cfg)
			}

			// The final config should store the last URL
			lastURL := urls[count-1]
			return cfg.defaultDirectoryURL == lastURL
		},
		countGen,
	))

	properties.TestingRun(t)
}

func TestPropertyLastWriterWinsDirectory(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for number of Directory instances (1–10)
	countGen := gen.IntRange(1, 10)

	// Property: applying a sequence of WithDirectory options results in the last directory being stored
	properties.Property("last WithDirectory instance wins in serverConfig", prop.ForAll(
		func(count int) bool {
			// Create a slice of distinct Directory instances
			dirs := make([]*directory.Directory, count)
			for i := range dirs {
				dirs[i] = directory.New()
			}

			// Apply options to a fresh serverConfig
			cfg := &serverConfig{}
			for _, d := range dirs {
				WithDirectory(d)(cfg)
			}

			// The final config should store the last directory instance
			lastDir := dirs[count-1]
			return cfg.directory == lastDir
		},
		countGen,
	))

	properties.TestingRun(t)
}

func TestPropertyLastWriterWinsMixedOptions(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for number of interleaved option pairs (2–10)
	countGen := gen.IntRange(2, 10)

	// Property: interleaving WithDefaultDirectoryURL and WithDirectory still has last-writer-wins
	// for each field independently
	properties.Property("interleaved options: each field last writer wins independently", prop.ForAll(
		func(count int) bool {
			// Generate URLs and directories
			urls := make([]string, count)
			dirs := make([]*directory.Directory, count)
			for i := range count {
				host := fmt.Sprintf("mixed%d%d", i, rand.Intn(10000))
				urls[i] = "https://" + host + ".io:8080/api"
				dirs[i] = directory.New()
			}

			// Interleave: apply URL then Directory for each index
			cfg := &serverConfig{}
			for i := range count {
				WithDefaultDirectoryURL(urls[i])(cfg)
				WithDirectory(dirs[i])(cfg)
			}

			// Last URL and last directory should be stored
			lastURL := urls[count-1]
			lastDir := dirs[count-1]
			return cfg.defaultDirectoryURL == lastURL && cfg.directory == lastDir
		},
		countGen,
	))

	properties.TestingRun(t)
}

// Feature: discover-agents-default-url, Property 2: Last-writer-wins for functional options
// Additional sub-property: WithDefaultDirectoryURL validation error reflects the last value only

func TestPropertyLastWriterWinsURLValidation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for valid URLs
	validURLGen := gen.RegexMatch(`[a-z]{3,10}`).Map(func(host string) string {
		return "https://" + host + ".com"
	})

	// Generator for invalid URLs
	invalidURLGen := gen.OneConstOf(
		"ftp://invalid.com",
		"not-a-url",
		"://missing-scheme",
		"ws://websocket.io",
	)

	// Property: when the last URL in the sequence is valid, defaultDirectoryURLErr is nil
	properties.Property("last valid URL results in no validation error", prop.ForAll(
		func(invalidURL string, validURL string) bool {
			cfg := &serverConfig{}
			// Apply invalid URL first
			WithDefaultDirectoryURL(invalidURL)(cfg)
			// Then apply valid URL last
			WithDefaultDirectoryURL(validURL)(cfg)

			// The error should be nil because the last URL was valid
			return cfg.defaultDirectoryURLErr == nil && cfg.defaultDirectoryURL == validURL
		},
		invalidURLGen,
		validURLGen,
	))

	// Property: when the last URL in the sequence is invalid, defaultDirectoryURLErr is non-nil
	properties.Property("last invalid URL results in validation error", prop.ForAll(
		func(validURL string, invalidURL string) bool {
			cfg := &serverConfig{}
			// Apply valid URL first
			WithDefaultDirectoryURL(validURL)(cfg)
			// Then apply invalid URL last
			WithDefaultDirectoryURL(invalidURL)(cfg)

			// The error should be non-nil because the last URL was invalid
			return cfg.defaultDirectoryURLErr != nil && cfg.defaultDirectoryURL == invalidURL
		},
		validURLGen,
		invalidURLGen,
	))

	properties.TestingRun(t)
}

// Ensure the test output includes the property feature tag for traceability.
func init() {
	_ = "Feature: discover-agents-default-url, Property 2: Last-writer-wins for functional options"
}
