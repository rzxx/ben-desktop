# Dynamic theme visual direction

Date: 2026-06-28
Status: product direction with the version 1 theme foundation implemented

This document defines how dynamic color in Ben should look and behave. It is a
visual policy rather than a commitment to one extraction algorithm, color
space, palette format, or UI implementation.

The central decision is **guided fidelity**:

> Ben should feel connected to the current album artwork without allowing the
> artwork to take control of the interface.

Artwork supplies evidence for hue and color relationships. Ben supplies the
surface hierarchy, tonal structure, chroma limits, accessibility constraints,
and interaction semantics. We may restrain or remap a color found in the art,
but we do not invent an unrelated color merely because a generic harmony rule
says that it would work.

---

## Decision summary

- Backgrounds and surfaces are product colors influenced by artwork, not
  sampled pixels from artwork.
- Surface lightness remains stable between albums. Album art may tint a
  surface, but it does not decide how light or dark that surface is.
- Surface chroma is deliberately low and tightly bounded. The application
  should read as calm and coherent before it reads as colorful.
- An accent should normally be visibly present in the album artwork. Small but
  compositionally important colors are valid accents.
- If the artwork has no credible contrasting accent, the theme remains
  monochromatic or single-hue. Ben does not synthesize a complementary color
  just to make the controls pop.
- Light and dark themes express the same artwork identity through different,
  stable tone assignments. Neither mode is an inversion of sampled cover
  colors.
- Accessibility and role clarity are hard constraints. Fidelity never excuses
  unreadable text or an ambiguous control.
- Numeric palettes may exist internally, but product UI consumes semantic
  roles such as surfaces, content, borders, and actions.
- Material Color Utilities is a major technical and architectural reference,
  especially for tonal roles, gamut-safe generation, and contrast constraints.
  Material's generated color relationships are not automatically Ben's visual
  policy.

## Scope

This direction covers artwork-driven application color:

- the application canvas and layered surfaces;
- foreground content and borders on those surfaces;
- playback and selection accents;
- light and dark schemes;
- transitions between recording themes;
- grayscale, single-hue, and multicolor artwork.

It does not require every color in the application to come from artwork.
Semantic colors such as danger, warning, and success belong to the product.
Artwork must not make an error state stop looking like an error state.

This document also does not prescribe exact tone or chroma values. Those are
versioned design parameters to establish through a representative artwork
corpus. The behavioral boundaries in this document are the durable part.

## Vocabulary

Clear names matter because `primary`, `theme`, and `accent` are otherwise easy
to confuse.

### Artwork candidates

Colors observed in the source image, with evidence such as pixel population,
perceptual salience, hue distribution, spatial placement, and contrast with
surrounding colors.

### Atmosphere seed

The artwork-derived color identity used to tint neutral surfaces. It is not a
surface color itself and does not need to appear at full chroma anywhere in the
interface.

This replaces the ambiguous idea that the application's base palette should
pass through a literal `primary` cover color.

### Accent seed

An artwork-derived color suitable for interactive emphasis. It should be
distinct from the atmosphere after both colors have been transformed into
their UI roles.

### Tonal foundation

An internal range generated from a seed under Ben's chroma and gamut policy.
Foundations are ingredients, not component APIs.

### Semantic role

A color with a product purpose, such as canvas, raised surface, primary text,
subtle border, accent action, or content on accent. Components consume roles,
not arbitrary palette positions.

### Faithfulness

Preserving the authored color identity and relationships of the artwork. It
does not mean reproducing source pixels exactly.

## The desired visual character

### Calm structure, changing atmosphere

Changing tracks should change the atmosphere of the application, not make it
feel like an unrelated application each time.

The stable parts are:

- surface lightness and elevation relationships;
- foreground hierarchy;
- border strength;
- control meaning;
- contrast;
- the approximate intensity of color allowed in each role.

The dynamic parts are:

- atmosphere hue when the cover provides one;
- accent hue when the cover provides a credible accent;
- bounded variations in chroma;
- the relationship between atmosphere and accent as authored by the cover.

