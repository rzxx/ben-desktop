package desktopcore

import (
	"sync"
	"time"
)

type JobPhase string

const (
	JobPhaseQueued    JobPhase = "queued"
	JobPhaseRunning   JobPhase = "running"
	JobPhaseCompleted JobPhase = "completed"
	JobPhaseFailed    JobPhase = "failed"
)

type JobSnapshot struct {
	JobID      string    `json:"jobId"`
	Kind       string    `json:"kind"`
	LibraryID  string    `json:"libraryId,omitempty"`
	Phase      JobPhase  `json:"phase"`
	Progress   float64   `json:"progress,omitempty"`
	Message    string    `json:"message,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
}

type JobsService struct {
	mu   sync.RWMutex
	jobs map[string]JobSnapshot
}

func NewJobsService() *JobsService {
	return &JobsService{jobs: make(map[string]JobSnapshot)}
}

func (s *JobsService) Put(snapshot JobSnapshot) {
	if s == nil || snapshot.JobID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now().UTC()
	}
	snapshot.UpdatedAt = time.Now().UTC()
	if snapshot.Phase == JobPhaseCompleted || snapshot.Phase == JobPhaseFailed {
		snapshot.FinishedAt = snapshot.UpdatedAt
	}
	s.jobs[snapshot.JobID] = snapshot
}

func (s *JobsService) Get(jobID string) (JobSnapshot, bool) {
	if s == nil {
		return JobSnapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	return job, ok
}
