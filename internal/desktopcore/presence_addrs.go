package desktopcore

import (
	"net"
	"sort"
	"strings"

	ma "github.com/multiformats/go-multiaddr"
)

const (
	// Keep registry presence compact: it is a rendezvous payload, not a dump of
	// every address libp2p discovered on the host.
	presenceAnnounceAddressBudget = 12
	presenceAnnounceRelayBudget   = 3
)

type presenceAddrClass int

const (
	presenceAddrRelay presenceAddrClass = iota
	presenceAddrPublicDirect
	presenceAddrPrivateDirect
	presenceAddrFallbackDirect
)

type presenceAddrCandidate struct {
	value    string
	class    presenceAddrClass
	score    int
	protocol int
}

func selectPresenceAnnounceAddrs(localPeerID string, relayAddrs []string, listenAddrs []string) []string {
	localPeerID = strings.TrimSpace(localPeerID)
	candidates := classifyPresenceAddrCandidates(localPeerID, append(append([]string(nil), relayAddrs...), listenAddrs...))
	if len(candidates) == 0 {
		return nil
	}

	byClass := map[presenceAddrClass][]presenceAddrCandidate{}
	for _, candidate := range candidates {
		byClass[candidate.class] = append(byClass[candidate.class], candidate)
	}
	for class := range byClass {
		byClass[class] = interleavePresenceAddrProtocols(byClass[class])
	}

	relayBudget := minInt(presenceAnnounceRelayBudget, presenceAnnounceAddressBudget)
	selectedRelay := takePresenceAddrCandidates(byClass[presenceAddrRelay], relayBudget)
	directBudget := presenceAnnounceAddressBudget - len(selectedRelay)
	privateReserve := 0
	if len(byClass[presenceAddrPrivateDirect]) > 0 && directBudget > 1 {
		privateReserve = minInt(len(byClass[presenceAddrPrivateDirect]), maxInt(2, directBudget/3))
		if privateReserve >= directBudget {
			privateReserve = directBudget - 1
		}
	}
	selectedPublic := takePresenceAddrCandidates(byClass[presenceAddrPublicDirect], directBudget-privateReserve)
	selectedPrivate := takePresenceAddrCandidates(byClass[presenceAddrPrivateDirect], directBudget-len(selectedPublic))
	directBudget -= len(selectedPublic) + len(selectedPrivate)
	selectedFallback := takePresenceAddrCandidates(byClass[presenceAddrFallbackDirect], directBudget)

	selectedCount := len(selectedRelay) + len(selectedPublic) + len(selectedPrivate) + len(selectedFallback)
	if selectedCount < presenceAnnounceAddressBudget && len(byClass[presenceAddrRelay]) > len(selectedRelay) {
		extraRelay := takePresenceAddrCandidates(byClass[presenceAddrRelay][len(selectedRelay):], presenceAnnounceAddressBudget-selectedCount)
		selectedRelay = append(selectedRelay, extraRelay...)
	}

	out := make([]string, 0, presenceAnnounceAddressBudget)
	if len(selectedRelay) > 0 {
		out = append(out, selectedRelay[0].value)
	}
	out = appendPresenceAddrValues(out, selectedPublic)
	out = appendPresenceAddrValues(out, selectedPrivate)
	if len(selectedRelay) > 1 {
		out = appendPresenceAddrValues(out, selectedRelay[1:])
	}
	out = appendPresenceAddrValues(out, selectedFallback)
	if len(out) > presenceAnnounceAddressBudget {
		out = out[:presenceAnnounceAddressBudget]
	}
	return compactNonEmptyStrings(out)
}

func classifyPresenceAddrCandidates(localPeerID string, values []string) []presenceAddrCandidate {
	seen := make(map[string]struct{}, len(values))
	out := make([]presenceAddrCandidate, 0, len(values))
	for _, value := range values {
		addr, ok := parsePresenceAddr(value, localPeerID)
		if !ok {
			continue
		}
		canonical := addr.String()
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		candidate, ok := classifyPresenceAddr(addr)
		if !ok {
			continue
		}
		candidate.value = canonical
		out = append(out, candidate)
	}
	sortPresenceAddrCandidates(out)
	return out
}

func parsePresenceAddr(value, localPeerID string) (ma.Multiaddr, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}
	addr, err := ma.NewMultiaddr(value)
	if err != nil {
		return nil, false
	}
	if localPeerID != "" && finalPresenceMultiaddrPeerID(addr) != localPeerID {
		return nil, false
	}
	return addr, true
}

