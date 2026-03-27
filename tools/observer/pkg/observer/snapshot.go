package observer

import (
	"sync"

	"github.com/multigres/multigres-operator/tools/observer/pkg/report"
)

// snapshot stores the latest cycle's status response.
type snapshot struct {
	mu   sync.RWMutex
	data *report.StatusResponse
}

func (s *snapshot) Store(r *report.StatusResponse) {
	s.mu.Lock()
	s.data = r
	s.mu.Unlock()
}

func (s *snapshot) Load() *report.StatusResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data
}