Color should be noticeable in aggregate and restrained in any individual large
surface. A user should be able to recognize that the theme came from the album
without feeling that the UI has become a blown-up copy of the cover.

### Opinionated restraint is intentional

The most statistically representative cover color is not automatically the
best UI color. The most faithful chroma is not automatically the best UI
chroma. The darkest and lightest cover colors are not appropriate application
backgrounds merely because they are present in the image.

Ben deliberately normalizes those properties:

- Lightness is role-owned. Canvas and surface levels stay at designed tones.
- Chroma is jointly influenced by the source and capped by the role.
- Hue is the part most strongly owned by the artwork.

This makes hue the main carrier of album identity, chroma a bounded expression
of that identity, and tone the main carrier of UI structure and accessibility.

## The faithfulness envelope

Faithfulness is a range of allowed transformations rather than literal color
copying.

### Allowed and expected

- Move an observed color to a designed lightness for a surface or foreground
  role.
- Reduce chroma to keep surfaces calm and themes consistent.
- Generate lighter and darker tones around an observed hue.
- Make small hue or chroma corrections required for gamut-safe rendering.
- Reject a highly populated candidate when it is unsuitable for UI use, such
  as photographic near-black, near-white, skin tone noise, or an extreme neon
  outlier.
- Choose a smaller color region as the accent when its visual placement,
  isolation, repetition, or contrast makes it compositionally important.
- Use neutral product colors when the artwork itself is neutral.

### Allowed with caution

- Slightly shift a source hue when the unmodified hue produces unstable
  perceived chroma across the required tone range.
- Reduce the distinction between atmosphere and accent when preserving the
  artwork relationship is more important than manufacturing stronger UI
  contrast.
- Fall back from a rejected accent candidate to the atmosphere hue, provided
  interactive roles remain clear through tone, shape, and placement.

These transformations should be bounded and explainable. They must not become
a hidden route for inventing a new color family.

### Not allowed

- Generate a complementary, analogous, or rotated hue that has no credible
  presence in the artwork merely because color theory predicts harmony.
- Force every cover into a two-hue theme.
- Add a brand accent to grayscale artwork by default.
- Use exact sampled whites or blacks as application surfaces.
- Allow a tiny compression artifact or incidental pixel region to dominate the
  theme because it has high chroma.
- Preserve source chroma when it makes a large surface loud, inconsistent, or
  uncomfortable.
- Recolor semantic danger, warning, or success roles until their meaning is
  weakened.

## Interpreting artwork

### Evidence is broader than population

Population is useful for finding the atmosphere of a cover, but it is a weak
definition of an accent. Human visual salience also depends on:

- contrast against neighboring colors;
- isolation;
- centrality and placement;
- repetition;
- use in typography or a logo;
- chroma relative to the rest of the image;
- the amount of hue diversity in the image.

A small orange title on a blue cover can be the obvious accent despite having
very low pixel population. Conversely, a saturated strip at the edge of a
photograph may be statistically significant but compositionally irrelevant.

The implementation should therefore treat population as evidence, not truth.

### Atmosphere selection

The atmosphere seed should represent the broad identity of the artwork after
discounting unusable extremes. It may be colorful, muted, or neutral.

It does not need to be the most exciting color in the cover. Its purpose is to
provide a coherent tint for large, quiet areas after strong chroma reduction.

When a cover is predominantly neutral with a small colored highlight, a
neutral atmosphere plus a colored accent is a valid and often preferable
interpretation.

### Accent selection

The accent is not simply the second-ranked palette color. It has a distinct
job: provide artwork-faithful emphasis relative to the resolved surfaces.

A credible accent:

- has observable support in the source;
- remains perceptually distinct from the atmosphere after UI transformations;
- can produce usable action and on-action roles;
- is not an incidental artifact;
- does not require excessive chroma to remain visible.

