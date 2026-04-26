# Openzro · Brand instructions for Claude Code

You are working on **Openzro**, an open-source zero-trust mesh networking
project (fork of NetBird). Follow the brand and design rules below for
every UI you build — landing pages, dashboards, CLI output, READMEs, etc.

---

## Brand at a glance

- **Name (always):** `openZro` — capital Z in the middle, the rest lowercase.
  Never `Openzro`, `OpenZro`, `OPENZRO`, or `openzro` unless inside a URL,
  package name, or shell command.
- **Icon:** `brand/openzro-icon.svg` — solid violet disc with a fused O+Z
  glyph inside (the disc IS the O; two white sails define the Z). Always
  use this file; do not redraw it inline.
- **Tone:** technical, modern, friendly. Not enterprise-stuffy, not
  cyberpunk-edgy. Think Vercel × Tailscale.

---

## Wordmark rules

- Sans: **Geist** (`font-weight: 600`).
- The middle **Z** is set in `font-weight: 700` and `color: var(--oz-violet-600)`
  on light backgrounds, `var(--oz-violet-400)` on dark. This visually
  echoes the icon's accent. Always render the wordmark as:
  ```html
  <span class="oz-wordmark">open<span class="oz-z">Z</span>ro</span>
  ```
- Letter-spacing: `-0.025em`. Line-height: `1`.
- Minimum size: 14px. Below that, use the icon alone.

---

## Color tokens

Drop this `:root` block into your global stylesheet. Use the semantic
aliases (e.g. `--oz-primary`) wherever possible — they keep theming
swappable.

```css
:root {
  /* Violet scale */
  --oz-violet-50:  #f5f3ff;
  --oz-violet-100: #ede9fe;
  --oz-violet-200: #ddd6fe;
  --oz-violet-300: #c4b5fd;
  --oz-violet-400: #a78bfa;
  --oz-violet-500: #8b5cf6;
  --oz-violet-600: #7c3aed;  /* primary */
  --oz-violet-700: #6d28d9;
  --oz-violet-800: #5b21b6;
  --oz-violet-900: #4c1d95;

  /* Ink (neutrals) */
  --oz-ink:    #0f0a1f;
  --oz-ink-2:  #1a1330;
  --oz-paper:  #faf9fc;

  /* Semantic aliases — prefer these */
  --oz-primary:       var(--oz-violet-600);
  --oz-primary-hover: var(--oz-violet-700);
  --oz-primary-soft:  var(--oz-violet-100);
  --oz-text:          var(--oz-ink);
  --oz-text-muted:    var(--oz-violet-700);
  --oz-bg:            #ffffff;
  --oz-bg-soft:       var(--oz-violet-50);
  --oz-bg-dark:       var(--oz-ink);
  --oz-border:        var(--oz-violet-200);

  /* Type */
  --oz-sans: 'Geist', -apple-system, system-ui, sans-serif;
  --oz-mono: 'JetBrains Mono', ui-monospace, monospace;
}
```

### Combinations that are known-good

| Surface           | Background        | Text                 | Accent              |
| ----------------- | ----------------- | -------------------- | ------------------- |
| Page (light)      | `#fff`            | `--oz-ink`           | `--oz-violet-600`   |
| Page (soft)       | `--oz-violet-50`  | `--oz-ink`           | `--oz-violet-600`   |
| Page (dark)       | `--oz-ink`        | `#fff`               | `--oz-violet-400`   |
| Card              | `#fff`            | `--oz-ink`           | border `--oz-violet-200` |
| Primary button    | `--oz-violet-600` | `#fff`               | hover `--oz-violet-700` |
| Code / terminal   | `--oz-ink`        | `--oz-violet-200`    | prompt `--oz-violet-400` |
| Link              | inherit           | `--oz-violet-600`    | hover underline      |

### Things to avoid

- ❌ Inventing new violets outside this scale.
- ❌ Mixing in other accent colors (no orange CTAs, no green checkmarks).
  Success/error states use violet variants + neutral grays.
- ❌ Glassmorphism, heavy gradients on body backgrounds, or neon glows.
  Subtle gradients are OK on the icon and on hero surfaces only.
- ❌ Drop shadows with non-violet tints. Use `rgba(76, 29, 149, X)`.

---

## Typography

```html
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600;700;800&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
```

- **Geist** for everything UI: headings, body, buttons, labels.
- **JetBrains Mono** for code, CLI output, technical labels (uppercase
  with `letter-spacing: 0.08em` for caps treatment).
- Body size: 16px. Line-height: 1.55.
- Headings: 600 weight, `letter-spacing: -0.02em`, line-height `1.1`.

---

## Components to reuse

If you create reusable components, name them with the `Oz` prefix
(`OzButton`, `OzCard`, `OzTerminal`, etc.) so they're searchable.

### Button (primary)
```css
.oz-btn {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 10px 18px; border-radius: 8px;
  background: var(--oz-primary); color: #fff;
  font-family: var(--oz-sans); font-weight: 600; font-size: 14px;
  border: 0; cursor: pointer;
  transition: background 0.15s;
}
.oz-btn:hover { background: var(--oz-primary-hover); }
.oz-btn:focus-visible { outline: 2px solid var(--oz-violet-500); outline-offset: 2px; }
```

### Wordmark
```css
.oz-wordmark {
  font-family: var(--oz-sans); font-weight: 600;
  letter-spacing: -0.025em; line-height: 1;
  color: var(--oz-text);
}
.oz-wordmark .oz-z { color: var(--oz-primary); font-weight: 700; }
.oz-wordmark--dark { color: #fff; }
.oz-wordmark--dark .oz-z { color: var(--oz-violet-400); }
```

### Lockup (icon + wordmark)
```html
<a class="oz-lockup" href="/">
  <img src="/brand/openzro-icon.svg" width="32" height="32" alt="">
  <span class="oz-wordmark">open<span class="oz-z">Z</span>ro</span>
</a>
```
```css
.oz-lockup { display: inline-flex; align-items: center; gap: 10px; text-decoration: none; }
.oz-lockup .oz-wordmark { font-size: 20px; }
```

---

## When in doubt

- Match the visual style of the assets shipped under `brand/`.
- Don't add extra ornamentation (icons, gradients, decorative SVGs)
  unless the user asks. Less is more.
- All UI copy uses the exact name `openZro` with capital Z.
