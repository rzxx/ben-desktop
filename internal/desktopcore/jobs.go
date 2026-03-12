package desktopcore

import (
	"sort"
	"strings"
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

type JobTracker struct {
	service   *JobsService
	jobID     string
	kind      string
	libraryID string
}

func NewJobsService() *JobsService {
	return &JobsService{jobs: make(map[string]JobSnapshot)}
}

func (s *JobsService) Track(jobID, kind, libraryID string) *JobTracker {
	if s == nil {
		return nil
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil
	}
	return &JobTracker{
		service:   s,
		jobID:     jobID,
		kind:      strings.TrimSpace(kind),
		libraryID: strings.TrimSpace(libraryID),
	}
}

func (s *JobsService) Put(snapshot JobSnapshot) JobSnapshot {
	if s == nil || snapshot.JobID == "" {
		return JobSnapshot{}
	}
	snapshot.JobID = strings.TrimSpace(snapshot.JobID)
	snapshot.Kind = strings.TrimSpace(snapshot.Kind)
	snapshot.LibraryID = strings.TrimSpace(snapshot.LibraryID)
	snapshot.Message = strings.TrimSpace(snapshot.Message)
	snapshot.Error = strings.TrimSpace(snapshot.Error)
	snapshot.Progress = clampJobProgress(snapshot.Progress)

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.jobs[snapshot.JobID]; ok {
		if snapshot.Kind == "" {
			snapshot.Kind = existing.Kind
		}
		if snapshot.LibraryID == "" {
			snapshot.LibraryID = existing.LibraryID
		}
		if snapshot.CreatedAt.IsZero() {
			snapshot.CreatedAt = existing.CreatedAt
		}
		if snapshot.Phase == "" {
			snapshot.Phase = existing.Phase
		}
		if snapshot.Phase == JobPhaseCompleted || snapshot.Phase == JobPhaseFailed {
			if snapshot.FinishedAt.IsZero() && !existing.FinishedAt.IsZero() {
				snapshot.FinishedAt = existing.FinishedAt
			}
		}
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = now
	}
	snapshot.UpdatedAt = now
	if snapshot.Phase == JobPhaseCompleted || snapshot.Phase == JobPhaseFailed {
		if snapshot.FinishedAt.IsZero() {
			snapshot.FinishedAt = snapshot.UpdatedAt
		}
	} else {
		snapshot.FinishedAt = time.Time{}
	}
	s.jobs[snapshot.JobID] = snapshot
	return snapshot
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

func (s *JobsService) List(libraryID string) []JobSnapshot {
	if s == nil {
		return nil
	}
	libraryID = strings.TrimSpace(libraryID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]JobSnapshot, 0, len(s.jobs))
	for _, job := range s.jobs {
		if libraryID != "" && strings.TrimSpace(job.LibraryID) != libraryID {
			continue
		}
		out = append(out, job)
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i].UpdatedAt
		right := out[j].UpdatedAt
		if !left.Equal(right) {
			return left.After(right)
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].JobID < out[j].JobID
	})
	return out
}

func (t *JobTracker) Queued(progress float64, message string) JobSnapshot {
	return t.put(JobPhaseQueued, progress, message, "")
}

func (t *JobTracker) Running(progress float64, message string) JobSnapshot {
	return t.put(JobPhaseRunning, progress, message, "")
}

func (t *JobTracker) Complete(progress float64, message string) JobSnapshot {
	if progress <= 0 {
		progress = 1
	}
	return t.put(JobPhaseCompleted, progress, message, "")
}

func (t *JobTracker) Fail(progress float64, message string, err error) JobSnapshot {
	errorText := ""
	if err != nil {
		errorText = strings.TrimSpace(err.Error())
	}
	if message == "" {
		message = errorText
	}
	return t.put(JobPhaseFailed, progress, message, errorText)
}

func (t *JobTracker) put(phase JobPhase, progress float64, message, errorText string) JobSnapshot {
	if t == nil || t.service == nil || t.jobID == "" {
		return JobSnapshot{}
	}
	return t.service.Put(JobSnapshot{
		JobID:     t.jobID,
		Kind:      t.kind,
		LibraryID: t.libraryID,
		Phase:     phase,
		Progress:  progress,
		Message:   message,
		Error:     errorText,
	})
}

func clampJobProgress(progress float64) float64 {
	switch {
	case progress < 0:
		return 0
	case progress > 1:
		return 1
	default:
		return progress
	}
}
