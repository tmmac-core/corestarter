package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
)

// Snapshot is what the tray needs to know about the node, read over IPC.
type Snapshot struct {
	Syncing  bool
	Starting uint64
	Current  uint64
	Highest  uint64
	Pulled   uint64 // state entries processed (fast sync)
	Known    uint64 // state entries still to pull (fast sync)
	Head     uint64 // xcb_blockNumber: last fully imported block
	Peers    uint64
	Inbound  int // peers that dialled this node; -1: not known
}

type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// queryNode asks the node for sync progress, head block and peer count.
// gocore speaks newline-free JSON-RPC 2.0 over its IPC pipe; one request at a time.
func queryNode(rw io.ReadWriter) (Snapshot, error) {
	var s Snapshot
	dec := json.NewDecoder(rw)
	enc := json.NewEncoder(rw)

	call := func(id int, method string) (json.RawMessage, error) {
		if err := enc.Encode(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: []interface{}{}}); err != nil {
			return nil, err
		}
		var resp rpcResponse
		if err := dec.Decode(&resp); err != nil {
			return nil, err
		}
		if resp.ID != id {
			return nil, fmt.Errorf("%s: unexpected response id %d", method, resp.ID)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, resp.Error.Message)
		}
		return resp.Result, nil
	}

	raw, err := call(1, "xcb_syncing")
	if err != nil {
		return s, err
	}
	if err := parseSyncing(raw, &s); err != nil {
		return s, err
	}
	if raw, err = call(2, "xcb_blockNumber"); err != nil {
		return s, err
	}
	if s.Head, err = parseHexQuantity(raw); err != nil {
		return s, fmt.Errorf("xcb_blockNumber: %w", err)
	}
	if raw, err = call(3, "net_peerCount"); err != nil {
		return s, err
	}
	if s.Peers, err = parseHexQuantity(raw); err != nil {
		return s, fmt.Errorf("net_peerCount: %w", err)
	}
	// Only for the window's "reachable from outside" line: a node that does not
	// answer this still has a status.
	s.Inbound = -1
	if raw, err = call(4, "admin_peers"); err == nil {
		s.Inbound = countInbound(raw)
	}
	return s, nil
}

// countInbound counts the peers of admin_peers that connected to this node.
func countInbound(raw json.RawMessage) int {
	var peers []struct {
		Network struct {
			Inbound       bool   `json:"inbound"`
			RemoteAddress string `json:"remoteAddress"`
		} `json:"network"`
	}
	if err := json.Unmarshal(raw, &peers); err != nil {
		return -1
	}
	n := 0
	for _, p := range peers {
		// A node from the own home network has not come in from outside. The
		// address is only looked at here, never kept or logged.
		if p.Network.Inbound && !localPeer(p.Network.RemoteAddress) {
			n++
		}
	}
	return n
}

var cgnatNet = netip.MustParsePrefix("100.64.0.0/10")

// localPeer: a peer address from a private, loopback, link-local or carrier
// network, which cannot have reached this node through the router.
func localPeer(remote string) bool {
	ap, err := netip.ParseAddrPort(remote)
	if err != nil {
		return false
	}
	a := ap.Addr().Unmap()
	return a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || cgnatNet.Contains(a)
}

// parseSyncing handles both answers of xcb_syncing: `false` or a progress object.
func parseSyncing(raw json.RawMessage, s *Snapshot) error {
	if strings.TrimSpace(string(raw)) == "false" {
		s.Syncing = false
		return nil
	}
	var p struct {
		StartingBlock string `json:"startingBlock"`
		CurrentBlock  string `json:"currentBlock"`
		HighestBlock  string `json:"highestBlock"`
		PulledStates  string `json:"pulledStates"`
		KnownStates   string `json:"knownStates"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("xcb_syncing: %w", err)
	}
	fields := []struct {
		in  string
		out *uint64
	}{
		{p.StartingBlock, &s.Starting}, {p.CurrentBlock, &s.Current}, {p.HighestBlock, &s.Highest},
		{p.PulledStates, &s.Pulled}, {p.KnownStates, &s.Known},
	}
	for _, f := range fields {
		v, err := parseHex(f.in)
		if err != nil {
			return fmt.Errorf("xcb_syncing: %w", err)
		}
		*f.out = v
	}
	s.Syncing = true
	return nil
}

func parseHexQuantity(raw json.RawMessage) (uint64, error) {
	var str string
	if err := json.Unmarshal(raw, &str); err != nil {
		return 0, err
	}
	return parseHex(str)
}

func parseHex(str string) (uint64, error) {
	if str == "" {
		return 0, nil
	}
	if !strings.HasPrefix(str, "0x") {
		return 0, errors.New("hex quantity without 0x prefix: " + str)
	}
	return strconv.ParseUint(str[2:], 16, 64)
}
