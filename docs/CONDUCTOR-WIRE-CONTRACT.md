# Conductor Wire Contract — billy-tui ↔ billy-runtime (v1 + v2)

Status: **v1 (§1–§7) pinned 2026-09-01 · v2 amendment (§8–§12) pinned 2026-09-02 —
both proposed to the runtime architect** (runtime side = Modernization card 1 for
v1; the v2 surfaces are the floor-table and mode-set halves of Modernization 5/6,
governed single-lane in billy-runtime).
Date: 2026-09-01 (v1) · 2026-09-02 (v2) · Home: the client repo, so all client
lanes build against ONE shape.

Billy's conductor now picks a brain per turn (auto-routing is live), but nothing it
decides is on the API. This contract adds the three missing surfaces. The client
lanes build against the Go structs in §5 **now**, with fixtures; the runtime lands
the producing side independently.

---

## 1. Rules that bind both sides

1. **Additive only.** Every field below is new and optional on the wire. An older
   runtime that omits them leaves the client behaving exactly as today; an older
   client ignores them (Go `json.Unmarshal` drops unknown fields — already true).
2. **Absence semantics.**
   - `routing_mode` missing → legacy runtime → client shows today's pinned display.
   - `brain` missing on a reply → no per-turn routing decision was recorded for that
     turn (pinned mode, or a deterministic-lane reply that used no LLM). The client
     must NOT invent one.
   - `approval` missing → nothing awaits approval.
3. **`lane` is not routing.** `lane` on `GET /api/v1/llm/config` selects the
   tool-calling grammar (ADR-0213). The client continues to ignore it and must never
   render it as routing.
4. **Names are the runtime's own.** The `brain` keys below are verbatim
   `BrainSelection.to_dict()` keys (`billy_runtime/conductor/brain_selector.py`) —
   the runtime can serialize the `last_brain_selection` record it already produces,
   as a subset or in full (extra keys are fine; the client ignores them).

## 2. Surface 1 — routing mode · `GET /api/v1/llm/config`

Add one field, mirroring `read_routing_mode()`:

```json
{
  "provider": "ollama",
  "model": "qwen3.5:9b",
  "base_url": "http://llm.workshop.home:11434/v1",
  "configured": true,
  "lane": "native",
  "routing_mode": "auto"
}
```

`routing_mode`: `"auto"` | `"pinned"`. Under `"auto"` the provider/model pair is the
**pin/home config**, not necessarily what answers a given turn — the client stops
presenting it as the fixed answerer (Modernization 2).

## 3. Surface 2 — per-answer brain report · `POST /ask` and `GET /ask/stream`

`POST /ask` response gains an optional `brain` object:

```json
{
  "message": "…",
  "foreman_mode": false,
  "session_id": "tui-…",
  "brain": {
    "placement": "home",
    "provider": "ollama",
    "model_id": "qwen3.5:9b",
    "reason": "routine turn; floor small; resolved at home",
    "escalated": false,
    "pinned_home": false,
    "degraded_for_privacy": false,
    "failsafe": false,
    "effective_tier": "small"
  }
}
```

`GET /ask/stream` frames stay `data: {"chunk": <str>, "done": <bool>}`. The final
frame (`done: true`) **should** additionally carry `brain` (and `approval`, §4). The
client accepts these fields on any frame; first non-null wins.

When to attach `brain`:
- **Required** on every auto-routed LLM turn (the record already exists as
  `last_brain_selection`).
- Optional on pinned-mode turns.
- Omit when the turn made no LLM routing decision.

Required keys: `placement` (`"home"`|`"cloud"`), `provider`, `model_id`, `reason`
(human-readable, deterministic), `escalated`, `pinned_home`,
`degraded_for_privacy` (booleans). Optional: `failsafe` (present on the fail-safe
synthetic trace; absent reads as false), `effective_tier`.

Client badge derivation (informative — so both sides know what renders):
`placement` → **local** / **cloud**; short label by flag precedence
`failsafe` → "fail-safe (kept home)" · `degraded_for_privacy`/`pinned_home` →
"kept home (private)" · `escalated` → "escalated" · else "routine". The full
`reason` string is preserved for debug captures (Modernization 7), not the badge.

## 4. Surface 3 — approval flag · `POST /ask` and the stream's final frame

Optional `approval` object on a reply that awaits the operator:

```json
{
  "message": "I want to restart nginx. Reply yes to run it.",
  "approval": {
    "pending": true,
    "id": "appr-…",
    "summary": "restart nginx",
    "command": "systemctl restart nginx",
    "target": "barn"
  }
}
```

