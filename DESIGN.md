---
name: pix-sandbox console
description: The embedded read-only surface of a Pix emulator, set as a concrete grid.
colors:
  ink: "#0b0b0b"
  ink-raised: "#131313"
  paper: "#edeae3"
  dim: "#96918a"
  rule: "#3b3934"
  rule-strong: "#56534e"
  concrete-red: "#c81e14"
  concrete-blue: "#1b3fd4"
  slate: "#4a463f"
typography:
  display:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "clamp(2.75rem, 8vw, 5rem)"
    fontWeight: 700
    lineHeight: 0.9
    letterSpacing: "-0.045em"
    fontVariation: "tabular-nums"
  headline:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "clamp(1.6rem, 2.4vw, 2.1rem)"
    fontWeight: 700
    lineHeight: 0.95
    letterSpacing: "-0.035em"
  title:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "1rem"
    fontWeight: 600
    lineHeight: 1.45
    letterSpacing: "-0.015em"
  body:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.45
    letterSpacing: "normal"
  data:
    fontFamily: "Fragment Mono, ui-monospace, monospace"
    fontSize: "0.8125rem"
    fontWeight: 400
    lineHeight: 1.5
    fontFeature: "'liga' 0"
  label:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "0.6875rem"
    fontWeight: 400
    lineHeight: 1.45
    letterSpacing: "0.12em"
rounded:
  none: "0"
spacing:
  gutter: "clamp(1.25rem, 3vw, 2.75rem)"
  rail: "17rem"
  module-y: "0.7rem"
  cell: "0.75rem 0.9rem"
  step: "2.5rem"
components:
  status-ativa:
    textColor: "{colors.paper}"
    typography: "{typography.label}"
    rounded: "{rounded.none}"
    padding: "0.3rem 0.5rem"
  status-concluida:
    backgroundColor: "{colors.concrete-blue}"
    textColor: "{colors.paper}"
    typography: "{typography.label}"
    rounded: "{rounded.none}"
    padding: "0.3rem 0.5rem"
  status-expirada:
    backgroundColor: "{colors.concrete-red}"
    textColor: "{colors.paper}"
    typography: "{typography.label}"
    rounded: "{rounded.none}"
    padding: "0.3rem 0.5rem"
  status-removida:
    backgroundColor: "{colors.slate}"
    textColor: "{colors.paper}"
    typography: "{typography.label}"
    rounded: "{rounded.none}"
    padding: "0.3rem 0.5rem"
  ledger-row:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.paper}"
    rounded: "{rounded.none}"
    padding: "0.7rem 0"
  ledger-row-hover:
    backgroundColor: "{colors.ink-raised}"
    textColor: "{colors.paper}"
  fact-cell:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.paper}"
    rounded: "{rounded.none}"
    padding: "{spacing.cell}"
  event-serial:
    backgroundColor: "{colors.concrete-blue}"
    textColor: "{colors.paper}"
    typography: "{typography.data}"
    rounded: "{rounded.none}"
    padding: "0.15rem 0.4rem"
---

# Design System: pix-sandbox console

> This file records the **visual** system of the embedded console. The engineering
> specification — architecture, state machines, invariants, phases — lives at
> [docs/DESIGN.md](docs/DESIGN.md) and is a separate document.

## Overview

**Creative North Star: "The Concrete Grid"**

The console is set the way Amilcar de Castro set the *Jornal do Brasil* in 1959 and the way the Brazilian concretists composed a series: a fixed modular grid, hairline rules doing the structural work, flat fields of a single ink standing in for ornament, and empty space treated as a material rather than as leftovers. The choice is not decoration borrowed from a Brazilian subject. An append-only event log *is* a serial progression — identical modules whose content varies by rule — which is precisely the argument concretism was making about form.

The register is dark because of where the surface lives: a browser window next to a terminal, usually at night, usually while something is wrong. Ink ground, offset-paper ink, and two poster colours that mean exactly one thing each. Nothing here is soft. There are no rounded corners, no shadows, no icons, no gradients, and no glass; depth is conveyed by rule weight and by a single raised surface tone. Density is high and deliberate — the reader is scanning for the one row that went wrong — but the rhythm is quiet enough to read for a while.

The system's honesty rule is inherited from the product: the console shows recorded transitions and never inferred state. That extends to its visuals. Colour carries meaning but never carries it alone; a status is always spelled out. A failure is shown in the margin without painting over the data it qualifies. Nothing on the page reaches outside the binary.

**Key Characteristics:**
- Modular grid with 1px rules; 3px paper rules open and close a field.
- Flat colour fields, never chips with radius or shadow.
- Archivo for voice, Fragment Mono for every identifier, amount and timestamp.
- Zero radius, zero shadow, zero icons — the form language is the rule and the field.
- Motion appears exactly once: a row that arrived since the last poll prints itself on.

## Colors

Ink and paper carry the whole surface; two saturated poster inks carry meaning, and each means one thing everywhere it appears.

