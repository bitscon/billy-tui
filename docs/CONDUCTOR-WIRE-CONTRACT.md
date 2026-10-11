# Conductor Wire Contract — billy-tui ↔ billy-runtime (v1 + v2 + v3)

Status: **v1 (§1–§7) pinned 2026-09-01 · v2 amendment (§8–§12) pinned 2026-09-02 —
both proposed to the runtime architect · v3 amendment (§13–§17) pinned 2026-10-10,
documenting a runtime surface that is ALREADY SEALED on the runtime side** (runtime side = Modernization card 1 for
v1; the v2 surfaces are the floor-table and mode-set halves of Modernization 5/6,
governed single-lane in billy-runtime).
Date: 2026-09-01 (v1) · 2026-09-02 (v2) · 2026-10-10 (v3) · Home: the client repo,
so all client lanes build against ONE shape.

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

---

# Amendment v3 — the pending question and its picker (pinned 2026-10-10)

## 13. v3 scope and rules

One surface: **the structured question a reply can carry**, so the client can offer
the operator a choice instead of making them type a number into prose. The runtime
half is already live-sealed (billy-runtime I262-C3, ADR-0258); this section pins the
shape the client decodes and the rules the client answers under.

Every §1 rule binds v3 unchanged — additive only, absence = legacy, names are the
runtime's own. Two rules are added:

6. **A malformed question is "nothing pending", never an error.** The field is
   additive and operator-facing on both sides: the runtime drops a question it cannot
   guard rather than failing the reply, and the client does the same in reverse — any
   shape it does not admit (a non-object, a null, a missing id, fewer than two
   options, a blank option) leaves the reply intact and simply arms no picker. A
   question must never cost the operator the answer it came attached to.
7. **The answer channel is unchanged chat text, on the same session.** There is no
   answer endpoint. The operator's next message on the SAME session is matched
   against the open question by the runtime (§14). The client sends that message
   through the one send path a typed message uses, and never sends an answer on a
   session other than the one the question arrived on.

## 14. Surface 6 — the pending question · `POST /ask` and `GET /ask/stream`

`POST /ask` carries `pending_question`: **always present, null when nothing is
pending** (the runtime builds it with no `exclude_none`).

```json
{
  "message": "Reading your Kanboard board means sending it to the cloud brain. …\n\n1. Allow once\n2. Allow for this conversation\n3. Always allow\n4. Don't allow\n\nReply with the number or the option itself.",
  "session_id": "tui-…",
  "pending_question": {
    "question_id": "consentq-7f3c91a20b4e",
    "question": "Reading your Kanboard board means sending it to the cloud brain. What should I do?",
    "options": ["Allow once", "Allow for this conversation", "Always allow", "Don't allow"],
    "allow_free_text": false,
    "kind": "cloud_read_consent",
    "source": "board"
  }
}
```

### What each field means to the client

| Field | Meaning to the client |
|---|---|
| `question_id` | The question's identity. Required and non-blank; a question without one is dropped by the runtime and must be treated as nothing pending. The client does not send it — the answer is matched by session and recency, not by id. |
| `question` | The question **in the brain's own words**, already through the runtime's egress guards. Shown **verbatim**; never paraphrased, truncated or re-worded. |
| `options` | The choice labels, **in order**. Their 1-based position in THIS list is the number the operator's answer may carry. The runtime admits two to four; the client accepts two or more and never reorders, filters or renumbers them. |
| `allow_free_text` | `true` = the operator's own words are accepted **on the turn immediately after** the question, and only that turn. `false` = only a number or an exact option label counts. Either way the picker is offered; it is modal while open, and Esc hands the keyboard back to the input box, which still answers. |
| `kind` | Which question this is, and therefore **what answers it** (§15). Absent reads as `"question"`. |
| `tool`, `target` | `permission` kind only: the tool being asked about and its scrubbed target. Present so a client can SHOW what is being asked about without parsing the sentence. Absent on every other kind. |
| `source` | `cloud_read_consent` and `cloud_read_permissions` only: which of the operator's sources the question is about — one of `board`, `annex`, `files`, `memory`. Never anything read FROM the source. Absent on every other kind. |

### The kinds, and what answers each

| `kind` | Armed by | A number answers it | Notes |
|---|---|---|---|
| `question` | the brain's own `ask_operator` tool | **yes** | the ordinary choice |
| `lift_read_only` | the brain's `ask_operator` tool | answers, but **never lifts** | the hold comes off only on the option typed out in full |
| `permission` | the loop's own chokepoint — never the brain | **no** | a number matches nothing; only the exact option label counts |
| `cloud_read_consent` | the cloud-read chokepoint — never the brain | **yes** | one held cloud-bound read, per source (ADR-0258) |
| `cloud_read_permissions` | the `/permissions` handler — never the brain | **yes** | the two-step settings flow: pick a source, then pick its setting |

