# Metric internals & Tdarr quirks

Maintainer notes on *why* some metrics behave the way they do. These are
upstream Tdarr behaviors the exporter passes through — not bugs.

**These are inferences, not documented facts.** Tdarr's server is closed-source,
so everything below was derived empirically — from observed API payloads, live
`/metrics`, and controlled experiments (e.g. delete/reinstall a file and diff
the responses) — not from Tdarr source code or official docs. Treat each claim
as a well-supported assumption that can still be wrong or go stale. They were
verified against **Tdarr server v2.77.01**; any Tdarr upgrade can change them, so
re-verify against the raw API before relying on a claim here.

## Tdarr API sources

Every metric is derived from one of these Tdarr endpoints. When a Tdarr upgrade
changes behavior, re-verify against the raw response from the relevant call.

Tdarr publishes API docs at <https://docs.tdarr.io/docs/api/> (also bundled with
your installation). Each instance also serves a **Swagger 2.0** spec — Swagger UI
at `<your-tdarr-instance>/#/tools/api-docs`, raw JSON at
`<your-tdarr-instance>/api/v2/public/api-docs/json`.

Response-schema coverage is **uneven** (verified against a running instance), and
it is thinnest exactly where this doc needs it. `/api/v2/status` is fully typed
(`status`, `os`, `version`, `uptime`, … — the fields behind `tdarr_server_*`),
but the stats endpoints are skeletons: `cruddb` responds with a bare
`additionalProperties: true` object (no field names), `get-pies` types only the
outer `pieStats` wrapper and leaves its contents free-form, and `get-nodes` is an
untyped object. So for the `StatisticsJSONDB.*`, `pieStats.*`, and node/worker
fields mapped above, the ground truth is a **captured live response** plus the Go
models in `internal/collector/tdarr_models.go`. Request bodies, by contrast, are
typed with field-level detail — use the spec for request shapes and endpoint
discovery.

| Endpoint | Source | Feeds |
| --- | --- | --- |
| `POST /api/v2/cruddb` (collection `StatisticsJSONDB`) | global stats document (`TdarrMetric`) | global `tdarr_*` |
| `POST /api/v2/cruddb` (collection `LibrarySettingsJSONDB`) | library list | library ids/names → `tdarr_library_info` |
| `POST /api/v2/stats/get-pies` (one call per `libraryId`) | per-library pie stats (`TdarrPieStat`) | per-library `tdarr_library_*` |
| `GET /api/v2/get-nodes` | nodes + workers | `tdarr_node_*`, `tdarr_node_worker_*` |
| `GET /api/v2/status` | server status | `tdarr_server_*` |

Source field → metric, for the behaviorally-relevant ones (full field set lives
in `internal/collector/tdarr_models.go` and the fixtures under
`internal/collector/testdata/`):

