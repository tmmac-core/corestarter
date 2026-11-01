package minercore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Public web API of Catch That Rabbit (open-ethereum-pool style), for earnings,
// balance and payout. Allowed (16.10.2026): same operator as the pool,
// it knows address, IP and hashrate anyway. HTTPS GET only; nothing is sent but
// the address in the path.
//
//	/api/settings           -> PayoutThreshold (1e-9 XCB), PayoutInterval ("240m"), PoolFee (%)
//	/api/stats              -> hashrate, blockReward (1e-18 XCB), nodes[0].difficulty/.blocktime (s)
//	/api/accounts/<address> -> stats.balance / .immature / .paid (1e-9 XCB)
//
// Every region is its own pool with its own balance and pays out only above the
// threshold, so the account is queried in all regions and summed.

var APIRegions = []string{"de", "fi", "sg", "hk"}

type PoolSettings struct {
	PayoutThresholdXCB float64 `json:"payoutThreshold"`
	PayoutIntervalMin  int     `json:"payoutIntervalMin"`
	FeePercent         float64 `json:"feePercent"`
}

type PoolNetwork struct {
	PoolHashrate   float64 `json:"poolHashrate"`
	Difficulty     float64 `json:"difficulty"`
	BlockTimeS     float64 `json:"blockTime"`
	BlockRewardXCB float64 `json:"blockReward"`
}

type Account struct {
	BalanceXCB  float64 `json:"balance"`
	ImmatureXCB float64 `json:"immature"`
	PaidXCB     float64 `json:"paid"`
}

// RegionAccount: Account nil = region not reachable right now.
type RegionAccount struct {
	Region  string   `json:"region"`
	Account *Account `json:"account"`
}

type Earnings struct {
	Settings PoolSettings    `json:"settings"`
	Network  PoolNetwork     `json:"network"`
	Regions  []RegionAccount `json:"regions"`
	Fetched  time.Time       `json:"fetched"`
}

// Total sums all reachable regions; nil if none (or no address).
func (e Earnings) Total() *Account {
	var sum Account
	found := false
	for _, r := range e.Regions {
		if r.Account != nil {
			found = true
			sum.BalanceXCB += r.Account.BalanceXCB
			sum.ImmatureXCB += r.Account.ImmatureXCB
			sum.PaidXCB += r.Account.PaidXCB
		}
	}
	if !found {
		return nil
	}
	return &sum
}

// Incomplete: at least one region is missing from the total.
func (e Earnings) Incomplete() bool {
	if e.Total() == nil {
		return false
	}
	for _, r := range e.Regions {
		if r.Account == nil {
			return true
		}
	}
	return false
}

// OpenRegions are regions with an open amount (balance or immature), largest first.
func (e Earnings) OpenRegions() []RegionAccount {
	var out []RegionAccount
	for _, r := range e.Regions {
		if r.Account != nil && r.Account.BalanceXCB+r.Account.ImmatureXCB > 0 {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		x, y := out[a].Account, out[b].Account
		return x.BalanceXCB+x.ImmatureXCB > y.BalanceXCB+y.ImmatureXCB
	})
	return out
}

const nano = 1e9

// APIBaseURL maps a pool host to its region's API; unknown -> DE.
func APIBaseURL(poolHost string) string {
	region := strings.ToLower(strings.SplitN(poolHost, ".", 2)[0])
	for _, r := range APIRegions {
		if r == region {
			return regionURL(r)
		}
	}
	return regionURL("de")
}

func regionURL(r string) string { return "https://" + r + "-api.catchthatrabbit.com" }

// The API sends numbers partly as strings ("534722549"), partly as numbers.
func num(m map[string]json.RawMessage, key string) float64 {
	raw, ok := m[key]
	if !ok {
		return 0
	}
	s := strings.Trim(string(raw), `"`)
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return 0 // "NaN", "Inf", 1e400: not a pool value, and JSON cannot carry it on
	}
	return f
}