`permission` and `lift_read_only` are the two kinds the runtime deliberately fences
to a **typed** answer, for the same reason in both cases: a bare number left over
from an older question on the operator's screen must never grant a privilege they
did not read. The rendered message says so in words. The client honours the fence by
sending the **option label verbatim** for those two kinds and the **number** for
every other kind (§15) — never a number the runtime would discard.

### Where it rides on the stream

`GET /ask/stream` does **not** put the question on the terminal frame. It rides its
own frame, **after** the message frames and **before** the terminal one, with an
empty chunk and `done: false`:

```
data: {"chunk": "…reply text…", "done": false}
data: {"chunk": "", "done": false, "pending_question": { … }}
data: {"chunk": "", "done": true, "usage": { … }}
```

The client therefore accepts `pending_question` on **any** frame and carries the
first non-null one through to the turn's completion — the same rule §3 sets for
`brain` and `approval`. A frame carrying only a question adds nothing to the reply
text (its chunk is empty), so content parity holds.

### Lifetime

- **One at a time.** The runtime holds exactly one pending question per session.
  Arming a new one silently replaces any held one, so the client's picker is
  replaced by every newer reply and a reply carrying no question clears it.
- **In memory only.** The question dies with the runtime process; a restart leaves
  nothing to answer. The client must never persist one or re-offer one across a
  session change.
- **It expires by turn count.** An answer typed after the question lapsed changes
  nothing and is reported as lapsed. The client does not track expiry — it sends the
  answer and renders what comes back.

## 15. Rules the client answers under

1. **What is sent.** Choosing option *i* (1-based) sends:
   - the **decimal number** `i` — for every kind except the two fenced ones;
   - the **option label, verbatim** — for `permission` and `lift_read_only`.

   Two refusals, both on fenced kinds only:
   - a label that is itself **all digits** is ambiguous on the wire (the runtime
     would read it as a position): nothing is sent, and the label goes to the
     input box for the operator to send;
   - a label carrying **control characters** is refused: nothing is sent and the
     PICKER echoes it nowhere — not the transcript, not the input box, not the
     input history. A fenced answer is the label byte-exact, so choosing it would
     otherwise put those bytes through display paths that do not sanitise, and a
     `lift_read_only` label is written by the brain, which neither the
     `ask_operator` validator nor the runtime's egress guards screen for control
     characters. **Bounded claim:** this covers what the picker sends and echoes.
     The reply TEXT the runtime rendered already carries the same bytes, and the
     client's pre-existing transcript, live-stream and export paths are unchanged
     by this surface — sanitising those is a registered follow-up, not a claim
     made here.
2. **Same session, always.** The session recorded with a question is the session
   **the turn ran on**, frozen when the turn was submitted — not whatever session
   the client happens to hold when the reply lands. (`:session new` is reachable
   while Billy is still working, so stamping from the live client would re-stamp an
   old question as belonging to the new session, and the blocking `/ask` fallback
   could even send the turn itself on a session it had not started on. The
   session is a request PARAMETER for that reason, never read off the client when
   the request runs.) If the session has since changed,
   the picker sends nothing and says why — the runtime would match the answer
   against nothing.
3. **Esc sends nothing.** Closing the picker is not an answer. The picker is
   modal while it is open — like every other picker in this client — and Esc hands
   the keyboard back to the input box. The question stays answerable either way,
   because the reply text still carries the numbered list.
4. **One picker.** A newer reply replaces the open picker outright; a reply with no
   question closes it. Any submission — typed, queued or picked — closes it, so one
   choice can never be sent twice.
5. **Verbatim.** The question and every option label are rendered exactly as
   received, in the order received, under the numbers the runtime matches. The
   client adds no label of its own to a choice, never re-words one, and never
   filters, reorders or renumbers the list — dropping even a blank entry would
   silently renumber every option after it.

   Two bounded exceptions, both **display-only** — what a choice SENDS is always
   the bytes the runtime sent:
   - **Control characters are made inert.** The runtime's egress guards neutralise
     tool markup and persona claims, not C0/C1 controls; a question carrying CSI
     sequences could otherwise repaint the screen the operator is consenting on,
     a newline inside a label could forge a second numbered line under a real
     one, and a bidirectional override could make a label read as something other
     than the bytes a choice sends. None of these is text the operator can read,
     so replacing them with a space is not a re-wording.
   - **The overlay is bounded to the terminal.** A very long question is clipped
     with an ellipsis, a long option list is windowed with a "N more" row, and
     every line is bounded by display cells — not rune count, since a
     wide-character label is twice as wide as its rune count. The row budget is
     computed from what will actually be drawn, so the title, the key line and
     the selected option can never be pushed off the screen.
