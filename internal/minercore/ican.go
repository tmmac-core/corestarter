// Package minercore holds the platform-independent parts of the miner: address
// check, pool protocol, server choice, support share and pool API. Ported from
// the Android miner (core-mobile-miner, module miner-core) with its tests.
package minercore

import (
	"errors"
	"math/big"
	"regexp"
	"strings"
)

// Network of a Core address (CIP-100 / ICAN).
type Network int

const (
	Mainnet Network = iota
	Testnet
	Private
)

// ICAN is a checked Core address in lower case.
type ICAN struct {
	Address string
	Network Network
}

var (
	ErrAddressFormat   = errors.New("not a Core address (CB + 2 check digits + 40 hex characters)")
	ErrAddressChecksum = errors.New("address check digits do not match (typo?)")
	ErrNotMainnet      = errors.New("not a mainnet address (must start with cb)")

	icanFormat = regexp.MustCompile(`^(CB|AB|CE)[0-9]{2}[0-9A-F]{40}$`)
	icanPrefix = regexp.MustCompile(`(?i)^(?:payto://xcb/|corepass:)([^?#/:]+)`)
	icanSpace  = regexp.MustCompile(`[\s\x{00A0}]`)
)

// ParseICAN accepts bare addresses (also lower case or grouped with spaces),
// corepass:<address> and payto://xcb/<address>?… . The check digits work like
// an IBAN (ISO 7064 MOD-97-10), so a typo is caught before payouts go astray.
func ParseICAN(input string) (ICAN, error) {
	s := strings.TrimSpace(input)
	if m := icanPrefix.FindStringSubmatch(s); m != nil {
		s = m[1]
	}
	s = strings.ToUpper(icanSpace.ReplaceAllString(s, ""))
	if !icanFormat.MatchString(s) {
		return ICAN{}, ErrAddressFormat
	}
	if !icanChecksumOK(s) {
		return ICAN{}, ErrAddressChecksum
	}
	n := Mainnet
	switch s[:2] {
	case "AB":
		n = Testnet
	case "CE":
		n = Private
	}
	return ICAN{Address: strings.ToLower(s), Network: n}, nil
}

func icanChecksumOK(addr string) bool {
	rearranged := addr[4:] + addr[:4]
	var digits strings.Builder
	for _, c := range rearranged {
		if c >= '0' && c <= '9' {
			digits.WriteRune(c)
		} else {
			digits.WriteString(big.NewInt(int64(c - 'A' + 10)).String())
		}
	}
	n, ok := new(big.Int).SetString(digits.String(), 10)
	return ok && new(big.Int).Mod(n, big.NewInt(97)).Int64() == 1
}
