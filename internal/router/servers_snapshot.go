package router

import "sort"

// List returns a stable snapshot of all registered downstream servers.
func (r *ServerRegistry) List() []Server {
	r.mu.RLock()
	defer r.mu.RUnlock()

	servers := make([]Server, 0, len(r.servers))
	for _, server := range r.servers {
		servers = append(servers, server)
	}
	sort.Slice(servers, func(i, j int) bool {
		return servers[i].Name < servers[j].Name
	})
	return servers
}
