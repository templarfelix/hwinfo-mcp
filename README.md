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
this server reads, so the executable is released for Windows on x64 and on Arm.
The plugin and the bundles carry the x64 build, and nothing has been tested on
Windows on Arm. Linux inside WSL 2 does not see the PC's sensor chips either:
run the server on the Windows side.

## Quick start

Three steps: install the server, get HWiNFO publishing, and ask.

### 1. Install the bundle or the plugin

There are five ways to install the server. From the easiest to the most
involved:

| Way | For | What it takes |
|---|---|---|
| The bundle with HWiNFO | Claude Desktop, on a PC that has no HWiNFO | Download one file and open it. HWiNFO comes inside. |
| The bundle | Claude Desktop | Download one file and open it. |
| The plugin | Claude Code | One command. |
| [The executable](#installing-the-server-by-hand) | Any MCP client | Download it and register it with the client yourself. |
| [Your own build](#building-from-source) | Any MCP client | The same, after compiling it with Go. |

**Claude Desktop: a bundle.** Two files install the same extension. Take
[`hwinfo-mcp-with-hwinfo.mcpb`](https://github.com/templarfelix/hwinfo-mcp/releases/latest/download/hwinfo-mcp-with-hwinfo.mcpb)
(24 MB) if the PC has no HWiNFO yet: it also carries
[HWiNFO's portable package](#the-copy-of-hwinfo-in-the-bundle). Take
[`hwinfo-mcp.mcpb`](https://github.com/templarfelix/hwinfo-mcp/releases/latest/download/hwinfo-mcp.mcpb)
(4 MB) if HWiNFO is installed already, or if you would rather install it
yourself. Open the file, or drag it into Claude Desktop. Neither bundle is
signed. If it does not install,
[install the server by hand](#installing-the-server-by-hand), and please open
an issue.

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

The plugin brings the server together with four
[skills](#skills-that-come-with-the-plugin) that give the assistant a method
for reading the sensors, so there is no executable to download. Inside a
session (version 2.1.275 or later),
`/plugin install hwinfo --marketplace templarfelix/hwinfo-mcp` adds the
marketplace and opens the plugin's page, where you install it.

**Any other MCP client**, or if you would rather register the server yourself:
see [Installing the server by hand](#installing-the-server-by-hand).

### 2. Get HWiNFO publishing

HWiNFO does the measuring; this project only reads what HWiNFO publishes.
HWiNFO is a separate program under its own licence. It asks for administrator
rights when it starts, which it needs to reach the hardware, and its publishing
is off by default.

**With the bundle that carries HWiNFO**, ask the assistant:

> Is HWiNFO publishing sensor data?

While it is not, the answer names a file, `Setup-HWiNFO.cmd`, in the
extension's folder. Running it is your step: paste its path into the Run box
(Win+R). It unpacks HWiNFO into a folder of its own, puts a settings file next
to it that switches the publishing on, and starts it. When Windows asks for
administrator rights, the publisher it names should be REALiX.
[The copy of HWiNFO in the bundle](#the-copy-of-hwinfo-in-the-bundle) says
exactly what the file does and what it leaves alone.

**Otherwise**, install HWiNFO with:

```
winget install REALiX.HWiNFO
```

Or download [HWiNFO64](https://www.hwinfo.com/download/). Start it and switch
its publishing on once:

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

Either way, the free edition of HWiNFO stops publishing after 12 hours of run
time; see [HWiNFO settings](#hwinfo-settings).

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

The plugin adds four skills. The assistant uses them on its own when a
question calls for one, and you can also start one by name:

| Skill | What it does |
|---|---|
| `/hwinfo:thermal-check` | Judges whether the CPU or graphics card runs too hot, is held back by temperature, or needs new thermal paste or pads. It works from temperature and power read at the same moment, under load, and says what the sensors cannot show. |
| `/hwinfo:voltage-check` | Judges whether the power supply rails and the graphics card's power inputs are in range, and how they behave when the load changes. It says first what a mainboard's voltage sensors cannot show. |
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
shortened. The CPU is at 60 °C now and peaked at 95.4 °C since HWiNFO started.
No throttling flag was raised, yet `thermal_limit` peaked at 100 %: at that
peak the CPU was holding itself at its temperature limit.

```json
{
  "active": true,
  "last_update": "2026-11-23T22:55:31-03:00",
  "cpus": [
    {
      "name": "AMD Ryzen 9 9950X3D",
      "temperature": { "label": "CPU (Tctl/Tdie)", "value": 60.125, "unit": "°C", "min": 51.5, "max": 95.375, "avg": 69.557 },
      "package_power": { "label": "CPU Package Power", "value": 98.109, "unit": "W", "min": 55.714, "max": 237.232, "avg": 125.216 },
      "thermal_limit": { "label": "Thermal Limit", "value": 58.913, "unit": "%", "min": 51.351, "max": 100.016, "avg": 69.562 },
      "thermal_throttling": { "active": false, "occurred": false }
    }
  ],
  "gpus": [
    {
      "name": "NVIDIA GeForce RTX 5080",
      "temperature": { "label": "GPU Temperature", "value": 39.813, "unit": "°C", "min": 36.895, "max": 72.27, "avg": 52.341 },
      "hot_spot": { "label": "GPU Hot Spot Temperature", "value": 44.188, "unit": "°C", "min": 41, "max": 94.344, "avg": 60.328 },
      "power": { "label": "GPU Power", "value": 25.117, "unit": "W", "min": 20.717, "max": 319.839, "avg": 115.054 },
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
the settings. HWiNFO Pro has no such limit. Enabling it again is a step for
you to take: this project does not do it for you, and its tools tell the
assistant not to. When the readings stop, the assistant can tell you why; see
[Troubleshooting](#troubleshooting).

### The copy of HWiNFO in the bundle

`hwinfo-mcp-with-hwinfo.mcpb` has a folder, `hwinfo`, that the other bundle
does not. In it:

| File | What it is |
|---|---|
| `hwi_860.zip` | HWiNFO's portable package, version 8.60, exactly as its author publishes it. Its SHA-256 is `0f7726ee30f6b040cc13aa9de5a19619545a00dd7515fc17ebea9f9f2996e83c`, and each executable in it carries REALiX's signature. It is included with its author's permission. |
| `HWiNFO64.INI` | A settings file of this project: sensors-only mode, Shared Memory Support on, no welcome screen. |
| `Setup-HWiNFO.cmd` | A setup helper of this project. |
| `README.txt` | The same explanation, next to the files. |

HWiNFO is not part of this project. It belongs to REALiX, is freeware for
non-commercial use only, and comes under
[its own licence](https://www.hwinfo.com/licenses/), not under this project's.

Nothing in that folder runs unless you start it. The server only tells the
assistant where the helper is, and tells it to leave running it to you.
The helper is for a PC that has no HWiNFO. Run by you, it does three things:

1. It unpacks the package into `%LOCALAPPDATA%\hwinfo-mcp\HWiNFO`, unless
   HWiNFO is already there. The package is not run from the extension's own
   folder, because Claude Desktop replaces that folder with every update.
2. It puts the settings file next to it, unless a settings file is already
   there.
3. It starts HWiNFO, unless a HWiNFO is already running.

And it leaves the rest alone. It never closes, restarts or reconfigures a
HWiNFO that is running, it does not make HWiNFO start with Windows, and it has
no part in the 12-hour limit: when HWiNFO switches Shared Memory Support off,
switching it on again is done by hand, as above. Where HWiNFO is installed in
its usual place, `C:\Program Files\HWiNFO64`, the helper says so and sets
nothing up; `Setup-HWiNFO.cmd /portable` sets its copy up all the same. On
Windows on Arm it starts `HWiNFO_ARM64.exe` and adds no settings file.

To remove that copy of HWiNFO, close it and delete the folder it was unpacked
into. Options such as starting with Windows are yours to set, in HWiNFO's
settings.

## Installing the server by hand

The bundles and the plugin carry the server with them. For any other MCP client,
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
| `sample_readings` | What the chosen readings did over a few seconds: first and last value, lowest, highest and mean, the largest change between two samples, and when each extreme happened. |
| `thermal_overview` | The readings that say how hot the PC runs: per CPU the control temperature (Tctl/Tdie or package), die average, per-CCD temperatures, hottest core, package power, how much of its temperature limit it uses and the throttling flags; per GPU the core, hot spot and memory junction temperatures, power draw, fans and thermal limit flag; plus the remaining temperatures and fan speeds. |

All five are annotated as read-only. `get_readings` and `sample_readings` take
arguments, all of them optional:

| Argument | Tool | Meaning |
|---|---|---|
| `sensor` | both | Part of a sensor name, in any letter case: `CPU`, `RTX 5080`, `S.M.A.R.T.` |
| `label` | both | Part of a reading label, in any letter case: `Temperature`, `Tctl`, `Limit` |
| `types` | both | Reading types to include, any of `temperature`, `voltage`, `fan`, `current`, `power`, `clock`, `usage`, `other` |
| `stats` | `get_readings` | `true` to add the minimum, maximum and average |
| `offset` | `get_readings` | How many matching readings to skip; pass the `next_offset` of the previous answer to read on |
| `seconds` | `sample_readings` | How long to watch, from 1 to 30 seconds; 10 if left out |
| `values` | `sample_readings` | `true` to also get every sampled value, for up to 16 readings |

For example, the GPU's temperatures and fans with their statistics:

```json
{ "sensor": "GPU", "types": ["temperature", "fan"], "stats": true }
```

About the output:

- A PC has several hundred readings (484 on the development machine), and one
  `get_readings` call returns at most 300 of them. When more match, `truncated`
  is set, `matched` gives the total and `next_offset` says where to continue:
  narrow the filters, or pass it as `offset` to read on.
- Flags that HWiNFO shows as Yes/No come as `1` or `0` with the unit `Yes/No`.
- A `value` is `null` when HWiNFO holds no number for the reading.

### Watching readings over time

`sample_readings` watches for up to 30 seconds and takes one sample per HWiNFO
poll, every 2 seconds unless you changed that in HWiNFO. For example, the
voltages and the power readings of the graphics card over half a minute:

```json
{ "sensor": "GPU", "types": ["voltage", "power"], "seconds": 30, "values": true }
```

Two limits are worth knowing:

- Nothing shorter than a poll is visible, to this tool or to HWiNFO itself. A
  voltage spike that lasts milliseconds leaves no trace; catching one takes an
  oscilloscope.
- It finds no extreme that the minimum and maximum would miss. Those cover
  every poll since HWiNFO started, so "did it ever go too high" is answered by
  `get_readings` with `stats`. What sampling adds is the moment: whether a
  voltage dipped just as the power draw rose.

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
| shared memory not found | HWiNFO is not running, the Sensors window is closed, Shared Memory Support is off, or it was enabled without restarting HWiNFO. With the bundle that carries HWiNFO, the answer also names the setup helper. |
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
a published release: the download matches its checksum, its server connects,
and its tools and skills register. The bundle was installed in Claude Desktop
6.6395.1 for Windows: its server connects and its five tools register. A
question answered from a Claude Desktop chat, and the skills run by a model,
have not been observed yet.

The bundle that carries HWiNFO was installed in Claude Desktop for Windows
(package 6.28899.6.0) on the same machine, from a build of this version. The
package inside the installed extension matched its pinned SHA-256, and while no
HWiNFO was running `hwinfo_status` named the setup helper. Run by hand, the
helper unpacked the package and started that copy after Windows asked for
administrator rights, and four seconds later the copy was publishing its
readings with no step in its settings. Two things about that run: the machine
already had HWiNFO installed, with HWiNFO's per-user settings in the registry,
so a PC that has never run HWiNFO has not been tried; and the helper learned to
stand back from an installed HWiNFO after it, which only automated tests have
exercised since. Nothing of this has been tried on Windows on Arm.

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

The tool versions are pinned in `.tool-versions`: Go, and Node.js, which is
only needed to check the MCP bundle with the official `mcpb` tool. With
[asdf](https://asdf-vm.com) and its `golang` and `nodejs` plugins, this
installs them:

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
| `main.go` | The MCP server and its five tools. |
| `internal/hwinfo/layout.go` | The shared memory layout and its decoding. |
| `internal/hwinfo/snapshot.go` | `Parse`, the snapshot types and the reader's portable half. |
| `internal/hwinfo/read_windows.go` | Opening, locking and copying the shared memory. |
| `internal/hwinfo/filter.go` | Filtering and the thermal overview heuristics. |
| `internal/hwinfo/hwinfotest` | Builds synthetic shared memory blocks for tests. |
| `plugin/` | The Claude Code plugin: manifest, MCP configuration and skills. |
| `mcpb/` | The MCP bundles' manifest and icon, and the MCP Registry entry. |
| `mcpb/hwinfo.json` | Which HWiNFO portable package one bundle carries: its version, size and SHA-256, and where to get it. |
| `mcpb/hwinfo/` | This project's files for that bundle's `hwinfo` folder: the settings file, the setup helper and the notice. |
| `tools/release` | Builds and packs every file of a release. |
| `tools/marketplace` | Points the marketplace entry, and the pin of HWiNFO's package, at a published release. |
| `.claude-plugin/marketplace.json` | The marketplace entry, which points at the plugin of the latest release. |

Dependencies are the Go standard library, `golang.org/x/sys` and the official
MCP Go SDK.

### Releasing

1. Push a tag such as `v1.2.3`. CI runs `go run ./tools/release` and attaches
   the executables, the plugin, the two bundles, `server.json` and
   `SHA256SUMS` to the release. The same command run by hand builds the same
   files into `dist/`, which is how to try them before tagging:
   `claude --plugin-dir dist/hwinfo-plugin.zip` loads the plugin for one
   session.
2. Once the release is published, run
   `go run ./tools/marketplace -version v1.2.3` and commit the changes. It
   points `.claude-plugin/marketplace.json` at the new `hwinfo-plugin.zip`,
   after checking the download against the release's `SHA256SUMS`; plugin
   users get the update from that commit. And it points `mcpb/hwinfo.json` at
   the release's bundle that carries HWiNFO, after checking that the bundle
   holds the pinned package.

HWiNFO's portable package is not in the repository. `tools/release` downloads
it from the address in `mcpb/hwinfo.json` into its `-hwinfo` directory and
uses only the file whose size and SHA-256 are pinned there. That address is a
mirror named on HWiNFO's download page, and it keeps only HWiNFO's current
version; once it has dropped the file, the tool takes the package out of the
published bundle the pin names. To move to a newer HWiNFO, download its
portable package, check that Windows shows a valid REALiX signature on every
executable in it, and set `version`, `file`, `size`, `sha256` and `url` in
the pin, with `bundle` empty again. `mcpb/hwinfo/HWiNFO64.INI` and the README
sections that name the version go with it.

## License

MIT. See [LICENSE](LICENSE).

This project is not affiliated with HWiNFO or its authors, and its licence does
not cover HWiNFO: HWiNFO belongs to REALiX and is freeware for non-commercial
use under [its own licence](https://www.hwinfo.com/licenses/). One file of a
release, `hwinfo-mcp-with-hwinfo.mcpb`, carries HWiNFO's portable package
unmodified, with its author's permission. The settings file and the setup
helper next to it are this project's own.
