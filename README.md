# hwinfo-mcp

**English** · [Português (Brasil)](README.pt-BR.md) · [Español](README.es.md)

Ask an AI assistant about your PC's hardware, and let it read the real sensors.

hwinfo-mcp is a read-only [Model Context Protocol](https://modelcontextprotocol.io)
server that gives an assistant such as Claude access to everything
[HWiNFO](https://www.hwinfo.com) monitors on a Windows PC: temperatures,
voltages, fan speeds, power draw, clocks, usage, drive health and hardware
error counters.

That makes it a tool for finding hardware problems. Instead of scanning
hundreds of rows in HWiNFO yourself, you ask:

> Is my CPU overheating? Check the current and peak temperatures and whether it
> has throttled.

The assistant reads the sensors, compares the current values with the peaks
HWiNFO has recorded, and tells you what it sees.

It is a single executable that speaks MCP over stdin/stdout. It reads the block
of memory HWiNFO shares with other programs and never writes to it, opens no
network connection and changes nothing on the system.

**Windows only.** HWiNFO exists only for Windows, and so does the shared memory
this server reads, so the releases are for Windows on x64 and on Arm. Linux
inside WSL 2 does not see the PC's sensor chips either: run the server on the
Windows side.

## Quick start

Three steps: set up HWiNFO, install the bundle or the plugin, and ask.

### 1. Install HWiNFO and switch on its shared memory

HWiNFO does the measuring; this project only reads what HWiNFO publishes.
HWiNFO is a separate program under its own licence and is not included here.
Install it with:

```
winget install REALiX.HWiNFO
```

Or download [HWiNFO64](https://www.hwinfo.com/download/). Start it; it asks for
administrator rights, which it needs to reach the hardware. Its publishing is
off by default, so switch it on once:

1. On the welcome screen, tick **Sensors-only** and click **Settings**.
2. On the **General / User Interface** tab, enable **Shared Memory Support**.
   That is the option this project depends on. On the same tab, enable
   **Show Sensors on Startup**, **Minimize Sensors on Startup**,
   **Minimize Main Window on Startup**, **Minimize Sensors instead of closing**
   and **Auto Start**, and switch off **Show Welcome Screen and Progress**, so
   that HWiNFO starts with Windows and keeps publishing from the notification
   area. [HWiNFO settings](#hwinfo-settings) says what each one is for.
3. Confirm with **OK**, then restart HWiNFO: close it from its notification
   area icon and start it again. On the machine this was developed on, the
   readings were only published after that restart.

The free edition of HWiNFO stops publishing after 12 hours of run time; see
[HWiNFO settings](#hwinfo-settings).

### 2. Install the bundle or the plugin

There are four ways to install the server. From the easiest to the most
involved:

| Way | For | What it takes |
|---|---|---|
| The bundle | Claude Desktop | Download one file and open it. |
| The plugin | Claude Code | One command. |
| [The executable](#installing-the-server-by-hand) | Any MCP client | Download it and register it with the client yourself. |
| [Your own build](#building-from-source) | Any MCP client | The same, after compiling it with Go. |

**Claude Desktop: the bundle.** Download
[`hwinfo-mcp.mcpb`](https://github.com/templarfelix/hwinfo-mcp/releases/latest/download/hwinfo-mcp.mcpb)
and open it, or drag it into Claude Desktop. The bundle is new in v0.2.0 and
has so far only been checked with the bundle tool itself. If it does not
install, [install the server by hand](#installing-the-server-by-hand), and
please open an issue.

**Claude Code: the plugin.** One command installs it:

```
claude plugin install hwinfo --marketplace templarfelix/hwinfo-mcp
```

It needs Claude Code 2.1.292 or later. On an older version, use these two
instead:

```
claude plugin marketplace add templarfelix/hwinfo-mcp
claude plugin install hwinfo@hwinfo-mcp
```

The plugin brings the server together with three
[skills](#skills-that-come-with-the-plugin) that give the assistant a method
for reading the sensors, so there is no executable to download. Inside a
session (version 2.1.275 or later),
`/plugin install hwinfo --marketplace templarfelix/hwinfo-mcp` adds the
marketplace and opens the plugin's page, where you install it.

**Any other MCP client**, or if you would rather register the server yourself:
see [Installing the server by hand](#installing-the-server-by-hand).

### 3. Ask

Start with:

> Is HWiNFO publishing sensor data?

The assistant calls `hwinfo_status` and should answer that the sensors are
active and how fresh the last reading is. If not, the answer says what is wrong
and how to fix it; with the plugin, `/hwinfo:setup` walks you through it.

Then ask what you came for; [Asking questions](#asking-questions) has examples.

## Asking questions

You do not call the tools yourself. Ask in plain words, in any language; the
assistant picks the tool and the filters. A few to start with:

- How hot are my CPU and GPU right now?
- What are my fan speeds?
- How much power are the processor and the graphics card drawing?
- Which sensors does HWiNFO see on this PC?

### Skills that come with the plugin

The plugin adds three skills. The assistant uses them on its own when a
question calls for one, and you can also start one by name:

| Skill | What it does |
|---|---|
| `/hwinfo:thermal-check` | Judges whether the CPU or graphics card runs too hot, is held back by temperature, or needs new thermal paste or pads. It works from temperature and power read at the same moment, under load, and says what the sensors cannot show. |
| `/hwinfo:hardware-checkup` | Goes through hardware error counters, drive health, voltages, fans, memory and limits, and reports what needs attention, what is fine and what could not be checked. |
| `/hwinfo:setup` | Gets HWiNFO publishing its readings, or explains why they stopped. |

Without the plugin the same questions work; the assistant then has the tools
but not the method.

### Hunting hardware problems

| You suspect | Ask something like | What the assistant reads |
|---|---|---|
| The CPU overheats | *Is my CPU overheating? Compare the current temperature with the peak and tell me if it throttled.* | The CPU's control temperature (Tctl/Tdie or package) with minimum, maximum and average, the per-CCD and hottest-core temperatures, package power and the thermal throttling flags. |
| Performance drops under load | *Is anything holding my CPU or GPU back? Look at the thermal, power and current limits.* | Readings such as `Thermal Limit`, `CPU PPT Limit`, `Thermal Throttling (HTC)` and the GPU's `Performance Limit - Thermal` and `Performance Limit - Power`. |
| The graphics card runs hot, or its paste and pads are worn | *Compare my GPU's hot spot with its core temperature, and check the memory temperature.* | GPU core, hot spot and memory junction temperatures, board power and fan speeds. |
| A fan has died or stalls | *Are all fans spinning? Show the current, lowest and highest speed of each.* | Every fan reading with its statistics. |
| Shutdowns or restarts under load | *My PC restarts when I game. Check temperatures, the 12 V, 5 V and 3.3 V rails, and the hardware error counters.* | Temperatures, the mainboard's voltage readings with their minimum and maximum, and the Windows hardware error (WHEA) count. |
| A drive is failing | *How healthy is my SSD?* | The S.M.A.R.T. sensor: remaining life, available spare, failure and warning flags, temperature. |
| Unstable memory or overclock | *Any sign of memory trouble? Check the DIMM temperatures and flags, and hardware errors.* | The DIMM sensors (temperature, voltages, PMIC flags) and the WHEA count. |
| A bad PCIe slot, riser or cable | *Does my graphics card report PCIe errors?* | The error counters of the GPU sensor. |

Which of these readings exist depends on your hardware and on what HWiNFO
supports for it. Ask the assistant to list the sensors to see what your PC
offers.

### Seeing what happens under load

HWiNFO keeps the minimum, maximum and average of every reading since it started
(or since you last reset them in its Sensors window). That turns two questions
into a load test:

1. While the PC is idle, ask for a thermal overview.
2. Run the game, render or stress test that causes trouble.
3. Ask again: *What were the peak CPU and GPU temperatures and power draw, and
   did anything throttle?*

### Getting good answers

- Ask for the peak, not only the current value. A CPU that sits at 60 °C now
  may have reached its limit ten minutes ago.
- Say what was running. A peak means little without the load behind it: 95 °C
  during a render is a different finding from 95 °C while browsing.
- Name the part you mean: "GPU", "SSD", "DIMM", "mainboard". The assistant
  then reads fewer, more relevant readings.
- If the assistant says sensor data is unavailable, ask it to check the HWiNFO
  status.
- The server reports what HWiNFO measures. What the assistant concludes from
  the numbers is its own judgement: use it as a lead to check, not as a
  diagnosis.

### What an answer is built from

This is part of what `thermal_overview` returned on the development machine,
shortened. The CPU is at 61.75 °C now, peaked at 95.4 °C since HWiNFO started,
and no throttling flag was raised:

```json
{
  "active": true,
  "last_update": "2026-11-23T19:01:38-03:00",
  "cpus": [
    {
      "name": "AMD Ryzen 9 9950X3D",
      "temperature": { "label": "CPU (Tctl/Tdie)", "value": 61.75, "unit": "°C", "min": 51.5, "max": 95.375, "avg": 70.259 },
      "package_power": { "label": "CPU Package Power", "value": 84.284, "unit": "W", "min": 55.714, "max": 237.232, "avg": 129.425 },
      "thermal_throttling": { "active": false, "occurred": false }
    }
  ],
  "gpus": [
    {
      "name": "NVIDIA GeForce RTX 5080",
      "temperature": { "label": "GPU Temperature", "value": 48.434, "unit": "°C", "min": 37.316, "max": 67.414, "avg": 51.215 },
      "hot_spot": { "label": "GPU Hot Spot Temperature", "value": 53.531, "unit": "°C", "min": 41.5, "max": 86.031, "avg": 59.193 },
      "thermal_throttling": { "active": false, "occurred": false }
    }
  ]
}
```

## HWiNFO settings

The options from the quick start are on the **General / User Interface** tab of
HWiNFO's settings; this is what each one is for. After the first start, the
settings are in the menu of HWiNFO's icon in the notification area.

| Option | Why |
|---|---|
| **Shared Memory Support** | Required. It makes HWiNFO publish its readings to other programs. |
| **Show Sensors on Startup** | Readings are only published while the sensors are running. |
| **Minimize Sensors on Startup**, **Minimize Main Window on Startup** | HWiNFO starts straight into the notification area. |
| **Minimize Sensors instead of closing** | Closing the Sensors window by accident does not stop the readings. |
| **Auto Start** | HWiNFO starts with Windows, so the readings are always there. |
| **Show Welcome Screen and Progress**, switched off | HWiNFO does not stop at the welcome screen every time it starts. |

With these, HWiNFO only needs to be running, with its Sensors window open or
minimized to the notification area.

The labels may read a little differently in your version of HWiNFO, and they
are translated when HWiNFO runs in another language. The settings file is the
same either way: `HWiNFO64.INI`, in HWiNFO's folder (normally
`C:\Program Files\HWiNFO64`). This is the configuration the server was
developed and tested with:

```ini
[Settings]
SensorsOnly=1
SensorsSM=1
ShowWelcomeAndProgress=0
OpenSensors=1
MinimalizeMainWnd=1
MinimalizeSensors=1
MinimalizeSensorsClose=1
Autorun=1
```

`SensorsSM=1` is Shared Memory Support, the one that matters.

**The 12-hour limit.** The free edition of HWiNFO switches Shared Memory
Support off after 12 hours of run time, and it then has to be enabled again in
the settings. HWiNFO Pro has no such limit. When the readings stop, the
assistant can tell you why; see [Troubleshooting](#troubleshooting).

## Installing the server by hand

The bundle and the plugin carry the server with them. For any other MCP client,
or to register the server yourself, download the executable from the
[latest release](https://github.com/templarfelix/hwinfo-mcp/releases/latest).
In PowerShell:

```powershell
$dir = "$env:USERPROFILE\tools\hwinfo-mcp"
New-Item -ItemType Directory -Force $dir | Out-Null
Invoke-WebRequest "https://github.com/templarfelix/hwinfo-mcp/releases/latest/download/hwinfo-mcp-windows-amd64.exe" -OutFile "$dir\hwinfo-mcp.exe"
```

On Windows on Arm, take `hwinfo-mcp-windows-arm64.exe` instead. To check the
download, compare `Get-FileHash "$dir\hwinfo-mcp.exe"` with the `SHA256SUMS`
file of the release. The executable is not code-signed, so a browser or Windows
may warn about it.

Then register the executable with your client.

**Claude Code** (PowerShell):

```powershell
claude mcp add --scope user hwinfo -- "$env:USERPROFILE\tools\hwinfo-mcp\hwinfo-mcp.exe"
claude mcp get hwinfo
```

The second command should report `Connected`. In `cmd.exe`, write the path as
`%USERPROFILE%\tools\hwinfo-mcp\hwinfo-mcp.exe`. Start a new session
afterwards; one that was already open may not see the server yet.

**Claude Desktop**: add the server to `claude_desktop_config.json` and restart
the app.

```json
{
  "mcpServers": {
    "hwinfo": {
      "command": "C:\\Users\\you\\tools\\hwinfo-mcp\\hwinfo-mcp.exe"
    }
  }
}
```

**Any other MCP client** that can start a stdio server works the same way: the
command is the path to the executable, with no arguments.

### Building from source

Instead of downloading the executable, you can build it with Go:

```
go install github.com/templarfelix/hwinfo-mcp@latest
```

That puts `hwinfo-mcp.exe` in Go's bin folder, normally `%USERPROFILE%\go\bin`;
register that path as described above.

## Tools

| Tool | What it returns |
|---|---|
| `hwinfo_status` | Whether HWiNFO is publishing, when it last polled, how many sensors and readings there are, and whether the snapshot was taken under HWiNFO's lock. When something is wrong it says what and how to fix it. |
| `list_sensors` | Every sensor with the number of readings of each type. |
| `get_readings` | Current readings grouped by sensor, narrowed by the filters below. |
| `thermal_overview` | The readings that say how hot the PC runs: per CPU the control temperature (Tctl/Tdie or package), die average, per-CCD temperatures, hottest core, package power and throttling flags; per GPU the core, hot spot and memory junction temperatures, board power, fans and thermal limit flag; plus the remaining temperatures and fan speeds. |

All four are annotated as read-only, and only `get_readings` takes arguments,
all of them optional:

| Argument | Meaning |
|---|---|
| `sensor` | Part of a sensor name, in any letter case: `CPU`, `RTX 5080`, `S.M.A.R.T.` |
| `label` | Part of a reading label, in any letter case: `Temperature`, `Tctl`, `Limit` |
| `types` | Reading types to include, any of `temperature`, `voltage`, `fan`, `current`, `power`, `clock`, `usage`, `other` |
| `stats` | `true` to add the minimum, maximum and average |

For example, the GPU's temperatures and fans with their statistics:

```json
{ "sensor": "GPU", "types": ["temperature", "fan"], "stats": true }
```

About the output:

- A PC has several hundred readings (484 on the development machine), and one
  `get_readings` call returns at most 300 of them. When more match, `truncated`
  is set and `matched` gives the total, so narrow the filters to see the rest.
- Flags that HWiNFO shows as Yes/No come as `1` or `0` with the unit `Yes/No`.
- A `value` is `null` when HWiNFO holds no number for the reading.

### What the client gets to see

The server passes on HWiNFO's sensor names as they are. Some of them carry more
than a model name: a drive's sensor includes its serial number, and the
PresentMon sensor names the application it is watching. An assistant that calls
`list_sensors` or `get_readings` receives those, like everything else it reads.

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
checks that the block still reads the same afterwards. Everything is released
before the call returns. Offsets and sizes come from the block's header and are
checked against the size of the mapping before anything is read.

## Troubleshooting

Ask the assistant to check the HWiNFO status: `hwinfo_status` reports the
cause. The usual ones:

| Problem | Cause |
|---|---|
| shared memory not found | HWiNFO is not running, the Sensors window is closed, Shared Memory Support is off, or it was enabled without restarting HWiNFO. |
| `active: false`, values stale | The Sensors window was closed or the free edition's 12-hour limit was reached. |
| access denied | HWiNFO and the MCP client run at different integrity levels. |
| the assistant has no hwinfo tools | The client was not restarted after the plugin or the server was added, or the path to the executable is wrong. In Claude Code, `claude mcp list` shows the server and whether it connects. |

## What has been verified

The server was validated against HWiNFO64 8.60 on a Ryzen 9 9950X3D with a
GeForce RTX 5080, with HWiNFO running in Portuguese, and used from Claude Code.
The labels of that machine are kept as a test fixture
(`internal/hwinfo/testdata`), and the first pattern of every `thermal_overview`
field is covered by a regression test built from it.

The plugin was installed in Claude Code 2.1.319 through the marketplace, from
the published release: the download matches its checksum, its server connects,
and its tools and skills register. The MCP bundle was validated and packed with
the official `mcpb` tool, but had not been installed in Claude Desktop when
v0.2.0 was released.

The patterns for Intel processors, older Ryzen models and AMD or Intel graphics
cards follow HWiNFO's naming but have not been checked on such hardware. If
`thermal_overview` misses a reading on your system, `get_readings` still returns
it, and the patterns live in `internal/hwinfo/filter.go` (`cpuLabel`,
`gpuLabel`).

## Questions and bug reports

Open an [issue](https://github.com/templarfelix/hwinfo-mcp/issues). If
`thermal_overview` picks the wrong reading or misses one on your hardware, the
output of `list_sensors` and of `get_readings` for the CPU or GPU sensor is what
it takes to fix it. Remove serial numbers from it first.

## Development

The tool versions are pinned in `.tool-versions`: Go, and Node.js for packing
the MCP bundle. With [asdf](https://asdf-vm.com) and its `golang` and `nodejs`
plugins, this installs them:

```
asdf install
```

`go.mod` asks for the same Go version, so a different Go release fetches that
toolchain on its own, and CI reads both versions from the same file.

```
go vet ./...
go test -race ./...
go test -run '^$' -fuzz FuzzParse -fuzztime 30s ./internal/hwinfo
go build -trimpath -ldflags "-s -w" -o hwinfo-mcp.exe .
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
| `plugin/` | The Claude Code plugin: manifest, MCP configuration and skills. |
| `mcpb/` | The MCP bundle's manifest and the MCP Registry entry. |
| `tools/release` | Builds and packs every file of a release. |
| `.claude-plugin/marketplace.json` | The marketplace entry, which points at the plugin of the latest release. |

Dependencies are the Go standard library, `golang.org/x/sys` and the official
MCP Go SDK.

### Releasing

1. Push a tag such as `v1.2.3`. CI runs `go run ./tools/release` and attaches
   the executables, the plugin, the bundle, `server.json` and `SHA256SUMS` to
   the release. The same command run by hand builds the same files into
   `dist/`, which is how to try them before tagging:
   `claude --plugin-dir dist/hwinfo-plugin.zip` loads the plugin for one
   session.
2. Once the release is published, point `.claude-plugin/marketplace.json` at
   the new `hwinfo-plugin.zip`: its URL, and its SHA-256 from the release's
   `SHA256SUMS`. Plugin users get the update from that commit.

## License

MIT. See [LICENSE](LICENSE).

This project is not affiliated with HWiNFO or its authors.
