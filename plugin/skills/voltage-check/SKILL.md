---
name: voltage-check
description: Judge from HWiNFO sensor readings whether a Windows PC's voltages are in order, covering the power supply rails, the graphics card's power inputs, and how voltages behave when the load changes. Use when the user asks about voltage spikes or drops, a failing or weak power supply, unstable rails, shutdowns or restarts under load, or coil whine and flicker that they suspect is power related.
license: MIT
compatibility: Needs the hwinfo MCP server, which runs on Windows with HWiNFO64 and its Shared Memory Support enabled.
allowed-tools: mcp__plugin_hwinfo_hwinfo__hwinfo_status mcp__plugin_hwinfo_hwinfo__list_sensors mcp__plugin_hwinfo_hwinfo__get_readings mcp__plugin_hwinfo_hwinfo__sample_readings mcp__plugin_hwinfo_hwinfo__thermal_overview mcp__hwinfo__hwinfo_status mcp__hwinfo__list_sensors mcp__hwinfo__get_readings mcp__hwinfo__sample_readings mcp__hwinfo__thermal_overview
---

# Voltage check

Say what the voltage readings show, what they cannot show, and what the smallest next step is. The sensors on a mainboard are coarse and slow; a confident verdict about a power supply from them alone is the failure this skill exists to prevent.

The tools come from the hwinfo MCP server: `hwinfo_status`, `list_sensors`, `get_readings`, `sample_readings` and `thermal_overview`. Labels in the output are HWiNFO's original English ones even when HWiNFO runs in another language.

## 1. Check that the data is live

Call `hwinfo_status`. If it is not `active`, pass on its `problem` and `hint` and stop; the `setup` skill covers the fix. Note `polling_period_ms`: it is the finest detail anything below can show.

## 2. What these readings can and cannot show

Tell the user this before any number, because it decides what the numbers are worth:

- **Nothing shorter than a poll is visible.** HWiNFO reads its sensors once per polling period, 2 seconds unless the user changed it. A spike or a dip that lasts milliseconds falls between two polls and leaves no trace. Catching one needs an oscilloscope or a power supply tester, not software.
- **`min` and `max` already cover every poll.** With `"stats": true`, each reading carries the lowest and highest value of every poll since HWiNFO started or since the user last reset them. That is the first place to look for a voltage that went out of range, and watching for a while cannot find an extreme those two missed. Ask how long HWiNFO has been running and what the PC did in that time.
- **The sensors are coarse.** A mainboard measures its rails through a monitoring chip of limited resolution, with its own error. The same rail can read differently on two boards.
- **Some inputs are not wired.** A monitoring chip has more inputs than a board uses, and HWiNFO shows them all. A reading that sits at an implausible, constant value is such an input, not a fault. Do not diagnose from it; say that it looks unconnected.

## 3. Read the voltages

1. `list_sensors` to see which sensors exist.
2. `get_readings` with `{"types": ["voltage"], "stats": true}`. This is everything HWiNFO measures as a voltage: usually the mainboard's rails, the processor's own telemetry, the memory modules where they report it, and the graphics card's inputs. If `truncated` is set, continue at `next_offset` or narrow by `sensor`.
3. Group what you got by sensor and say what each group is. Where a label does not tell you what the reading measures, say so instead of guessing.

## 4. Judge them

**Power supply rails.** For the `+12V`, `+5V` and `+3.3V` rails the ATX tolerance is 5 % either way: 11.40 to 12.60 V, 4.75 to 5.25 V and 3.135 to 3.465 V. Look at `min` first, since a rail sags under load, then at `max`. Because the sensor is coarse, a value just outside the range is a reason to measure properly, not proof of a failing power supply. A value far outside it, on a rail that otherwise behaves plausibly, deserves attention.

**Graphics card inputs.** Where the GPU sensor reports the voltage of its 12 V inputs, from the PCIe slot and from the power connectors, that is the same 12 V rail measured at the card, so the 11.40 to 12.60 V range applies. An input that reads clearly lower than the others, or drops as the card's power rises, can point at that cable or connector. A power connector on a high-power card is worth checking by hand for a full, straight fit, with the PC off.

**Processor and memory voltages.** These are set by the board and change by design: a processor's core voltage moves constantly with load and power states, and it is normal for it to span a wide range. Do not judge them against a fixed number, and do not recall limits from memory. If the user needs a limit, it comes from the manufacturer's documentation for that exact part, and you should look it up rather than quote one. What you can report is whether a voltage is steady when the load is steady.

**Other signs that go with a power problem.** `get_readings` with `{"sensor": "WHEA", "stats": true}` for hardware errors, and the limit and throttling readings (`{"label": "Limit", "stats": true}`) for a processor or card being held back. Voltage trouble rarely comes alone.

## 5. Watch them against the load

A rail that looks fine at idle can sag when the graphics card and processor draw power together. `sample_readings` shows that: it takes one sample per poll for up to 30 seconds and reports, for each reading, the lowest and highest value and how many seconds into the window each occurred.

1. Ask the user to start the game, render or stress test that causes trouble, and to tell you when it is running. A window that only covers idle shows nothing.
2. Call `sample_readings` with a few readings that belong together and `"values": true`, for example `{"sensor": "<mainboard sensor>", "label": "+12V", "seconds": 30, "values": true}`, and in a second call the power readings, or `{"types": ["voltage", "power"], "sensor": "<graphics card>", "seconds": 30, "values": true}` to get both from one sensor in one window. Every value is only returned for up to 16 readings; narrow the filters to stay under that.
3. Compare within one window: `min_at` of a voltage against `max_at` of a power reading, and the `values` side by side. A voltage whose lowest point lines up with the highest power draw is sagging under load. One that moves with no relation to the load is more likely sensor noise.
4. Repeat while the load changes, if one window was not enough. Each call is one window; you cannot watch between calls, so say how many windows the verdict rests on.

If the server has no `sample_readings` tool, call `get_readings` several times a few seconds apart and compare the values yourself. It is the same information with less timing detail.

## 6. Say what you cannot know

State the gaps that apply: anything faster than a poll, the accuracy of the board's sensor, what the load was when a minimum or maximum was recorded, the age and model of the power supply, and whether the heaviest load was reached at all. Do not turn them into guesses.

## 7. Recommend the least invasive step

1. **Nothing**, when the rails stay inside tolerance under load and nothing else points at power.
2. **Check by hand**: reseat the power connectors of the graphics card and mainboard with the PC switched off, and try another cable or outlet where that is easy.
3. **Measure properly**: a multimeter on the rails under load, a power supply tester, or trying a known-good power supply. This is what settles a suspicion that the readings raised. Never suggest opening a power supply.

Offer to keep today's rail minimums at a given load as a baseline. Repeating the same load later and comparing is the most reliable way to see a power supply ageing.

## 8. Answer

Reply in the user's language. Lead with the verdict in one sentence. Then give the readings it rests on, with `min` and `max` and the load they were taken under, in a small table when there are several; what the sensors cannot show; and the next step. Call a verdict provisional when it rests on idle readings or on a single window.
