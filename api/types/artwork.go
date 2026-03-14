package apitypes

type ArtworkRef struct {
	BlobID  string
	MIME    string
	FileExt string
	Variant string
	Width   int
	Height  int
	Bytes   int64
}
