# Session Inspector & Event Model

The transcript is the single append-only event log that backs every
persistent session behavior in `gencode`. It serves two readers:

1. **Restore** — replay the log to reconstruct messages, system prompt,
   tools, and projected metadata. Powers `gen --continue`, `gen --resume`,
   forks, compact restore, and subagent persistence.
2. **Inspect** — replay the log up to any record and answer the question
   "what did the model see at this point, and why?" Powers `gen
   inspector`, the local web tool used for context debugging.

The file format IS the wire format. There is no separate save schema, no
projected database, no parallel snapshot. Anything a reader needs is in
the JSONL — anything that can be derived from the JSONL is not stored.

## Storage Layout

```text
~/.gen/projects/<encoded-cwd>/
├── transcripts/
│   └── <session-id>.jsonl       # append-only event log
├── transcripts-index.json       # projected list view (title, branch, ...)
└── blobs/
    ├── summary/                 # compact summaries
    └── tool-result/<session-id>/<tool-call-id>
                                 # spilled large tool output
```

- `<session-id>.jsonl` is authoritative.
- `transcripts-index.json` is a cache rebuilt from the JSONL files; deleting
  it is safe and triggers a rescan on next list.
- `blobs/tool-result/` holds tool outputs that exceeded inline size; the
  corresponding message carries a marker
  `[Full output persisted to blobs/tool-result/<session-id>/<tool-call-id>]`
  which is rehydrated at load.

## First Principles

A reader holding only one JSONL must be able to answer:

- which messages are in the active chain;
- which system sections and tools were active at any record;
- which exact context was sent at any model call, and which prior records
  caused it;
- what came back from the model.

These four rules follow:

1. **Record causes before consumers.** Any system, tool, or message change
   must appear before the `inference.requested` that uses it.
2. **One record per durable change.** A trace is not a UI render stream.
   It records state mutations and model I/O boundaries only.
3. **Snapshots are projections.** Title, message chain, system prompt,
   tools, projected state are all rebuilt from records — never stored.
4. **Record what changed, not what is.** Per-record envelopes and patches
   omit fields whose value is unchanged from the last write. Constants of
   the session (provider, model, agent ID, cwd, schema version, max
   tokens) live on `session.started` and are not restamped. Sparse writes
   keep the log readable, smaller, and easier to diff.
5. **Integrity records carry references, not duplicate payloads.**
   `inference.requested` stores `systemDigest`, `toolsDigest`, and
   `messageIds`. The inspector recomputes the same digests and active-chain
   message IDs during replay and surfaces any mismatch.

## Envelope

```jsonc
{
  "id":        "<sessionId>:<stable-key>",
  "sessionId": "<session id>",
  "time":      "2026-05-15T00:00:00.000000000Z",
  "type":      "message.appended",

  // Only when meaningful:
  "parentId":  "<previous message id>",        // message.appended only
  "isSidechain": true,                         // subagent messages only
  "gitBranch": "feat/x",                       // emitted only when changed
  "agentId":   "subagent-1",                   // emitted only when overriding session default

  // One payload object, keyed by the first segment of "type".
  "message":   { "...": "..." }
}
```

Constants of the session are recorded on `session.started` and inherited
by every following record: `version`, `cwd`, `provider`, `model`,
`maxTokens`, `agentId`. The envelope's other fields are emitted only when
they introduce new information.

Record types are lowercase, dot-separated, past tense:
`<entity>[.<subentity>].<past-tense-verb>`. The payload object is keyed
by the first segment of `type` — `system.*` → `system`, `tools.*` → `tools`,
and so on.

## Core Event Set

| Type | Payload | Meaning |
|---|---|---|
| `session.started` | `session` | Session constants: provider, model, max tokens, cwd, agent ID, optional parent. |
| `session.forked` | `session` | New session copied from another session. |
| `session.compacted` | `session` | Compaction boundary for the active chain. |
| `system.section.added` | `system` | A named system section was added or replaced. |
| `system.section.removed` | `system` | A named system section was removed. |
| `tool.added` | `tool` | One tool schema became available. |
| `tool.removed` | `tool` | One tool schema was removed. |
| `message.appended` | `message` | A user, assistant, or tool message entered the chain. |
| `inference.requested` | `inference` | Model call boundary with context digests and message IDs. |
| `inference.responded` | `inference` | Model response boundary with stop reason, latency, usage. |
| `session.state.patched` | `state` | Sparse patch of UI/session metadata (title, lastPrompt, mode, tasks, worktree...). |
| `hook.fired` | `hook` | One hook invocation completed (sync or async). Carries event, source, outcome, latency. |
| `permission.required` | `permission` | A tool call needs external adjudication (user prompt or hook). Joined to a later `permission.decided` by `requestId`. |
| `permission.decided` | `permission` | Terminal permit/reject decision for a tool call. Emitted directly for config-level decisions; or after a matching `permission.required` resolves. |
| `skill.state.changed` | `skill` | A skill transitioned between disable / enable / active. Captures provenance via `caller`. |

