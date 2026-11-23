---
name: thermal-check
description: Judge from HWiNFO sensor readings whether a Windows PC's CPU or graphics card runs too hot, is being held back by temperature, or needs new thermal paste or thermal pads. Use when the user asks about temperatures, overheating, throttling, hot spots, thermal paste, thermal pads, fan curves or whether cleaning the cooler would help.
license: MIT
compatibility: Needs the hwinfo MCP server, which runs on Windows with HWiNFO64 and its Shared Memory Support enabled.
allowed-tools: mcp__plugin_hwinfo_hwinfo__hwinfo_status mcp__plugin_hwinfo_hwinfo__thermal_overview mcp__plugin_hwinfo_hwinfo__get_readings mcp__plugin_hwinfo_hwinfo__list_sensors mcp__hwinfo__hwinfo_status mcp__hwinfo__thermal_overview mcp__hwinfo__get_readings mcp__hwinfo__list_sensors
---

# Thermal check

Give the user a verdict they can act on, built only from what the sensors show, and say plainly what the sensors cannot show. A confident answer from one number is the failure this skill exists to prevent.

The tools come from the hwinfo MCP server: `hwinfo_status`, `thermal_overview`, `get_readings` and `list_sensors`. Labels in the output are HWiNFO's original English ones even when HWiNFO runs in another language.

## 1. Check that the data is live

Call `hwinfo_status` first. If it is not `active`, pass on the `problem` and `hint` it returns and stop; the `setup` skill covers the fix. Stale numbers must not be judged.

Every reading has a minimum, maximum and average that HWiNFO has kept since it started or since the user last reset them. Ask how long that has been and what ran in that time, because a peak means little without the load behind it.

## 2. Read the sensors

1. `thermal_overview` for the CPU and GPU summary, the other temperatures and the fans.
2. `get_readings` with `{"label": "Limit", "stats": true}` and with `{"label": "Throttling", "stats": true}`. These hold what actually caps the hardware: `Thermal Limit`, `CPU PPT Limit`, `CPU TDC Limit`, `CPU EDC Limit`, `Thermal Throttling (...)` on the CPU, and `Performance Limit - Thermal` and `Performance Limit - Power` on the GPU. Which of them exist depends on the hardware.
3. For the graphics card in detail, `get_readings` with `{"sensor": "GPU", "types": ["temperature", "fan", "power"], "stats": true}`. A fan appears twice under the same label: once as a speed in RPM and once, with type `other`, as a duty cycle in percent.

## 3. Get readings under load

A verdict needs the hardware working hard. Idle readings only show that nothing is badly wrong at idle.

- If the PC is idle, ask the user to start the game, render or stress test that worries them, and read again several times while it runs, a minute or so apart. Use the heaviest moments.
- A reading taken in one call is a snapshot. You cannot watch the PC between calls, so say how many snapshots the verdict rests on. If one snapshot is all there is, call the verdict provisional.
- The picture changes with the load. A graphics card that looks comfortable at 250 W can show a clearly larger hot-spot gap at 300 W. Do not carry a conclusion from a lighter load over to a heavier one.

## 4. Rules for reading the numbers

**Peaks are not pairs.** HWiNFO tracks each reading's maximum on its own. The highest temperature and the highest power did not necessarily happen together, so never compare, subtract or divide one maximum by another. For any relationship between two readings, use their current values from the same call: they share one `last_update`.

**Temperature only means something next to power.** Report pairs: this temperature at this many watts. To tell whether something got worse, compare temperatures at similar power, not peak against peak.

### CPU

- **Find the limit that stops it.** `Thermal Limit` is how close the CPU is to its temperature limit, in percent. At 100 % the CPU is being capped by temperature, even while the `Thermal Throttling` flags stay at 0. `thermal_overview` reports only those flags under `thermal_throttling`, so read `Thermal Limit` yourself and report both.
- **Open power limits end at the temperature limit by design.** If the power and current limit readings peak far below 100 % while `Thermal Limit` reaches 100 %, the board lets the CPU draw far more than it does, and any all-core load will climb to the temperature limit whatever the cooler. That alone says nothing about paste. What matters then is how much power the CPU sustains at that temperature, and the fix, if one is wanted, is a power or temperature limit in the BIOS.
- **Compare the dies.** On AMD CPUs with two CCDs, both should read close to each other under an all-core load. One staying well above the other under the same load can point to uneven contact.
- **Watch for creep.** At a constant load, a temperature that settles within a minute means the heat is being carried away. One that keeps climbing for minutes points at the cooler filling up: airflow, dust, radiator or pump, rather than paste.
- **Fans.** Compare fan speeds at idle and under load. Fans that do not speed up when the CPU gets hot are a fan-curve setting, and usually the cheapest gain.

### Graphics card

- **The hot spot is always above the GPU temperature.** It is the hottest sensor on the die, while the GPU temperature is a representative value. Work out the gap between the two from the same call.
- **The gap grows with power.** Always quote it with the board power, and compare gaps only at similar power.
- **Flags and headroom.** `Performance Limit - Thermal` shows whether the card ever limited itself (`max` of 1) or is doing so now (`value` of 1). A fan duty well below 100 % at the hottest moment means the card still has cooling in reserve.
- **Memory.** The memory chips should read close to each other. One chip well above the rest can point to a thermal pad that is not making contact.

### Reference points, and what they are worth

Use these to place a reading, and tell the user they are rules of thumb from reviewers and forums, not manufacturer specifications:

- A hot-spot gap up to about 15 to 20 °C at the card's normal gaming load is commonly called normal. A gap of 25 to 30 °C or more is commonly read as poor contact between die and cooler.
- Hot-spot readings of 90 to 100 °C are commonly seen on high-end cards under heavy load. Cards have been reported to throttle with the hot spot around 105 to 110 °C.

Manufacturers publish a maximum for the GPU temperature and for the CPU, not for the hot spot. If you quote such a limit, say where it comes from, and look it up rather than recall it. Reported gaps for the same card model vary widely between samples, so a single threshold cannot settle the question: the trend on this one PC, at similar power, is the stronger evidence.

## 5. Say what you cannot know

State the gaps that apply: the room temperature, the cooler model, the case airflow, how many snapshots were taken, what was running when a peak was recorded, and whether the heaviest load was reached at all. Do not turn them into guesses.

## 6. Recommend the least invasive step that fits

1. **Nothing**, when temperatures have margin, no limit was reached and the gaps are ordinary.
2. **Settings and airflow**: a steeper fan curve, more case airflow, cleaning dust from filters, fins and fans, or a power or temperature limit for the CPU or the graphics card. Cleaning is cheap and safe and comes before any repaste.
3. **New paste or pads**, only when the evidence points at contact: a limit reached at modest power, a large gap at ordinary load, one die or one memory chip far from the others, or a clear worsening against an earlier reading at similar power. Mention that opening a graphics card can affect its warranty.

Offer to keep today's pairs (temperature at a given power, hot-spot gap at a given power) as a baseline. Repeating the same load later and comparing against them is the most reliable way to see paste ageing.

## 7. Answer

Reply in the user's language. Lead with the verdict in one sentence. Then give the pairs it rests on, in a small table when there are several, the limits and flags, the caveats from step 5, and the recommendation. Keep rules of thumb labelled as such, and do not present a provisional verdict as final.
