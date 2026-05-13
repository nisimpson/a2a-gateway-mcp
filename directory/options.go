package directory

// Option configures the Directory at initialization time.
type Option func(*Directory)

// WithRegistry sets a custom Registry for agent card storage.
// If not provided, the Directory uses a MemoryRegistry.
func WithRegistry(r Registry) Option {
	return func(d *Directory) {
		d.registry = r
	}
}

// WithFilterResolver sets a custom FilterResolver for filtering agent cards.
// If not provided, the Directory uses the DefaultResolver.
func WithFilterResolver(r FilterResolver) Option {
	return func(d *Directory) {
		d.resolver = r
	}
}
