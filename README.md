# hwinfo-mcp

A read-only [Model Context Protocol](https://modelcontextprotocol.io) server that
lets an AI assistant read the hardware sensors [HWiNFO](https://www.hwinfo.com)
monitors on a Windows PC: temperatures, voltages, fan speeds, power draw, clocks
and usage.

It is a single executable that speaks MCP over stdin/stdout. It reads the block
of memory HWiNFO shares with other programs and never writes to it, opens no
network connection and changes nothing on the system.

## Requirements

- Windows with HWiNFO64 running and its **Sensors** window open (minimized to
  the tray is fine).
- **Shared Memory Support** enabled in HWiNFO's settings (General / User
  Interface).

Two things to know about that option:

- If you switch it on while HWiNFO is already running, restart HWiNFO. The
  shared memory only appeared after a restart on the machine this was
  developed on.
- The free edition of HWiNFO switches it off again after 12 hours. Re-enable
  it or restart HWiNFO when that happens; `hwinfo_status` tells you.

## Install

Download `hwinfo-mcp-windows-amd64.exe` from the releases page, or build it:

```
go build -trimpath -ldflags "-s -w" -o %USERPROFILE%\tools\hwinfo-mcp\hwinfo-mcp.exe .
```

Then register it with your MCP client.

Claude Code:

```
claude mcp add --scope user hwinfo -- %USERPROFILE%\tools\hwinfo-mcp\hwinfo-mcp.exe
```

Claude Desktop, in `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "hwinfo": {
      "command": "C:\\Users\\you\\tools\\hwinfo-mcp\\hwinfo-mcp.exe"
    }
  }
}
```

## Tools

| Tool | What it returns |
|---|---|
| `hwinfo_status` | Whether HWiNFO is publishing, when it last polled, how many sensors and readings there are, and whether the snapshot was taken under HWiNFO's lock. When something is wrong it says what and how to fix it. |
| `list_sensors` | Every sensor with the number of readings of each type. |
| `get_readings` | Current readings grouped by sensor. Filters: `sensor` and `label` (case-insensitive substrings), `types` (`temperature`, `voltage`, `fan`, `current`, `power`, `clock`, `usage`, `other`), and `stats` to add minimum, maximum and average. |
| `thermal_overview` | The readings that say how hot the PC runs: per CPU the control temperature (Tctl/Tdie or package), die average, per-CCD temperatures, hottest core, package power and throttling flags; per GPU the core, hot spot and memory junction temperatures, board power, fans and thermal limit flag; plus the remaining temperatures and fan speeds. |

All four are annotated as read-only. Without filters `get_readings` returns
everything, which is several hundred readings (about 50 KB of JSON on the
development machine, 67 KB with `stats`), so filter where you can.

### Names and languages

HWiNFO keeps two names for every sensor and reading: its own English name and
the one it displays, which follows HWiNFO's language and any renaming you did.
A Portuguese HWiNFO shows `GPU Temperature` as `Temperatura GPU`.

- Output uses the original English name as `label` (or `sensor`) and adds
  `display` when the displayed name differs.
- The `sensor` and `label` filters match either name.
- `thermal_overview` recognises readings by their original name only, so it
  works whatever language HWiNFO runs in.

## How it reads the sensors

On every tool call the server opens the mapping `Global\HWiNFO_SENS_SM2`
read-only, maps it, and copies the part in use. While copying it holds HWiNFO's
mutex `Global\HWiNFO_SM2_MUTEX` (waiting at most 200 ms for it), so that the
copy cannot be half-updated; if the mutex cannot be had it copies anyway and
checks that the header did not change meanwhile. Everything is released before
the call returns. Offsets and sizes come from the block's header and are
checked against the size of the mapping before anything is read.

## Troubleshooting

`hwinfo_status` reports the cause. The usual ones:

| Problem | Cause |
|---|---|
| shared memory not found | HWiNFO is not running, the Sensors window is closed, Shared Memory Support is off, or it was enabled without restarting HWiNFO. |
| `active: false`, values stale | The Sensors window was closed or the free edition's 12-hour limit was reached. |
| access denied | HWiNFO and the MCP client run at different integrity levels. |

## What has been verified

The server was validated against HWiNFO64 8.60 on a Ryzen 9 9950X3D with a
GeForce RTX 5080, with HWiNFO running in Portuguese. The labels of that machine
are kept as a test fixture (`internal/hwinfo/testdata`), and the first pattern
of every `thermal_overview` field is covered by a regression test built from it.

The patterns for Intel processors, older Ryzen models and AMD or Intel graphics
cards follow HWiNFO's naming but have not been checked on such hardware. If
`thermal_overview` misses a reading on your system, `get_readings` still returns
it, and the patterns live in `internal/hwinfo/filter.go` (`cpuLabel`,
`gpuLabel`).

## Development

```
go vet ./...
go test -race ./...
go test -run '^$' -fuzz FuzzParse -fuzztime 30s ./internal/hwinfo
```

The race detector needs a C compiler; on a Windows machine without one, run
`go test ./...` there and leave `-race` to CI or to Linux.

| Path | Contents |
|---|---|
| `main.go` | The MCP server and its four tools. |
| `internal/hwinfo/layout.go` | The shared memory layout and its decoding. |
| `internal/hwinfo/snapshot.go` | `Parse`, the snapshot types and the reader's portable half. |
| `internal/hwinfo/read_windows.go` | Opening, locking and copying the shared memory. |
| `internal/hwinfo/filter.go` | Filtering and the thermal overview heuristics. |
| `internal/hwinfo/hwinfotest` | Builds synthetic shared memory blocks for tests. |

Dependencies are the Go standard library, `golang.org/x/sys` and the official
MCP Go SDK.
