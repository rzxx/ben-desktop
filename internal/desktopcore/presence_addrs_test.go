package desktopcore

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestSelectPresenceAnnounceAddrsCapsBudget(t *testing.T) {
	localPeerID := mustGenerateTestPeerID(t)
	relayPeerID := mustGenerateTestPeerID(t)
	relayAddrs := make([]string, 0, 8)
	for i := 0; i < 8; i++ {
		relayAddrs = append(relayAddrs, peerAddr(relayBaseAddr(i, relayPeerID)+"/p2p-circuit", localPeerID))
	}
	listenAddrs := []string{
		peerAddr("/dns4/node.example.com/tcp/4001", localPeerID),
		peerAddr("/ip4/8.8.8.8/tcp/4001", localPeerID),
		peerAddr("/ip4/1.1.1.1/udp/4001/quic-v1", localPeerID),
		peerAddr("/ip4/192.168.1.10/tcp/4001", localPeerID),
		peerAddr("/ip4/10.0.0.20/udp/4001/quic-v1", localPeerID),
		peerAddr("/ip6/2001:4860:4860::8888/tcp/4001", localPeerID),
		peerAddr("/ip6/fd00::1/tcp/4001", localPeerID),
		peerAddr("/ip4/100.64.1.20/tcp/4001", localPeerID),
		peerAddr("/ip4/172.16.1.20/tcp/4001", localPeerID),
		peerAddr("/ip4/8.8.4.4/tcp/4001", localPeerID),
		peerAddr("/ip4/9.9.9.9/tcp/4001", localPeerID),
		peerAddr("/ip4/192.168.1.11/tcp/4001", localPeerID),
		peerAddr("/ip4/10.0.0.21/tcp/4001", localPeerID),
		peerAddr("/dns6/node6.example.com/tcp/4001", localPeerID),
		peerAddr("/ip4/198.51.100.10/tcp/4001", localPeerID),
		peerAddr("/ip4/203.0.113.10/tcp/4001", localPeerID),
		peerAddr("/ip6/2001:4860:4860::8844/udp/4001/quic-v1", localPeerID),
		peerAddr("/ip4/192.168.50.10/udp/4001/quic-v1", localPeerID),
	}

	got := selectPresenceAnnounceAddrs(localPeerID, relayAddrs, listenAddrs)

	if len(got) != presenceAnnounceAddressBudget {
		t.Fatalf("selected addrs len = %d, want %d: %#v", len(got), presenceAnnounceAddressBudget, got)
	}
	if !anyAddrContains(got, "/p2p-circuit/") {
		t.Fatalf("selected addrs missing relay addr: %#v", got)
	}
	if !anyAddrContains(got, "/dns4/node.example.com/") && !anyAddrContains(got, "/dns6/node6.example.com/") {
		t.Fatalf("selected addrs missing dns direct addr: %#v", got)
	}
	if !anyAddrContains(got, "/ip4/192.168.1.10/") && !anyAddrContains(got, "/ip4/10.0.0.20/") {
		t.Fatalf("selected addrs missing private direct addr: %#v", got)
	}
}

func TestSelectPresenceAnnounceAddrsDropsUnusableDirectAddrs(t *testing.T) {
	localPeerID := mustGenerateTestPeerID(t)
	relayPeerID := mustGenerateTestPeerID(t)
	relayAddr := peerAddr(relayBaseAddr(1, relayPeerID)+"/p2p-circuit", localPeerID)
	listenAddrs := []string{
		"not-a-multiaddr",
		peerAddr("/ip4/0.0.0.0/tcp/4001", localPeerID),
		peerAddr("/ip6/::/tcp/4001", localPeerID),
		peerAddr("/ip4/127.0.0.1/tcp/4001", localPeerID),
		peerAddr("/ip6/::1/tcp/4001", localPeerID),
		peerAddr("/ip4/169.254.1.1/tcp/4001", localPeerID),
		peerAddr("/ip6/fe80::1/tcp/4001", localPeerID),
		peerAddr("/ip4/224.0.0.1/tcp/4001", localPeerID),
		peerAddr("/ip4/192.168.1.10/tcp/4001", localPeerID),
	}

	got := selectPresenceAnnounceAddrs(localPeerID, []string{relayAddr}, listenAddrs)

	for _, bad := range []string{"/ip4/0.0.0.0/", "/ip6/::/", "/ip4/127.0.0.1/", "/ip6/::1/", "/ip4/169.254.1.1/", "/ip6/fe80::1/", "/ip4/224.0.0.1/"} {
		if anyAddrContains(got, bad) {
			t.Fatalf("selected addrs included unusable addr containing %q: %#v", bad, got)
		}
	}
	if !slices.Contains(got, relayAddr) {
		t.Fatalf("selected addrs missing relay addr: %#v", got)
	}
	if !anyAddrContains(got, "/ip4/192.168.1.10/") {
		t.Fatalf("selected addrs missing valid private addr: %#v", got)
	}
}

func TestSelectPresenceAnnounceAddrsRejectsPeerMismatch(t *testing.T) {
	localPeerID := mustGenerateTestPeerID(t)
	otherPeerID := mustGenerateTestPeerID(t)

	got := selectPresenceAnnounceAddrs(localPeerID, nil, []string{
		peerAddr("/ip4/8.8.8.8/tcp/4001", otherPeerID),
		peerAddr("/ip4/192.168.1.10/tcp/4001", localPeerID),
	})

	if anyAddrContains(got, otherPeerID) {
		t.Fatalf("selected addrs included peer mismatch: %#v", got)
	}
	if !anyAddrContains(got, localPeerID) {
		t.Fatalf("selected addrs missing matching peer addr: %#v", got)
	}
}

