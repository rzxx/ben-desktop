package apitypes

// Surface is the default player-facing core API boundary.
// Queue management, sync orchestration, moderation, and diagnostics remain
// outside of this interface.
type Surface interface {
	Close() error
	RuntimeSurface
	ActivitySurface
	IdentitySurface
	LibrarySurface
	IngestSurface
	CatalogSurface
	CacheSurface
	PlaylistSurface
	InviteJoinSurface
	PlaybackSurface
}

// OperatorSurface extends the default surface with maintenance, diagnostics,
// invite administration, and network orchestration controls.
type OperatorSurface interface {
	Surface
	RuntimeAdminSurface
	IdentityDiagnosticsSurface
	InviteAdminSurface
	NetworkAdminSurface
}
