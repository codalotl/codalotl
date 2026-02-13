# Manual

The manual is a user manual for codalotl, the coding agent for Go. This SPEC.md is a guide and rulebook (ie, for an LLM) for constructing the manual.

The manual should answer questions that actual users have when using codalotl. It should contain instructions on how to do things, reference material, and describe important concepts. It is not a marketing document.

It is also not a behavior specification. One temptation of LLMs writing manuals is to dump all behavior and rules related to a topic into a section of the manual. This is not useful. There is no need to describe edge cases, error handling, exact sorting semantics, or things of that nature to the end user. Filter all information though the lens of: "Does this provide an important concept to the user's mental model? Does it teach them how to do something?"

For example, descriptions like this should be avoided: "`codalotl -h` and `codalotl version` skip config/tool validation". From a user point of view, that is a completely unimportant detail.

## Build Target

The user-facing manual articfact is `manual/MANUAL.md`. It is a **single** markdown file describing the usage of `codalotl`. Since it's just one file, it may be long.

Terminology note: this document may refer to `the manual`, `the target manual`, `the output manual`, etc. This means `manual/MANUAL.md`.

## Mental Model

The manual must use words and concepts based on the mental model of the user. It should avoid code-based terms and jargon.

Examples:
- Some code uses "code unit" (a generic multi-language term for a 'package'). The corresponding user word is "package"; we don't support multi-language.
- The user shouldn't need to know what the names of tools are (ex: `shell` vs `skill_shell`).
- When discussing `@` and "grants": the user shouldn't care about the word 'grant' as a specific term.

## Supporting Material

The `manual` dir contains various supporting files that we take as true and mandatory manual inclusions:
- `_toc.md`: the target table of contents (which headers exist). The output manual MUST have these sections/headings. These TOC are a floor. The manual may have additional sections (either peer sections, or nested sections).

Supporting material may have sections with text wrapped in [[double brackets]]. This means the **gist** of the bracketed text must be included, but it should be re-written. These can also include instructions. Examples:
- `[[type /skills to list skills]]` means to include 1+ sentences describing that /skills lists skills (but the final manual is free to include **additional** information about skills. Eg, where one might type /skills, what its output format is, what skills are, etc).
- `[[describe how skills work]]` is giving instructions to the translator of `supporting material`->`the manual` that this text should describe how skills work.

If you find the supporting material is false or out-of-date:
- **Continue** what you were doing, and assume the supporting material is true-ish.
- **Report** the potential error in supporting material at your stopping point (eg, your final message).

### Supporting Commits

The manual is LLM-generated, but human edited. The following commits are human edits of the manual. Inspect these commits. As long as they are still true, persist their intent. Sometimes their intent is in the commit message; other times you must infer it.
- 16fd3efa988bada542e6697ffbc85aa850d45f7f
- 9a0bfe0eea012237059c1af0252ab32de0298e21
- b7323463b633211f2fdd96a227ae6c1bbbc61d2f
- d99d4204f0106333806de14ed1b50d1475e3e3cb
- 386887bcf5eeec73a10d024d573d25e7eccbb390
- 588ca58cbe584486facf6a5465dbbe3af5647201
- c00c928db8a8dcb882a2b718413b87333703c16e
- e51ab400648d698ee01178684c557c177ccc46a2
- e4e033b2c3f0ff383707420aec6a0ecadb99f1d1
- 5723ddeabe2a71402829274db45ac6cc094e4767
- 550dc7c695fee970b692dc3fd4c6348f0a3ad5bf
- aa94b1a42f435d7bac9e2dfa531cd90a8ac43113

## Tone and Writing Style

- Use a matter-of-fact, capability-driven voice: describe what it does in plain verbs (read/change/run, generate/inspect), avoid superlatives.
- Address the reader directly (second person) and prefer simple imperatives ("Run…", "Install…", "Choose…").
- Keep paragraphs short (1–2 sentences), then move quickly into structured steps and sections.
- Present onboarding as numbered procedures with clear action headings like "Setup / Install / Run / Upgrade."
- Offer options in a neutral “Choose…” pattern and mark a recommended path without overselling.
- Write feature lists as "Label: one-sentence explanation" bullets (short label + colon + concrete outcome).
- Include minimal command snippets (one canonical command per step) and keep surrounding text focused on what happens next.
- Phrase cautions as practical, calm guidance ("X can modify…, so consider Y…") rather than alarmist warnings.
- State platform/support constraints plainly and early, and give a straightforward workaround when needed.
- Use light, professional contractions ("it's", "you’ll") to stay approachable without sounding chatty.

## Procedures

### Update Supporting Material

Use `git` to find all changes since a given piece of supporting material was updated. Scan through the commits to see if that supporting material may have changed. If it may have, dig into it. Update the supporting material in place (but don't commit it yet). Ask for human review.

### Refresh Manaul

If the manual already exists, we may want to refresh it. To do so:
- Make sure supporting material is up-to-date (or be told it is).
- Identify changes in supporting material since the last manual update.
- Identify changes in the codebase since the last manual update.
- Edit the manual.
- Ask for human review.

### Building Manaul from Scratch

- Make sure supporting material is up-to-date (or be told it is).
- Inspect Code and SPEC.md files throughout repository.
- Write manual.
- Ask for human review.
