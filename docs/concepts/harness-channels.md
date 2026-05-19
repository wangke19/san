# Harness Channels

Gen Code delivers context to the model through **three distinct channels**,
each with different cache, lifecycle, and stability properties:

| Channel | What lives there | Cache-friendly? | Mutable mid-session? |
|---|---|---|---|
| **System prompt** | Identity, output style, engineering defaults, provider quirks, policy, guidelines, environment footer. Slot-sectioned. | Yes — invariant per session unless a section mutates. | Yes (Use/Drop), but expensive (cache miss). |
| **`<system-reminder>` blocks** | Session-level / project-level dynamic content: **active-skills directory**, GEN.md/CLAUDE.md memory, ad-hoc notices. Attached below the next user message. | Yes — once attached, the user message is immutable. | No (re-emitted as new attachments, never mutated). |
| **User messages** | The actual prompt the user typed. | Yes — already cached. | No. |

> The active-skills list (what skills the model is currently aware of) used
> to live in the system prompt. It now rides on user messages as a
> `<system-reminder source="skills-directory">` block — toggling a skill
> no longer busts the prompt cache.

The harness chooses which channel to use based on **how often the content
changes** and **whether the LLM's prompt cache should survive the
change**.

## Why Three Channels?

The LLM's prompt cache works on **exact prefix match**. Anything in the
system prompt that mutates invalidates the cache prefix from that point
onward — so frequent system-prompt edits are expensive.

The harness optimizes:

- **System prompt** = "things true for every turn of this session".
  Identity, policy, communication style, engineering defaults, tool-
  usage guidelines, environment. Mutates rarely. (Tool *schemas* are
  passed separately via the LLM API's `tools` parameter, not in the
  system prompt text.)
- **`<system-reminder>` blocks** = "things true now, but may change". Each
  reminder is attached to a *user message* (not the system prompt) and
  re-emitted on session start and after every PostCompact. Because user
  messages are immutable once attached, the cache from prior turns stays
  valid; only the new user message + reminder is freshly evaluated.
- **User messages** = actual user input.

## System Prompt: Slot Sections

The system prompt is composed of **Sections**, each owning a numbered
**Slot**. Slots define ordering. Sections within the same slot use
insertion order, so several sections can stack inside one slot (e.g.
`identity` + `output` + `engineering` all live in slot 0). Mutations to a
section trigger `Refresh` (lazy re-render).

```
slot 0   identity        built-in or custom persona; output and
                         engineering sections stack here too
slot 1   provider        optional provider-specific quirks
slot 2   policy          safety contract — never overridden
slot 3   guidelines      tool-usage, task-workflow, when-to-ask,
                         git-safety (filtered by Scope and isGit)
slot 4   environment     cwd, git, date, platform, model — volatile,
                         placed last so daily/cwd changes don't bust
                         the cache prefix above
```

Slot constants live in `internal/core/section.go`; the default applier
and renderers live in `internal/core/system/catalog.go`. Skills, memory,
and the agent directory are intentionally **not** slots — they ride on
the reminder channel (skills, memory) or on tool schemas (agent
directory) instead.

See [`packages/core.md`](../packages/core.md) for the `Section` and
`System` types.

## Reminders

Reminders carry "session-level" or "project-level" mutable content. The
harness has standard providers:

| Provider ID | Source | Re-emit triggers |
|---|---|---|
| `skills-directory` | active skills (and the "use Skill tool to invoke" preamble) | session start, PostCompact, skill enable/disable/activate |
| `memory-user` | `~/.gen/GEN.md` and `~/.claude/CLAUDE.md` | session start, PostCompact, file change |
| `memory-project` | `<project>/GEN.md` and `<project>/CLAUDE.md` | session start, PostCompact, file change, cwd change |

Each provider has a stable ID; re-emitting from the same ID **drops the
previous queued entry**, so toggling a skill three times in a row
produces one final reminder, not three.

Reminders wrap their body in:

```xml
<system-reminder source="skills-directory">
  Enabled skills:
  - github:create-pr — ...
  - jira:link-ticket — ...
</system-reminder>
```

The LLM is instructed (in the system prompt) to treat the
`<system-reminder>` tag as a system instruction even though it appears
inside a user message.

Implementation: [`packages/reminder.md`](../packages/reminder.md).

## Memory: GEN.md / CLAUDE.md

Two memory tiers:

- **User memory**: `~/.gen/GEN.md` (Gen Code) and `~/.claude/CLAUDE.md`
  (Claude Code compat). Loaded once per session, attached as
  `memory-user` reminder.
- **Project memory**: `<project>/GEN.md`, `<project>/CLAUDE.md`, plus
  recursively-loaded `<dir>/GEN.md` upwards from the start path.
  Attached as `memory-project` reminder.

Memory is **never** in the system prompt — that would invalidate the
prompt cache every time the user edited their memory file.

## Compaction

When the context window approaches its limit, the harness compacts:

1. Pick the prefix of messages to summarize (everything except the most
   recent N turns).
2. Call the LLM with a "summarize the following conversation" prompt to
   produce a `CompactInfo` summary.
3. Replace the prefix with a single synthetic message containing the
   summary.
4. Re-emit all reminders (`EnqueueAllProviders`) so the post-compact
   conversation has fresh skill/memory context.

Compaction is **not** a channel by itself — it's a mutation of the
user-message channel. The reminder re-emission step is what makes
compaction safe across the reminder channel.

Implementation: `internal/app/conv/compact.go`. The agent emits
`OnCompact` events for observers.

## See Also

- [`concepts/extension-model.md`](extension-model.md) — skills (one
  reminder source) and how plugins contribute to it.
- [`packages/core.md`](../packages/core.md) — `System`, `Section`, slot
  layout.
- [`packages/reminder.md`](../packages/reminder.md) — runtime API.
- [`packages/session.md`](../packages/session.md) — how compaction
  records flow into the transcript.
