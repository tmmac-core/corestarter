//go:build darwin

package main

// gocoreVersion: the official release the Intel build bundles
// (scripts/build-mac.sh MAC_ARCH=x86_64 checks its pinned download). Core
// ships no darwin-x86_64 file from v2.2.2 on; v2.2.1 differs from it only in
// an RPC parameter (balanceOf) the starter does not use, not in consensus.
const gocoreVersion = "v2.2.1"