Intentionally not yet in the core set:

- `tool.invoked` / `tool.completed`: useful for tool latency/error audit
  but not required to reconstruct context — tool calls are assistant
  `tool_use` blocks and results are `tool` role messages.
- `model.changed` / `params.changed`: only needed if mid-session model or
  param changes become a feature.

## Payloads

### Session

```jsonc
// session.started — emits every session-wide constant exactly once.
"session": {
  "provider":  "anthropic",
  "model":     "claude-sonnet-4-6",
  "maxTokens": 16384,
  "agentId":   "main",
  "parentId":  "<optional source session>"
}

// session.forked
"session": { "parentId": "<source session>" }

// session.compacted
"session": { "boundaryId": "<first active message after compaction>" }
```

`Cwd` lives on the envelope of `session.started`, not in this payload.

### System

```jsonc
// system.section.added
"system": {
  "name":    "identity",
  "slot":    0,
  "content": "You are ...",
  "caller":  "system:init"
}

// system.section.removed
"system": { "name": "identity", "caller": "command:/identity" }
```

Replay keeps a map by section name. Replacing preserves the first
insertion order. Rendering sorts by `(slot asc, firstInserted asc)` and
joins non-empty contents with blank lines — the same ordering used by
`core.System.Prompt()` and by `inference.requested.systemDigest`.

### Tool

```jsonc
// tool.added
"tool": {
  "schema": {
    "name":         "Read",
    "description":  "read a file",
    "input_schema": { "type": "object" }
  },
  "caller": "tools:init"
}

// tool.removed
"tool": { "name": "Read", "caller": "mode:plan" }
```

`schema` mirrors the model-facing `core.ToolSchema` JSON exactly, because
`inference.requested.toolsDigest` is computed from that canonical shape.

### Message

```jsonc
"message": {
  "messageId": "m_...",
  "role":      "user",
  "content": [
    { "type": "text", "text": "hello", "source": "user" }
  ]
}
```

`parentId` lives on the envelope, not in this payload. `source` is
optional provenance for injected text (reminders, hooks, slash commands,
compaction summaries). `gitBranch` is emitted on the envelope only when
it differs from the last message's branch.

### Inference

```jsonc
// inference.requested
"inference": {
  "turn":         7,
  "systemDigest": "sha256:...",
  "toolsDigest":  "sha256:...",
  "messageIds":   ["m1", "m2"]
}

// inference.responded
"inference": {
  "turn":       7,
  "stopReason": "end_turn",
  "latencyMs":  1820,
  "usage": {
    "inputTokens":       4321,
    "outputTokens":      512,
    "cacheCreateTokens": 0,
    "cacheReadTokens":   1024
  }
}
```

Provider, model, and max tokens come from `session.started` — they are not
restated per turn. Requests and responses are joined by `turn`.

### State

```jsonc
"state": {
  "ops": [
    { "path": "title",      "value": "Fix login bug" },
    { "path": "lastPrompt", "value": "explain the failure" }
  ]
}
```

Each op is a (path, JSON value) pair. Recognized paths: `title`,
`lastPrompt`, `tag`, `mode`, `tasks`, `worktree`. Unknown paths are
ignored on replay (forward compatibility).

The writer emits **only paths whose value changed** since the last patch
in the same process. `null` and empty values are valid and clear the
field (last-wins semantics on read). Sparse patches keep restore correct
because earlier records still own unmodified fields.

## Recording Path

Two writers share the same JSONL file:

- **`internal/session.Store.Save`** writes durable session facts:
  `session.started`, `message.appended`, and `session.state.patched`.
- **`internal/session.Recorder`** observes `core.Config.OnEvent` and
  writes model-context facts the snapshot path cannot see:
  `system.section.*`, `tools.*`, `inference.requested`,
  `inference.responded`.

`NewRecorder` writes `session.started` *before* attaching observers.
Attaching system/tool observers replays existing sections/tools as
synthetic events; those records must not appear before session metadata
exists or `Store.Save → Start` will short-circuit on `fileExists` and the
provider/model metadata will be lost.

### Durability classes

`appendRecord` toggles `fsync` per-record:

- **Sync writes**: user input (`message.appended`), turn completion
  (`inference.responded`), lifecycle records (`session.started`,
  `session.compacted`). A crash must not lose these.
- **Buffered writes**: pure telemetry (`system.section.*`, `tools.*`,
  `inference.requested`, `session.state.patched`). Worst case after crash: the
  in-flight turn's telemetry is lost, but the next turn's
  `inference.responded` (sync) flushes it before then.

The order of records is always preserved because writes are O_APPEND on
a single-process owner; only durability differs by class.