func classifyPresenceAddr(addr ma.Multiaddr) (presenceAddrCandidate, bool) {
	info := inspectPresenceAddr(addr)
	candidate := presenceAddrCandidate{
		class:    presenceAddrFallbackDirect,
		score:    presenceAddrScore(info),
		protocol: info.transport,
	}
	if info.relay {
		candidate.class = presenceAddrRelay
		return candidate, true
	}
	if !info.hasRoutableName && !info.hasUsableIP {
		return presenceAddrCandidate{}, false
	}
	switch {
	case info.hasDNS || info.hasPublicIP:
		candidate.class = presenceAddrPublicDirect
	case info.hasPrivateIP:
		candidate.class = presenceAddrPrivateDirect
	default:
		candidate.class = presenceAddrFallbackDirect
	}
	return candidate, true
}

type presenceAddrInfo struct {
	relay           bool
	hasDNS          bool
	hasRoutableName bool
	hasUsableIP     bool
	hasPublicIP     bool
	hasPrivateIP    bool
	hasIPv4         bool
	hasIPv6         bool
	transport       int
}

func inspectPresenceAddr(addr ma.Multiaddr) presenceAddrInfo {
	var info presenceAddrInfo
	for _, component := range addr {
		switch component.Code() {
		case ma.P_CIRCUIT:
			info.relay = true
		case ma.P_DNS, ma.P_DNS4, ma.P_DNS6, ma.P_DNSADDR:
			info.hasDNS = true
			info.hasRoutableName = true
		case ma.P_IP4, ma.P_IP6:
			ip := net.ParseIP(strings.TrimSpace(component.Value()))
			if ip == nil || isUnusablePresenceIP(ip) {
				continue
			}
			info.hasUsableIP = true
			if component.Code() == ma.P_IP4 {
				info.hasIPv4 = true
			} else {
				info.hasIPv6 = true
			}
			if isPrivatePresenceIP(ip) {
				info.hasPrivateIP = true
			} else {
				info.hasPublicIP = true
				info.hasRoutableName = true
			}
		case ma.P_TCP, ma.P_UDP:
			if info.transport == 0 {
				info.transport = component.Code()
			}
		}
	}
	return info
}

func isUnusablePresenceIP(ip net.IP) bool {
	return ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast()
}

func isPrivatePresenceIP(ip net.IP) bool {
	if ip.IsPrivate() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
	}
	return false
}

func presenceAddrScore(info presenceAddrInfo) int {
	score := 0
	if info.hasDNS {
		score -= 100
	}
	if info.hasPublicIP {
		score -= 60
	}
	if info.hasPrivateIP {
		score -= 30
	}
	if info.transport == ma.P_TCP {
		score -= 10
	}
	if info.hasIPv4 {
		score -= 4
	}
	if info.hasIPv6 {
		score -= 2
	}
	return score
}

func sortPresenceAddrCandidates(items []presenceAddrCandidate) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].class != items[j].class {
			return items[i].class < items[j].class
		}
		if items[i].score != items[j].score {
			return items[i].score < items[j].score
		}
		return items[i].value < items[j].value
	})
}

func interleavePresenceAddrProtocols(items []presenceAddrCandidate) []presenceAddrCandidate {
	if len(items) <= 2 {
		return append([]presenceAddrCandidate(nil), items...)
	}
	groups := make(map[int][]presenceAddrCandidate)
	order := make([]int, 0, 3)
	for _, item := range items {
		key := item.protocol
		if key == 0 {
			key = -1
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], item)
	}
	sort.SliceStable(order, func(i, j int) bool {
		left := groups[order[i]][0]
		right := groups[order[j]][0]
		if left.score != right.score {
			return left.score < right.score
		}
		return left.value < right.value
	})
	out := make([]presenceAddrCandidate, 0, len(items))
	for len(out) < len(items) {
		for _, key := range order {
			if len(groups[key]) == 0 {
				continue
			}
			out = append(out, groups[key][0])
			groups[key] = groups[key][1:]
		}
	}
	return out
}

func takePresenceAddrCandidates(items []presenceAddrCandidate, limit int) []presenceAddrCandidate {
	if limit <= 0 || len(items) == 0 {
		return nil
	}
	if len(items) < limit {
		limit = len(items)
	}
	return append([]presenceAddrCandidate(nil), items[:limit]...)
}

func appendPresenceAddrValues(out []string, items []presenceAddrCandidate) []string {
	for _, item := range items {
		out = append(out, item.value)
	}
	return out
}

func finalPresenceMultiaddrPeerID(addr ma.Multiaddr) string {
	if addr == nil {
		return ""
	}
	var value string
	for _, component := range addr {
		if component.Code() == ma.P_P2P {
			value = strings.TrimSpace(component.Value())
		}
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