There may be several equally defensible accents. The goal is not to reproduce a
single human-selected hex value. The goal is to avoid choices that feel clearly
less intentional than another available source color.

Hue-distance preferences should be broad. There is no universal ideal such as
an 88-degree separation. Related, split-complementary, and opposing colors can
all be intentional. Very small hue differences generally provide less accent
value, but even that is acceptable for deliberately single-hue artwork.

### When no accent exists

The fallback is faithful:

- Grayscale artwork produces a neutral scheme and neutral emphasis.
- Single-hue colorful artwork uses that hue family with tone and restrained
  chroma differences.
- Muted artwork remains muted.
- A cover with no usable candidate does not receive an invented complementary
  or brand color as an artwork-derived accent.

Controls must still be discoverable through tone, contrast, shape, iconography,
and state. Dynamic hue is not the only available form of emphasis.

## Artwork classes

Classification is useful because these cases express different artistic
intent.

### Grayscale

The source has no meaningful chromatic evidence. Both atmosphere and accent
remain neutral. This is a first-class result, not a failed extraction.

Pure black, pure white, and black-and-white covers must be handled explicitly;
they must not fall through to an accidental generic theme.

### Chromatic single-hue

The source contains color but little meaningful hue diversity. Ben preserves
the hue family. Accent emphasis comes from role tone, local contrast, and a
bounded chroma difference rather than a synthesized second hue.

### Multicolor with a credible accent

The atmosphere and accent use distinct colors supported by the artwork. The
selected relationship should resemble the authored relationship after Ben's
normalization.

### Multicolor without a credible accent

Multiple candidates exist, but none is sufficiently salient, stable, or useful
as an accent. Ben falls back to the atmosphere family instead of promoting a
weak candidate or inventing a hue.

## Surface policy

Surfaces belong primarily to Ben's design system.

### Stable tone hierarchy

The future design should define several fixed surface levels for each mode,
for example:

- canvas;
- low or recessed surface;
- default surface;
- raised surface;
- overlay or highest surface.

Exact tones are design parameters, but their ordering and approximate visual
distance must remain consistent between albums. Artwork does not make one
album's dark canvas nearly black and another album's dark canvas mid-gray.

Surface separation should primarily come from tone. Borders and shadows may
support the hierarchy, but dynamic chroma should not be required to explain
elevation.

### Strict chroma budget

Large surfaces use a narrow, low chroma range. The limit should be strict
enough that switching from a muted cover to a saturated cover does not change
the apparent loudness of the entire application.

Source chroma may reduce surface chroma further, including all the way to
neutral. Source chroma must not raise it beyond the surface role's budget.

Neutral-variant foundations may be slightly more chromatic than canvas
foundations for borders, selected surfaces, or subtle grouping, but they remain
background colors rather than accents.

### Light and dark are separately resolved

Light and dark schemes share source identities but have independent role
assignments. Dark mode is not a reversed light palette.

Both modes should preserve:

- the same atmosphere family;
- the same accent identity when one exists;
- comparable perceived restraint;
- equivalent semantic contrast.

Mode-specific chroma limits are acceptable. In particular, bright accents on
dark surfaces often need stronger restraint to avoid a neon appearance.

## Accent policy

Accent roles may be more colorful than surfaces, but they still belong to a
designed system.

- Accent chroma has a moderate, role-specific ceiling.
- A saturated cover does not automatically produce a maximally saturated UI
  accent.
- A muted cover may produce a muted accent if increasing chroma would misstate
  its identity.
- The same accent seed can produce distinct rest, hover, pressed, subtle, and
  on-accent roles through controlled tone and chroma changes.
- Interactive contrast is guaranteed by role resolution, not by hoping that
  two extracted swatches contrast sufficiently.

The accent should pop relative to the restrained surface system, not
necessarily relative to the raw primary cover swatch. Keeping surfaces calm
means an accent can remain moderate and still be effective.

## Product-owned semantic colors

Some meanings are more important than album fidelity.

