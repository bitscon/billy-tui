# CHANGE: Save the chat debug capture to a durable location again

Date: 2026-09-11
Type: fix
ADR: none

## What changed

- `export.go`: `saveChat` (ctrl+s) writes to `~/.billy/debug` again — back through
  the `billyDir` helper, so `BILLY_DEBUG_DIR` still overrides it. The
  `defaultDebugDir = "/tmp/billy/chat-log"` constant is gone. Added the
  `debugDirMode` (0755) / `debugFileMode` (0644) constants and the `writeCapture`
  helper: the directory mode is re-asserted on every save (MkdirAll applies the
  umask, and an existing directory keeps its original mode) and each capture's
  mode is set explicitly after the write (`os.WriteFile` only applies a mode when
  it *creates* the file, so the rewritten "latest" pointer kept whatever mode it
  was first created with). `exportChat` (`:export`) is unchanged.
- `export_test.go`: restored `TestSaveChatWritesUnderBillyDebug` (default resolves
  to `~/.billy/debug`, archive alongside, `$HOME` root clean); added
  `TestSaveChatHonorsDebugDirOverride` (override honored verbatim),
  `TestSaveChatDefaultIsNotATempDir` (the durability regression guard that
  replaces `TestDefaultDebugDirIsOutsideHome` — the default must be rooted in
  `$HOME`, never the old temp path) and `TestSaveChatCapturesStayAgentReadable`
  (a seeded 0700 directory holding a 0600 capture must come out 0755/0644).
- `PROJECT_STATUS.md`: the ctrl+s location in the header and in the Step 75 line
  now states `~/.billy/debug/` and why it is reachable (modes, not location).

## Why

`a2a0605` (2026-08-11) moved the capture to `/tmp/billy/chat-log` to solve a real
problem — the operator saves the capture, and the agent account that runs the
"debug billy" investigation reads it at a different uid — but it solved it with
the wrong property. It read `/tmp` as "world-readable" and treated "a reboot
clearing it is fine for a transient debug log" as acceptable. It is not: the
capture is the sole evidence for a root-cause investigation, and it is often
saved hours or days before it is read. The barn rebooted on 2026-09-08 and every
capture taken since 2026-08-11 was destroyed. The surviving captures in
`~/.billy/debug` (2026-06-26 → 2026-08-06) are `0644` inside a `0755` directory
and the agent account can already open them — the durable location satisfied the
readability goal all along. Cross-account reach is a mode property, not a
location property, so it is now stated as one in the code.

Operator ruling, 2026-09-11: temp directories do not hold critical files.

Secondary: `~/.billy/debug/billy-chat-debug-latest.md` is the path the architect
role contract (`billy-runtime/agents/architect/AGENTS.md` §12) opens on "debug
billy". That contract was never updated for the temp path, so since 2026-08-11
the documented location and the written location have disagreed.

## Risk

LOW — one client-side path default plus explicit modes on files billy-tui already
wrote. No service, systemd or runtime change; `:export` untouched; the
`BILLY_DEBUG_DIR` override is preserved, so any durable shared directory can be
selected without a rebuild.

Two residuals, both stated rather than silently handled:

1. **Reach depends on the launching account's home being traversable.** Under
   `billyb` (home `0755`) the agent account reads the capture today — verified.
   Under `billybs` (home `0750`, group `billybs`) it cannot, no matter what mode
   the capture carries, because the home root blocks traversal first. Widening a
   home directory is the operator's call, not this client's; the alternative that
   needs no such change is `BILLY_DEBUG_DIR` pointed at a durable shared path.
2. **The chmod is best effort on a directory or file owned by another account.**
   A capture it cannot chmod is accepted only if the mode already carries group
   or other read (all three barn principals share group `dev`); otherwise the
   save reports the failure instead of returning a path nobody else can open.

## Verified

- [x] `go build ./...` passes
- [x] `go vet ./...` passes
- [x] `go test ./...` passes
- [x] Behavior matches intent (default resolves to `~/.billy/debug`; override
      honored; directory 0755 and captures 0644 even over a seeded 0700/0600)
- [x] No unrelated files changed
