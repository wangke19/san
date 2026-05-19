---
package: github.com/genai-io/gen-code/internal/skill
layer: feature
---

# skill

Loads markdown-defined skills from user / project / plugin scopes, tracks
their enable state, and renders the active-skills directory that the
harness attaches to user messages via the `skills-directory` reminder
(see [`concepts/harness-channels.md`](../concepts/harness-channels.md)).

## Purpose

A skill is a markdown file (with YAML frontmatter) that the model can be
made aware of (`active`), made invocable via slash command (`enabled`), or
hidden (`disabled`). This package:

1. Discovers skills across six scopes
   (`~/.claude/skills/`, `~/.gen/plugins/*/skills/`, `~/.gen/skills/`,
   `.claude/skills/`, `.gen/plugins/*/skills/`, `.gen/skills/`) with
   project overriding user overriding Claude-compat.
2. Persists per-skill state in user / project state stores.
3. Renders the active-skills block consumed by the `skills-directory`
   reminder provider in `internal/app`.

For the broader extension model see
[`concepts/extension-model.md`](../concepts/extension-model.md). A
how-to-author-a-skill guide is tracked in `notes/tech-debt.md`.

## Contract

The seam consumed by `internal/app` (wires `PromptSection` into the
`skills-directory` reminder provider) and `internal/command` (slash
command surface):

```go
package skill

// Service is the public contract for the skill module.
type Service interface {
    // query
    List() []*Skill                       // all loaded skills
    Get(name string) (*Skill, bool)       // lookup by name
    IsEnabled(name string) bool           // check if enabled
    FindByPartialName(name string) *Skill // partial/suffix match
    GetEnabled() []*Skill                 // all enabled or active skills
    GetActive() []*Skill                  // all active skills (model-aware)
    Count() int                           // total number of loaded skills

    // mutation
    SetEnabled(name string, enabled bool, userLevel bool) error
    GetDisabledAt(userLevel bool) map[string]bool

    // rendering — body consumed by the skills-directory reminder, not
    // the system prompt
    PromptSection() string                       // rendered active-skills block
    GetSkillInvocationPrompt(name string) string // full skill content for injection

    // concrete access
    Registry() *Registry
}

// Skill, SkillState, SkillScope — see types.go for value types.
```

### Known Violations

Tracked for PR-3. The contract above is verbatim from today's code.

- **Rule 1 (small).** **11 methods** across four concerns. Suggested split:
  - `SkillQuery` → `List`, `Get`, `Count` (or pick three of the seven
    query methods; consolidate `IsEnabled` / `GetEnabled` / `GetActive`
    behind a `Filter(state SkillState)` if downstream usage permits)
  - `SkillStateStore` → `SetEnabled`, `GetDisabledAt`
  - `SkillPrompt` → `PromptSection`, `GetSkillInvocationPrompt`
  - Remove `Registry()` (see Rule 7)
- **Rule 7 (no escape hatch).** `Registry() *Registry` lets every caller
  reach the concrete type. Drop it; if a caller needs methods that aren't
  on `Service`, add them to the appropriate split interface or have the
  caller depend on `*Registry` directly.
- **Rule 5 (constructors return concrete types).** `Default()` returns
  `Service` (interface). Should return `*Registry` if callers are
  collaborators in the same module.
- **Singleton via `Default()` and `DefaultIfInit()`.** Same issue as
  `hook` and `agent`: two-flavor accessors paper over racy init. Move
  construction into the app composition root.

*Resolved in this PR:* removed the unused exported `AddPluginSkills`
method (and its `addPluginPath` / `additionalPaths` plumbing), which
was the only Rule 4 (anonymous struct) violation in the package.

## Internals

- `Registry` (`registry.go`) is the only implementation, holding:
  - `skills []*Skill` — loaded by `loader`
  - `userStore`, `projectStore` — JSON-backed persistence of per-skill
    `SkillState`
  - `cwd` — for project-scope resolution
- `loader.go` walks the six scopes in priority order, parsing
  `SKILL.md` frontmatter and bundled resource directories
  (`scripts/` / `references/` / `assets/`).
- State (`disable` → `enable` → `active` → `disable`) cycles via
  `SkillState.NextState()`; the TUI's `/skill` flow uses this.
- Active skills render through `PromptSection()` and are delivered to
  the model via the `skills-directory` reminder attached to each user
  message; enable-only skills surface as slash commands but stay out of
  the model's awareness entirely.

## Lifecycle

- Construction: `Initialize(Options{CWD})` at app startup, before
  `internal/command` builds its slash-command list. Singleton thereafter.
- Mutation: `SetEnabled` writes through to user or project store
  immediately; in-memory `skills` slice is updated in place.
- Plugin sources are added post-init by `internal/plugin` via
  `AddPluginSkills`.
- Concurrency: registry mutations are mutex-guarded; reads are
  RWMutex-locked.

## Tests

```
internal/skill/skill_test.go            — loader, state cycling,
                                            scope priority, prompt rendering.
internal/skill/lazy_loading_test.go     — verifies content stays on disk
                                            until GetInstructions().
```

## See Also

- Code: `internal/skill/`
- Concepts: [`concepts/extension-model.md`](../concepts/extension-model.md), [`concepts/harness-channels.md`](../concepts/harness-channels.md)
- Related: [`packages/command.md`](command.md) (slash-command surface), [`packages/plugin.md`](plugin.md) (plugin-scoped skills), [`packages/reminder.md`](reminder.md) (the channel that delivers `PromptSection`)
- Layer: `feature` (see [`reference/dependency-rules.md`](../reference/dependency-rules.md))