`pending` + `id` + `summary` required; `command` and `target` optional but wanted
(the prompt should show *what will run* and *against which server*). **The reply
path is unchanged:** the operator's next plain `"yes"` / `"no"` over `/ask`
resolves it, exactly as the chat-text flow works today. This object exists so the
client can render an unmissable prompt and guarantee the answer is never dropped —
it is not a second approval channel.

## 5. Pinned Go structs (client side — lanes build against these NOW)

```go
// BrainReport mirrors the subset of the runtime's BrainSelection trace the
// client renders. Nil = no routing decision reported for this reply.
type BrainReport struct {
    Placement          string `json:"placement"` // "home" | "cloud"
    Provider           string `json:"provider"`
    ModelID            string `json:"model_id"`
    Reason             string `json:"reason"`
    Escalated          bool   `json:"escalated"`
    PinnedHome         bool   `json:"pinned_home"`
    DegradedForPrivacy bool   `json:"degraded_for_privacy"`
    Failsafe           bool   `json:"failsafe"`
    EffectiveTier      string `json:"effective_tier"`
}

// ApprovalRequest marks a reply that waits on the operator's yes/no.
// Nil = nothing pending.
type ApprovalRequest struct {
    Pending bool   `json:"pending"`
    ID      string `json:"id"`
    Summary string `json:"summary"`
    Command string `json:"command"`
    Target  string `json:"target"`
}

// LLMConfig gains: ("" = legacy runtime → behave as today)
//     RoutingMode string `json:"routing_mode"`

// The /ask decode and the stream-frame decode each gain:
//     Brain    *BrainReport     `json:"brain"`
//     Approval *ApprovalRequest `json:"approval"`
```

## 6. Degradation matrix

| Runtime | `routing_mode` | `brain` | Client behavior |
|---|---|---|---|
| legacy (today) | absent | absent | exactly today's display; no badge, no approval affordance |
| new, pinned | `"pinned"` | optional | pinned model shown as such; badge when present |
| new, auto | `"auto"` | on routed turns | mode shown honestly; per-reply badge; no fixed-model claim |
| new runtime, old client | ignored | ignored | today's behavior (unknown fields dropped) |

## 7. Second wave — flagged now, contracted later (v2)

Not in v1, listed so the runtime architect sees the whole program:
- **Modernization 5** needs a read/write API for the role→tier floor table
  (today file-only: `.billy/brain_tier_config.json`).
- **Modernization 6** needs a mode-set surface (`routing_mode` auto|pinned) —
  today file-only. The client's `:model` picker will warn under auto until then
  (it writes the pin, which auto may override next turn).

*(Both are now contracted below — the v2 amendment supersedes these flags.)*

---

# Amendment v2 — floor table + mode set (pinned 2026-09-02)

## 8. v2 scope and rules

Two surfaces, completing the §7 flags: **set the routing mode from the client**
(Modernization 6) and **read/write the role→brain floor table** (Modernization 5).
Every §1 rule binds v2 unchanged — additive only, absence = legacy, `lane` is not
routing, names are the runtime's own. One rule is added:

5. **Capability gate — the client never sends a v2 write shape blind.**
   - Surface 4 (mode set): the client includes `routing_mode` in a config POST —
     and sends mode-only POSTs at all — **only after** the latest
     `GET /api/v1/llm/config` carried a non-empty `routing_mode` (§2). A legacy
     runtime therefore never receives a request shape it predates (its POST
     validation, required fields, and unknown-field handling are never exercised).
   - Surface 5 (floor table): self-gating — the client GETs first; a 404 marks the
     whole surface absent and the client never POSTs to it.

## 9. Surface 4 — set routing mode · `POST /api/v1/llm/config`

The existing config-set endpoint gains one optional field, mirroring
`set_routing_mode()` (`billy_runtime/model_config.py`, Store A):

```json
{ "routing_mode": "pinned" }
```

- `routing_mode`: `"auto"` | `"pinned"`. Optional; when present, `provider`
  becomes optional too:
  - **Mode-only request** (the toggle, shown above): flips only the mode. The
    provider / model / base_url pin is untouched.
  - **Combined request** (`provider` + optional `model`/`base_url` +
    `routing_mode`): the model switch behaves exactly as today (§ existing
    endpoint semantics, catalog validation included), then the mode is applied.
  - A request with **neither** `provider` nor `routing_mode` is a 400.
- **Validation order (pinned so failures are atomic in practice):** an invalid
  `routing_mode` is refused — 400, code `invalid_routing_mode` — **before any
  side effect**; the mode write itself happens **last**, so a model-switch
  rejection (unknown model, provider error) leaves the mode unapplied as well.