- Danger remains recognizably dangerous.
- Warning remains recognizably cautionary.
- Success remains recognizably successful.
- Focus remains visible.
- Disabled state remains distinguishable from enabled state without suggesting
  an unrelated semantic meaning.

These families may be fixed product colors or cautiously harmonized, but their
meaning and contrast are never delegated to artwork. They should not disappear
on a red, yellow, green, or grayscale cover.

## Semantic consumption

Components should express intent rather than palette arithmetic.

Preferred concepts include:

- `surface-canvas`, `surface-recessed`, `surface-default`, `surface-raised`,
  and `surface-overlay`;
- `content-primary`, `content-secondary`, `content-disabled`, and
  `content-inverse`;
- `border-subtle`, `border-default`, `border-strong`, and `focus-ring`;
- `accent`, `accent-hover`, `accent-pressed`, `accent-subtle`, and
  `on-accent`;
- product-owned semantic action roles.

Names and the final number of levels will follow the redesign. The durable rule
is that a component asks for a purpose, never for `theme-700` or `accent-200`.

Internal tonal foundations remain useful. They provide constrained defaults,
intermediate values, and a stable way to derive roles. They simply stop being
the public styling language of the application.

## Decision priority

When goals conflict, decisions follow this order:

1. **Meaning and accessibility.** Text, controls, focus, and semantic states
   must work.
2. **No unsupported invention.** Dynamic hues require credible source evidence.
3. **Product consistency and comfort.** Surface structure and chroma restraint
   remain stable.
4. **Artwork identity.** Preserve the source's recognizable hue relationships
   within those constraints.
5. **Accent effectiveness.** Prefer a source-supported color that provides
   clear emphasis.
6. **Literal source accuracy.** Exact pixel values and source extremes are the
   lowest priority.

This ordering explains why reducing chroma is acceptable, while generating an
absent complement is not. One preserves identity under product constraints;
the other replaces part of the identity.

## Representative outcomes

These examples are normative illustrations of the desired behavior.

### Black-and-white cover

- Neutral light and dark surface systems.
- Neutral accent/action treatment with adequate tonal emphasis.
- No injected blue, purple, or complementary brand color.

### Saturated blue cover with a small orange title

- Very subtly blue-tinted surfaces.
- Orange may be selected as the accent despite low population when it is
  spatially and compositionally salient.
- Both colors are reduced to their role chroma budgets.

### Blue cover containing only blue shades

- Blue-tinted neutral surfaces.
- Blue-family accent roles separated by tone and bounded chroma.
- No synthesized orange complement.

### Predominantly gray cover with one deliberate red mark

- Neutral atmosphere and surfaces.
- Red accent if the mark has credible visual salience.
- Red semantic danger remains separately resolved so accent and error are not
  accidentally interchangeable.

### Highly saturated multicolor cover

- Atmosphere and accent selected from the artwork.
- Large surfaces remain calm through the same strict chroma budget used for
  other albums.
- Accent intensity is capped; the UI does not attempt to reproduce the full
  saturation of the art.

### Muted sepia cover

- Muted warm-neutral surfaces.
- Muted source-supported emphasis or a single-family fallback.
- No saturation boost solely to make the theme look more dynamic.

## Transitions

Theme transitions should reinforce continuity rather than call attention to
the mechanics of palette replacement.

- Semantic roles transition together over a short, consistent duration.
- Surfaces, content, borders, and accents should not visibly update in separate
  phases.
- Intermediate colors must remain usable; a transition must not pass through
  unreadable foreground/background combinations when practical to avoid.
- Motion and color animation respect reduced-motion preferences.
- Rapid track changes must not briefly apply a stale recording's theme.
- A failed extraction resolves to a faithful neutral fallback, not a flash of
  an unrelated color.

The use of CSS custom properties is compatible with this direction. The
important change is to transition semantic color roles rather than exposing
and animating raw palette positions.

## Evaluation

No generic algorithm can prove subjective taste on arbitrary artwork. We need
a repeatable visual evaluation process rather than tuning against the most
recent failure.

