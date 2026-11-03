package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tmmac-core/corestarter/internal/minercore"
)

// The test binary doubles as the miner: the tests start it as a subprocess
// with CORE_MINER_TEST_MAIN=1, talking to it over stdin/stdout like the tray.
func TestMain(m *testing.M) {
	if os.Getenv("CORE_MINER_TEST_MAIN") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

const donation = "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963"

var idRe = regexp.MustCompile(`"id":(\d+)`)

// verifyingPool hands out one job with an easy target and recomputes every
// submitted share with RandomY: accepted only if nonce and hash really match.
type verifyingPool struct {
	ln                 net.Listener
	header, boundary   [32]byte
	mu                 sync.Mutex
	accepted, rejected int
	logins             []string
	vm                 *rxVM
	ctx                *rxContext
	rejectLogin        bool
}

func newVerifyingPool(t *testing.T, rejectLogin bool) *verifyingPool {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := newContext(false, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	vm, _ := ctx.newVM()
	p := &verifyingPool{ln: ln, vm: vm, ctx: ctx, rejectLogin: rejectLogin}
	for i := range p.header {
		p.header[i] = byte(i * 7)
	}
	// About one share per 16 hashes: fast even in light mode on a CI runner.
	p.boundary[0] = 0x0f
	for i := 1; i < 32; i++ {
		p.boundary[i] = 0xff
	}
	go p.serve()
	t.Cleanup(func() {
		ln.Close()
		p.mu.Lock() // a handler may still be verifying a share
		vm.destroy()
		ctx.release()
		p.vm = nil
		p.mu.Unlock()
	})
	return p
}

func (p *verifyingPool) addr() string { return p.ln.Addr().String() }

func (p *verifyingPool) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		go p.handle(c)
	}
}

func (p *verifyingPool) handle(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	reply := func(s string) { c.Write([]byte(s + "\n")) }
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		switch {
		case strings.Contains(line, "eth_submitLogin"):
			p.mu.Lock()
			p.logins = append(p.logins, line)
			p.mu.Unlock()
			if p.rejectLogin {
				reply(`{"id":1,"jsonrpc":"2.0","result":null,"error":{"code":-1,"message":"invalid address"}}`)
				continue
			}
			reply(`{"id":1,"jsonrpc":"2.0","result":true}`)
		case strings.Contains(line, "eth_getWork"):
			reply(fmt.Sprintf(`{"id":5,"jsonrpc":"2.0","result":["0x%x","0x00","0x%x"]}`, p.header, p.boundary))
		case strings.Contains(line, "eth_submitWork"):
			var msg struct {
				Params []string `json:"params"`
			}
			json.Unmarshal([]byte(line), &msg)
			ok := p.verify(msg.Params)
			p.mu.Lock()
			if ok {
				p.accepted++
			} else {
				p.rejected++
			}
			p.mu.Unlock()
			reply(fmt.Sprintf(`{"id":%s,"jsonrpc":"2.0","result":%v}`, idRe.FindStringSubmatch(line)[1], ok))
		}
	}
}

func (p *verifyingPool) verify(params []string) bool {
	if len(params) != 3 || params[1] != fmt.Sprintf("0x%x", p.header) {
		return false
	}
	nonce, err := strconv.ParseUint(strings.TrimPrefix(params[0], "0x"), 16, 64)
	if err != nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.vm == nil {
		return false
	}
	h := p.vm.hash(p.header, nonce)
	return "0x"+hex.EncodeToString(h[:]) == params[2] && string(h[:]) <= string(p.boundary[:])
}

func (p *verifyingPool) counts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.accepted, p.rejected
}

type minerProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	mu     sync.Mutex
	status []minercore.MinerStatus
	logs   []string
	done   chan error
}

func startMiner(t *testing.T, args ...string) *minerProc {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"--light", "--api=false"}, args...)...)
	cmd.Env = append(os.Environ(), "CORE_MINER_TEST_MAIN=1")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	m := &minerProc{cmd: cmd, stdin: stdin, done: make(chan error, 1)}
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			var l minercore.MinerLine
			if json.Unmarshal(sc.Bytes(), &l) != nil {
				continue
			}
			m.mu.Lock()
			if l.Status != nil {
				m.status = append(m.status, *l.Status)
			} else {
				m.logs = append(m.logs, l.Msg)
			}
			m.mu.Unlock()
		}
		m.done <- cmd.Wait()
	}()
	t.Cleanup(func() {
		cmd.Process.Kill()
		if t.Failed() {
			m.mu.Lock()
			t.Logf("miner log:\n%s", strings.Join(m.logs, "\n"))
			m.mu.Unlock()
		}
	})
	return m
}

