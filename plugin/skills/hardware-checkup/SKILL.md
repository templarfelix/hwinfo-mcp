---
name: hardware-checkup
description: Run a broad health check of a Windows PC from HWiNFO readings, covering hardware error counters, drive health, voltages, fans, memory and performance limits. Use when the user asks whether their PC has a hardware problem, reports crashes, restarts, freezes or stutter, or wants a general check-up.
license: MIT
compatibility: Needs the hwinfo MCP server, which runs on Windows with HWiNFO64 and its Shared Memory Support enabled.
allowed-tools: mcp__plugin_hwinfo_hwinfo__hwinfo_status mcp__plugin_hwinfo_hwinfo__thermal_overview mcp__plugin_hwinfo_hwinfo__get_readings mcp__plugin_hwinfo_hwinfo__list_sensors mcp__hwinfo__hwinfo_status mcp__hwinfo__thermal_overview mcp__hwinfo__get_readings mcp__hwinfo__list_sensors
---

# Hardware check-up

Go through the readings that reveal hardware trouble, report what is wrong, what is fine and what could not be checked, and keep measurement apart from interpretation.

The tools come from the hwinfo MCP server: `hwinfo_status`, `list_sensors`, `get_readings` and `thermal_overview`. Labels in the output are HWiNFO's original English ones. What exists differs from PC to PC, so a missing reading is something to report, not an error.

## 1. Start from what the PC offers

1. Call `hwinfo_status`. If it is not `active`, pass on its `problem` and `hint` and stop; the `setup` skill covers the fix.
2. Call `list_sensors` to see which sensors exist. Use the names it returns in the `sensor` filter of `get_readings`.
3. Ask what the symptom is and when it happens (under load, at idle, at random, after waking), unless the user already said. It decides which readings matter most.

## 2. How to read the output

- Every reading has `min`, `max` and `avg` kept since HWiNFO started or since they were last reset; request them with `"stats": true`. Ask how long HWiNFO has been running: a clean record over ten minutes proves much less than one over a day of use.
- Flags that HWiNFO shows as Yes/No arrive as `1` or `0` with the unit `Yes/No`. `value` is the state now; a `max` of `1` means the flag was set at some point.
- Counters only say something through their change. A total that is not zero may be old; one that grows between two readings is active.
- One `get_readings` call returns at most 300 readings and sets `truncated` when more matched. Filter by sensor, label or type instead of asking for everything; when you do need the rest, pass the `next_offset` it returns as `offset`.

## 3. What to check

Work through the list, skipping what the PC does not report.

**Hardware errors.** `get_readings` with `{"sensor": "WHEA", "stats": true}`. `Total Errors` above zero means Windows logged hardware errors; a count that grows during the session is a live problem. Unstable overclocks, memory and undervolting are common causes, so ask about those.

**Drives.** `get_readings` with `{"sensor": "S.M.A.R.T."}`. Look at `Drive Failure` and `Drive Warning` (either set is serious), `Drive Remaining Life`, `Drive Available Spare` and the drive temperatures. A drive can report more than one temperature; they are different points of the same drive.

**Voltages.** `get_readings` with `{"types": ["voltage"], "stats": true}`, narrowed to the mainboard sensor. For the `+12V`, `+5V` and `+3.3V` rails the ATX tolerance is 5 % either way: 11.40 to 12.60 V, 4.75 to 5.25 V and 3.135 to 3.465 V. Pay most attention to the minimum, which shows the sag under load. Mainboard voltage sensors are coarse, so a value just outside the range is a reason to measure properly, not proof of a failing power supply. For a closer look, including how the rails behave while the load changes, use the `voltage-check` skill.

**Fans.** `get_readings` with `{"types": ["fan"], "stats": true}`. A graphics card whose fans stop at low temperature is normal. A case fan, CPU fan or pump that reads 0 RPM, or whose minimum dropped to 0, is not. Fans that hold one speed whatever the temperature point to a fixed fan curve.

**Memory.** The DIMM sensors, where present, report temperature, voltages and PMIC flags such as `PMIC High Temperature`, `PMIC Over Voltage` and `PMIC Under Voltage`. Separately, `Physical Memory Load` and `Virtual Memory Load` near 100 % mean the PC is running out of memory. That is a software and capacity matter, but it causes stutter and crashes that look like hardware faults, so report it.

**Graphics card bus.** `get_readings` with `{"sensor": "GPU", "label": "Error"}` and with `{"sensor": "GPU", "label": "Count"}`. Error counters above zero and growing point to the slot, a riser cable or the card. Some counts rise in normal operation when the link changes speed to save power; say so rather than calling them errors, and judge by the counters named as errors.

**Limits and throttling.** `get_readings` with `{"label": "Limit", "stats": true}` and with `{"label": "Throttling", "stats": true}`. They show whether temperature, power or current capped the CPU or the graphics card.

**Temperatures.** `thermal_overview` gives the CPU, GPU, mainboard, memory and drive temperatures in one call. For a judgement on cooling, thermal paste or pads, use the `thermal-check` skill instead of improvising one here.

## 4. Match the readings to the symptom

- **Restarts or shutdowns under load**: temperatures and thermal limits first, then the voltage minimums, then hardware errors.
- **Crashes and blue screens at random**: hardware errors, memory flags and temperatures, memory load.
- **Stutter or falling performance**: limits and throttling, memory load, drive activity and temperature.
- **Graphics glitches or black screens**: graphics card temperatures, its bus error counters, its power readings.

A check-up that finds nothing does not clear the hardware. Sensors do not see everything, and a fault that shows only under a load that did not run during the session leaves no trace.

## 5. Report

Reply in the user's language, in this order:

1. **What needs attention**, most serious first, each with the reading and its value.
2. **What looks fine**, briefly, so the user knows it was checked.
3. **What could not be checked**: sensors the PC does not have, and anything that needs a load that has not run yet.
4. **What to do next**: the smallest step that would confirm or rule out each finding.

Keep measurements and conclusions apart. A reading is a fact; what caused it is your judgement, so present it as a lead to verify, not as a diagnosis.
