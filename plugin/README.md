# hwinfo plugin for Claude Code

Lets Claude read the hardware sensors [HWiNFO](https://www.hwinfo.com) monitors
on a Windows PC, and gives it a method for judging what they say.

The plugin bundles:

- the **hwinfo MCP server**, a read-only Windows executable with four tools:
  `hwinfo_status`, `list_sensors`, `get_readings` and `thermal_overview`;
- three skills:

  | Skill | Use it for |
  |---|---|
  | `/hwinfo:thermal-check` | Is the CPU or graphics card running too hot, being held back by temperature, or in need of new thermal paste or pads? |
  | `/hwinfo:hardware-checkup` | A broad check of error counters, drive health, voltages, fans, memory and limits. |
  | `/hwinfo:setup` | Getting HWiNFO to publish its readings, or finding out why they stopped. |

Claude also picks the skills up on its own when you ask about temperatures or
hardware problems in plain words.

## Install

In a Claude Code session:

```
/plugin install hwinfo --marketplace templarfelix/hwinfo-mcp
```

Or from a shell:

```
claude plugin marketplace add templarfelix/hwinfo-mcp
claude plugin install hwinfo@hwinfo-mcp
```

If you had already added the server by hand with `claude mcp add`, remove that
entry, or every tool shows up twice: `claude mcp remove hwinfo`, with `-s user`
if you added it with `--scope user`.

## Requirements

- Windows. HWiNFO exists only for Windows, so on macOS, Linux and inside WSL
  the server has nothing to read.
- HWiNFO64 running with **Shared Memory Support** enabled. `/hwinfo:setup`
  walks you through it.

The executable is not code-signed. It reads HWiNFO's shared memory and nothing
else: it opens no network connection and changes nothing on the system.

Source, documentation and issues: https://github.com/templarfelix/hwinfo-mcp

This project is not affiliated with HWiNFO or its authors.
