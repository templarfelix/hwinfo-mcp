# hwinfo plugin for Claude Code

Lets Claude read the hardware sensors [HWiNFO](https://www.hwinfo.com) monitors
on a Windows PC, and gives it a method for judging what they say.

The plugin bundles:

- the **hwinfo MCP server**, a read-only Windows executable with five tools:
  `hwinfo_status`, `list_sensors`, `get_readings`, `sample_readings` and
  `thermal_overview`;
- four skills:

  | Skill | Use it for |
  |---|---|
  | `/hwinfo:thermal-check` | Is the CPU or graphics card running too hot, being held back by temperature, or in need of new thermal paste or pads? |
  | `/hwinfo:voltage-check` | Are the power supply rails and the graphics card's power inputs in range, and how do they behave when the load changes? |
  | `/hwinfo:hardware-checkup` | A broad check of error counters, drive health, voltages, fans, memory and limits. |
  | `/hwinfo:setup` | Getting HWiNFO to publish its readings, or finding out why they stopped. |

Claude also picks the skills up on its own when you ask about temperatures or
hardware problems in plain words.

## Install

One command, with Claude Code 2.1.292 or later:

```
claude plugin install hwinfo --marketplace templarfelix/hwinfo-mcp
```

On an older version, use these two instead:

```
claude plugin marketplace add templarfelix/hwinfo-mcp
claude plugin install hwinfo@hwinfo-mcp
```

Inside a session (version 2.1.275 or later),
`/plugin install hwinfo --marketplace templarfelix/hwinfo-mcp` adds the
marketplace and opens the plugin's page, where you install it.

If you had already added the server by hand with `claude mcp add`, remove that
entry, or every tool shows up twice: `claude mcp remove hwinfo`, with `-s user`
if you added it with `--scope user`.

## Requirements

- Windows. HWiNFO exists only for Windows, so on macOS, Linux and inside WSL
  the server has nothing to read.
- HWiNFO64 running with **Shared Memory Support** enabled. HWiNFO is a separate
  program under its own licence and is not included: get it with
  `winget install REALiX.HWiNFO` or from https://www.hwinfo.com/download/, and
  `/hwinfo:setup` walks you through the settings.

The executable is not code-signed. It reads HWiNFO's shared memory and nothing
else: it opens no network connection and changes nothing on the system.

Source, documentation and issues: https://github.com/templarfelix/hwinfo-mcp

This project is not affiliated with HWiNFO or its authors.
