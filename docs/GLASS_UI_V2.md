# Reverie Glass v2

## Owner-confirmed direction

- macOS desktop application structure, with an application wallpaper rather than
  transparency into the real desktop.
- Pink/white sakura wallpaper generated for this project.
- Glass v2 enabled by default; Classic remains selectable.
- Follow Apple's material layering: lighter navigation/control glass, thicker
  translucent reading materials for writing and tables.

Apple's Materials HIG explicitly separates Liquid Glass controls/navigation from
standard materials in the content layer. The owner confirmed this interpretation
after that distinction was explained. Reference:
https://developer.apple.com/design/human-interface-guidelines/materials
and https://developer.apple.com/design/human-interface-guidelines/designing-for-macos.
This React/CSS implementation is a cross-platform interpretation, not native
AppKit Liquid Glass or a pixel-identical copy of a particular Apple application.
No nonfunctional macOS traffic-light buttons or Apple-owned artwork are added.

## Structure and materials

A generated wallpaper extends behind a floating navigation panel and a rounded
content window. The inset toolbar floats above the reading material. System
fonts, compact sidebar hierarchy, pill primary actions, restrained separators and
layered highlights give writing and data a clear place in the hierarchy.
Home, templates, documents, all five database views, project tools, settings and
native app dialogs share the tokens. Preview data and frequent keyboard actions
stay still; existing 120ms press/color and 250ms occasional dialog tokens remain.
No animation library or continuous wallpaper motion is added.

Appearance is available from the toolbar or Settings:

- Glass / Classic;
- Sakura / Mist wallpaper;
- Clear / Balanced / Frosted material strength.

The reading layer stays thicker than navigation even in Clear. Dark mode adds a
wallpaper dimming layer and dark reading materials. System Reduce Transparency,
Increase Contrast and unsupported blur fall back to solid surfaces; Reduce Motion
keeps keyboard/dialog operation immediate. Mobile uses the existing modal drawer,
safe areas, readable input sizes and a compact toolbar without horizontal overflow.

## Portable settings boundary

`GET /api/appearance` returns validated defaults without writing.
`PUT /api/appearance` accepts only style/wallpaper/material/version, rejects unknown
fields/values/duplicate JSON, and performs a version-checked transaction plus
readback. Stale windows must explicitly reload. Errors retain the prior active
appearance. One namespaced app_state key prevents preference updates from
clobbering favorites/recent visits/import receipts. Settings survive SQLite
restart and full workspace archive recovery. Existing Light/Dark remains the
current browser's theme preference; this increment does not silently migrate it.
No remote image service or account is required at runtime.

## Generated wallpaper provenance

Built-in image generation was used. The original image remains in the generating
session; the optimized application asset is
`src/frontend/public/wallpapers/sakura.webp` (approximately 53 KB). Conversion to
WebP changes delivery encoding, not composition. No Apple wallpaper was copied.

Prompt:

> Use case: stylized-concept. Asset type: a finished wallpaper for Reverie Planner, a macOS-inspired translucent productivity workspace. Generate a single elegant 16:9 landscape wallpaper, 1920x1080 or higher. Primary request: pink-white sakura themed wallpaper with very low visual noise, suitable behind a full frosted-glass writing and database interface. Scene: airy luminous pearl-white atmosphere with delicate pale blush sakura petals and a few soft-focus cherry blossom branches near the far edges, abundant calm empty space through the entire middle and left where application text will appear. Style: refined photographic botanical abstraction with soft optical depth, diffused daylight and smooth pearlescent blush gradients, restrained and sophisticated. Palette: warm ivory, pale rose, dusty pink and a hint of cool lavender shadow. Keep petals subtle and sparse; avoid dark branches crossing the center, strong contrast, busy repeated patterns, landscapes or ornate decor. No people, no text, no logos, no UI, no watermark, no Apple logos or copied Apple wallpaper artwork. Wallpaper should feel quietly beautiful behind clear glass and remain usable when cropped for narrow screens.

## Verification

New Go regressions cover no-write defaults, CAS/no-op/stale rejection, transaction
readback failure, restart/archive persistence and unrelated state/provider byte
preservation. New browser regressions cover wallpaper delivery, Classic/Glass and
material persistence across contexts, error/reload behavior, narrow keyboard focus,
solid-material preferences and saved writing/database screenshots in both themes.
Execution evidence belongs in DEVELOPMENT_PLAN.md at the exact tested source SHA.
Existing regression assertions stay in place. Screenshots and automated checks do
not establish manual hardware or assistive-technology certification.
