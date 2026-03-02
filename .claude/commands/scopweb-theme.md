# /scopweb-theme

Aplica el tema visual de scopweb.com a un proyecto de documentación Starlight.

## Pasos a seguir

### 1. Detectar la estructura del proyecto

Busca el directorio que contiene `astro.config.mjs` con `@astrojs/starlight`. Puede estar en:
- `./website/`
- `./docs/`
- `./docs-website/`
- `./` (raíz)

Lee el `astro.config.mjs` para encontrar la ruta de `customCss`. Si no existe, usa `src/styles/custom.css` por defecto.

### 2. Verificar / actualizar astro.config.mjs

Asegúrate de que tenga `expressiveCode`, `head` (Google Fonts) y `customCss`:

```js
starlight({
  expressiveCode: {
    themes: ['starlight-dark', 'starlight-light'],
  },
  head: [
    {
      tag: 'link',
      attrs: { rel: 'preconnect', href: 'https://fonts.googleapis.com' },
    },
    {
      tag: 'link',
      attrs: { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: true },
    },
    {
      tag: 'link',
      attrs: {
        rel: 'stylesheet',
        href: 'https://fonts.googleapis.com/css2?family=DM+Sans:ital,opsz,wght@0,9..40,300;0,9..40,400;0,9..40,500;0,9..40,600;1,9..40,400&family=Space+Mono:ital,wght@0,400;0,700;1,400&display=swap',
      },
    },
  ],
  // ... resto de la config
  customCss: ['./src/styles/custom.css'],
})
```

Si `head` o `customCss` ya existen, actualízalos en vez de duplicarlos.

### 3. Enlace de vuelta a scopweb.com

Añade un enlace al principio del array `sidebar` para que los usuarios puedan volver a scopweb.com desde cualquier subdominio de documentación:

```js
sidebar: [
  { label: '← scopweb.com', link: 'https://scopweb.com' },
  {
    label: 'Getting Started',
    items: [/* ... */],
  },
  // ... resto del sidebar
]
```

Esto debe ser **siempre el primer elemento** del sidebar. El texto `← scopweb.com` usa la flecha Unicode `\u2190` como indicador visual de "volver".

### 4. Escribir el CSS del tema

Crea o sobreescribe el archivo CSS con exactamente este contenido:

