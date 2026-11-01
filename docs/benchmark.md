# Speed with 200 devices and 90 days of history

Bield keeps device history as raw samples for 48 hours, then as averages of 5 minutes (for up to 14 days) and of an hour (for as long as the settings say, up to 90 days). The goal was a Bield that stays small with 200 devices and a quarter of history. This page says how to measure that, and what it found.

## What is summed up

Every 30 seconds Bield records each device's CPU, memory, disk, load, network, temperature and battery, and every minute the round-trip time of a ping. Every 5 minutes, it folds the samples whose time is over into buckets (`metrics_rollup` and `pings_rollup` in the database), and the 5-minute buckets into buckets of an hour, weighing each by the samples in it. Once a bucket exists, the raw samples it sums up can go: Bield deletes them after 48 hours, and never before they're in a bucket.

| What | Kept for |
| --- | --- |
| Raw metrics and pings | 48 hours |
| Buckets of 5 minutes | 14 days, or the device history setting if it's less |
| Buckets of an hour | The device history setting: 7 days by default, up to 90 |
| What used a machine's CPU, and what its containers used | 7 days at most, since those aren't summed up |

A device's history of 24 hours or less reads the raw samples, or the buckets of 5 minutes. A week, a month and a quarter read the buckets of an hour, so a chart costs the same however far back it goes.

## How to measure

Two commands, both on a demo with made-up devices (see [Demo mode](demo.md)):

```sh
make bench   # the store's queries and the usage recorder, one at a time
make load    # the whole of Bield, in a browser's eyes
```

`make load` starts Bield in demo mode in the test process, waits for it, and says what it costs while nothing looks at it, while 10 browsers hold its live stream, and while someone clicks through the devices' pages. `BIELD_LOAD_DEVICES` (200), `BIELD_LOAD_DAYS` (7) and `BIELD_LOAD_SECONDS` (30, for each measurement) change it. Add `-cpuprofile cpu.out` to the `go test` command in the Makefile, and read the profile with `go tool pprof`. A demo seeds its history before it starts, which takes most of the time: about 80 seconds for 200 devices and 90 days.

The numbers below are from a VPS with 4 slow virtual CPUs (2.0 GHz, about 3.6 times slower than a recent laptop), so a laptop does better.

## What it found

With 200 devices and 90 days of history, a demo's database is 580 MB, and 276 MB with 7 days. Without the buckets, 90 days of raw samples would be 52 million rows of metrics, and that many for a query to read through.

| | 12 devices, 7 days | 200 devices, 7 days | 200 devices, 90 days |
| --- | --- | --- | --- |
| Nothing looking | 0.4% of a core, 59 MB | 3.2% of a core, 126 MB | 3.2% of a core, 129 MB |
| 10 browsers on the live stream | 0.7% of a core | 4.6% of a core | 4.7% of a core |
| A snapshot (what the stream sends each second) | 30 KB of JSON | 334 KB of JSON | 334 KB of JSON |

The CPU and the memory grow with the devices, a little less than evenly, and not at all with the history. The live stream costs almost nothing for each browser, since a snapshot is built and compressed once for everyone.

Where the time went, and what changed:

- **Reading a device's history.** Every chart read the raw samples of its range, and with buckets, it still read the last 48 hours of them, which is 5,760 rows for the device. A history now reads the buckets up to the latest one, and raw samples for what isn't in a bucket yet.

  | One device, 200 devices in the database | Before | After |
  | --- | --- | --- |
  | Metrics, 24 hours | 12.7 ms | 2.1 ms |
  | Metrics, 7 days | 24.5 ms | 1.6 ms |
  | Metrics, 30 days | 26.3 ms | 3.5 ms |
  | Metrics, 90 days | 28.5 ms | 7.3 ms |
  | Round-trip times, 24 hours | 5.3 ms | 1.1 ms |
  | Round-trip times, 90 days | 12.6 ms | 3.6 ms |

- **Storing what runs on each machine.** Bield deflates each device's minute of processes before it stores it, and made a new compressor each time, which allocates over a megabyte. For 200 devices that is up to 240 MB of garbage a minute, and the compressor was the biggest cost of the usage recorder. It reuses them now: 777 µs and 1.2 MB for a minute became 430 µs and 59 KB.

What's left, and fine at this size:

- **Starting up** reads the last 24 hours of pings and results of every device and check, to bring the uptime figures back. That is about 3 seconds on this VPS with 200 devices.
- **The live stream** builds a snapshot, for admins and for viewers, about once a second, since something always changes with 200 devices. That is the biggest cost left, at about 1.5% of a core here.
- **The hourly prune** deletes the hour of raw samples that expired, which is about 36,000 rows and under a tenth of a second.

Clicking through a device's pages costs about 7 ms of CPU for each request, and the other pages 0.4 to 9 ms each.

## What this doesn't measure

The demo's machines run in Bield's own process, so what it costs includes making them up: their heartbeats, their fake SSH and Docker. An agent's heartbeat goes straight to the registry, without the HTTP request, Tailscale's identity lookup and the encryption a real one has, and Bield's connection to a real tailnet isn't there either. Those cost something for each agent every 15 seconds that this leaves out. The percentages are an upper bound for what Bield itself does with the data, and a lower bound for a real tailnet.
