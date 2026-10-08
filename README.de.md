# modbusread – Deutsche Kurzfassung

🇬🇧 [Full documentation in English](README.md)

**modbusread liest Register aus einem beliebigen Modbus-Gerät – über TCP oder RS-485, nur lesend,
die Rohwerte immer mit dabei.**

Adresse, Register und Typ hinein, Wert heraus. Das Programm kennt kein einziges Gerät und hat keine
eingebaute Registerkarte; gedacht ist es zum **Prüfen von Registerkarten** aus Handbüchern oder aus
Reverse Engineering der Community gegen das echte Gerät:

- **nur lesend** – es ruft keine einzige Schreibfunktion der Modbus-Bibliothek auf; was keinen
  Schreibpfad hat, kann nicht versehentlich schreiben,
- **Modbus TCP und Modbus RTU** (seriell, RS-485) – das Ziel entscheidet: `192.168.1.50` ist TCP,
  `/dev/ttyUSB0` oder `COM3` ist RTU,
- **Adressen werden nie umgerechnet** – was du eingibst, geht so auf die Leitung (0-basiert). Viele
  Registerkarten sind 1-basiert dokumentiert; die Umrechnung bleibt bewusst bei dir,
- **Rohwörter immer in der Ausgabe**, auch wenn ein Wert dekodiert wurde, dazu Wort- und
  Byte-Reihenfolge, Polling und „nur bei Änderung" zum Finden unbekannter Register.

Ein einzelnes, statisch gelinktes Programm für Linux (auch jeder Raspberry Pi), macOS und Windows.

## Schnellstart

Mit Go:

```
go install github.com/womat/modbusread@latest
```

Ohne Go: das Archiv für deinen Rechner unter
[Releases](https://github.com/womat/modbusread/releases/latest) herunterladen und entpacken –
`linux_armv6` für Pi 1 und Zero, `linux_armv7` für 32-Bit-, `linux_arm64` für 64-Bit-Systeme.

```console
$ modbusread 192.168.1.50 42082 uint16
addr     raw                  value
42082    0x0064               100

$ modbusread /dev/ttyUSB0 40069 uint16 --baud 9600 --parity even --unit 3
```

Alle Flags, Ziele und Beispiele stehen in der [englischen Dokumentation](README.md),
`modbusread --help` zeigt sie ebenfalls.

## Lizenz

MIT, siehe [`LICENSE`](LICENSE).
