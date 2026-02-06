package check

import "fmt"

// Factory is a constructor function that creates a new Check instance.
// Each check type registers a factory that captures its CLI flags
// and returns a configured check ready to Run().
type Factory func() Check

// Registry maps check names (e.g., "cpu", "memory") to their Factory
// constructors. It provides the dispatch mechanism for the CLI subcommands.
type Registry struct {
	factories map[string]Factory
}

// NewRegistry creates an empty check registry.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]Factory),
	}
}

// Register adds a check factory under the given name.
// Panics if a factory with the same name is already registered,
// preventing silent overwrites from misconfigured registrations.
func (r *Registry) Register(name string, f Factory) {
	if _, exists := r.factories[name]; exists {
		panic(fmt.Sprintf("check %q already registered", name))
	}
	r.factories[name] = f
}

// Get returns the factory for the given check name and a boolean
// indicating whether it was found.
func (r *Registry) Get(name string) (Factory, bool) {
	f, ok := r.factories[name]
	return f, ok
}

// Names returns all registered check names in no particular order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}