func TestSelectPresenceAnnounceAddrsRelayOnlyFallback(t *testing.T) {
	localPeerID := mustGenerateTestPeerID(t)
	relayPeerID := mustGenerateTestPeerID(t)
	relayAddrs := make([]string, 0, presenceAnnounceAddressBudget+4)
	for i := 0; i < presenceAnnounceAddressBudget+4; i++ {
		relayAddrs = append(relayAddrs, peerAddr(relayBaseAddr(i, relayPeerID)+"/p2p-circuit", localPeerID))
	}

	got := selectPresenceAnnounceAddrs(localPeerID, relayAddrs, nil)

	if len(got) != presenceAnnounceAddressBudget {
		t.Fatalf("selected relay-only addrs len = %d, want %d: %#v", len(got), presenceAnnounceAddressBudget, got)
	}
	for _, addr := range got {
		if !strings.Contains(addr, "/p2p-circuit/") {
			t.Fatalf("selected relay-only addrs included direct addr %q: %#v", addr, got)
		}
	}
}

func TestSelectPresenceAnnounceAddrsDirectOnlyFallback(t *testing.T) {
	localPeerID := mustGenerateTestPeerID(t)
	listenAddrs := make([]string, 0, presenceAnnounceAddressBudget+4)
	for i := 0; i < presenceAnnounceAddressBudget+4; i++ {
		listenAddrs = append(listenAddrs, peerAddr("/ip4/10.0.0."+strconv.Itoa(10+i)+"/tcp/4001", localPeerID))
	}

	got := selectPresenceAnnounceAddrs(localPeerID, nil, listenAddrs)

	if len(got) != presenceAnnounceAddressBudget {
		t.Fatalf("selected direct-only addrs len = %d, want %d: %#v", len(got), presenceAnnounceAddressBudget, got)
	}
	for _, addr := range got {
		if strings.Contains(addr, "/p2p-circuit/") {
			t.Fatalf("selected direct-only addrs included relay addr %q: %#v", addr, got)
		}
	}
}

func TestSelectPresenceAnnounceAddrsDeterministic(t *testing.T) {
	localPeerID := mustGenerateTestPeerID(t)
	relayPeerID := mustGenerateTestPeerID(t)
	relayAddrs := []string{
		peerAddr(relayBaseAddr(2, relayPeerID)+"/p2p-circuit", localPeerID),
		peerAddr("/dns4/relay.example.com/tcp/4001/p2p/"+relayPeerID+"/p2p-circuit", localPeerID),
		peerAddr(relayBaseAddr(1, relayPeerID)+"/p2p-circuit", localPeerID),
	}
	listenAddrs := []string{
		peerAddr("/ip4/10.0.0.20/udp/4001/quic-v1", localPeerID),
		peerAddr("/dns4/node.example.com/tcp/4001", localPeerID),
		peerAddr("/ip4/8.8.8.8/tcp/4001", localPeerID),
		peerAddr("/ip4/192.168.1.10/tcp/4001", localPeerID),
	}
	reversedRelays := append([]string(nil), relayAddrs...)
	reversedListen := append([]string(nil), listenAddrs...)
	slices.Reverse(reversedRelays)
	slices.Reverse(reversedListen)

	first := selectPresenceAnnounceAddrs(localPeerID, relayAddrs, listenAddrs)
	second := selectPresenceAnnounceAddrs(localPeerID, reversedRelays, reversedListen)

	if !slices.Equal(first, second) {
		t.Fatalf("selected addrs are not deterministic:\nfirst=%#v\nsecond=%#v", first, second)
	}
}

func TestSeedPeerstoreAddrs(t *testing.T) {
	hostNode, err := libp2p.New()
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	defer func() { _ = hostNode.Close() }()

	remotePeerID, err := peer.Decode(mustGenerateTestPeerID(t))
	if err != nil {
		t.Fatalf("decode remote peer id: %v", err)
	}
	otherPeerID := mustGenerateTestPeerID(t)
	transport := &libp2pSyncTransport{host: hostNode}
	transport.seedPeerstoreAddrs(remotePeerID, []string{
		peerAddr("/ip4/8.8.8.8/tcp/4001", remotePeerID.String()),
		peerAddr("/ip4/192.168.1.10/tcp/4001", remotePeerID.String()),
		peerAddr("/ip4/10.0.0.10/tcp/4001", otherPeerID),
		"not-a-multiaddr",
	})

	addrs := hostNode.Peerstore().Addrs(remotePeerID)
	values := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		values = append(values, addr.String())
	}
	if !slices.Contains(values, "/ip4/8.8.8.8/tcp/4001") || !slices.Contains(values, "/ip4/192.168.1.10/tcp/4001") {
		t.Fatalf("peerstore addrs = %#v, want seeded direct addresses", values)
	}
	if slices.Contains(values, "/ip4/10.0.0.10/tcp/4001") {
		t.Fatalf("peerstore addrs included mismatched peer addr: %#v", values)
	}
}

func peerAddr(base, peerID string) string {
	return base + "/p2p/" + peerID
}

func relayBaseAddr(index int, relayPeerID string) string {
	return "/ip4/203.0.113." + strconv.Itoa(10+index) + "/tcp/4001/p2p/" + relayPeerID
}

func anyAddrContains(addrs []string, value string) bool {
	for _, addr := range addrs {
		if strings.Contains(addr, value) {
			return true
		}
	}
	return false
}