6. **What the reply said AROUND the question is shown with it.** The runtime
   deliberately keeps load-bearing sentences out of the question text — the
   read-only lift warning, and the whole settings listing that is the "view" half
   of `/permissions` — so a picker that covered the reply would hide exactly what
   the operator is meant to read before choosing. The client carries that prose
   with the question and draws it above it, and gives it rows ahead of the
   question text when the terminal is short. It is obtained by a deterministic
   filter on strings the client already holds verbatim (drop the question line,
   drop `"<n>. <label>"` for the option actually at position n, collapse blank
   runs) — never by reading the prose, and a question that is itself several
   lines is dropped as one block so it cannot appear twice. Rows go to the
   question first, up to what it needs, and the prose is clipped with a visible
   ellipsis rather than dropped. Any notice the picker itself raises is drawn
   inside the overlay too, since the overlay covers the status bar — and it is
   the picker's OWN notice, never the client's global status line, so a leftover
   sentence about an export or a model switch is never drawn on the screen a
   choice is sent from. Whether the picker can be drawn at all never depends on
   whether a notice is showing.
7. **A choice the operator cannot see is not a choice.** The picker is offered
   only when it can be drawn WHOLE. One predicate decides both whether the
   overlay renders and whether it owns the keyboard, so the screen and the keys
   can never disagree; on a terminal too small the client degrades to exactly
   today's behaviour — the numbered list in the reply text, answered from the
   input box. For the same reason a number key chooses only an option that is on
   screen: with a windowed list the key line stops offering the numbers at all,
   and a number naming a scrolled-away option does nothing — while a number
   naming a row that IS printed still chooses that row.
8. **`:permissions`** sends the literal message `/permissions` on the current
   session, and nothing else: the runtime's own recognizer requires it as the whole
   message. The two-step flow that follows (pick a source, then pick its setting) is
   driven entirely by the `pending_question` of each reply — the client holds no
   knowledge of sources or settings.

## 16. Pinned Go structs (v3 — the client decodes exactly this)

```go
// PendingQuestion mirrors the runtime's PendingQuestion model (v3 §14).
// Nil = nothing pending: absent, null, or a shape this contract does not admit.
type PendingQuestion struct {
    QuestionID    string   `json:"question_id"`
    Question      string   `json:"question"`
    Options       []string `json:"options"`
    AllowFreeText bool     `json:"allow_free_text"`
    Kind          string   `json:"kind"`   // "" reads as "question"
    Tool          string   `json:"tool"`   // permission kind only
    Target        string   `json:"target"` // permission kind only
    Source        string   `json:"source"` // consent kinds only
}

// The /ask decode and the stream-frame decode each gain the field as RAW json,
// decoded by a tolerant helper — a malformed question must not fail the reply
// (v3 §13 rule 6):
//     PendingQuestion json.RawMessage `json:"pending_question"`
```

## 17. Degradation matrix (v3)

| Runtime / reply | `pending_question` | Client behavior |
|---|---|---|
| legacy (predates the field) | absent | exactly today's behavior; no picker, nothing sent |
| new, ordinary turn | `null` | nothing pending; any open picker closes |
| new, question armed, kind takes a number | object, 2–4 options | picker opens; a choice sends the option's number on the same session |
| new, question armed, `permission` / `lift_read_only` | object | picker opens; a choice sends the option label typed out verbatim — never a number |
| new, question armed, fenced kind with an all-digit label | object | picker sends nothing; the label goes to the input box for the operator to send |
| question armed, fenced kind with a control character in the label | object | picker sends nothing and echoes nothing; it says the option cannot be sent safely |
| question armed, reply carried prose around the question | object | the prose is drawn above the question, clipped with an ellipsis before the question is |
| malformed (non-object, null id, <2 options, blank option) | object | nothing pending; the reply text is delivered untouched and no picker opens |
| question armed, session changed since | object | picker sends nothing and says the session changed |
| question armed, turn still in flight | object | picker sends nothing and says Billy is still working (defensive: a submission closes the picker, so the client's own flow does not reach this) |
| question armed, terminal too small to draw the picker whole | object | overlay is not drawn, picker owns no keys, nothing sent; the numbered list in the reply text still answers |
| question armed, more options than fit on screen | object | list windowed with a "N more" row; the key line stops advertising the numbers, a number naming a scrolled-away option does nothing, and a number naming a visible row still chooses it |
| question armed, text or label carries control characters | object | the controls are made inert on screen; a choice still sends the bytes as received |
| new runtime, old client | ignored | today's behavior (unknown fields dropped) |