```css
/* Custom CSS — scopweb.com theme para Starlight */

/* ─── Typography ────────────────────────────────────────────────── */
:root {
  --sl-font:        'DM Sans', ui-sans-serif, system-ui, sans-serif;
  --sl-font-system: 'DM Sans', ui-sans-serif, system-ui, sans-serif;
  --font-mono:      'Space Mono', ui-monospace, monospace;
}

body {
  font-family: 'DM Sans', ui-sans-serif, system-ui, sans-serif;
  font-weight: 300;
  line-height: 1.75;
  letter-spacing: 0.01em;
}

/* H1 — Space Mono, terminal feel, full accent */
h1 {
  font-family: 'Space Mono', monospace;
  font-weight: 700;
  letter-spacing: -0.03em;
  line-height: 1.15;
}

/* H2 — Space Mono, slightly lighter, with bottom rule */
h2 {
  font-family: 'Space Mono', monospace;
  font-weight: 400;
  letter-spacing: -0.01em;
  line-height: 1.3;
}

/* H3-H6 — DM Sans, semi-bold, clean */
h3, h4, h5, h6 {
  font-family: 'DM Sans', sans-serif;
  font-weight: 600;
  letter-spacing: 0;
  line-height: 1.4;
}

/* Sidebar group labels — Space Mono uppercase */
.group-label span,
[data-open-class] .group-label span {
  font-family: 'Space Mono', monospace;
  font-size: 0.65rem;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

/* Sidebar nav links — DM Sans */
.sidebar-content a,
starlight-menu-button,
nav a {
  font-family: 'DM Sans', sans-serif;
  font-weight: 400;
}

/* Site title / logo */
.site-title {
  font-family: 'Space Mono', monospace;
  font-weight: 700;
  letter-spacing: -0.02em;
}

/* Hero tagline */
.hero .tagline {
  font-family: 'DM Sans', sans-serif;
  font-weight: 300;
  letter-spacing: 0.02em;
}

/* Inline code — Space Mono */
code:not(pre code) {
  font-family: 'Space Mono', monospace;
  font-size: 0.82em;
}

/* ─── Dark theme ──────────────────────────────────────────────── */
:root,
:root[data-theme='dark'] {
  --sl-color-accent-low:  #0f1e08;
  --sl-color-accent:      #7ec832;
  --sl-color-accent-high: #d4e8b8;

  --sl-color-white: #edf5e1;
  --sl-color-black: #080c05;

  --sl-color-gray-1: #edf5e1;
  --sl-color-gray-2: #d4e8b8;
  --sl-color-gray-3: #7a9960;
  --sl-color-gray-4: #3d5c28;
  --sl-color-gray-5: #1e2e12;
  --sl-color-gray-6: #111a0a;
  --sl-color-gray-7: #0d1209;

  --sl-color-bg:             #080c05;
  --sl-color-bg-nav:         #0d1209;
  --sl-color-bg-sidebar:     #0d1209;
  --sl-color-bg-inline-code: #111a0a;

  --sl-color-hairline-light: rgba(85, 163, 0, 0.20);
  --sl-color-hairline:       rgba(85, 163, 0, 0.12);

  --sl-color-text:        #d4e8b8;
  --sl-color-text-accent: #7ec832;

  --sl-color-green-low:  #0f2a08;
  --sl-color-green:      #55a300;
  --sl-color-green-high: #7ec832;
}

/* ─── Light theme ──────────────────────────────────────────────── */
:root[data-theme='light'] {
  --sl-color-accent-low:  #e2f0c8;
  --sl-color-accent:      #3d7100;
  --sl-color-accent-high: #1a3500;

  --sl-color-white: #0d1f06;
  --sl-color-black: #f9fdf4;

  --sl-color-gray-1: #1a2f10;
  --sl-color-gray-2: #2d4a1a;
  --sl-color-gray-3: #3d7100;
  --sl-color-gray-4: #557a20;
  --sl-color-gray-5: #7a9960;
  --sl-color-gray-6: #c8dfa8;
  --sl-color-gray-7: #e8f5d0;

  --sl-color-bg:             #f9fdf4;
  --sl-color-bg-nav:         #f0f8e8;
  --sl-color-bg-sidebar:     #f0f8e8;
  --sl-color-bg-inline-code: #e8f5d0;

  --sl-color-hairline-light: rgba(61, 113, 0, 0.15);
  --sl-color-hairline:       rgba(61, 113, 0, 0.10);

  --sl-color-text:        #1a2f10;
  --sl-color-text-accent: #3d7100;

  --sl-color-green-low:  #e2f0c8;
  --sl-color-green:      #3d7100;
  --sl-color-green-high: #1a3500;
}

/* ─── Heading colors ─────────────────────────────────────────── */
:root h1,
:root[data-theme='dark'] h1 {
  color: #7ec832;
}

:root h2,
:root[data-theme='dark'] h2 {
  color: #edf5e1;
  padding-bottom: 0.35em;
  border-bottom: 1px solid rgba(85, 163, 0, 0.20);
}

:root h3, :root h4, :root h5, :root h6,
:root[data-theme='dark'] h3,
:root[data-theme='dark'] h4,
:root[data-theme='dark'] h5,
:root[data-theme='dark'] h6 {
  color: #d4e8b8;
}

:root[data-theme='light'] h1 { color: #1a3500; }
:root[data-theme='light'] h2 {
  color: #2d5200;
  border-bottom-color: rgba(61, 113, 0, 0.15);
}
:root[data-theme='light'] h3,
:root[data-theme='light'] h4,
:root[data-theme='light'] h5,
:root[data-theme='light'] h6 { color: #3d5200; }

/* ─── Background decoration ─────────────────────────────────── */
body::before {
  content: '';
  position: fixed;
  inset: 0;
  background-image:
    linear-gradient(rgba(85, 163, 0, 0.04) 1px, transparent 1px),
    linear-gradient(90deg, rgba(85, 163, 0, 0.04) 1px, transparent 1px);
  background-size: 48px 48px;
  pointer-events: none;
  z-index: 0;
}

body::after {
  content: '';
  position: fixed;
  top: -200px;
  left: 50%;
  transform: translateX(-50%);
  width: 800px;
  height: 500px;
  background: radial-gradient(ellipse, rgba(61, 113, 0, 0.18) 0%, transparent 70%);
  pointer-events: none;
  z-index: 0;
}

:root[data-theme='light'] body::before,
:root[data-theme='light'] body::after {
  content: none;
}

/* ─── Code blocks (Expressive Code) ─────────────────────────── */
:root .expressive-code,
:root[data-theme='dark'] .expressive-code {
  --ec-frm-trmTtlBarBg:       #111a0a;
  --ec-frm-trmTtlBarBrdBtm:   rgba(85, 163, 0, 0.25);
  --ec-frm-edTabBarBg:        #111a0a;
  --ec-frm-edTabBarBrdBtm:    rgba(85, 163, 0, 0.25);
  --ec-frm-frameBoxShdCssVal: 0 0 0 1px rgba(85, 163, 0, 0.30);
}

:root[data-theme='light'] .expressive-code {
  --ec-frm-trmTtlBarBg:       #d4e8b8;
  --ec-frm-trmTtlBarBrdBtm:   rgba(61, 113, 0, 0.20);
  --ec-frm-edTabBarBg:        #d4e8b8;
  --ec-frm-edTabBarBrdBtm:    rgba(61, 113, 0, 0.20);
  --ec-frm-frameBoxShdCssVal: 0 0 0 1px rgba(61, 113, 0, 0.25);
}

:root .expressive-code pre,
:root[data-theme='dark'] .expressive-code pre {
  background-color: #091208 !important;
  font-family: 'Space Mono', monospace !important;
  font-size: 0.82rem !important;
  line-height: 1.7 !important;
}

:root[data-theme='light'] .expressive-code pre {
  background-color: #eaf5d8 !important;
}

:root .expressive-code .frame .header,
:root[data-theme='dark'] .expressive-code .frame .header {
  background-color: #111a0a !important;
  border-bottom-color: rgba(85, 163, 0, 0.25) !important;
}

:root[data-theme='light'] .expressive-code .frame .header {
  background-color: #d4e8b8 !important;
  border-bottom-color: rgba(61, 113, 0, 0.20) !important;
}

:root .expressive-code .frame,
:root[data-theme='dark'] .expressive-code .frame {
  border-color: rgba(85, 163, 0, 0.30) !important;
  box-shadow: 0 0 0 1px rgba(85, 163, 0, 0.30) !important;
}

:root[data-theme='light'] .expressive-code .frame {
  border-color: rgba(61, 113, 0, 0.25) !important;
  box-shadow: 0 0 0 1px rgba(61, 113, 0, 0.25) !important;
}

/* ─── Callout "tip" ──────────────────────────────────────────── */
:root .starlight-aside--tip,
:root[data-theme='dark'] .starlight-aside--tip {
  --sl-color-asides-text-accent: #7ec832;
  --sl-color-asides-border: #3d5c28;
  background-color: #0f2a08;
  border-inline-start-color: #55a300;
}

:root[data-theme='light'] .starlight-aside--tip {
  --sl-color-asides-text-accent: #2d5200;
  --sl-color-asides-border: #c8dfa8;
  background-color: #e2f0c8;
  border-inline-start-color: #3d7100;
}

/* ─── Utility components ─────────────────────────────────────── */
.badge {
  display: inline-block;
  padding: 0.25rem 0.5rem;
  border-radius: 0.25rem;
  font-family: 'Space Mono', monospace;
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.04em;
}

.badge-new {
  background-color: #55a300;
  color: #edf5e1;
}

.feature-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
  gap: 1.5rem;
  margin: 2rem 0;
}

.metrics-value {
  font-family: 'Space Mono', monospace;
  font-size: 2rem;
  font-weight: 700;
  color: var(--sl-color-accent);
  letter-spacing: -0.02em;
}
```