### Reference corpus

Maintain a versioned set of representative covers including:

- pure black, pure white, grayscale, and high-contrast monochrome;
- chromatic single-hue covers;
- muted and sepia covers;
- highly saturated covers;
- covers with tiny but obvious accent typography or marks;
- photographs with skin, sky, vegetation, and edge artifacts;
- covers with two or more plausible accents;
- covers where no accent should be chosen.

The corpus should include real failure cases encountered during normal use.

### Review artifact

Generate a contact sheet for each candidate implementation showing:

- source cover;
- observed candidate colors and their evidence;
- selected atmosphere and accent seeds;
- light and dark surface levels;
- representative text, border, button, slider, selection, and error roles;
- measured contrast and actual rendered chroma.

Reviewers should compare complete schemes rather than isolated swatches.

### Questions for visual review

- Does the theme unmistakably belong to the cover?
- Are the surfaces calm enough for extended use?
- Is one album substantially louder than another without a good artistic
  reason?
- Is the chosen accent supported by the artwork?
- Was a more compositionally obvious accent ignored?
- Does monochrome art remain intentionally monochrome?
- Does either mode look neon, muddy, or unexpectedly colorful?
- Are surface levels and foreground hierarchy immediately understandable?
- Do semantic states retain their meaning?

### Measurable invariants

The implementation should additionally track:

- zero required contrast failures;
- zero unmanaged out-of-gamut channel clipping;
- stable surface tones across the corpus;
- bounded surface and accent chroma distributions;
- selected dynamic hues backed by source evidence;
- monochrome preservation rate;
- stale-theme and fallback behavior during rapid changes.

Metrics constrain bad outcomes. Pairwise human review decides between multiple
valid outcomes.

## Architecture implied by the visual direction

The visual policy implies four separate responsibilities:

```text
artwork observation
  -> seed selection
  -> Ben scheme policy
  -> semantic role resolution
```

### Artwork observation

Reports what exists without applying product taste: candidate colors,
population, hue distribution, chroma, tone, spatial evidence, and artwork
class.

### Seed selection

Chooses atmosphere and, when supported, accent evidence. Selection can improve
independently from the design system.

### Ben scheme policy

Encodes the product's opinionated tone ladders, chroma budgets, faithful
fallback, and light/dark character.

### Semantic role resolution

Produces accessible component-facing colors and enforces contrast, tone
separation, gamut safety, and state relationships.

This separation is more important than whether the implementation uses OKLab,
OKLCH, HCT, Celebi quantization, or another well-tested technique. Those choices
should serve the policy rather than define it accidentally.

## Relationship to the current implementation

The current implementation already establishes several parts of this taste:

- restrained light and dark anchors;
- a tinted neutral-like base scale;
- a separate accent scale;
- explicit low-chroma monochrome behavior;
- deterministic extraction and bounded defaults.

Those decisions should be preserved as intent even if their implementation is
replaced.

The main limitations relative to this direction are:

- observation, seed selection, and style policy are interleaved;
- accent choice contains a narrow preferred hue relationship;
- chroma can become stronger and less consistent than desired;
- gamut handling can alter requested colors through channel clipping;
- pure black and pure white artwork are not first-class successful outcomes;
- components consume numeric palette positions rather than semantic roles;
- role contrast is not generated and tested as a complete scheme.

## Relationship to Material Color Utilities

