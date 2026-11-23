---
name: setup
description: Get HWiNFO publishing its sensor readings so the hwinfo tools work, or explain why the readings stopped. Use when a hwinfo tool reports that sensor data is unavailable, inactive or stale, when the hwinfo tools are missing, or when the user is setting up HWiNFO for the first time.
license: MIT
compatibility: Needs the hwinfo MCP server, which runs on Windows with HWiNFO64.
allowed-tools: mcp__plugin_hwinfo_hwinfo__hwinfo_status mcp__hwinfo__hwinfo_status
---

# HWiNFO setup

HWiNFO does the measuring. The hwinfo MCP server only reads what HWiNFO publishes through its Shared Memory Support, and that publishing is off until the user switches it on. Guide the user through it; the settings live in HWiNFO's own window and are theirs to change.

## 1. Find out what is wrong

Call `hwinfo_status` and read `available`, `active`, `problem` and `hint`.

| What you see | What it means |
|---|---|
| `active: true` | Everything works. `last_update` and `age_seconds` say how fresh the data is. |
| `available: false`, shared memory not found | HWiNFO is not running, its Sensors window is closed, Shared Memory Support is off, or it was switched on without restarting HWiNFO. |
| `available: true`, `active: false` | HWiNFO stopped publishing. The Sensors window was closed, or the free edition's 12-hour limit was reached. The readings returned are the last ones written and are stale. |
| access denied | HWiNFO and the program running the server have different privilege levels. |
| The hwinfo tools do not exist at all | The server did not start. It is a Windows program: on macOS, Linux and inside WSL there is nothing for it to read. On Windows, check that the plugin is enabled and that security software did not block `hwinfo-mcp.exe`. |

## 2. Walk the user through the settings

1. Install HWiNFO64 from https://www.hwinfo.com/download/ and start it. It asks for administrator rights, which it needs to reach the hardware.
2. On the welcome screen, tick **Sensors-only** and click **Settings**. If HWiNFO is already running, the same settings are in the menu of its icon in the notification area.
3. On the **General / User Interface** tab, enable:
   - **Shared Memory Support**. This is the one that matters.
   - **Show Sensors on Startup**, because readings are published only while the sensors run.
   - **Minimize Sensors on Startup** and **Minimize Main Window on Startup**, so HWiNFO starts into the notification area.
   - **Minimize Sensors instead of closing**, so closing the window by accident does not stop the readings.
   - **Auto Start**, so HWiNFO starts with Windows.
   - And switch off **Show Welcome Screen and Progress**, so it does not wait at the welcome screen.
4. Confirm with **OK**, then close HWiNFO from its notification-area icon and start it again. Switching the option on in a running HWiNFO may not take effect until that restart.

The labels can read a little differently between HWiNFO versions and are translated when HWiNFO runs in another language. The settings file is the same either way: `HWiNFO64.INI` in HWiNFO's folder, normally `C:\Program Files\HWiNFO64`, contains `SensorsSM=1` once Shared Memory Support is on. The user can open it to check.

## 3. The 12-hour limit

The free edition of HWiNFO switches Shared Memory Support off after 12 hours of run time, and it then has to be enabled again in the settings. HWiNFO Pro has no such limit. Tell the user this is how HWiNFO is licensed, and do not suggest ways around it.

## 4. Confirm

After the user has changed the settings and restarted HWiNFO, call `hwinfo_status` again. With `active: true` and a recent `last_update`, the other hwinfo tools are ready. If it still fails, report the new `problem` text instead of repeating the same steps.

Reply in the user's language.