| Tdarr source field | Exporter metric | Notes |
| --- | --- | --- |
| `StatisticsJSONDB.totalTranscodeCount` | `tdarr_transcodes_completed` | global, gauge (can decrease) |
| `StatisticsJSONDB.totalHealthCheckCount` | `tdarr_health_checks_completed` | global, gauge |
| `StatisticsJSONDB.totalFileCount` | `tdarr_files` | global inventory |
| `StatisticsJSONDB.sizeDiff` (GB) | `tdarr_size_diff_bytes` | ×1024³, signed (GB inferred from observed magnitudes; Tdarr core is closed-source) |
| `StatisticsJSONDB.healthCheckScore` (string) | `tdarr_health_check_score_ratio` | Tdarr sends the score as a **string**; parsed then ÷100 |
| `StatisticsJSONDB.table0..6Count` | per-status transcode/health queue/success/failed | Tdarr's UI table buckets; success (`table2`) bundles "not required", failed (`table3`/`table6`) bundles "cancelled" |
| `StatisticsJSONDB.streamStats.{duration,bit_rate,nb_frames}` | `tdarr_stream_stats_duration_seconds` / `_bit_rate` / `_num_frames` | source keys are ffprobe field names; each split by `stat_type` = average/highest/total; **`bit_rate` stays bits/sec** (see Stream stats note) |
| `pieStats.totalTranscodeCount` | `tdarr_library_transcodes_completed_total` | per-library, counter (sticky) |
| `pieStats.totalHealthCheckCount` | `tdarr_library_health_checks_completed_total` | per-library, counter |
| `pieStats.totalFiles` | `tdarr_library_files` | per-library inventory |
| `pieStats.sizeDiff` (GB) | `tdarr_library_size_diff_bytes` | ×1024³, signed |
| `pieStats.status.transcode[]` / `.healthcheck[]` | `tdarr_library_transcodes` / `_health_checks` (`status` label) | current snapshot |
| node worker `isFlowWorker` | `flow_worker` label on `tdarr_node_worker_info` | see scanning-phase note below |
| node worker `status` | `tdarr_node_worker_status` | free-form Tdarr status string |
| node worker `file` | `worker_file` label on `tdarr_node_worker_info` | |
| node `pid` | `node_pid` label on `tdarr_node_info` | the node's pid (see worker-pid note) |
| status `uptime` / `status` / `version` / `os` | `tdarr_server_uptime_seconds` / `_status_info` / `_info` | |

## Two independent stat scopes

Tdarr exposes the same lifetime statistics through two separate accumulators,
and the exporter surfaces both:

- **Global** `tdarr_*` — from the core/general stats document (`TdarrMetric`),
  e.g. `tdarr_transcodes_completed`, `tdarr_size_diff_bytes`.
- **Per-library** `tdarr_library_*` — from each library's pie stats
  (`TdarrPieStat`), e.g. `tdarr_library_transcodes_completed_total`,
  `tdarr_library_size_diff_bytes`.

`get-pies` scope is set by the `libraryId` field in the request body: a blank
`libraryId: ""` returns an **all-libraries aggregate**, while a specific id
returns **just that one library**. The response JSON has the same shape either
way, so the scope depends on what you send, not on the payload. The exporter
iterates the library list and always sends a **specific** `libraryId` per
library, so the per-library `tdarr_library_*` series come from per-library pies
— it never uses the blank-id aggregate. (The global `tdarr_*` come from
`StatisticsJSONDB`, a different source again.)

```json
{ "data": { "libraryId": "" } }   // blank id -> all-libraries aggregate (unused by the exporter)
```

These two scopes **do not agree** for lifetime/cumulative values, and that is
expected. Current-inventory values (file counts) do agree.

Observed example (live `/metrics` vs raw Tdarr API):

| Metric | Global | Sum of per-library | Equal? |
| --- | --: | --: | :-: |
| files | 28961 | 28961 | yes |
| transcodes | 46447 | 49715 | no |
| size-diff | 225.95 | 290.48 | no |

### Why they diverge

A controlled delete/reinstall experiment (delete one episode, capture before /
after, reinstall) showed the mechanism:

- **The global total is a live-record aggregate.** It rises as files currently
  present accrue new transcode/health events, and *decrements* when a file is
  deleted — when a record is purged from Tdarr's `FileJSONDB`, its lifetime
  events leave the global total (observed: transcodes `−1`, health checks `−1`,
  size-diff went down). It is therefore an accumulator scoped to the present
  file set, not a per-scrape recompute of current-file stats.