Recorder failures are logged and do not break the session. The trace is
diagnostic and persistence — not a reason to fail model execution.

## Replay

Reconstructing context at a record:

```text
sections = map[name]section          # by insertion order, then slot
tools    = map[name]schema           # by name
messages = map[id]message            # all message nodes observed so far
order    = []                        # append order for latest-leaf selection
boundary = ""                        # compaction boundary, if any

for each record up to target:
  session.started        -> provider/model/maxTokens/agent/cwd from payload + envelope
  system.section.added   -> sections[name] = payload (replace, preserve first order)
  system.section.removed -> delete sections[name]
  tool.added             -> tools[schema.name] = schema
  tool.removed           -> delete tools[name]
  message.appended       -> messages[id] = node; order.append(id)
  session.compacted      -> boundary = session.boundaryId
  inference.requested    -> compare recorded digests/messageIds with replayed context; emit integrity check

activeMessages = leafWalk(messages, latestLeaf(order), boundary)
```

The inspector exposes this replay through:

```text
GET /api/sessions/{id}/state/{recordId}
```

Response contains the replayed system sections, tools, active-chain
messages with content, current provider/model/max tokens/cwd/agent,
recomputed digests, and any integrity mismatch observed so far.

## Recovery Flow

`Store.Load` resumes a session by:

1. Load all records from `transcripts/<session-id>.jsonl`.
2. Project them into messages + state via the active-chain walk.
3. Hydrate large tool results from `blobs/tool-result/<session-id>/`.
4. Convert transcript nodes into app `Entry` objects.
5. Restore the compact summary (if any) from the state patches.

The CLI resume paths (`-c`, `-r`, `--fork`) consume only this projection.
There is no legacy session format.

## Forking

`Store.Fork` rewrites every record of the source session under the new
session ID and appends a `session.forked` marker. The source file is
left untouched. The new session is independent — appending to it never
affects the source.

## `gen inspector`

`gen inspector` starts a localhost-only, read-only web tool for the
current project's transcripts.

```text
gen inspector                    # start on 127.0.0.1, auto-port
gen inspector --addr 127.0.0.1:38080
gen inspector --no-open          # print URL only
```

HTTP surface:

| Method | Path | Returns |
|---|---|---|
| GET | `/` | Embedded static UI. |
| GET | `/api/sessions` | Sessions in the project transcript directory. |
| GET | `/api/sessions/{id}/records?after=<byteOffset>` | Raw JSONL bytes from a byte offset (no JSON re-serialization). |
| GET | `/api/sessions/{id}/stream` | SSE stream; replays the file then polls for appended records. |
| GET | `/api/sessions/{id}/state/{recordId}` | Replayed context at one record (system, tools, active messages, digests, integrity). |

The UI shows the raw record timeline. Clicking a row renders both the
raw record and the replayed context at that point — the workflow for
"why did the model see this?"

## Review Conclusions

The current event set is intentionally small and is not redundant:

- `session.*` declares lifecycle constants and chain boundaries.
- `system.section.*`, `tools.*`, and `message.appended` are the only
  durable inputs needed to reconstruct model context.
- `inference.*` records the model-call boundary and integrity references;
  it does not duplicate the already replayable payloads.
- `session.state.patched` is isolated from model context because title, task list,
  mode, and worktree metadata are list/resume concerns.

The recording path is reasonable when it preserves these invariants:

- `session.started` is first, before observer replay can emit synthetic
  `system` or `tools` records.
- Every context mutation is recorded before the `inference.requested` that
  consumes it.
- Writers emit sparse changes; readers apply last-wins and ignore unknown
  patch paths.
- Web replay must project the same active chain as resume: parent links,
  latest leaf, and compaction boundary. A timeline-ordered message list is
  not a valid model-context projection.

The design is clear as long as records stay model-facing rather than
UI-render-facing. Tool schemas use `input_schema` because that is the
canonical request shape. Hook, permission, and tool-execution audit records
are useful, but adding them to the core restore set before their payload
contract is defined would blur the boundary between context reconstruction
and policy auditing.

The implementation stays simple by keeping JSONL as the wire format.
Inspector APIs either return raw JSONL (`/records`) or a projection that can be
recomputed from raw JSONL (`/state`). There is no second persistence layer,
and integrity checks compare the replayed context against the
`inference.requested` references.

## Known Gaps

- `hook.fired`, `permission.decided`, and permission mode changes are not
  recorded. Troubleshooting context evolution works; auditing policy
  decisions is incomplete.
- Compaction has a transcript record type, but app-level compaction paths
  still primarily persist the resulting summary via `message.appended`
  through `Store.Save`.
- Multi-process writes to the same transcript file are not coordinated
  beyond the in-process `FileStore` mutex.
- Transcript export is not implemented; the inspector is local and live.
