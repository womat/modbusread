# modbusread

**Read registers from any Modbus device over TCP or RS-485 – read-only, raw words always shown.**

[![CI](https://github.com/womat/modbusread/actions/workflows/ci.yml/badge.svg)](https://github.com/womat/modbusread/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/womat/modbusread)](https://github.com/womat/modbusread/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/womat/modbusread)](go.mod)

🇩🇪 [Deutsche Kurzfassung](README.de.md)

A universal, **read-only** Modbus reader – address, register and type in, value out. No
device knowledge in the tool, no built-in register map; it works with any Modbus device.
Meant for checking register maps from manuals or community reverse engineering against
the real device.

Speaks Modbus TCP and Modbus RTU (serial). It calls no write function of the Modbus
library at all – a tool without a write path cannot write by accident.

```
modbusread <target> <address> <type> [flags]
```

Address in decimal (`42082`) or hex (`0xA462`), type `raw`, `uint16`, `int16`, `uint32`,
`int32`, `float32`, `float64` or `string`.

The **target** decides the transport, without an extra flag:

| Input                                             | Transport                            |
|---------------------------------------------------|--------------------------------------|
| `192.168.1.50`, `plc.local:1502`                  | Modbus TCP (port defaults to 502)    |
| `/dev/ttyUSB0`, `/dev/tty.usbserial-…`, `COM3`    | Modbus RTU over the serial line      |
| `rtu://…`, `tcp://…`, `udp://…`, `rtuovertcp://…`, `rtuoverudp://…` | explicit, overrides the detection |

`tcp+tls://` is rejected outright rather than silently ignored – the library could do
it, but offering it untested would be a promise with nothing behind it.

```console
$ modbusread 192.168.1.50 42082 uint16
addr     raw                  value
42082    0x0064               100

# A float with swapped words (high word in the second register)
$ modbusread 192.168.1.50 40574 float32 --word-order low

# Dump a range to find unknown registers
$ modbusread 192.168.1.50 40520 raw --count 120 --out hex

# Find out which register reacts to a change on the device
$ modbusread 192.168.1.50 40520 raw --count 120 --interval 1s --on-change
```

The addresses in these examples come from the EcoFlow PowerOcean, the device the tool was
first written for; its register map lives in
[womat/ecoflow](https://github.com/womat/ecoflow/blob/main/modbus-registers.md).

Important: **addresses are never converted** – they go on the wire exactly as typed
(0-based). Many register maps are documented 1-based, some are not, and often it is not
clear which. Converting is therefore left to the human, so the tool does not hide an
assumption.

With a serial target the line parameters come in – `--baud` (19200), `--databits` (8),
`--parity` (none) and `--stopbits`. For the latter, `0` follows the Modbus rule: two stop
bits without parity, one with. They have to match the device exactly, otherwise you get
garbage or nothing at all. A bus typically has several devices on it, so `--unit` is no
longer a formality there:

```console
$ modbusread /dev/ttyUSB0 40069 uint16 --baud 9600 --parity even --unit 3
```

On a TCP target these flags are **rejected** rather than ignored – a baud rate that
silently has no effect sends you off debugging the wiring.

More flags: `--unit`, `--fc holding|input`, `--byte-order`, `--timeout`, `--json`,
`--samples`. `modbusread --help` shows everything.

The raw words are always printed, even when a value was decoded – when reverse
engineering a register map, the raw value matters more than the interpretation.

## Installation

With a Go toolchain:

```
go install github.com/womat/modbusread@latest
```

For machines **without Go** – such as a Raspberry Pi next to the device – ready-made
binaries are under [Releases](https://github.com/womat/modbusread/releases). They are
statically linked (`CGO_ENABLED=0`), so there is nothing to install: unpack and run.

```bash
VERSION=0.8.0    # without the v - see the releases page for the latest
ARCH=linux_arm64 # see the table below

curl -LO "https://github.com/womat/modbusread/releases/download/v$VERSION/modbusread_${VERSION}_$ARCH.tar.gz"
tar -xzf "modbusread_${VERSION}_$ARCH.tar.gz" modbusread
./modbusread --version
```

| Machine                                            | `ARCH`          |
|----------------------------------------------------|-----------------|
| Raspberry Pi 3/4/5/Zero 2 W with a 64-bit OS       | `linux_arm64`   |
| Raspberry Pi 2/3/4/5/Zero 2 W with a 32-bit OS     | `linux_armv7`   |
| Raspberry Pi 1 and Zero (1st gen)                  | `linux_armv6`   |
| ordinary Linux PC/server, NAS                      | `linux_amd64`   |
| Mac with Apple Silicon                             | `darwin_arm64`  |
| Mac with Intel                                     | `darwin_amd64`  |
| Windows                                            | `windows_amd64` (`.zip`) |

The download can be checked against the `checksums.txt` of the same release:

```bash
curl -LO "https://github.com/womat/modbusread/releases/download/v$VERSION/checksums.txt"
sha256sum -c checksums.txt --ignore-missing
```

## Releases

Every release on the [releases page](https://github.com/womat/modbusread/releases) carries
archives for Linux (including all Raspberry Pi architectures), macOS and Windows with the
binary, `README.md` and `LICENSE`, plus a `checksums.txt` and a changelog. Versions follow
[semantic versioning](https://semver.org/).

`modbusread --version` reports the release a binary was built from. A local build reports
the commit instead, with `+dirty` when the working tree had changes – which is how the two
are told apart.

Up to v0.7.0 the archives were named `modbusread-v0.7.0-linux-arm64.tar.gz`; from v0.8.0
on they follow the scheme above.

## History

Up to v0.6.0 `modbusread` lived in [womat/ecoflow](https://github.com/womat/ecoflow) as
`cmd/modbusread`; those releases stay there, and
`go install github.com/womat/ecoflow/cmd/modbusread@v0.6.0` still works. From v0.7.0 on
it is released here. The version numbers continue.

## Development

Building from source needs Go and `make`; `make help` lists the targets:

```
make test       # go test -race ./... - no hardware: the integration tests start a Modbus server
make lint       # gofmt, go vet, govulncheck
make build      # ./modbusread for this machine
make snapshot   # all release archives into ./dist, without publishing (needs goreleaser)
```

A release is a tag `vX.Y.Z` on `main`, made with `make release TAG=vX.Y.Z`; the workflow
tests the tagged commit again and GoReleaser publishes the archives.

## License

modbusread is released under the MIT License – see [`LICENSE`](LICENSE) for the full text.

### Third-party licenses

The source tree contains no third-party code, but a **compiled binary statically links** the
modules below. Their terms apply to anyone distributing that binary, not to the sources here.

| Module                         | License |
|--------------------------------|---------|
| `github.com/simonvetter/modbus` | MIT     |
| `github.com/goburrow/serial`    | MIT     |

Both are permissive.
