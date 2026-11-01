package main

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"
)

// fakeNode answers JSON-RPC requests on one end of a pipe like gocore's IPC does.
func fakeNode(t *testing.T, answers map[string]string) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	go func() {
		defer server.Close()
		dec := json.NewDecoder(bufio.NewReader(server))
		for {
			var req rpcRequest
			if err := dec.Decode(&req); err != nil {
				return
			}
			res, ok := answers[req.Method]
			var out string
			if ok {
				out = `{"jsonrpc":"2.0","id":` + itoa(req.ID) + `,"result":` + res + "}"
			} else {
				out = `{"jsonrpc":"2.0","id":` + itoa(req.ID) + `,"error":{"code":-32601,"message":"method not found"}}`
			}
			if _, err := server.Write([]byte(out)); err != nil {
				return
			}
		}
	}()
	return client
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func TestQueryNodeSyncing(t *testing.T) {
	c := fakeNode(t, map[string]string{
		"xcb_syncing":     `{"startingBlock":"0x0","currentBlock":"0x868a40","highestBlock":"0x127bc32","pulledStates":"0x0","knownStates":"0x0"}`,
		"xcb_blockNumber": `"0x0"`,
		"net_peerCount":   `"0xc"`,
	})
	defer c.Close()
	s, err := queryNode(c)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Syncing || s.Current != 0x868a40 || s.Highest != 0x127bc32 || s.Peers != 12 || s.Head != 0 {
		t.Fatalf("unexpected snapshot %+v", s)
	}
}

func TestQueryNodeSynced(t *testing.T) {
	c := fakeNode(t, map[string]string{
		"xcb_syncing":     `false`,
		"xcb_blockNumber": `"0x127bc32"`,
		"net_peerCount":   `"0x19"`,
	})
	defer c.Close()
	s, err := queryNode(c)
	if err != nil {
		t.Fatal(err)
	}
	if s.Syncing || s.Head != 19381298 || s.Peers != 25 {
		t.Fatalf("unexpected snapshot %+v", s)
	}
}

func TestQueryNodeRPCError(t *testing.T) {
	c := fakeNode(t, map[string]string{"xcb_syncing": `false`})
	defer c.Close()
	if _, err := queryNode(c); err == nil || !strings.Contains(err.Error(), "xcb_blockNumber") {
		t.Fatalf("want xcb_blockNumber error, got %v", err)
	}
}

func TestParseHex(t *testing.T) {
	if _, err := parseHex("12"); err == nil {
		t.Fatal("missing 0x prefix must fail")
	}
	if v, err := parseHex("0xff"); err != nil || v != 255 {
		t.Fatalf("got %d, %v", v, err)
	}
}