### Primary
- **Concrete Blue** (`#1b3fd4`): settlement. The `CONCLUIDA` status field, the serial number of any event that settled (`pix.received`, `cob.settled`, `webhook.delivered`), and the legend swatch that names it. Never used as a link colour, a focus ring, or decoration.

### Secondary
- **Concrete Red** (`#c81e14`): the end of a road. `EXPIRADA`, failed callbacks, and the offline field in the rail. The alarm ink means the same thing on every surface, which is what makes it worth having.

### Tertiary
- **Slate** (`#4a463f`): withdrawal without failure — the `REMOVIDA_*` states, which are neither settled nor expired. Deliberately desaturated so it reads as a state that no longer moves.

### Neutral
- **Ink** (`#0b0b0b`): the ground of every surface.
- **Ink Raised** (`#131313`): the one raised tone — hovered rows, the BR Code payload, structured payload blocks. It is how this system says "surface" without a shadow.
- **Offset Paper** (`#edeae3`): all primary text, the heavy 3px rules, and the knockout field used for a devolução. Warm on purpose; a pure white would read as a screen colour rather than as ink.
- **Dim** (`#96918a`): labels, secondary data, timestamps at rest. 5.3:1 on ink.
- **Rule** (`#3b3934`): the hairline that defines every module.
- **Rule Strong** (`#56534e`): structural rules and the outlined `ATIVA` field.

### Named Rules

**The Never-Colour-Alone Rule.** A state is always spelled out in words inside its field. The colour accelerates recognition for a reader who already knows the system; it never carries the meaning by itself. A console whose statuses only differ by hue fails the one person it exists for.

**The One Meaning Per Ink Rule.** Blue is settlement, red is failure or expiry, slate is withdrawal, paper is a knockout. An ink acquires no second job — not for links, not for focus, not for emphasis.

## Typography

**Display / UI Font:** Archivo (with `system-ui, sans-serif`)
**Data Font:** Fragment Mono (with `ui-monospace, monospace`)

**Character:** Archivo is a rational grotesk with the flat, worked terminals a modernist grid wants; Fragment Mono is a monospaced Helvetica descendant, so identifiers and amounts read in the same voice as the grid instead of announcing themselves as "technical". Both are self-hosted as latin and latin-ext woff2 subsets inside the binary — the console makes no font request, ever.

### Hierarchy
- **Display** (700, `clamp(2.75rem, 8vw, 5rem)`, lh 0.9, ls -0.045em, tabular): the charge amount, and only that. One number per page, at the scale a poster gives its headline.
- **Headline** (700, `clamp(1.6rem, 2.4vw, 2.1rem)`, lh 0.95, ls -0.035em): the masthead in the margin. Lowercase, because the product's name is lowercase.
- **Title** (600, 1rem, ls -0.015em): the event type at the head of a timeline module.
- **Body** (400, 15px, lh 1.45): prose. Capped at 62–68ch wherever it runs as a paragraph.
- **Data** (Fragment Mono 400, 0.8125rem, lh 1.5, `liga` off): every txid, e2eId, rtrId, amount, timestamp and payload value.
- **Label** (400, 0.6875rem, ls 0.12em, uppercase, dim): column heads and field names.

### Named Rules

**The Mono-Is-Data Rule.** Fragment Mono appears where a value is meant to be compared, copied, or matched against a terminal — identifiers, amounts, timestamps, payloads. It is never used as a costume for "technical". Prose is Archivo even on a page full of hashes.

**The Copy-In-One-Gesture Rule.** Any value a reader will paste elsewhere — txid, endToEndId, BR Code payload — carries `user-select: all`, so one click takes the whole value and nothing adjacent.

## Layout

The page is a sheet: a fixed left margin of `17rem` carrying the masthead, the run's seed and version, the counts and the connection state, and a field to its right holding the content. A single 1px rule divides them, and it runs the full height. Below `60rem` the margin becomes a top band and the counts reflow into an auto-fit grid; nothing else about the composition changes.

The ledger is a five-track grid — txid, amount, chave, created, status — with 1px rules between modules and a 3px paper rule opening and closing the field. It closes even with four rows: the sheet must read as finished, not as a page that stopped. Below `60rem` the column heads disappear and each row reflows into three lines with the status field pinned top-right and the amount carrying an explicit `BRL` unit.

The timeline is a single column of modules that **step in from the margin by aggregate**: a charge event sits at the margin, the payment it settled into one step in (`2.5rem`), the callback that announced that payment two (`5rem`). The step is the reading — it is how the eye sees causation without a diagram. At narrow widths the padding shrinks and a 1px rule at the module's edge carries the step instead, because two rem of indent is not a step anyone can see.

Spacing rhythm: one gutter (`clamp(1.25rem, 3vw, 2.75rem)`) frames both regions; modules are `0.7rem` on the y and cells are `0.75rem 0.9rem`; more space sits above a heading than below it.

## Elevation & Depth

