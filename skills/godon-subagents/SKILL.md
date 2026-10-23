---
name: godon-subagents
description: Decide whether to hand work to subagents, and how. Use before spawning any subagent, parallel agent or delegated task — subagents are for large research that splits into independent parts, never for small tasks, and never for editing.
---

# Subagents: only for large, splittable research

A subagent starts with an empty context. It re-reads files the main agent may
already know, costs tens of thousands of tokens before it does anything
useful, and hands back only a summary that may have dropped the detail that
mattered. Delegate only when that cost buys something the main agent cannot
get cheaper: keeping a flood of file reads out of the main context, or
answering independent questions side by side.

## The gate — every condition must hold, or do the work yourself

1. **Large.** The work spans **four or more independent areas** (packages,
   modules, services), or answering it means reading more than about twenty
   files the main agent does not need afterwards.
2. **Splittable.** The parts do not depend on each other: no part needs
   another's answer first, and each can be judged complete on its own.
3. **Substantial parts.** Each part needs more than a handful of tool calls.
   A part that is one search or one file read is done inline.
4. **Read-only.** The parts are research — find, read, compare, explain. A
   subagent never edits files, runs migrations, commits or calls anything
   outward-facing. All changes are made by the main agent.

If any condition fails, do not delegate. When in doubt, do not delegate.

## Never delegate

- An edit of any size, including "mechanical" edits across many files.
- A bug fix, even one whose cause is unclear — investigate it yourself.
- A declaration lookup: `godon symbol` answers it in one call.
- Running tests or checks: `godon test` and `godon check` already keep the
  output out of context.
- A question, a discussion, a plan, or anything that needs the user's input
  partway through.
- A review of your own conclusion: a subagent handed your hypothesis tends to
  return it confirmed.

## Limits when the gate passes

- **At most two subagents at a time.** Start them together when they are
  independent; never more than two in flight.
- **No nesting.** A subagent does not start subagents of its own.
- **Read-only, stated in the brief.** Prefer the agent's read-only or
  exploration agent type where one exists (Claude Code: the `Explore` agent).

## The brief — the only context a subagent has

Write each brief as for a capable colleague who has seen nothing:

1. **Goal**: the question to answer, in one or two sentences.
2. **Scope**: the directories or files to look at, and what is out of scope.
3. **Already known or ruled out**: so it does not repeat your work.
4. **Read-only**: "Do not edit, create or delete files."
5. **Tools**: use `godon symbol` for declarations and search tools for usages,
   rather than reading whole files.
6. **Report**: at most ~300 words — the answer, the `file:line` evidence for
   it, and what it could not determine. No file dumps.

## After they report

- Treat a report as a lead, not a fact: open the `file:line` evidence for any
  claim you are about to act on.
- Make the changes yourself, then verify with `godon test` (and `--with-all`
  when the change is large) and `godon check`.
- Tell the user what the subagents found only as far as you verified it.
