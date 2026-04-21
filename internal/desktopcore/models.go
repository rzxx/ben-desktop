package desktopcore

import "time"

type Library struct {
	LibraryID      string    `gorm:"primaryKey;size:64"`
	Name           string    `gorm:"size:256;not null"`
	RootPublicKey  string    `gorm:"size:512"`
	RootPrivateKey string    `gorm:"size:1024"`
	LibraryKey     string    `gorm:"size:512"`
	CreatedAt      time.Time `gorm:"not null"`
}

type Device struct {
	DeviceID        string     `gorm:"primaryKey;size:64"`
	Name            string     `gorm:"size:256;not null"`
	PeerID          string     `gorm:"size:256"`
	ActiveLibraryID *string    `gorm:"size:64;index"`
	JoinedAt        time.Time  `gorm:"not null"`
	LastSeenAt      *time.Time `gorm:"index"`
}

type LocalSetting struct {
	Key       string    `gorm:"primaryKey;size:128"`
	Value     string    `gorm:"type:TEXT;not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

type Membership struct {
	LibraryID        string    `gorm:"primaryKey;size:64"`
	DeviceID         string    `gorm:"primaryKey;size:64"`
	Role             string    `gorm:"size:32;not null"`
	CapabilitiesJSON string    `gorm:"type:TEXT;not null"`
	JoinedAt         time.Time `gorm:"not null"`
}

type ScanRoot struct {
	LibraryID string    `gorm:"primaryKey;size:64"`
	DeviceID  string    `gorm:"primaryKey;size:64"`
	RootPath  string    `gorm:"primaryKey;size:4096"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

type LocalSourcePath struct {
	LibraryID    string    `gorm:"primaryKey;size:64"`
	DeviceID     string    `gorm:"primaryKey;size:64"`
	SourceFileID string    `gorm:"primaryKey;size:128;column:source_file_id"`
	LocalPath    string    `gorm:"size:4096;not null"`
	PathKey      string    `gorm:"size:4096;not null;index"`
	UpdatedAt    time.Time `gorm:"not null"`
}

func (LocalSourcePath) TableName() string { return "local_source_paths" }

type LocalArtworkSourceRef struct {
	LibraryID       string    `gorm:"primaryKey;size:64"`
	ScopeType       string    `gorm:"primaryKey;size:32"`
	ScopeID         string    `gorm:"primaryKey;size:64"`
	Variant         string    `gorm:"primaryKey;size:64"`
	ChosenSource    string    `gorm:"size:64;not null"`
	ChosenSourceRef string    `gorm:"size:4096;not null"`
	UpdatedAt       time.Time `gorm:"not null"`
}

func (LocalArtworkSourceRef) TableName() string { return "local_artwork_source_refs" }

type ScanMaintenanceState struct {
	LibraryID      string    `gorm:"primaryKey;size:64"`
	DeviceID       string    `gorm:"primaryKey;size:64"`
	RepairRequired bool      `gorm:"not null;default:false"`
	Reason         string    `gorm:"size:128"`
	Detail         string    `gorm:"size:512"`
	UpdatedAt      time.Time `gorm:"not null;index"`
}

func (ScanMaintenanceState) TableName() string { return "scan_maintenance_states" }

type OfflineMember struct {
	LibraryID          string    `gorm:"primaryKey;size:64"`
	DeviceID           string    `gorm:"primaryKey;size:64"`
	LibraryRecordingID string    `gorm:"primaryKey;size:64;column:library_recording_id"`
	HasLocalSource     bool      `gorm:"not null;default:false"`
	HasLocalCached     bool      `gorm:"not null;default:false"`
	OfflineSince       time.Time `gorm:"not null;index"`
	UpdatedAt          time.Time `gorm:"not null;index"`
}

func (OfflineMember) TableName() string { return "offline_members" }

type PinRoot struct {
	LibraryID        string     `gorm:"primaryKey;size:64"`
	DeviceID         string     `gorm:"primaryKey;size:64"`
	Scope            string     `gorm:"primaryKey;size:32"`
	ScopeID          string     `gorm:"primaryKey;size:128"`
	Profile          string     `gorm:"size:128;not null"`
	PendingCount     int        `gorm:"not null;default:0"`
	CreatedAt        time.Time  `gorm:"not null"`
	UpdatedAt        time.Time  `gorm:"not null"`
	LastReconciledAt *time.Time `gorm:"index"`
}

func (PinRoot) TableName() string { return "pin_roots" }

type PinMember struct {
	LibraryID          string    `gorm:"primaryKey;size:64"`
	DeviceID           string    `gorm:"primaryKey;size:64"`
	Scope              string    `gorm:"primaryKey;size:32"`
	ScopeID            string    `gorm:"primaryKey;size:128"`
	Profile            string    `gorm:"primaryKey;size:128"`
	VariantRecordingID string    `gorm:"primaryKey;size:64;column:variant_recording_id"`
	LibraryRecordingID string    `gorm:"size:64;not null;index;column:library_recording_id"`
	ResolutionPolicy   string    `gorm:"size:32;not null;column:resolution_policy"`
	Pending            bool      `gorm:"not null;default:false;index"`
	LastError          string    `gorm:"size:512"`
	UpdatedAt          time.Time `gorm:"not null;index"`
}

func (PinMember) TableName() string { return "pin_members" }

type PinBlobRef struct {
	LibraryID        string    `gorm:"primaryKey;size:64"`
	DeviceID         string    `gorm:"primaryKey;size:64"`
	Scope            string    `gorm:"primaryKey;size:32"`
	ScopeID          string    `gorm:"primaryKey;size:128"`
	Profile          string    `gorm:"primaryKey;size:128"`
	BlobID           string    `gorm:"primaryKey;size:128"`
	RefKind          string    `gorm:"primaryKey;size:16;column:ref_kind"`
	SubjectID        string    `gorm:"primaryKey;size:160;column:subject_id"`
	RecordingID      string    `gorm:"size:64;index;column:recording_id"`
	ArtworkScopeType string    `gorm:"size:32;column:artwork_scope_type"`
	ArtworkScopeID   string    `gorm:"size:128;column:artwork_scope_id"`
	ArtworkVariant   string    `gorm:"size:64;column:artwork_variant"`
	UpdatedAt        time.Time `gorm:"not null;index"`
}

func (PinBlobRef) TableName() string { return "pin_blob_refs" }

type MembershipCert struct {
	LibraryID        string `gorm:"primaryKey;size:64"`
	DeviceID         string `gorm:"primaryKey;size:64"`
	PeerID           string `gorm:"size:256;not null;index"`
	Role             string `gorm:"size:32;not null"`
	AuthorityVersion int64  `gorm:"not null;default:1"`
	Serial           int64  `gorm:"not null;default:1"`
	IssuedAt         int64  `gorm:"not null"`
	ExpiresAt        int64  `gorm:"not null;index"`
	Sig              []byte `gorm:"not null"`
}

type MembershipCertRevocation struct {
	LibraryID string    `gorm:"primaryKey;size:64"`
	DeviceID  string    `gorm:"primaryKey;size:64"`
	Serial    int64     `gorm:"primaryKey"`
	PeerID    string    `gorm:"size:256;not null"`
	Reason    string    `gorm:"size:512;not null"`
	RevokedAt time.Time `gorm:"not null;index"`
}

type MembershipRecovery struct {
	LibraryID        string    `gorm:"primaryKey;size:64"`
	DeviceID         string    `gorm:"primaryKey;size:64"`
	TokenHash        string    `gorm:"size:128;not null"`
	IssuedByDeviceID string    `gorm:"size:64;not null"`
	CreatedAt        time.Time `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"not null"`
}

type AdmissionAuthority struct {
	LibraryID    string    `gorm:"primaryKey;size:64"`
	Version      int64     `gorm:"primaryKey"`
	PublicKey    string    `gorm:"size:512;not null"`
	PrevVersion  int64     `gorm:"not null"`
	SignedByKind string    `gorm:"size:32;not null"`
	Sig          []byte    `gorm:"not null"`
	CreatedAt    time.Time `gorm:"not null;index"`
}

type InviteJoinRequest struct {
	RequestID          string `gorm:"primaryKey;size:64"`
	LibraryID          string `gorm:"size:64;not null;index"`
	TokenID            string `gorm:"size:128;not null;index"`
	MaxUses            int    `gorm:"not null;default:1"`
	DeviceID           string `gorm:"size:64;not null;index"`
	DeviceName         string `gorm:"size:256;not null"`
	PeerID             string `gorm:"size:256;not null;index"`
	DeviceFingerprint  string `gorm:"size:64;not null"`
	RequestedRole      string `gorm:"size:32"`
	ApprovedRole       string `gorm:"size:32"`
	OwnerDeviceID      string `gorm:"size:64"`
	OwnerRole          string `gorm:"size:32"`
	OwnerPeerID        string `gorm:"size:256"`
	OwnerFingerprint   string `gorm:"size:64"`
	JoinPubKey         []byte `gorm:"not null"`
	Status             string `gorm:"size:32;not null;index"`
	Message            string `gorm:"size:512"`
	EncryptedMaterial  []byte
	MembershipCertJSON string    `gorm:"type:TEXT"`
	ExpiresAt          time.Time `gorm:"not null;index"`
	CreatedAt          time.Time `gorm:"not null;index"`
	UpdatedAt          time.Time `gorm:"not null;index"`
}

type InviteTokenRedemption struct {
	LibraryID string    `gorm:"primaryKey;size:64"`
	TokenID   string    `gorm:"primaryKey;size:128"`
	RequestID string    `gorm:"primaryKey;size:64"`
	UsedAt    time.Time `gorm:"not null;index"`
}

type IssuedInvite struct {
	InviteID     string     `gorm:"primaryKey;size:128"`
	LibraryID    string     `gorm:"size:64;not null;index"`
	TokenID      string     `gorm:"size:128;not null;uniqueIndex"`
	ServiceTag   string     `gorm:"size:128;not null"`
	InviteCode   string     `gorm:"type:TEXT;not null"`
	Role         string     `gorm:"size:32"`
	MaxUses      int        `gorm:"not null;default:1"`
	ExpiresAt    time.Time  `gorm:"not null;index"`
	CreatedAt    time.Time  `gorm:"not null;index"`
	RevokedAt    *time.Time `gorm:"index"`
	RevokeReason string     `gorm:"size:512"`
}

type JoinSession struct {
	SessionID          string `gorm:"primaryKey;size:64"`
	InviteCode         string `gorm:"type:TEXT;not null"`
	InviteToken        string `gorm:"type:TEXT;not null"`
	LibraryID          string `gorm:"size:64;not null;index"`
	ServiceTag         string `gorm:"size:128;not null"`
	PeerAddrHint       string `gorm:"size:512"`
	ExpectedPeerIDHint string `gorm:"size:256"`
	DeviceID           string `gorm:"size:64;not null;index"`
	DeviceName         string `gorm:"size:256;not null"`
	RequestID          string `gorm:"size:64;index"`
	Status             string `gorm:"size:32;not null;index"`
	Message            string `gorm:"size:512"`
	Role               string `gorm:"size:32"`
	LocalPeerID        string `gorm:"size:256"`
	DeviceFingerprint  string `gorm:"size:64"`
	OwnerDeviceID      string `gorm:"size:64"`
	OwnerRole          string `gorm:"size:32"`
	OwnerPeerID        string `gorm:"size:256"`
	OwnerFingerprint   string `gorm:"size:64"`
	MaterialJSON       string `gorm:"type:TEXT"`
	ExpiresAt          time.Time
	CreatedAt          time.Time `gorm:"not null;index"`
	UpdatedAt          time.Time `gorm:"not null;index"`
}

type Artist struct {
	LibraryID string `gorm:"primaryKey;size:64"`
	ArtistID  string `gorm:"primaryKey;size:64"`
	Name      string `gorm:"size:512;not null"`
	NameSort  string `gorm:"size:512;not null"`
}

type Credit struct {
	LibraryID  string `gorm:"primaryKey;size:64"`
	EntityType string `gorm:"primaryKey;size:16"`
	EntityID   string `gorm:"primaryKey;size:64"`
	ArtistID   string `gorm:"primaryKey;size:64"`
	Role       string `gorm:"primaryKey;size:64"`
	Ord        int    `gorm:"primaryKey"`
}

type AlbumVariantModel struct {
	LibraryID      string `gorm:"primaryKey;size:64;index:idx_album_variant_key,priority:1"`
	AlbumVariantID string `gorm:"primaryKey;size:64;column:album_variant_id"`
	AlbumClusterID string `gorm:"size:64;not null;index;column:album_cluster_id"`
	Title          string `gorm:"size:512;not null"`
	Year           *int   `gorm:"index"`
	Edition        string `gorm:"size:256"`
	KeyNorm        string `gorm:"size:1024;not null;index:idx_album_variant_key,priority:2"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (AlbumVariantModel) TableName() string { return "album_variants" }

type TrackVariantModel struct {
	LibraryID      string `gorm:"primaryKey;size:64;index:idx_track_variant_key,priority:1"`
	TrackVariantID string `gorm:"primaryKey;size:64;column:track_variant_id"`
	TrackClusterID string `gorm:"size:64;not null;index;column:track_cluster_id"`
	KeyNorm        string `gorm:"size:1024;not null;index:idx_track_variant_key,priority:2"`
	Title          string `gorm:"size:512;not null"`
	DurationMS     int64  `gorm:"not null"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (TrackVariantModel) TableName() string { return "track_variants" }

type AlbumTrack struct {
	LibraryID      string  `gorm:"primaryKey;size:64"`
	AlbumVariantID string  `gorm:"primaryKey;size:64;column:album_variant_id"`
	TrackVariantID string  `gorm:"primaryKey;size:64;column:track_variant_id"`
	DiscNo         int     `gorm:"primaryKey"`
	TrackNo        int     `gorm:"primaryKey"`
	TitleOverride  *string `gorm:"size:512"`
}

type DeviceVariantPreference struct {
	LibraryID       string    `gorm:"primaryKey;size:64"`
	DeviceID        string    `gorm:"primaryKey;size:64"`
	ScopeType       string    `gorm:"primaryKey;size:16"`
	ClusterID       string    `gorm:"primaryKey;size:64"`
	ChosenVariantID string    `gorm:"size:64;not null"`
	UpdatedAt       time.Time `gorm:"not null"`
}

type SourceFileModel struct {
	LibraryID         string    `gorm:"primaryKey;size:64;index:idx_source_file_path,unique,priority:1;index:idx_source_file_fingerprint,priority:1"`
	DeviceID          string    `gorm:"primaryKey;size:64;index:idx_source_file_path,unique,priority:2;index:idx_source_file_fingerprint,priority:2"`
	SourceFileID      string    `gorm:"primaryKey;size:128;column:source_file_id"`
	TrackVariantID    string    `gorm:"size:64;not null;index;column:track_variant_id"`
	LocalPath         string    `gorm:"size:4096;not null"`
	PathKey           string    `gorm:"size:4096;not null;index:idx_source_file_path,unique,priority:3"`
	SourceFingerprint string    `gorm:"size:256;not null;index:idx_source_file_fingerprint,priority:3"`
	EditionScopeKey   string    `gorm:"size:512;not null;index;column:edition_scope_key"`
	HashAlgo          string    `gorm:"size:32;not null"`
	HashHex           string    `gorm:"size:128;not null;index"`
	MTimeNS           int64     `gorm:"not null"`
	SizeBytes         int64     `gorm:"not null"`
	Container         string    `gorm:"size:64"`
	Codec             string    `gorm:"size:64"`
	Bitrate           int       `gorm:"not null;default:0"`
	SampleRate        int       `gorm:"not null;default:0"`
	Channels          int       `gorm:"not null;default:0"`
	IsLossless        bool      `gorm:"not null;default:false;index"`
	QualityRank       int       `gorm:"not null;default:0;index"`
	DurationMS        int64     `gorm:"not null"`
	TagsJSON          string    `gorm:"type:TEXT"`
	LastSeenAt        time.Time `gorm:"not null;index"`
	IsPresent         bool      `gorm:"not null;default:true;index"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (SourceFileModel) TableName() string { return "source_files" }

type OptimizedAssetModel struct {
	LibraryID         string    `gorm:"primaryKey;size:64;index:idx_optimized_asset_source_profile,unique,priority:1"`
	OptimizedAssetID  string    `gorm:"primaryKey;size:64;column:optimized_asset_id"`
	SourceFileID      string    `gorm:"size:128;not null;index;index:idx_optimized_asset_source_profile,unique,priority:2;column:source_file_id"`
	TrackVariantID    string    `gorm:"size:64;not null;index;column:track_variant_id"`
	Profile           string    `gorm:"size:128;not null;index:idx_optimized_asset_source_profile,unique,priority:3"`
	BlobID            string    `gorm:"size:128;not null;index"`
	MIME              string    `gorm:"size:128;not null"`
	DurationMS        int64     `gorm:"not null"`
	Bitrate           int       `gorm:"not null"`
	Codec             string    `gorm:"size:64;not null"`
	Container         string    `gorm:"size:64;not null"`
	CreatedByDeviceID string    `gorm:"size:64;not null"`
	CreatedAt         time.Time `gorm:"not null"`
	UpdatedAt         time.Time `gorm:"not null"`
}

func (OptimizedAssetModel) TableName() string { return "optimized_assets" }

type DeviceAssetCacheModel struct {
	LibraryID        string     `gorm:"primaryKey;size:64"`
	DeviceID         string     `gorm:"primaryKey;size:64"`
	OptimizedAssetID string     `gorm:"primaryKey;size:64;column:optimized_asset_id"`
	IsCached         bool       `gorm:"not null;default:false;index"`
	LastVerifiedAt   *time.Time `gorm:"index"`
	UpdatedAt        time.Time  `gorm:"not null"`
}

func (DeviceAssetCacheModel) TableName() string { return "device_asset_caches" }

type ArtworkVariant struct {
	LibraryID       string    `gorm:"primaryKey;size:64;index:idx_artwork_scope,priority:1"`
	ScopeType       string    `gorm:"primaryKey;size:32;index:idx_artwork_scope,priority:2"`
	ScopeID         string    `gorm:"primaryKey;size:64;index:idx_artwork_scope,priority:3"`
	Variant         string    `gorm:"primaryKey;size:64;index:idx_artwork_scope,priority:4"`
	BlobID          string    `gorm:"size:128;not null"`
	MIME            string    `gorm:"size:128;not null"`
	FileExt         string    `gorm:"size:16;not null;default:''"`
	W               int       `gorm:"not null"`
	H               int       `gorm:"not null"`
	Bytes           int64     `gorm:"not null"`
	ChosenSource    string    `gorm:"size:64;not null"`
	ChosenSourceRef string    `gorm:"size:4096;not null"`
	UpdatedAt       time.Time `gorm:"not null"`
}

type Playlist struct {
	LibraryID  string     `gorm:"primaryKey;size:64"`
	PlaylistID string     `gorm:"primaryKey;size:64"`
	Name       string     `gorm:"size:512;not null"`
	Kind       string     `gorm:"size:32;not null;index"`
	CreatedBy  string     `gorm:"size:64;not null;index"`
	CreatedAt  time.Time  `gorm:"not null"`
	UpdatedAt  time.Time  `gorm:"not null"`
	DeletedAt  *time.Time `gorm:"index"`
}

type PlaylistItem struct {
	LibraryID      string     `gorm:"primaryKey;size:64;index:idx_playlist_position,priority:1"`
	PlaylistID     string     `gorm:"primaryKey;size:64;index:idx_playlist_position,priority:2"`
	ItemID         string     `gorm:"primaryKey;size:64"`
	TrackVariantID string     `gorm:"size:64;not null;index;column:track_variant_id"`
	AddedAt        time.Time  `gorm:"not null"`
	UpdatedAt      time.Time  `gorm:"not null"`
	PositionKey    string     `gorm:"size:128;not null;index:idx_playlist_position,priority:3"`
	DeletedAt      *time.Time `gorm:"index"`
}

type OplogEntry struct {
	LibraryID              string `gorm:"primaryKey;size:64;uniqueIndex:idx_oplog_device_seq,priority:1;index"`
	OpID                   string `gorm:"primaryKey;size:128"`
	DeviceID               string `gorm:"size:64;not null;uniqueIndex:idx_oplog_device_seq,priority:2;index"`
	Seq                    int64  `gorm:"not null;uniqueIndex:idx_oplog_device_seq,priority:3"`
	TSNS                   int64  `gorm:"not null;index"`
	EntityType             string `gorm:"size:64;not null;index"`
	EntityID               string `gorm:"size:128;not null;index"`
	OpKind                 string `gorm:"size:64;not null"`
	PayloadJSON            string `gorm:"type:TEXT;not null"`
	SignerPeerID           string `gorm:"size:256;not null;default:''"`
	SignerAuthorityVersion int64  `gorm:"not null;default:0"`
	SignerCertSerial       int64  `gorm:"not null;default:0"`
	SignerRole             string `gorm:"size:32;not null;default:''"`
	SignerIssuedAt         int64  `gorm:"not null;default:0"`
	SignerExpiresAt        int64  `gorm:"not null;default:0"`
	SignerCertSig          []byte
	Sig                    []byte
}

type DeviceClock struct {
	LibraryID   string `gorm:"primaryKey;size:64"`
	DeviceID    string `gorm:"primaryKey;size:64"`
	LastSeqSeen int64  `gorm:"not null"`
}

type PeerSyncState struct {
	LibraryID     string     `gorm:"primaryKey;size:64"`
	DeviceID      string     `gorm:"primaryKey;size:64"`
	PeerID        string     `gorm:"size:256;not null;index"`
	LastAttemptAt *time.Time `gorm:"index"`
	LastSuccessAt *time.Time `gorm:"index"`
	LastError     string     `gorm:"type:TEXT"`
	LastApplied   int64      `gorm:"not null;default:0"`
	UpdatedAt     time.Time  `gorm:"not null"`
}

type LibraryCheckpoint struct {
	LibraryID         string     `gorm:"primaryKey;size:64;index:idx_library_checkpoint_created_at,priority:1;index:idx_library_checkpoint_published,priority:1"`
	CheckpointID      string     `gorm:"primaryKey;size:64"`
	CreatedByDeviceID string     `gorm:"size:64;not null"`
	BaseClocksJSON    string     `gorm:"type:TEXT;not null"`
	ChunkCount        int        `gorm:"not null"`
	EntryCount        int        `gorm:"not null"`
	ContentHash       string     `gorm:"size:128;not null"`
	Status            string     `gorm:"size:32;not null;index"`
	CreatedAt         time.Time  `gorm:"not null;index:idx_library_checkpoint_created_at,priority:2,sort:desc"`
	UpdatedAt         time.Time  `gorm:"not null;index"`
	PublishedAt       *time.Time `gorm:"index:idx_library_checkpoint_published,priority:2,sort:desc"`
}

type LibraryCheckpointChunk struct {
	LibraryID    string `gorm:"primaryKey;size:64"`
	CheckpointID string `gorm:"primaryKey;size:64"`
	ChunkIndex   int    `gorm:"primaryKey"`
	EntryCount   int    `gorm:"not null"`
	ContentHash  string `gorm:"size:128;not null"`
	PayloadJSON  string `gorm:"type:TEXT;not null"`
}

type DeviceCheckpointAck struct {
	LibraryID    string    `gorm:"primaryKey;size:64"`
	DeviceID     string    `gorm:"primaryKey;size:64"`
	CheckpointID string    `gorm:"size:64;not null;index"`
	Source       string    `gorm:"size:32;not null"`
	AckedAt      time.Time `gorm:"not null;index"`
}
