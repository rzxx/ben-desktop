module ben/relayd

go 1.25.7

require (
	ben/registryauth v0.0.0
	github.com/libp2p/go-libp2p v0.48.0
	modernc.org/sqlite v1.46.1
)

replace ben/registryauth => ../shared/registryauth
