# CHANGE: Add a Unix-domain-socket transport to the billy-runtime client

Date: 2026-08-10
Type: feat
ADR: none — implements Modular Identity epic P10 (client half). The transport
decisions were already ratified for billy-runtime (ADR-0240 D6/D9/D12); the
runtime-side P9/I232 landed with "no new ADR" for the same reason. Selecting the
transport by address scheme is a config-shaped change within the existing client
abstraction, not a new architectural decision (AGENT_OS §7: ADR not required for
config changes).

## What changed

- client.go: added `unixSocketPath(addr)` — recognises `unix:///abs/path` (URL
  form) and the `unix:/abs/path` shorthand, returns the socket path; anything else
  (http://, https://, bare host:port) is not a socket.
- client.go: added `resolveTransport(addr)` — for TCP it returns `(addr, nil)` so
  both HTTP clients use net/http's DefaultTransport (byte-identical to prior
  behaviour); for `unix://` it returns the fixed dummy base URL `http://billy` plus
  an `*http.Transport` whose `DialContext` dials the socket via
  `net.Dial("unix", socketPath)`.
- client.go: `newBillyClient(addr)` now resolves the transport and applies it to
  BOTH `http` (plain GET/POST) and `streamHTTP` (SSE `/ask/stream`), so the whole
  session — /health, /ask, /ask/stream, /api/v1/llm/config and the sidebar GETs —
  rides whichever transport the address selects. Added the `net` import.
- client_test.go: `TestUnixSocketPath` (parser table), `TestResolveTransport`
  (TCP passthrough with nil transport vs unix selection), and
  `TestUnixSocketTransportEndToEnd` (a real AF_UNIX `http.Server` driving GET /
  POST / SSE through the built client). Added `TestLiveSocketSession`, an opt-in
  check gated on `BILLY_LIVE_SOCKET` that exercises the real running billy-runtime
  socket (/health + GET /api/v1/llm/config, no LLM turn) — skipped in CI.
- README.md: documented the two address forms, the SSH-forwarded-socket remote
  mode, and an "Identity by transport" table (which connection yields which
  principal).
- PROJECT_STATUS.md: recorded the P10 transport capability.

main.go is unchanged — the existing `--addr` / `BILLY_ADDR` surface already flows
the address through; the scheme is interpreted in the client.

## Why

Modular Identity P10. billy-runtime resolves the caller's principal from the Unix
socket's kernel peer credentials (SO_PEERCRED). Dialing the socket as the
operator's OS user (uid 1000) makes billy-runtime recognise the operator with no
text challenge — the old "just tell Billy you are billybs" text-spoofing bypass
(class I219) is dead on the socket path. The TUI is the operator's daily driver,
so it must be able to reach that socket. TCP stays the default and fallback so
nothing regresses; the operator flips his default to the socket once he's proven
it. This unblocks P8 (retiring the runtime's text self-ID detector).

The transport-derived model means the client does nothing identity-specific: it
injects no identity header and makes no principal claim; the connecting uid alone
decides who Billy sees. Verified: over the live socket this session (uid 1002,
the agent) was resolved as the agent and, when it claimed to be the operator, was
challenged — the runtime side (uid 1000 -> operator, no challenge) was live-proved
in P9 on 2026-08-10.

## Risk

LOW — The TCP path is byte-identical (`resolveTransport` returns `(addr, nil)` ->
DefaultTransport), pinned by `TestResolveTransport` and mutation-proven in the
adversarial round; every pre-existing test still drives the same request code over
TCP. The socket path is additive and opt-in via `--addr unix://`, and it fails
closed — a bad/missing socket errors, there is no silent downgrade to an
unverified TCP session. Blast radius is confined to client transport selection; no
endpoint, payload, SSE, or UI behaviour changed.

Adversarial round (AGENT_OS §21.4): 1 wave, 3 independent lenses (correctness /
behaviour-parity / resource-safety; security / identity-contract conformance;
test-adequacy / reproduction quality), each with a per-lens refutation pass by
reproduction. Hypotheses raised and refuted by reproduction: shared-transport
timeout leak into SSE, scheme misrouting, https downgrade, client-side identity
assertion, silent TCP fallback, socket-path-length flake, and "test theatre"
(mutation testing proved the e2e test fails if the unix transport is broken).
Confirmed findings: 0. Refuted: all. The round converged clean on wave one; with
no remediations there was no fresh unreviewed code to re-review. Registered
follow-up (out of P10 scope): the live socket is mode 0660 group `dev`, so any
`dev` member can open a session authenticated as their own uid — intended under
the epic's reachability-not-ownership model (ADR-0241) and the SO_PEERCRED
per-uid principal separation; noted for the operator, not a client defect.

## Verified

- [x] `go build ./...` passes
- [x] `go vet ./...` passes
- [x] `go test ./...` passes (hermetic suite green; `TestLiveSocketSession` skips)
- [x] `gofmt -l .` clean
- [x] Live: `BILLY_LIVE_SOCKET=/home/billyb/.billy/sock/billy.sock go test -run TestLiveSocketSession -v` PASS (provider=openrouter, model=anthropic/claude-haiku-4-5 read over the socket)
- [x] Live: identity over the socket is transport-derived — uid 1002 (agent) is not granted operator identity; runtime challenges a false operator claim
- [x] Behavior matches intent — TCP default unchanged; `unix://` selects the socket for plain + streaming clients
- [x] No unrelated files changed
