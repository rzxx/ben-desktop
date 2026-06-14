package apitypes

type AppUpdateStatus struct {
	AppVersion       string `json:"appVersion"`
	BuildCommit      string `json:"buildCommit"`
	BuildTime        string `json:"buildTime"`
	GitHubRepository string `json:"githubRepository"`
	Running          bool   `json:"running"`
}

type AppUpdateCheckResult struct {
	Started bool   `json:"started"`
	Message string `json:"message,omitempty"`
}
