// Package netmap reads what the local node knows about the network and
// reduces it to counts per country. Addresses never leave this package.
package netmap

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/binary"
	"io"
	"net/netip"
	"sort"
	"sync"
)

// country.bin.gz: DB-IP "IP to Country Lite" (CC BY 4.0), IPv4 only, built by
// tools/geogen. Six bytes per range: first address, country code.
//
//go:embed country.bin.gz
var countryGz []byte

var (
	countryOnce sync.Once
	countryTab  []byte
)

func countryTable() []byte {
	countryOnce.Do(func() {
		zr, err := gzip.NewReader(bytes.NewReader(countryGz))
		if err != nil {
			return
		}
		b, err := io.ReadAll(zr)
		if err != nil || len(b)%6 != 0 {
			return
		}
		countryTab = b
	})
	return countryTab
}

// Country returns the ISO 3166 code of a public IPv4 address, or "" for
// anything else: IPv6, private and special ranges, addresses the table lacks.
func Country(a netip.Addr) string {
	a = a.Unmap()
	if !a.Is4() || !a.IsGlobalUnicast() || a.IsPrivate() || cgnat.Contains(a) {
		return ""
	}
	return lookup(countryTable(), binary.BigEndian.Uint32(a.AsSlice()))
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func lookup(tab []byte, ip uint32) string {
	n := len(tab) / 6
	i := sort.Search(n, func(i int) bool { return binary.BigEndian.Uint32(tab[i*6:]) > ip }) - 1
	if i < 0 {
		return ""
	}
	cc := tab[i*6+4 : i*6+6]
	if cc[0] == 0 {
		return ""
	}
	return string(cc)
}