**There are no shadows in this system.** Not one, in any state. Depth is conveyed by rule weight and by a single raised tone: `#131313` against the `#0b0b0b` ground marks a hovered row, a payload block, or the BR Code field. The only inset in the stylesheet is a 1px ring standing in for a border on the outlined `ATIVA` field.

### Named Rules

**The Flat-Sheet Rule.** This is printed matter. If an element needs to separate from its neighbour, it gets a rule or a field of ink — never a shadow, never a radius, never a glow. A shadow appearing in this system is a sign that someone reached for a component from another world.

## Shapes

Every corner is square (`0`). The recurring silhouettes are exactly three: the **rule** (1px for modules, 3px in paper to open and close a field), the **field** (a flat rectangle of ink with a word knocked out of it), and the **module** (a row of the grid, identical to its neighbours, varying only by content and position). Borders are structural, never decorative: a 1px `border-left` marks the timeline step at narrow widths, and nothing thicker than 1px is ever used as a side stripe.

## Components

### Status field
- **Character:** a flat rectangle of ink with the state knocked out of it.
- **Shape:** square (`0` radius), `0.3rem 0.5rem` padding, uppercase label type with `0.08em` tracking.
- **Variants:** `ATIVA` is unfilled with a 1px `#56534e` inset ring — the state that has not happened yet is the state with no ink. `CONCLUIDA` fills blue, `EXPIRADA` fills red, `REMOVIDA` fills slate. All four print their word.
- **On the detail page:** the same field at `0.75rem`, inline (`status--lg`), sitting under the amount.

### Ledger row
- **Character:** a module, identical to every other, distinguished only by content.
- **Shape:** a five-track grid, `0.7rem` vertical padding, closed by a 1px rule; the last row's rule is dropped where the field's 3px close takes over.
- **Hover:** the ground lifts to `#131313` and the txid brightens from dim to paper. No transform, no shadow.
- **Fresh state:** a row that arrived since the viewer's last poll animates once — its status field wipes open by `clip-path` and its rule fades down from paper. Suppressed under `prefers-reduced-motion`.

### Fact cell
- **Character:** one field of a specification sheet.
- **Shape:** a fixed three-track grid (one at narrow widths) with cell-borne 1px rules; wide facts span all three so a row never orphans and the box always closes.
- **Content:** an uppercase dim label over a value, mono where the value is data.

### Timeline module
- **Character:** one recorded transition, positioned by what it belongs to.
- **Shape:** a three-track grid — serial, type, timestamp — with the payload spanning beneath it in auto-fit columns of `21rem` minimum, so a timestamp or an e2eId is never broken mid-token.
- **Serial:** the log id doubles as the transition's colour field — blue for settlement, red for failure, paper knockout for a devolução, plain dim for everything else. It is legended above the list, and its `title` explains that ids run across every aggregate.
- **Structured payloads:** any non-scalar value takes the full row on the raised ground, sorted after the scalar pairs. It scrolls on desktop and wraps at narrow widths.

### Connection state
- **Character:** the margin says whether what you are reading is current.
- **Default:** `Live · polling every 2s` in dim, present only on the surface that actually polls.
- **Lost:** a red field reading `No answer from the sandbox`, replacing the line in place while the stale rows stay on screen. The page reports that it is stale, never that it is empty.

### Empty and error states
- **Empty ledger:** a lead sentence, then the `curl` that ends the emptiness on the raised ground, then the line that says the row will appear on its own.
- **404 / 500:** the same lead-plus-hint block inside the full layout, rail included. A failure never falls back to the browser's default page.

## Do's and Don'ts

### Do:
- **Do** spell every state out in words inside its field, and let colour only accelerate the reading (**The Never-Colour-Alone Rule**).
- **Do** open and close a field with the 3px paper rule, so a short list still reads as a finished sheet.
- **Do** set every identifier, amount, timestamp and payload value in Fragment Mono, and everything else in Archivo.
- **Do** give structured values their own full-width block on `#131313` rather than squeezing them into a column.
- **Do** carry meaning with position: the timeline's step from the margin is how causation is shown.
- **Do** keep every asset inside the binary — fonts, stylesheet, script, favicon — and fingerprint their URLs so an upgrade cannot serve a stale stylesheet.
- **Do** suppress the one authored animation under `prefers-reduced-motion`.

### Don't:
- **Don't** add a radius, a shadow, a gradient, or a glass surface. This is printed matter (**The Flat-Sheet Rule**).
- **Don't** introduce an icon set, an emoji, or a Unicode glyph standing in for one. The system has no icons and does not need them.
- **Don't** give an ink a second job — blue is settlement and nothing else (**The One Meaning Per Ink Rule**).
- **Don't** use a coloured side stripe thicker than 1px to mark a category; the timeline step and the serial's field already do that work.
- **Don't** put an action in this surface. The console is read-only by design: the terminal acts, the console watches.
- **Don't** let a value break mid-token to fit a column. Widen the track, or give the value its own row.
- **Don't** show a poll as live when it is failing, or an empty page when the data is merely stale.
