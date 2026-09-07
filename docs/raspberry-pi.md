# Raspberry Pi support

Raspberry Pi is a first-class target (`docs/architecture.md`), not an
afterthought. Primary targets: Raspberry Pi 4 and 5, 64-bit
(`GOOS=linux GOARCH=arm64`).

## What's detected

`agent/monitoring`'s Linux collector (`agent/monitoring/linux.go`) reads:

- **Model**: `/proc/device-tree/model`, matched against `"Raspberry Pi"`.
  Surfaced as `system.platform.model` / `is_raspberry_pi` in
  `GET /api/v1/system`.
- **Temperature**: `/sys/class/thermal/thermal_zone0/temp` -- present on
  the Pi (and most modern Linux hosts with a thermal driver); reported
  `Unavailable` (not `Unsupported`) when the file is missing, since the
  mechanism works on Linux in general, this specific host just has no
  exposed sensor.
- **Throttling**: `/sys/devices/platform/soc/soc:firmware/get_throttled`
  -- the Pi firmware's under-voltage/thermal-throttle status register.
  This path only exists on Pi hardware, so it's `Unsupported` (not just
  unavailable) everywhere else.

None of this requires root.

## What's NOT claimed

Per `docs/architecture.md` principle 26: **this project makes no claim
about whether a Raspberry Pi is sufficient hardware to run a QRX
validator.** That depends entirely on QRX Core's actual resource
requirements, which have not been documented or tested against real
hardware in this codebase (see `docs/qrx-0.0.7-interface.md`'s broader
honesty note about what's verified). QRX Node Suite running well on a Pi
says nothing about whether `qrxd` itself will.

## Cross-compiling

```sh
cd agent
GOOS=linux GOARCH=arm64 go build -o agentd-pi ./cmd/agentd
```

`agent/storage/sqlite` is cgo, so cross-compiling from a non-arm64 host
needs an arm64 C toolchain and `libsqlite3` for that target -- see
`agent/storage/sqlite/README.md`. Building natively on a Pi (or in a
matching arm64 container/CI runner) avoids this entirely and is the
simplest path.