- **Response:** the unchanged `LLMConfigResponse` **including `routing_mode`
  (§2)** — the response is the proof the flip took effect. The client renders
  from it and keeps the polled GET as the standing authority (§2 rule: no stale
  mode may survive a runtime downgrade).
- **Effect:** live on the next turn — Store A is hot-read per turn; no restart.

## 10. Surface 5 — role→brain floor table · `/api/v1/llm/brain-floors`

The operator's floor table (`.billy/brain_tier_config.json`, read through
`billy_runtime/conductor/brain_tier_config.py`) goes on the API. Two operations.

### `GET /api/v1/llm/brain-floors`

```json
{
  "tiers": ["small", "medium", "large"],
  "default_floor": "small",
  "roles": {
    "chat": "small",
    "coder": "large",
    "companion": "small",
    "sysadmin": "medium"
  }
}
```

- `tiers`: the runtime's fixed **ordered** vocabulary, smallest → largest,
  verbatim (`brain_tier_config.TIERS`). The client renders and offers exactly
  these — it hardcodes no tier names, so a future tier appears with no client
  change.
- `roles`: the **effective** table — `read_role_tiers()`: the file when wholly
  valid, the built-in safe defaults otherwise. May be `{}` (a valid empty table);
  every role then floors at `default_floor`.
- `default_floor`: the floor for any role **absent** from `roles`
  (`DEFAULT_FLOOR`, today `"small"`).
- Extra keys are fine (the client drops unknowns, §1).
- **404 = legacy runtime**: the surface is absent; the client's floor screen
  shows "unavailable" and never writes (§8 gate).

### `POST /api/v1/llm/brain-floors/{role}`

```json
{ "tier": "medium" }
```

Sets ONE role's floor. Response: **the same shape as the GET, post-write** — the
client re-renders the table from it, which is the took-effect proof.

- **Merge rule (load-bearing):** the write persists the **effective table (what
  the GET returns) with the one role changed** — never the raw file. With no
  file on disk yet, a naive read-file→set→write would materialize only the one
  role and silently drop every other role's default floor to `default_floor`;
  the merge rule forbids exactly that.
- **Upsert:** a `{role}` not present in the table is added (any valid role
  string; the client's screen only offers roles the GET listed).
- **Role validity:** non-empty, no leading/trailing whitespace — the runtime
  refuses padded roles loudly, never trims (a floor stored under `" coder "`
  would silently never match). The client path-escapes `{role}`.
- **Atomic + hot-reload:** atomic file replace; the write busts the runtime's
  read memo, so the new floor is live on the **next turn** — no restart.
- **Errors:** 400 `unknown_tier` (value not in `tiers`; case-sensitive, never
  coerced; message names the valid set) · 400 `invalid_role` (empty/padded) ·
  404 = legacy runtime.

### Auth plane (both operations)

Same operator plane as `POST /api/v1/llm/config`: the Unix socket's
transport-derived principal is the authorization. **No `X-Billy-Internal-Admin`
header** — the client sends none; gating these routes on it would lock the
client out.

## 11. Pinned Go structs (v2 — client lanes build against these NOW)

```go
// BrainFloors mirrors GET /api/v1/llm/brain-floors and the POST response.
// Nil (the GET 404'd) = the runtime has no floor surface: the screen renders
// read-only "unavailable" and the client never POSTs (§8 gate).
type BrainFloors struct {
    Tiers        []string          `json:"tiers"`         // ordered smallest → largest
    DefaultFloor string            `json:"default_floor"` // floor for any role absent from Roles
    Roles        map[string]string `json:"roles"`         // role → floor tier; may be empty
}

// SetBrainFloorRequest is the POST /api/v1/llm/brain-floors/{role} body.
type SetBrainFloorRequest struct {
    Tier string `json:"tier"`
}

// The POST /api/v1/llm/config payload gains (sent ONLY when the latest config
// GET carried routing_mode — the §8 capability gate):
//     "routing_mode": "auto" | "pinned"   // optional; provider may then be omitted
```

## 12. Degradation matrix (v2)

| Runtime | config GET | brain-floors GET | Client behavior |
|---|---|---|---|
| legacy (today) | no `routing_mode` | 404 | `:routing` reports the runtime doesn't support it and sends nothing; floor screen "unavailable"; everything else exactly today |
| new | `routing_mode` present | 200 | `:routing` toggles live (display still follows the poll); floor screen lists/edits live |
| new runtime, old client | ignored | never called | today's behavior (unknown fields dropped) |
