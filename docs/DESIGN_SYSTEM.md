# Reverie workspace design system

## Intent

A quiet local workspace for writing, finding and organizing information. Use a frosted structural shell and readable opaque content. The product must work without an account, external fonts, imagery or animation libraries.

## Foundation

`src/frontend/src/App.css` owns reference colors and spacing, semantic theme aliases and component rules. Use the existing semantic tokens rather than new one-off values. Light/dark share structure and type hierarchy. Body uses the platform font; numeric data uses tabular figures. Spacing follows the 4px scale; controls, cards and panels have distinct radii. Blue indicates actions and selection, never decorative urgency.

## Patterns

| Pattern | Purpose and behavior |
| --- | --- |
| Frosted sidebar | Persistent navigation. Home, pages and templates first; secondary planning tools disclosed progressively. Favorites are SQLite-backed. Page expansion and page opening use separate keyboard controls. |
| Workspace toolbar | Sidebar control, meaningful breadcrumb, quick find and theme action. Text truncates before controls shrink. |
| Content paper | Opaque enough to read comfortably. Writing has a bounded measure and an optional full-width layout. |
| Home | Real page/project/task counts, favorite and recently opened pages, all pages and useful creation actions. No fabricated activity or progress. |
| Page cards | Actual workspace pages with clear open actions. Long titles truncate; complete title is the accessible button name. |
| Templates | Five built-in starting points create ordinary editable pages; creation errors preserve the picker and allow retry. Custom templates remain future work. |
| Dialog | Native HTML dialog supplies focus containment, inert background, Escape and focus restoration. Existing database-specific dialogs remain candidates for consolidation. |
| Mobile navigation | Native modal drawer with close button, backdrop and Escape. Automatically closes on successful navigation. |
| Page recovery | Native modal with explicit focus return; immediately rendered for repeated keyboard use. Bounded revision list, opaque readable preview, explicit restore, version-conflict feedback and retained newer writing. Current ownership/location stays authoritative. |
| Editor files | Markdown import appends; Markdown export simplifies rich blocks; JSON export preserves editor block structure. These are single-page transfers, not complete workspace/Notion migration. |
| Database controls | Existing schema/conflict guards stay intact. Controls share tokens and the toolbar has a clear structural boundary. |

## Materials and accessibility

Blur is reserved for navigation and floating surfaces. Content, inputs and database cells remain readable. `prefers-reduced-transparency` and unsupported blur fall back to solid backgrounds. `prefers-contrast: more` strengthens borders and text. Focus rings, named icon controls, skip-to-content and touch sizing ship with the design. This is not a claim of a complete WCAG audit.

## Motion

| Interaction | Purpose | Implementation |
| --- | --- | --- |
| Press | Immediate feedback | Transform scale .98; 120ms strong ease-out |
| Dialog entrance | Spatial consistency | Opacity + scale .96 to 1; 250ms strong ease-out, native starting styles |
| Navigation drawer entrance | Spatial consistency | Transform from the left; 250ms, no animated dimensions |
| Command palette / keyboard dialogs | Frequent keyboard interaction | Immediate; no entrance motion |
| Theme / hover | State indication | Color only; 120ms; pointer hover gated |
| Page/content navigation | Frequent work | Immediate; no stagger, bouncing text or delayed input |

Reduced motion disables spatial transitions and press scaling. No continuous background motion, gesture springs without gestures, `transition: all`, or animated layout dimensions. Exit currently closes immediately; do not advertise animated exit or spring physics. Validate feel on real devices as well as browser automation.

## Verification and remaining work

Browser regressions cover original workflows plus templates, favorites across browser contexts, recent navigation, Markdown roundtrip, retry, focus restoration, narrow windows and theme/reduced-motion screenshots. Backend checks cover persistent, independent favorite/visit writes and deleted-page filtering. CI is the native cross-platform gate.

Full Notion replacement still requires multi-page/attachment import/export, richer templates, cover management, database scale/advanced relations, undo/history and broader accessibility/platform acceptance. Track `PROJECT_DIRECTION.md` and `PRODUCT_PARITY_MATRIX.md`; visual improvement does not close these gates.
