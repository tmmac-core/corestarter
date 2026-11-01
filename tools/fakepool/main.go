// Command fakepool is a mining pool on loopback that accepts every share, for
// the end-to-end test build of the Mac app (e2e_darwin.go). The hash check
// itself is covered by cmd/core-miner's tests.
package main

import (
	"bufio"
	"flag"
	"log"
	"net"
	"regexp"
	"strings"
	"sync/atomic"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:47999", "address to listen on")
	flag.Parse()
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("fake pool on %s", ln.Addr())
	var accepted atomic.Int64
	idRe := regexp.MustCompile(`"id":(\d+)`)
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go func() {
			defer c.Close()
			log.Printf("miner connected from %s", c.RemoteAddr())
			r := bufio.NewReader(c)
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				switch {
				case strings.Contains(line, "eth_submitLogin"):
					log.Printf("login: %s", strings.TrimSpace(line))
					c.Write([]byte(`{"id":1,"jsonrpc":"2.0","result":true}` + "\n"))
				case strings.Contains(line, "eth_getWork"):
					c.Write([]byte(`{"id":5,"jsonrpc":"2.0","result":["0x` + strings.Repeat("ab", 32) + `","0x00","0x0f` + strings.Repeat("ff", 31) + `"]}` + "\n"))
				case strings.Contains(line, "eth_submitWork"):
					n := accepted.Add(1)
					if n%25 == 1 {
						log.Printf("shares accepted: %d", n)
					}
					c.Write([]byte(`{"id":` + idRe.FindStringSubmatch(line)[1] + `,"jsonrpc":"2.0","result":true}` + "\n"))
				}
			}
		}()
	}
}