func ParsePoolSettings(body []byte) (PoolSettings, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return PoolSettings{}, err
	}
	var interval string
	json.Unmarshal(m["PayoutInterval"], &interval)
	mins, _ := strconv.Atoi(strings.TrimSuffix(interval, "m"))
	return PoolSettings{
		PayoutThresholdXCB: num(m, "PayoutThreshold") / nano,
		PayoutIntervalMin:  mins,
		FeePercent:         num(m, "PoolFee"),
	}, nil
}

func ParsePoolStats(body []byte) (PoolNetwork, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return PoolNetwork{}, err
	}
	var nodes []map[string]json.RawMessage
	if err := json.Unmarshal(m["nodes"], &nodes); err != nil || len(nodes) == 0 {
		return PoolNetwork{}, errors.New("no network data")
	}
	return PoolNetwork{
		PoolHashrate:   num(m, "hashrate"),
		Difficulty:     num(nodes[0], "difficulty"),
		BlockTimeS:     num(nodes[0], "blocktime"),
		BlockRewardXCB: num(m, "blockReward") / 1e18,
	}, nil
}

func ParseAccount(body []byte) (Account, error) {
	var m struct {
		Stats map[string]json.RawMessage `json:"stats"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return Account{}, err
	}
	if m.Stats == nil {
		return Account{}, errors.New("no account")
	}
	return Account{num(m.Stats, "balance") / nano, num(m.Stats, "immature") / nano, num(m.Stats, "paid") / nano}, nil
}

// XCBPerDay is the expected yield for hashrate: share of the network hashrate
// (difficulty / block time) x blocks per day x block reward, minus pool fee.
// An average: the pool shares by PPLNS, short-term it varies.
func XCBPerDay(hashrate float64, n PoolNetwork, feePercent float64) float64 {
	if hashrate <= 0 || n.Difficulty <= 0 || n.BlockTimeS <= 0 {
		return 0
	}
	networkHashrate := n.Difficulty / n.BlockTimeS
	v := hashrate / networkHashrate * (86400 / n.BlockTimeS) * n.BlockRewardXCB * (1 - feePercent/100)
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return v
}

// HTTPError carries the status code, so 404 (never mined there) is told apart.
type HTTPError struct{ Code int }

func (e HTTPError) Error() string { return fmt.Sprintf("HTTP %d", e.Code) }

// FetchEarnings queries everything in one go; address "" = pool/network data only.
func FetchEarnings(poolHost, address string, get func(url string) ([]byte, error)) (Earnings, error) {
	if get == nil {
		get = httpGet
	}
	base := APIBaseURL(poolHost)
	var e Earnings
	body, err := get(base + "/api/settings")
	if err != nil {
		return e, err
	}
	if e.Settings, err = ParsePoolSettings(body); err != nil {
		return e, err
	}
	if body, err = get(base + "/api/stats"); err != nil {
		return e, err
	}
	if e.Network, err = ParsePoolStats(body); err != nil {
		return e, err
	}
	if address != "" {
		for _, r := range APIRegions {
			ra := RegionAccount{Region: r}
			body, err := get(regionURL(r) + "/api/accounts/" + address)
			var he HTTPError
			switch {
			case err == nil:
				if a, err := ParseAccount(body); err == nil {
					ra.Account = &a
				}
			case errors.As(err, &he) && he.Code == 404:
				// never mined there -> 0; other errors stay unknown, not a fake "0"
				ra.Account = &Account{}
			}
			e.Regions = append(e.Regions, ra)
		}
	}
	e.Fetched = time.Now()
	return e, nil
}

// apiClient follows redirects only to Catch That Rabbit itself over HTTPS: the
// starter connects nowhere else (no other host, no plain HTTP). Any other
// redirect ends with its 3xx response, which httpGet reports as an error.
var apiClient = &http.Client{Timeout: 10 * time.Second, CheckRedirect: ctrRedirectOnly}

func ctrRedirectOnly(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if !allowedAPIURL(req.URL.Scheme, req.URL.Hostname()) {
		return http.ErrUseLastResponse
	}
	return nil
}

func allowedAPIURL(scheme, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return scheme == "https" && (host == "catchthatrabbit.com" || strings.HasSuffix(host, ".catchthatrabbit.com"))
}

func httpGet(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := apiClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, HTTPError{resp.StatusCode}
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
