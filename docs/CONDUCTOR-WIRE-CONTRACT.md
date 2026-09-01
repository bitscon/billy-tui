# Conductor Wire Contract — billy-tui ↔ billy-runtime (v1)

Status: **v1 — pinned for the client build; proposed to the runtime architect**
(runtime side = Modernization card 1, governed single-lane in billy-runtime).
Date: 2026-09-01 · Home: the client repo, so all client lanes build against ONE shape.

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
