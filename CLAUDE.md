# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

`modbusread`, a Go CLI that reads registers from a Modbus device – TCP or RTU (serial),
**read-only**. Deliberately **universal**: it contains no device knowledge, no built-in
register map and no device-specific messages. The transport follows from the target
argument (`target.go`), not from a flag.

It was split out of [womat/ecoflow](https://github.com/womat/ecoflow) (up to v0.6.0 it was
`cmd/modbusread` there). The EcoFlow register map stays in that repo; here it may appear
at most as an example.

## Commands

```
go build ./...
go test ./...                                    # runs without hardware
go test -run TestParseAddr ./internal/decode/    # a single test
go vet ./...
gofmt -l .                                       # no output = fine; CI fails on it
```

The integration tests start the Modbus server from `github.com/simonvetter/modbus` on a
free port and read against it – no device needed.

## Structure

- `*.go` in the root – package `main`: flags, reading with chunking/error isolation,
  output, polling, target parsing. In the root so that
  `go install github.com/womat/modbusread@latest` works
- `internal/decode/` – pure functions over `[]uint16` (types, word/byte order, address
  parsing). This is where the logic lives that produces *wrong numbers* rather than
  crashes when it errs – hence kept network-free and fully testable
- `.github/workflows/` – `ci.yml` (gofmt, vet, build, test -race, govulncheck) and
  `release.yml` (vet, test and govulncheck again, then the binary for six platforms).
  Actions are pinned to a commit SHA with the release in a comment, never to a movable
  tag; `.github/dependabot.yml` proposes weekly updates, but not for the `go install` pin
  of govulncheck

## Branches & releases

One permanent branch: **`main`**. Work happens in short-lived feature branches that go to
`main` via PR, CI has to be green. A release is a tag `vX.Y.Z` on `main`; the release
workflow stamps the tag in via `-X main.version`. The numbering continues the one from
womat/ecoflow (first release here: v0.7.0).

## Language

Everything in the repo is English: documentation, code comments, `--help` text, error
messages and program output. Conversations with the maintainer may be in German.

## Code conventions

- **Read-only is a hard property, not a default:** no `Write*` method of the library is
  called. A tool without a write path cannot write by accident. Do not soften this.
- **No device knowledge:** no register maps, no device names in `--help` or output. A
  device-specific tool belongs in its own repo.
- **Addresses are never converted** – what is typed goes on the wire as it is (0-based).
  Many sources document 1-based; converting is deliberately left to the human, so the
  tool does not hide an assumption.
- **The tool computes word/byte order itself**, the client runs fixed on
  `BIG_ENDIAN, HIGH_WORD_FIRST` and only reads `ReadRegisters`. That way the raw words
  are always available for the output and the decoding stays purely testable.
- **Flags that cannot take effect are rejected rather than ignored** – the serial
  parameters on a TCP target are an error. A baud rate that silently has no effect sends
  people debugging the hardware.
- **Raw words are always in the output**, even when a value was decoded – in reverse
  engineering the raw value matters more than the interpretation. A word that was not
  read is shown as `-` (`null` in JSON), never as the zero in its slot.
- **Every loop of requests checks the context.** The program catches Ctrl-C itself, and a
  silent device makes each request wait out the timeout – without the check an
  interrupt is ignored until the whole range has timed out.
- **Bound a user-supplied number before computing with it** (`--count` before it is
  multiplied by the registers per value), so an overflow cannot slip past a range check.
- **Address parsing deliberately does not use `strconv.ParseUint(s, 0, …)`** (base 0
  would read `042` as octal 34).
- Whoever changes flags or behaviour carries the README and the `--help` text along.
