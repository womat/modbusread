package main

import (
	"context"

	"github.com/simonvetter/modbus"

	"github.com/womat/modbusread/internal/decode"
)

// sample is one register's worth of a read: either raw words plus a decoded
// value, or the error that register failed with.
type sample struct {
	addr  uint16
	words []uint16
	// failed marks the words that were not read. Their slot in words is
	// zero, which is not a value the device sent, so the output shows them
	// as missing rather than as 0x0000.
	failed []bool
	value  any
	err    error
}

// readRegisters reads total registers starting at addr, splitting the request
// into chunks of at most maxRegsPerRequest.
//
// If a chunk fails, its registers are re-read one by one so that a single
// invalid address in a scan does not take the whole range down with it — and
// so the output shows exactly which addresses answered and which did not.
//
// ctx is checked before every request. A device that has stopped answering
// makes each of those re-reads wait out the full timeout, and the program
// catches Ctrl-C itself – without the check a scan of 120 registers would
// ignore it for six minutes. Registers not read because of a cancellation
// carry ctx's error.
func readRegisters(ctx context.Context, mc *modbus.ModbusClient, addr uint16, total int, rt modbus.RegType) ([]uint16, []error) {
	words := make([]uint16, total)
	errs := make([]error, total)

	for off := 0; off < total; {
		n := min(total-off, maxRegsPerRequest)
		base := addr + uint16(off)

		if err := ctx.Err(); err != nil {
			fillErr(errs[off:], err)
			break
		}
		if vals, err := mc.ReadRegisters(base, uint16(n), rt); err == nil {
			copy(words[off:off+n], vals)
			off += n
			continue
		}

		for i := range n {
			if err := ctx.Err(); err != nil {
				fillErr(errs[off+i:], err)
				return words, errs
			}
			v, err := mc.ReadRegister(base+uint16(i), rt)
			if err != nil {
				errs[off+i] = err
			} else {
				words[off+i] = v
			}
		}
		off += n
	}
	return words, errs
}

func fillErr(errs []error, err error) {
	for i := range errs {
		errs[i] = err
	}
}

// failedWords reports which of the words carry an error.
func failedWords(errs []error) []bool {
	failed := make([]bool, len(errs))
	for i, err := range errs {
		failed[i] = err != nil
	}
	return failed
}

// buildSamples groups the registers into values of the requested type. A
// value is only decoded if every register it spans was read successfully.
func buildSamples(c *config, words []uint16, errs []error) []sample {
	if c.typ == decode.Str {
		s := sample{addr: c.addr, words: words, failed: failedWords(errs)}
		for _, err := range errs {
			if err != nil {
				s.err = err
				return []sample{s}
			}
		}
		s.value, s.err = decode.Decode(words, c.typ, c.wordOrder, c.byteOrder)
		return []sample{s}
	}

	per := decode.RegsPerValue(c.typ)
	out := make([]sample, 0, len(words)/per)
	for off := 0; off+per <= len(words); off += per {
		s := sample{addr: c.addr + uint16(off), words: words[off : off+per], failed: failedWords(errs[off : off+per])}
		for _, err := range errs[off : off+per] {
			if err != nil {
				s.err = err
				break
			}
		}
		if s.err == nil {
			s.value, s.err = decode.Decode(s.words, c.typ, c.wordOrder, c.byteOrder)
		}
		out = append(out, s)
	}
	return out
}