### 5. Verificar el build

Desde el directorio del proyecto Starlight, ejecuta:

```bash
npm run build
```

Si el build es exitoso, confirma al usuario que el tema se aplicó correctamente.

### 6. Informar al usuario

Muestra un resumen con:
- Archivos modificados (`astro.config.mjs` y el CSS)
- Tipografía aplicada: **Space Mono** (H1, H2, sidebar grupos, logo, badges) + **DM Sans** (body, H3-H6, nav)
- Colores principales: bg `#080c05`, text `#d4e8b8`, accent `#7ec832`, green `#55a300`
- WCAG dark mode: texto ~15:1 (AAA), acento ~9.9:1 (AAA)
- El grid de fondo no aparece en light mode (diseño intencional)

### 7. Logotipo

El logotipo de scopweb se usa como imagen hero en la landing page (`index.mdx`).

**Archivos del logo:**

| Archivo | Tamaño | Uso |
|---------|--------|-----|
| `src/assets/scopweb.png` | 137×137 px | Hero image en `index.mdx` (versión optimizada) |
| `scopweb.png` (raíz docs) | 273×273 px | Original cuadrado |
| `scopweb2.png` | 832×882 px | Versión grande (alta resolución) |
| `scopweb3.png` / `src/assets/scopweb3.png` | 396×684 px | Versión vertical |

**Configuración en `index.mdx`:**
```yaml
hero:
  image:
    file: ../../assets/scopweb.png
```

**CSS para el logo hero** (en `custom.css`):
```css
/* Hero image — keep logo compact */
.hero img {
  max-width: 140px;
  max-height: 140px;
  width: auto;
  height: auto;
}
```

El logo se renderiza a **140×140 px máximo** en el hero, independientemente del tamaño original del archivo. Si se necesita cambiar la imagen, reemplazar `src/assets/scopweb.png` manteniendo formato PNG y proporción cuadrada. Para Starlight, también se puede configurar un logo en el sidebar/header vía la opción `logo` de `starlight()` en `astro.config.mjs`:

```js
starlight({
  logo: {
    src: './src/assets/scopweb.png',
    alt: 'scopweb',
  },
  // ...
})
```

## Notas técnicas

- El tema es **solo para Starlight** (Astro). No aplica a Docusaurus, VitePress u otros.
- Las fuentes se cargan vía **Google Fonts** (`DM Sans` + `Space Mono`) usando el array `head` de Starlight — sin componente personalizado.
- **Jerarquía tipográfica:** H1/H2 en Space Mono (terminal/técnico) + H3-H6 y body en DM Sans (legible/limpio). El contraste refuerza el aesthetic green-on-dark de scopweb.com.
- Los colores son **idénticos a scopweb.com**: `--bg: #080c05`, `--text: #d4e8b8`, `--green: #55a300`, `--green-light: #7ec832`.
- WCAG ratios en dark mode: texto cuerpo ~15:1 (AAA), acento ~9.9:1 (AAA), headings ~6.6:1 (AA).