func (m *minerProc) last() minercore.MinerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.status) == 0 {
		return minercore.MinerStatus{}
	}
	return m.status[len(m.status)-1]
}

func (m *minerProc) send(c minercore.Command) {
	b, _ := json.Marshal(c)
	m.stdin.Write(append(b, '\n'))
}

func (m *minerProc) wait(t *testing.T, d time.Duration) int {
	t.Helper()
	select {
	case err := <-m.done:
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0
	case <-time.After(d):
		t.Fatal("miner did not exit")
		return -1
	}
}

func eventually(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestMinesVerifiedSharesAndStopsOnEOF(t *testing.T) {
	pool := newVerifyingPool(t, false)
	m := startMiner(t, "--address", donation, "--worker", "ci-test", "--pool", pool.addr(), "--power", "25")
	eventually(t, "3 shares the pool verified", 3*time.Minute, func() bool {
		a, _ := pool.counts()
		return a >= 3
	})
	if _, rej := pool.counts(); rej != 0 {
		t.Fatalf("%d shares failed verification", rej)
	}
	pool.mu.Lock()
	login := pool.logins[0]
	pool.mu.Unlock()
	if !strings.Contains(login, donation) || !strings.Contains(login, `"worker":"ci-test"`) {
		t.Fatalf("login %s", login)
	}
	eventually(t, "status mining with accepted shares", 30*time.Second, func() bool {
		s := m.last()
		return s.State == "mining" && s.Accepted >= 1 && s.Hashrate > 0
	})
	s := m.last()
	t.Logf("status: %+v", s)

	m.send(minercore.Command{Cmd: "power", Percent: 100})
	eventually(t, "power change", 30*time.Second, func() bool { return m.last().Threads == s.MaxThreads })

	m.stdin.Close() // like the tray going away
	if code := m.wait(t, 30*time.Second); code != minercore.ExitOK {
		t.Fatalf("exit code %d", code)
	}
}

func TestRejectedLoginExitsWithCode(t *testing.T) {
	pool := newVerifyingPool(t, true)
	m := startMiner(t, "--address", donation, "--pool", pool.addr())
	if code := m.wait(t, 2*time.Minute); code != minercore.ExitLoginRejected {
		t.Fatalf("exit code %d, want %d", code, minercore.ExitLoginRejected)
	}
	if s := m.last(); s.State != "error" || !strings.Contains(s.Error, "invalid address") {
		t.Fatalf("status %+v", s)
	}
}

func TestBadAddressExits(t *testing.T) {
	m := startMiner(t, "--address", donation[:len(donation)-1]+"4", "--pool", "127.0.0.1:1")
	if code := m.wait(t, 30*time.Second); code != minercore.ExitBadArgs {
		t.Fatalf("exit code %d", code)
	}
}

func TestSelfTestVectors(t *testing.T) {
	for _, jit := range []bool{true, false} {
		ctx, err := newContext(false, 1, jit)
		if err != nil {
			t.Fatal(err)
		}
		vm, _ := ctx.newVM()
		if msg := selfTest(vm); msg != "" {
			t.Errorf("jit=%v: %s", jit, msg)
		}
		vm.destroy()
		ctx.release()
	}
}

// TestBench: every --bench variant computes correct hashes and reports a rate.
func TestBench(t *testing.T) {
	defer func() { rxBench.secure, rxBench.largePages = false, false }()
	for _, v := range []struct{ secure, largePages bool }{{true, false}, {false, true}, {true, true}} {
		rxBench.secure, rxBench.largePages = v.secure, v.largePages
		ctx, err := newContext(false, 1, true)
		if err != nil {
			t.Fatal(err)
		}
		vm, _ := ctx.newVM()
		if vm == nil {
			t.Fatalf("%+v: no VM", v)
		}
		if msg := selfTest(vm); msg != "" {
			t.Errorf("%+v: %s", v, msg)
		}
		vm.destroy()
		ctx.release()
	}
	rxBench.secure, rxBench.largePages = false, false

	cmd := exec.Command(os.Args[0], "--bench", "1", "--threads", "2")
	cmd.Env = append(os.Environ(), "CORE_MINER_TEST_MAIN=1")
	outb, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, outb)
	}
	t.Logf("\n%s", outb)
	rate := regexp.MustCompile(`\s(\d+) H/s`)
	if n, want := len(rate.FindAllStringSubmatch(string(outb), -1)), len(benchVariants()); n != want {
		t.Fatalf("%d rates, want %d", n, want)
	}
	for _, m := range rate.FindAllStringSubmatch(string(outb), -1) {
		if v, _ := strconv.Atoi(m[1]); v <= 0 {
			t.Errorf("rate %s", m[1])
		}
	}
}