- **The per-library pie is a sticky, append-only tally.** Deleting a file does
  **not** decrement it — across the same delete it actually went *up* (the
  reinstall's new transcode landed first). It retains deleted files' events
  forever.
- **Current-inventory fields do decrement on delete** (pie `totalFiles`, the
  per-status success counts), because those reflect the present file set.

So the per-library sum exceeds the global total by exactly the lifetime events
of files that have since been deleted: kept by the per-library pies, dropped
from the global aggregate.

## Stats cache invalidation

Per-library pie stats (`tdarr_library_*`) are expensive to collect — one
`get-pies` call per library, fanned out across `HTTP_MAX_CONCURRENCY` workers —
so a scrape reuses the previous sweep's results (`TdarrLibStatsCache`) unless an
invalidation signal says something changed. See `shouldRefetch` in
`internal/collector/tdarr.go` for the code; this section covers the *why* and
the trade-offs.

### The two-part signal

Invalidation compares two things against the cached sweep, on every scrape:

- **10 numeric totals** from the general-stats document (`StatisticsJSONDB`):
  the three top-level counts plus the seven `table0Count`–`table6Count` queue
  buckets. Cheap (~ms), but aggregate-only — it carries no per-library
  breakdown and cannot see a change that doesn't move any of the ten numbers.
- **A library-list fingerprint**: a sorted-by-id copy of the library list
  (`LibrarySettingsJSONDB`, id + name pairs), fetched fresh every scrape and
  compared with the fingerprint stored alongside the totals at the last cache
  write. This catches drift the totals miss entirely — a library rename, or a
  newly added library with zero files (nothing to transcode yet, so none of
  the ten totals move).

Either signal differing triggers a full refetch of every library's pie stats,
matching the cache's existing all-or-nothing (not per-library) invalidation —
see the `shouldRefetch` doc comment for why a per-library cache isn't viable
given the Tdarr API's shape.

### Bandwidth cost

The library-list call is cheap in latency but not in bandwidth: measured live
against a 7-library Tdarr instance, the response was 104,532 bytes — roughly
**15 KB per library**. Fetching it every scrape (rather than only on a cache
miss) adds that payload to every scrape regardless of whether anything
changed: ~102 KB/scrape for 7 libraries, which is ~590 MB/day at a 15s scrape
interval. This is a deliberate trade against staleness — see below. The payload
scales with library count (each library contributes its full settings document),
not file count, so it stays flat as libraries fill up with media.

### Residual staleness (still self-heals)

The two-part signal closes the rename/new-library gap but is not exhaustive.
The following changes move no numeric total and leave the library-list
fingerprint identical (same ids, same names), so they do not trigger a refetch
on their own:

- **Cross-library file moves that stay in the same status bucket** — e.g.
  moving a fully-transcoded file from one library's folder to another's. Both
  libraries' `totalFiles` counts change, but that only reaches the *global*
  totals (which are also unaffected here, since the file's overall status
  bucket didn't change), not the fingerprint.
- **Offsetting add+delete across libraries** — a file added to one library and
  removed from another in the same window can leave every one of the 10
  global totals net unchanged.
- **Same-bucket rescans that shift codec/resolution distributions** — e.g. a
  library rescan that reclassifies files' detected video codec without
  changing transcode/health-check status counts moves the per-library pie
  slices but none of the 10 totals.

All three self-heal on the next scrape where a totals-visible event occurs
(any transcode, health check, or file add/delete that moves one of the ten
numbers) — the staleness window is bounded by how long an instance goes
between such events, not unbounded. This is the same accepted trade-off as
before the fingerprint was added, just narrowed to fewer cases.

## Counter vs gauge typing

This divergence drives the metric typing, and the two scopes are typed
**differently on purpose**:

- **Global lifetime totals are gauges** — `tdarr_transcodes_completed`,
  `tdarr_health_checks_completed`. Because the global aggregate can *decrease*
  when files are purged, it is not monotonic. Typing it as a counter would make
  `rate()`/`increase()` treat each purge as a counter reset and emit a phantom
  spike roughly equal to the whole counter value. A gauge is the honest type.
- **Per-library lifetime totals are counters** — `tdarr_library_*_completed_total`.
  These are sticky/append-only and only ever increase, so they are genuinely
  monotonic. Use these for `rate()`/`increase()`.

When you need a rate of completed transcodes/health checks, query the
per-library `*_completed_total` counters, not the global gauges.

## Size-diff sign convention

`tdarr_size_diff_bytes` and `tdarr_library_size_diff_bytes` are **signed**:

- **Positive = space saved** (output smaller than original).
- **Negative = file grew** (output larger than original).

Verified: transcoding a net-shrunk file moves the value up. Corroborated by
plugin behavior — a pure stream-remover library trends positive, while a
library that adds tracks (e.g. an AC3 5.1 audio add) trends negative.

Note: `sizeDiff` arrives already converted to GB-scale (e.g. `225.95`), so we
never see Tdarr's raw byte count. The exporter multiplies back by 1024³ (binary
GiB), but Tdarr may use decimal GB (1000³) — a ~7% difference we cannot tell
apart from the value alone. Tdarr's core is closed-source, so this is unverified
from code; it could be settled by transcoding a file of known size and backing
out the factor. Until then, treat `size_diff_bytes` as accurate to ~±7%.

## Stream stats and the bit-rate unit exception

`tdarr_stream_stats_duration_seconds`, `tdarr_stream_stats_bit_rate`, and
`tdarr_stream_stats_num_frames` come from `StatisticsJSONDB.streamStats`. The
source object keys are ffprobe field names (`duration`, `bit_rate`,
`nb_frames`), and each carries `average`/`highest`/`total` figures that Tdarr
pre-aggregates across scanned media — exposed via the `stat_type` label.

`tdarr_stream_stats_bit_rate` is **deliberately left in bits per second**, not
bytes. Bits/sec is the conventional unit for video bitrate, so converting it
would be more confusing than convention-correct. This is an intentional
exception to the exporter's base-unit (bytes) preference — do not "fix" it to
bytes.

Tdarr is closed-source, so how it computes the average/highest/total figures is
unverifiable; the HELP text hedges with "aggregated across scanned media", and
the units are grounded in the ffprobe field names rather than a documented Tdarr
contract.

## Status normalization & unknown statuses

Tdarr returns transcode/health-check statuses as free-form strings (e.g.
"Not required", "Transcode error"). The exporter cleans these into a fixed known
enum (see `internal/collector/tdarr_enums.go`) used as the `status` label.

- Known statuses are emitted with their real value (or 0 if absent from the
  response), so each series exists on every scrape.
- Unknown statuses — ones not in the enum — are still emitted with their real
  value (no data loss), warn-logged, and counted in `tdarr_unknown_status_total`.
  Alert on that counter to catch Tdarr API drift: a new or renamed status name
  upstream surfaces there before it silently breaks a dashboard.

## Flow workers briefly classify as "Classic" during scanning

Tdarr's node API reports `isFlowWorker: false` for a flow worker during its
initial **"Scanning"** startup phase, then flips it to `true` once flow
execution begins. It is the *same* worker flipping `false → true`, not a
separate short-lived classic worker.

Consequence: for the scan frame the exporter emits
`tdarr_node_worker_info{worker_type="transcode", flow_worker="false"}`, which
matches the dashboard's Classic worker panels. A flow worker therefore appears
in the Classic section for roughly one scrape, then moves to the Flow section.
It self-corrects in one poll.

This is intentionally left as-is. During "Scanning" the worker genuinely is not
yet classified as a flow worker (Tdarr itself has not set the flag), so
`flow_worker="false"` is accurate, and Classic is a reasonable default bucket.
Filtering `worker_status="Scanning"` out of the Classic panels was considered
and rejected: the scan frame matches neither the Flow nor the Classic filter,
so the worker would vanish entirely for one scrape — worse than briefly showing
in the wrong section. Visibility beats section purity.

## Removed: worker process id

`tdarr_node_worker_pid` was removed because newer Tdarr API versions no longer
expose a worker process id. There is no replacement. Note that
`tdarr_node_info` carries a `node_pid` label, but that is the **node's** process
id, not the worker's — they are different things.
