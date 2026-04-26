# Openzro · Design Tokens

Sistema de cores e tipografia do Openzro. Cole isso no Claude Code (ou em qualquer
projeto) como referência. Tudo deriva da paleta violeta + ink (preto-violeta).

---

## 🎨 Paleta — Violeta (primária)

Escala completa, do mais claro ao mais escuro:

| Token              | HEX       | RGB                | Uso recomendado                       |
| ------------------ | --------- | ------------------ | ------------------------------------- |
| `--oz-violet-50`   | `#f5f3ff` | rgb(245, 243, 255) | Backgrounds suaves, cards             |
| `--oz-violet-100`  | `#ede9fe` | rgb(237, 233, 254) | Backgrounds de seções, hover sutil    |
| `--oz-violet-200`  | `#ddd6fe` | rgb(221, 214, 254) | Borders suaves, separadores           |
| `--oz-violet-300`  | `#c4b5fd` | rgb(196, 181, 253) | Texto secundário sobre dark, acentos  |
| `--oz-violet-400`  | `#a78bfa` | rgb(167, 139, 250) | Hover states, ícones secundários      |
| `--oz-violet-500`  | `#8b5cf6` | rgb(139, 92, 246)  | **Primária** — botões, links, acentos |
| `--oz-violet-600`  | `#7c3aed` | rgb(124, 58, 237)  | Primária hover, CTAs principais       |
| `--oz-violet-700`  | `#6d28d9` | rgb(109, 40, 217)  | Primária pressed, headings sobre claro |
| `--oz-violet-800`  | `#5b21b6` | rgb(91, 33, 182)   | Backgrounds escuros violeta           |
| `--oz-violet-900`  | `#4c1d95` | rgb(76, 29, 149)   | Backgrounds mais escuros, sombras     |

## ⚫ Neutros — Ink

| Token         | HEX       | Uso                              |
| ------------- | --------- | -------------------------------- |
| `--oz-ink`    | `#0f0a1f` | Texto principal, fundos escuros  |
| `--oz-ink-2`  | `#1a1330` | Cards em modo dark, surfaces     |
| `--oz-paper`  | `#faf9fc` | Background light com toque quente|

---

## 📐 CSS Variables (cole no `:root`)

```css
:root {
  /* Violet scale */
  --oz-violet-50:  #f5f3ff;
  --oz-violet-100: #ede9fe;
  --oz-violet-200: #ddd6fe;
  --oz-violet-300: #c4b5fd;
  --oz-violet-400: #a78bfa;
  --oz-violet-500: #8b5cf6;
  --oz-violet-600: #7c3aed;
  --oz-violet-700: #6d28d9;
  --oz-violet-800: #5b21b6;
  --oz-violet-900: #4c1d95;

  /* Ink */
  --oz-ink:    #0f0a1f;
  --oz-ink-2:  #1a1330;
  --oz-paper:  #faf9fc;

  /* Semantic aliases */
  --oz-primary:        var(--oz-violet-600);
  --oz-primary-hover:  var(--oz-violet-700);
  --oz-primary-soft:   var(--oz-violet-100);
  --oz-text:           var(--oz-ink);
  --oz-text-muted:     var(--oz-violet-700);
  --oz-bg:             #ffffff;
  --oz-bg-soft:        var(--oz-violet-50);
  --oz-bg-dark:        var(--oz-ink);
  --oz-border:         var(--oz-violet-200);

  /* Type */
  --oz-sans: 'Geist', -apple-system, system-ui, sans-serif;
  --oz-mono: 'JetBrains Mono', ui-monospace, monospace;
}
```

---

## 🎨 Tailwind config (alternativa)

```js
// tailwind.config.js
module.exports = {
  theme: {
    extend: {
      colors: {
        violet: {
          50:  '#f5f3ff',
          100: '#ede9fe',
          200: '#ddd6fe',
          300: '#c4b5fd',
          400: '#a78bfa',
          500: '#8b5cf6',
          600: '#7c3aed',
          700: '#6d28d9',
          800: '#5b21b6',
          900: '#4c1d95',
        },
        ink: {
          DEFAULT: '#0f0a1f',
          2: '#1a1330',
        },
        paper: '#faf9fc',
      },
      fontFamily: {
        sans: ['Geist', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'ui-monospace', 'monospace'],
      },
    },
  },
};
```

---

## ✍️ Tipografia

- **Sans**: [Geist](https://vercel.com/font) — UI, headings, body
- **Mono**: [JetBrains Mono](https://www.jetbrains.com/mono/) — código, dados técnicos, labels

```html
<link href="https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600;700;800&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
```

---

## 📦 Como passar pro Claude Code

Três caminhos, em ordem de preferência:

### 1. Cole este arquivo no projeto
Adicione `design-tokens.md` (este arquivo) na raiz do repo. O Claude Code lê automaticamente arquivos do projeto.

### 2. Crie um `CLAUDE.md` no root do repo
```md
# Openzro

Use as cores e tipografia definidas em `design-tokens.md`.
Primária: violet-600 (#7c3aed). Texto: ink (#0f0a1f).
Sans: Geist. Mono: JetBrains Mono.
```

### 3. Cole as CSS vars direto
Quando pedir uma página/componente novo, cole o bloco `:root { ... }` acima junto com o prompt. O Claude Code vai usar como ground truth.

---

## 🎯 Combinações testadas

- Botão primário: bg `violet-600`, text `#fff`, hover bg `violet-700`
- Card: bg `#fff`, border `violet-200`, shadow `rgba(76,29,149,0.08)`
- Terminal/code block: bg `ink`, text `violet-200`, prompt `violet-400`
- Link: text `violet-600`, hover `violet-700`, underline em hover
- Foco: outline `violet-500` com 2px offset