[Material Color Utilities](https://github.com/material-foundation/material-color-utilities)
is the strongest reference for the structure of a dynamic color system. Ben
should learn from its separation of source colors, tonal foundations, scheme
variants, semantic roles, gamut-safe color generation, contrast curves, and
tone-delta constraints.

Ben does not adopt all Material visual decisions:

- Material variants may derive secondary or tertiary hues through rotations,
  analogous relationships, or complements. Ben requires artwork evidence for
  an artwork-derived accent.
- Material's chroma targets are not Ben's targets. Ben prefers calmer surfaces
  and moderately restrained accents.
- Material component roles and elevation model do not define Ben's future
  component vocabulary.

The goal is a Ben scheme with Material-grade rigor, not a Material-looking
application.

## Implementation direction

Future work should proceed in this order:

1. Establish the artwork corpus and contact-sheet comparison workflow.
2. Make atmosphere, accent, and artwork classification explicit outputs.
3. Fix faithful handling of black, white, grayscale, and unusable inputs.
4. Separate Ben's tone and chroma policy from source selection.
5. Add gamut-safe tonal generation and scheme-level contrast constraints.
6. Produce complete light and dark semantic role sets.
7. Migrate components from numeric palettes to semantic roles.
8. Add coordinated role transitions.
9. Compare alternative quantization and seed-selection algorithms against the
   corpus rather than adopting one by reputation alone.

Exact parameter tuning remains implementation work. Any implementation is
acceptable only if it satisfies the taste contract and decision priority in
this document.

## Implemented theme contract

The version 1 implementation follows the four-stage architecture above and
returns a versioned dynamic theme rather than a public numeric palette.

### Artwork evidence

The extractor observes all visible pixels, including exact black and white.
It reports:

- `neutral`, `single-hue`, or `multicolor` artwork classification;
- an atmosphere seed;
- an optional source-supported accent seed;
- a bounded list of candidates with population share and spatial centrality.

Population, usable tone, chroma, and centrality contribute independently to
selection. Accent selection has no preferred harmony angle and never rotates
or synthesizes a hue. Neutral and single-hue results intentionally omit an
accent seed; the scheme resolver then uses a same-family tonal fallback.

### Semantic schemes

The backend resolves complete light and dark schemes. Version 1 exposes these
component-facing groups:

- five surfaces: canvas, recessed, default, raised, and overlay;
- primary, secondary, disabled, and inverse content;
- subtle, default, and strong borders plus a focus ring;
- default, hover, pressed, subtle, and on-accent action roles;
- product-owned danger, warning, and success roles with subtle and on-color
  variants.

Surface and content tones are product-owned and stable between artworks.
Artwork controls hue and can lower chroma, while strict per-role budgets cap
it. On-accent and semantic on-colors are chosen against their resolved
backgrounds. The contract tests enforce text, interaction, focus, surface,
monochrome, source-fidelity, and gamut invariants.

### Gamut and browser application

Requested OKLCH colors are mapped into sRGB by reducing chroma while preserving
tone and hue. Raw RGB channel clipping is not used as the gamut strategy.

The browser publishes the active mode as semantic custom properties. Those
properties are registered as CSS colors when the runtime supports it and are
updated together, enabling coordinated interpolation. Reduced-motion settings
disable the transition. A version mismatch or extraction failure clears the
dynamic values and exposes the neutral product fallback.

### Temporary compatibility boundary

The returned theme contains a nested `compatibility` palette solely for the
existing pre-redesign UI. The runtime republishes it as the old
`theme-{tone}` and `accent-{tone}` variables so the theme-system replacement
does not require an unrelated component redesign.

This bridge is not part of the future component API. New and redesigned UI
must consume semantic roles. Removing the compatibility field becomes a
mechanical cleanup after numeric palette usage reaches zero.

### Work intentionally left outside this change

- Redesigning existing components onto the new roles.
- Choosing final visual parameters from a real, versioned album corpus.
- Building the contact-sheet review artifact for that corpus.
- Replacing the quantizer or adding richer typography/logo recognition before
  comparative evidence shows that it improves selection.

These are evaluation and UI-design tasks, not reasons to preserve the old
palette-first architecture.

## Durable acceptance statement

A successful Ben theme does not reproduce an album cover and does not decorate
the application with colors invented from it. It recognizes the artwork's
color identity, preserves its restraint or absence of color, and expresses it
through Ben's stable surfaces and controls.

The result should feel authored by both the album and the application: the
album chooses the atmosphere; Ben decides how an interface is allowed to use
it.
