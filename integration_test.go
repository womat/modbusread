package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simonvetter/modbus"
)

// testHandler is a tiny Modbus server backing store. Addresses outside the
// populated range answer with "illegal data address", which is what a real
// device does for a register that does not exist.
type testHandler struct {
	mu   sync.Mutex
	base uint16
	regs []uint16
}

func (h *testHandler) set(addr uint16, value uint16) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.regs[addr-h.base] = value
}

func (h *testHandler) HandleHoldingRegisters(req *modbus.HoldingRegistersRequest) ([]uint16, error) {
	if req.IsWrite {
		return nil, modbus.ErrIllegalFunction
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if req.Addr < h.base || int(req.Addr-h.base)+int(req.Quantity) > len(h.regs) {
		return nil, modbus.ErrIllegalDataAddress
	}
	off := req.Addr - h.base
	out := make([]uint16, req.Quantity)
	copy(out, h.regs[off:off+req.Quantity])
	return out, nil
}

func (h *testHandler) HandleInputRegisters(req *modbus.InputRegistersRequest) ([]uint16, error) {
	return nil, modbus.ErrIllegalFunction
}
func (h *testHandler) HandleCoils(*modbus.CoilsRequest) ([]bool, error) {
	return nil, modbus.ErrIllegalFunction
}
func (h *testHandler) HandleDiscreteInputs(*modbus.DiscreteInputsRequest) ([]bool, error) {
	return nil, modbus.ErrIllegalFunction
}

// startServer brings up a Modbus TCP server holding 300 registers starting at
// 42000, and returns its host:port.
func startServer(t *testing.T) (string, *testHandler) {
	t.Helper()

	h := &testHandler{base: 42000, regs: make([]uint16, 300)}
	h.regs[82] = 100     // 42082: a plausible SOC
	h.regs[100] = 0x0000 // 42100/42101: 1.0f, low word first
	h.regs[101] = 0x3F80 // (the word order many devices use for floats)
	h.regs[200] = 0xFFFB // 42200: -5 as int16

	addr := freePort(t)
	srv, err := modbus.NewServer(&modbus.ServerConfiguration{
		URL:        "tcp://" + addr,
		Timeout:    2 * time.Second,
		MaxClients: 4,
	}, h)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return addr, h
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// exec runs the command exactly as a user would and returns stdout plus the
// exit code.
func exec(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	if errOut.Len() > 0 {
		t.Logf("stderr: %s", errOut.String())
	}
	return out.String(), code
}

func TestReadSingleRegister(t *testing.T) {
	addr, _ := startServer(t)

	out, code := exec(t, addr, "42082", "uint16")
	if code != 0 {
		t.Fatalf("exit code %d, output:\n%s", code, out)
	}
	if !strings.Contains(out, "42082") || !strings.Contains(out, "0x0064") || !strings.HasSuffix(strings.TrimSpace(out), "100") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

// The hex form of an address must reach the same register as the decimal one.
func TestHexAddressEqualsDecimal(t *testing.T) {
	addr, _ := startServer(t)

	dec, _ := exec(t, addr, "42082", "uint16")
	hex, _ := exec(t, addr, fmt.Sprintf("0x%X", 42082), "uint16")
	if dec != hex {
		t.Fatalf("decimal and hex address disagree:\n%s\n%s", dec, hex)
	}
}

func TestHexOutput(t *testing.T) {
	addr, _ := startServer(t)

	out, code := exec(t, addr, "42200", "int16", "--out", "hex")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(out, "0xA4D8") { // the address
		t.Errorf("address not in hex:\n%s", out)
	}
	if !strings.Contains(strings.TrimSpace(out), "0xFFFB") { // two's complement
		t.Errorf("value not in hex:\n%s", out)
	}

	dec, _ := exec(t, addr, "42200", "int16")
	if !strings.Contains(dec, "-5") {
		t.Errorf("decimal output should show -5:\n%s", dec)
	}
}

func TestFloatWordOrder(t *testing.T) {
	addr, _ := startServer(t)

	out, code := exec(t, addr, "42100", "float32", "--word-order", "low")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(out, "1\n") {
		t.Fatalf("expected 1, got:\n%s", out)
	}
}

// A single invalid address inside a range must not take the whole scan down:
// the surrounding registers still have to be reported.
func TestRangeIsolatesBadAddress(t *testing.T) {
	addr, _ := startServer(t)

	// 42000..42299 exist, so a range crossing the end mixes good and bad.
	out, code := exec(t, addr, "42295", "raw", "--count", "10")
	if code != 0 {
		t.Fatalf("exit code %d, output:\n%s", code, out)
	}
	lines := dataLines(out)
	if len(lines) != 10 {
		t.Fatalf("expected 10 rows, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(out, "illegal data address") {
		t.Errorf("expected an illegal data address row:\n%s", out)
	}
	if !strings.Contains(lines[0], "42295") {
		t.Errorf("first row should be 42295:\n%s", out)
	}
}

// Reading more than 125 registers has to be split into several requests.
func TestChunking(t *testing.T) {
	addr, _ := startServer(t)

	out, code := exec(t, addr, "42000", "raw", "--count", "200")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if got := len(dataLines(out)); got != 200 {
		t.Fatalf("expected 200 rows, got %d", got)
	}
	if strings.Contains(out, "illegal") {
		t.Fatalf("chunked read should have succeeded:\n%s", out)
	}
}

func TestJSONOutput(t *testing.T) {
	addr, _ := startServer(t)

	out, code := exec(t, addr, "42082", "uint16", "--json", "--out", "hex")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	// --out must not leak into JSON: it stays decimal and numeric.
	want := `{"address":42082,"raw":["0x0064"],"value":100}`
	if strings.TrimSpace(out) != want {
		t.Fatalf("got %s, want %s", strings.TrimSpace(out), want)
	}
}

func TestPollingSamples(t *testing.T) {
	addr, _ := startServer(t)

	out, code := exec(t, addr, "42082", "uint16", "--interval", "20ms", "--samples", "3")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if got := len(dataLines(out)); got != 3 {
		t.Fatalf("expected 3 samples, got %d:\n%s", got, out)
	}
}

// --on-change prints the baseline, then only what actually changed.
func TestPollingOnChange(t *testing.T) {
	addr, h := startServer(t)

	go func() {
		time.Sleep(120 * time.Millisecond)
		h.set(42084, 7)
	}()

	out, code := exec(t, addr, "42082", "uint16", "--count", "4", "--interval", "40ms", "--samples", "8", "--on-change")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	lines := dataLines(out)
	if len(lines) != 5 {
		t.Fatalf("expected 4 baseline rows plus 1 change, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[4], "42084") || !strings.HasSuffix(strings.TrimSpace(lines[4]), "7") {
		t.Fatalf("the change row should be 42084 = 7:\n%s", out)
	}
}

func TestUnreachableHostFails(t *testing.T) {
	addr := freePort(t) // nothing listening there
	_, code := exec(t, addr, "42082", "uint16", "--timeout", "200ms")
	if code == 0 {
		t.Fatal("expected a non-zero exit code")
	}
}

func TestBadArguments(t *testing.T) {
	for _, args := range [][]string{
		{"127.0.0.1"},
		{"127.0.0.1", "42082"},
		{"127.0.0.1", "42082", "word"},
		{"127.0.0.1", "0xZZ", "uint16"},
		{"127.0.0.1", "42082", "uint16", "--fc", "coil"},
		{"127.0.0.1", "42082", "uint16", "--out", "oct"},
		{"127.0.0.1", "42082", "uint16", "--count", "0"},
		{"127.0.0.1", "65535", "uint32"}, // would run past the address space
		{"127.0.0.1", "0", "uint16", "--count", "65537"},
		// count*2 overflows int and would slip past the range check
		{"127.0.0.1", "0", "uint32", "--count", "4611686018427387904"},
	} {
		if _, code := exec(t, args...); code == 0 {
			t.Errorf("%v: expected a non-zero exit code", args)
		}
	}
}

// Flags that cannot take effect, or values out of range, are refused. The
// server answers, so a run that let the flag through would succeed – and a
// negative interval used to crash.
func TestFlagsRejected(t *testing.T) {
	addr, _ := startServer(t)

	for _, args := range [][]string{
		{"--interval", "-1s"},
		{"--interval", "20ms", "--samples", "-1"},
		{"--timeout", "-1s"},
		{"--samples", "3"}, // only with --interval
		{"--on-change"},    // only with --interval
		{"--unit", "256"},  // would wrap to 0, the broadcast address
		{"--word-order", "middle"},
		{"--byte-order", "middle"},
	} {
		if _, code := exec(t, append([]string{addr, "42082", "uint16"}, args...)...); code == 0 {
			t.Errorf("%v: expected a non-zero exit code", args)
		}
	}
}

// The exit code follows the values asked for, not the registers: a uint32
// whose second register does not exist was not read, even though one of its
// registers answered.
func TestPartlyReadValueFails(t *testing.T) {
	addr, _ := startServer(t)

	if _, code := exec(t, addr, "42299", "uint32"); code == 0 {
		t.Fatal("expected a non-zero exit code: the only value could not be read")
	}
}

// JSON has no number for NaN or infinity. 0xFFFF 0xFFFF is a common "not
// available" marker, and it must not end the output.
func TestJSONNonFiniteFloat(t *testing.T) {
	addr, h := startServer(t)
	h.set(42010, 0xFFFF) // NaN
	h.set(42011, 0xFFFF)
	h.set(42012, 0x7F80) // +Inf
	h.set(42013, 0x0000)
	h.set(42014, 0x3F80) // 1
	h.set(42015, 0x0000)

	out, code := exec(t, addr, "42010", "float32", "--count", "3", "--json")
	if code != 0 {
		t.Fatalf("exit code %d, output:\n%s", code, out)
	}
	want := `{"address":42010,"raw":["0xFFFF","0xFFFF"],"value":null}
{"address":42012,"raw":["0x7F80","0x0000"],"value":null}
{"address":42014,"raw":["0x3F80","0x0000"],"value":1}`
	if got := strings.TrimSpace(out); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A string is one value over all its registers: read in full it is decoded,
// and if any register is missing it fails as a whole rather than showing the
// text with the missing part silently dropped.
func TestString(t *testing.T) {
	addr, h := startServer(t)
	h.set(42296, 0x4543) // "EC"
	h.set(42297, 0x4F46) // "OF"

	out, code := exec(t, addr, "42296", "string", "--count", "2")
	if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), "ECOF") {
		t.Fatalf("exit code %d, want the text ECOF:\n%s", code, out)
	}

	// 42300 and 42301 do not exist.
	out, code = exec(t, addr, "42296", "string", "--count", "6")
	if code == 0 || !strings.Contains(out, "illegal data address") || strings.Contains(out, "ECOF") {
		t.Fatalf("exit code %d, want an error and no text:\n%s", code, out)
	}
}

func TestFormatValue(t *testing.T) {
	for _, tc := range []struct {
		v        any
		dec, hex string
	}{
		{uint16(100), "100", "0x0064"},
		{int16(-5), "-5", "0xFFFB"},
		{uint32(65536), "65536", "0x00010000"},
		{int32(-5), "-5", "0xFFFFFFFB"},
		// Floats and text stay as they are with --out hex.
		{float32(1.5), "1.5", "1.5"},
		{float64(0.1), "0.1", "0.1"},
		{"ECOF", "ECOF", "ECOF"},
		{nil, "", ""},
	} {
		if got := formatValue(tc.v, false); got != tc.dec {
			t.Errorf("%#v dec: got %q, want %q", tc.v, got, tc.dec)
		}
		if got := formatValue(tc.v, true); got != tc.hex {
			t.Errorf("%#v hex: got %q, want %q", tc.v, got, tc.hex)
		}
	}
}

// startDropProxy forwards Modbus TCP connections to target, but closes the
// first one right after its first response – a connection that dies while
// polling. It returns its address and a counter of accepted connections.
func startDropProxy(t *testing.T, target string) (string, *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var conns atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go forward(c, target, conns.Add(1) == 1)
		}
	}()
	return ln.Addr().String(), &conns
}

func forward(c net.Conn, target string, dropAfterFirst bool) {
	defer c.Close()
	up, err := net.Dial("tcp", target)
	if err != nil {
		return
	}
	defer up.Close()
	go func() { _, _ = io.Copy(up, c) }()

	if !dropAfterFirst {
		_, _ = io.Copy(c, up)
		return
	}
	// One response: the MBAP header, whose last two bytes give the length
	// of the rest.
	hdr := make([]byte, 6)
	if _, err := io.ReadFull(up, hdr); err != nil {
		return
	}
	body := make([]byte, binary.BigEndian.Uint16(hdr[4:]))
	if _, err := io.ReadFull(up, body); err != nil {
		return
	}
	_, _ = c.Write(append(hdr, body...))
}

// In polling mode a dead connection is reopened at the next interval rather
// than failing every pass after it.
func TestPollingReconnects(t *testing.T) {
	addr, _ := startServer(t)
	proxy, conns := startDropProxy(t, addr)

	out, code := exec(t, proxy, "42082", "uint16", "--interval", "20ms", "--samples", "3")
	if code != 0 {
		t.Fatalf("exit code %d, output:\n%s", code, out)
	}
	lines := dataLines(out)
	if len(lines) != 3 ||
		!strings.HasSuffix(lines[0], " 100") ||
		!strings.Contains(lines[1], "!") ||
		!strings.HasSuffix(lines[2], " 100") {
		t.Fatalf("want a value, an error, then a value again:\n%s", out)
	}
	if n := conns.Load(); n != 2 {
		t.Fatalf("%d connections, want 2: the second pass should have reconnected", n)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// A write error must end the run in the text output as it does with --json,
// or a redirect onto a full disk exits 0 with half a file.
func TestTextOutputReportsWriteError(t *testing.T) {
	p := newPrinter(failingWriter{}, &config{})
	s := sample{addr: 1, words: []uint16{1}, failed: []bool{false}, value: uint16(1)}
	if err := p.print(time.Now(), []sample{s}); err == nil {
		t.Fatal("expected the write error")
	}
}

// dataLines returns the output rows without the header.
func dataLines(out string) []string {
	var rows []string
	for l := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if l == "" || strings.Contains(l, "addr") && strings.Contains(l, "raw") {
			continue
		}
		rows = append(rows, l)
	}
	return rows
}

func TestVersionFlag(t *testing.T) {
	out, code := exec(t, "--version")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if !strings.HasPrefix(out, "modbusread ") || !strings.Contains(out, runtime.GOOS) {
		t.Fatalf("unexpected version output: %q", out)
	}
}

// A register that was not read has no raw value: showing the zero in its slot
// as 0x0000 would read as something the device sent.
func TestFailedRegisterHasNoRawValue(t *testing.T) {
	addr, _ := startServer(t)

	// 42298 and 42299 exist and hold zero, 42300 and 42301 do not exist.
	out, _ := exec(t, addr, "42298", "raw", "--count", "4")
	lines := dataLines(out)
	if len(lines) != 4 {
		t.Fatalf("expected 4 rows, got:\n%s", out)
	}
	for i, want := range []string{"0x0000", "0x0000", "-", "-"} {
		if f := strings.Fields(lines[i]); len(f) < 2 || f[1] != want {
			t.Errorf("row %d: raw %q, want %q:\n%s", i, f[1], want, out)
		}
	}

	out, _ = exec(t, addr, "42298", "raw", "--count", "4", "--json")
	var rows []jsonSample
	for l := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		var js jsonSample
		if err := json.Unmarshal([]byte(l), &js); err != nil {
			t.Fatalf("%v: %s", err, l)
		}
		rows = append(rows, js)
	}
	if len(rows) != 4 || rows[0].Raw[0] == nil || rows[2].Raw[0] != nil || rows[2].Error == "" {
		t.Fatalf("want raw for 42298 and null raw with an error for 42300, got:\n%s", out)
	}
}

// startSilentServer accepts connections and never answers, like a gateway
// whose serial side has the wrong baud rate or unit id.
func startSilentServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { c.Close() })
		}
	}()
	return ln.Addr().String()
}

// Every request to a silent device waits out the full timeout, and a failed
// range is re-read register by register. An interrupt must end that after the
// request in flight, not after all of them.
func TestInterruptStopsReadingSilentDevice(t *testing.T) {
	addr := startSilentServer(t)

	opts, pos, fs, err := parseArgs([]string{addr, "0", "raw", "--count", "20", "--timeout", "500ms"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := buildConfig(opts, pos, setFlags(fs))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()

	var out bytes.Buffer
	start := time.Now()
	code := poll(ctx, cfg, &out, &bytes.Buffer{})
	elapsed := time.Since(start)

	// Uninterrupted this takes 21 timeouts, 10.5s. Interrupted after 0.7s it
	// may still finish the request in flight: 1s, plus slack.
	if elapsed > 2*time.Second {
		t.Fatalf("took %v after the interrupt", elapsed)
	}
	if code == 0 {
		t.Error("expected a non-zero exit code: nothing was read")
	}
	if out.Len() > 0 {
		t.Errorf("an interrupted read must not print a half-done result:\n%s", out.String())
	}
}
