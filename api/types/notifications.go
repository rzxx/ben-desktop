package apitypes

import "time"

type NotificationAudience string

const (
	NotificationAudienceUser   NotificationAudience = "user"
	NotificationAudienceSystem NotificationAudience = "system"
)

type NotificationImportance string

const (
	NotificationImportanceImportant NotificationImportance = "important"
	NotificationImportanceNormal    NotificationImportance = "normal"
	NotificationImportanceDebug     NotificationImportance = "debug"
)

type NotificationPhase string

const (
	NotificationPhaseQueued  NotificationPhase = "queued"
	NotificationPhaseRunning NotificationPhase = "running"
	NotificationPhaseSuccess NotificationPhase = "success"
	NotificationPhaseError   NotificationPhase = "error"
)

type NotificationVerbosity string

const (
	NotificationVerbosityImportant    NotificationVerbosity = "important"
	NotificationVerbosityUserActivity NotificationVerbosity = "user_activity"
	NotificationVerbosityEverything   NotificationVerbosity = "everything"
)

type NotificationSubject struct {
	RecordingID string `json:"recordingId,omitempty"`
	Title       string `json:"title,omitempty"`
	Subtitle    string `json:"subtitle,omitempty"`
	ArtworkRef  string `json:"artworkRef,omitempty"`
}

type NotificationSnapshot struct {
	ID         string                 `json:"id"`
	Kind       string                 `json:"kind"`
	LibraryID  string                 `json:"libraryId,omitempty"`
	Audience   NotificationAudience   `json:"audience"`
	Importance NotificationImportance `json:"importance"`
	Phase      NotificationPhase      `json:"phase"`
	Message    string                 `json:"message,omitempty"`
	Error      string                 `json:"error,omitempty"`
	Progress   float64                `json:"progress,omitempty"`
	Sticky     bool                   `json:"sticky,omitempty"`
	CreatedAt  time.Time              `json:"createdAt"`
	UpdatedAt  time.Time              `json:"updatedAt"`
	FinishedAt time.Time              `json:"finishedAt,omitempty"`
	Subject    *NotificationSubject   `json:"subject,omitempty"`
}

type NotificationPreferences struct {
	Verbosity NotificationVerbosity `json:"verbosity"`
}

func NormalizeNotificationVerbosity(value NotificationVerbosity) NotificationVerbosity {
	switch value {
	case NotificationVerbosityImportant,
		NotificationVerbosityUserActivity,
		NotificationVerbosityEverything:
		return value
	default:
		return NotificationVerbosityUserActivity
	}
}
