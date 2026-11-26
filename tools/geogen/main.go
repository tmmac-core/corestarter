// geogen builds the two data files of the network map from their public
// sources. Both results are committed; this tool only runs to refresh them.
//
//	go run ./tools/geogen countries dbip-country-lite.csv internal/netmap/country.bin.gz
//	go run ./tools/geogen map ne_110m_admin_0_countries.geojson ui/worldmap.js
//
// countries: DB-IP "IP to Country Lite" (CC BY 4.0, https://db-ip.com), IPv4
// rows only, neighbouring ranges of one country merged, gzipped. Each record is the
// first address of a range (4 bytes, big endian) and its country (2 bytes);
// a range without a country carries two zero bytes.
//
// map: Natural Earth 1:110m countries (public domain), drawn as one path on
// a 1000x500 plate carrée, plus one point per country for the markers.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/netip"
	"os"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: geogen countries|map <in> <out>")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "countries":
		err = countries(os.Args[2], os.Args[3])
	case "map":
		err = worldMap(os.Args[2], os.Args[3])
	default:
		err = fmt.Errorf("unknown mode %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "geogen:", err)
		os.Exit(1)
	}
}

func countries(in, out string) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()
	r := csv.NewReader(bufio.NewReader(f))
	var buf []byte
	var next uint64 // first address after the last range
	last := ""
	add := func(start uint32, cc string) {
		if cc == last {
			return
		}
		var rec [6]byte
		binary.BigEndian.PutUint32(rec[:4], start)
		copy(rec[4:], cc)
		buf = append(buf, rec[:]...)
		last = cc
	}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		a, err1 := netip.ParseAddr(row[0])
		b, err2 := netip.ParseAddr(row[1])
		if err1 != nil || err2 != nil || !a.Is4() || !b.Is4() {
			continue
		}
		cc := row[2]
		if len(cc) != 2 || cc == "ZZ" {
			cc = "\x00\x00"
		}
		s, e := uint64(binary.BigEndian.Uint32(a.AsSlice())), uint64(binary.BigEndian.Uint32(b.AsSlice()))
		if e < s {
			return fmt.Errorf("range ends before it starts at %s", row[0])
		}
		if s < next {
			return fmt.Errorf("ranges not sorted at %s", row[0])
		}
		if s > next {
			add(uint32(next), "\x00\x00")
		}
		add(uint32(s), cc)
		next = e + 1
	}
	if len(buf) == 0 {
		return fmt.Errorf("no IPv4 rows in %s", in)
	}
	if next <= math.MaxUint32 {
		add(uint32(next), "\x00\x00")
	}
	// gzip without name or time in the header: the same input gives the same bytes.
	var z bytes.Buffer
	zw, err := gzip.NewWriterLevel(&z, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := zw.Write(buf); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(out, z.Bytes(), 0o644)
}

// extra holds label points of places the 1:110m map leaves out (lon, lat).
var extra = map[string][2]float64{
	"SG": {103.8, 1.35}, "HK": {114.2, 22.3}, "MO": {113.55, 22.2}, "MT": {14.4, 35.9}, "BH": {50.55, 26.05},
	"MU": {57.55, -20.3}, "LI": {9.55, 47.15}, "MC": {7.42, 43.74}, "AD": {1.55, 42.55}, "SM": {12.45, 43.94},
	"SC": {55.45, -4.65}, "MV": {73.5, 4.2}, "BB": {-59.55, 13.15}, "GI": {-5.35, 36.14}, "JE": {-2.13, 49.21},
	"GG": {-2.58, 49.46}, "IM": {-4.55, 54.24}, "FO": {-6.9, 62.0}, "BM": {-64.75, 32.3}, "KY": {-81.25, 19.3},
	"CW": {-68.95, 12.2}, "AW": {-69.97, 12.5}, "RE": {55.55, -21.1}, "GP": {-61.55, 16.25}, "MQ": {-61.0, 14.65},
	"GU": {144.8, 13.45}, "VG": {-64.6, 18.45}, "VI": {-64.8, 18.05}, "CV": {-23.6, 15.1}, "WS": {-172.1, -13.75},
	"TO": {-175.2, -21.2}, "AG": {-61.8, 17.07}, "LC": {-60.97, 13.9}, "GD": {-61.68, 12.12}, "KN": {-62.75, 17.33},
	"DM": {-61.36, 15.42}, "VC": {-61.2, 13.25}, "ST": {6.6, 0.25}, "KM": {43.35, -11.65}, "VA": {12.45, 41.9},
	"XK": {20.9, 42.6}, "FR": {2.5, 46.6}, "NO": {9.0, 61.0},
}

func worldMap(in, out string) error {
	raw, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	var fc struct {
		Features []struct {
			Properties map[string]any `json:"properties"`
			Geometry   struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(raw, &fc); err != nil {
		return err
	}
	px := func(lon, lat float64) (int, int) {
		return int(math.Round((lon + 180) / 360 * 1000)), int(math.Round((90 - lat) / 180 * 500))
	}
	var path strings.Builder
	points := map[string][2]int{}
	ring := func(r [][]float64) {
		lx, ly, n := 0, 0, 0
		var b strings.Builder
		for _, c := range r {
			x, y := px(c[0], c[1])
			switch {
			case n == 0:
				fmt.Fprintf(&b, "M%d %d", x, y)
			case x == lx && y == ly:
				continue
			default:
				fmt.Fprintf(&b, "l%d %d", x-lx, y-ly)
			}
			lx, ly = x, y
			n++
		}
		if n >= 3 { // smaller rings vanish at this scale
			path.WriteString(b.String())
			path.WriteString("z")
		}
	}
	for _, f := range fc.Features {
		name, _ := f.Properties["NAME"].(string)
		if name == "Antarctica" {
			continue
		}
		switch f.Geometry.Type {
		case "Polygon":
			var p [][][]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &p); err != nil {
				return err
			}
			for _, r := range p {
				ring(r)
			}
		case "MultiPolygon":
			var mp [][][][]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &mp); err != nil {
				return err
			}
			for _, p := range mp {
				for _, r := range p {
					ring(r)
				}
			}
		}
		cc, _ := f.Properties["ISO_A2_EH"].(string)
		lx, ok1 := f.Properties["LABEL_X"].(float64)
		ly, ok2 := f.Properties["LABEL_Y"].(float64)
		if len(cc) == 2 && cc != "-9" && ok1 && ok2 {
			x, y := px(lx, ly)
			points[cc] = [2]int{x, y}
		}
	}
	for cc, ll := range extra {
		x, y := px(ll[0], ll[1])
		points[cc] = [2]int{x, y}
	}
	ccs := make([]string, 0, len(points))
	for cc := range points {
		ccs = append(ccs, cc)
	}
	sort.Strings(ccs)
	var b strings.Builder
	b.WriteString("// Generated by tools/geogen from Natural Earth 1:110m (public domain). Do not edit.\n")
	fmt.Fprintf(&b, "const WORLD_PATH = %q;\n", path.String())
	b.WriteString("const WORLD_POINTS = {")
	for i, cc := range ccs {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%s:[%d,%d]", cc, points[cc][0], points[cc][1])
	}
	b.WriteString("};\n")
	return os.WriteFile(out, []byte(b.String()), 0o644)
}
