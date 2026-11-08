---
name: mermaid
description: Draw Mermaid diagrams that read well (ER diagrams, flowcharts, sequence, class and state diagrams), and style their nodes with classes and theme colors whose text always contrasts with its background, light or dark, following Material 3's color roles
tags: [mermaid, diagrams, er-diagram, flowchart, documentation, accessibility]
version: "1.0.0"
author: "Blitz"
category: documentation
---
# Mermaid diagrams that read well

Use this when you write a Mermaid diagram, above all one you style. Most diagrams are hard to read for one reason: text set on a background that doesn't contrast with it. Leave a diagram unstyled unless asked, and when you style it, follow the rules below exactly.

## Before you style anything

- **Unstyled is safe.** The default theme's colors are readable. If nobody asked for colors, don't add any.
- **Colors are hex only** (`#1d1b20`). Mermaid's theming ignores color names (`red`, `navy`).
- **Only the `base` theme takes your colors.** Set them in the diagram's front matter, before the diagram's first line:

  ```
  ---
  config:
    theme: base
    themeVariables:
      primaryColor: "#eaddff"
      primaryTextColor: "#21005d"
      primaryBorderColor: "#79747e"
      lineColor: "#79747e"
  ---
  ```

- **Every background gets a text color**, set by you, from the pair it belongs to (see *Contrast*). Never set a fill and leave the text to the theme, and never set a text color without knowing every background it lands on.

## Styling a node

Three ways, most to least reusable:

```
classDef entity fill:#eaddff,stroke:#79747e,stroke-width:1px,color:#21005d
classDef warn fill:#f9dedc,stroke:#8c1d18,color:#410e0b
class RECIPE,BOOK entity          %% several nodes at once
class AUDIT warn,entity            %% several classes on one node
RECIPE:::entity                    %% the ::: shorthand, also in a relationship: PERSON:::entity ||--o{ CAR : owns
style AUDIT stroke-width:3px       %% one node, one off: wins over its classes
classDef default fill:#f7f2fa,stroke:#79747e,color:#1d1b20   %% every node without a class
```

- `classDef` names a set of styles; `class` (or `:::`) attaches it. `classDef a,b font-size:12pt` defines two classes at once.
- `classDef default` styles every node that has no class of its own; a `style` or class statement wins over it.
- Properties that work: `fill`, `stroke`, `stroke-width`, `stroke-dasharray`, `color` (the text), `font-size`, `font-weight`. Separate them with commas, no spaces after the colons.
- In a flowchart, edges are styled by number: `linkStyle 0 stroke:#79747e,stroke-width:2px` (`linkStyle default …` for all).

## ER diagrams: the odd and even rows

An ER diagram's entity is a table: a header, then one row per attribute, alternating **odd** and **even** rows. In current Mermaid (11.x), with the usual themes:

- a class's (or `style`'s) **`fill` colors only the header and the even rows**;
- the **odd rows keep the theme's `attributeBackgroundColorOdd`**, light by default;
- the class's **`color` (the text) applies to every row**.

So `classDef entity fill:#1d4ed8,color:#ffffff` gives white text on white every other row: those attributes vanish (contrast 1:1). This is the mistake to avoid. Do one of these instead:

1. **Light rows, dark text (recommended).** Leave `fill` light or unset, give the text the dark "on" color, and set both row colors in the theme so they're yours:

   ```
   ---
   config:
     theme: base
     themeVariables:
       primaryColor: "#eaddff"
       primaryTextColor: "#1d1b20"
       primaryBorderColor: "#79747e"
       lineColor: "#79747e"
       attributeBackgroundColorOdd: "#ffffff"
       attributeBackgroundColorEven: "#f3edf7"
   ---
   erDiagram
       BOOK:::entity ||--o{ RECIPE:::entity : contains
       BOOK {
           int id PK
           string name
       }
       RECIPE {
           int id PK
           int book_id FK
           string title
       }
       classDef entity stroke:#79747e,stroke-width:1px,color:#1d1b20
   ```

2. **A dark diagram.** Set *both* `attributeBackgroundColorOdd` and `attributeBackgroundColorEven` to dark surfaces, `darkMode: true`, and the light "on" text color (the dark pairs below). Never a dark `fill` with the light rows left as they are.

Whatever you choose, the text color must contrast with the header, the odd rows and the even rows.

## Contrast: Material 3's color roles

Material 3 gives every background a matching text color, its **"on" color**, chosen to contrast with it. Use colors only in their pairs: a background, and its own "on" color for the text on it. Text needs at least **4.5:1** contrast with its background (WCAG AA), large text and borders at least **3:1**. Every pair below passes; the ratios are computed.

**Light** (Material 3's baseline):

| Background | Text on it ("on" color) | Contrast | Use for |
|---|---|---|---|
| `#6750a4` primary | `#ffffff` on-primary | 6.4:1 | a node to draw the eye to |
| `#eaddff` primary-container | `#21005d` on-primary-container | 13.3:1 | the main nodes, entity headers |
| `#e8def8` secondary-container | `#1d192b` on-secondary-container | 13.2:1 | a second kind of node |
| `#ffd8e4` tertiary-container | `#31111d` on-tertiary-container | 13.2:1 | a third kind |
| `#f9dedc` error-container | `#410e0b` on-error-container | 12.8:1 | warnings, failures |
| `#ffffff` / `#f7f2fa` / `#f3edf7` / `#ece6f0` surface tones | `#1d1b20` on-surface | 13.9–16.2:1 | rows, plain nodes, the canvas |
| `#e7e0ec` surface-variant | `#49454f` on-surface-variant | 7.2:1 | quieter labels |
| `#79747e` outline | (a border, on `#fef7ff`) | 4.3:1 | strokes and lines |

**Dark** (with `darkMode: true`):

| Background | Text on it | Contrast | Use for |
|---|---|---|---|
| `#d0bcff` primary | `#381e72` on-primary | 7.7:1 | a node to draw the eye to |
| `#4f378b` primary-container | `#eaddff` on-primary-container | 7.2:1 | the main nodes, entity headers |
| `#141218` / `#1d1b20` / `#2b2930` surface tones | `#e6e0e9` on-surface | 11.1–14.4:1 | rows, plain nodes, the canvas |
| `#938f99` outline | (a border, on `#141218`) | 5.9:1 | strokes and lines |

The rules that follow from them:

- **Text takes the "on" color of what's behind it**, never another pair's: on-primary (white) on a surface is 1.05:1, invisible.
- **Alternating rows are two tones of the same surface**, both with the same on-surface text (`#ffffff` and `#f3edf7` with `#1d1b20`). Never alternate a light and a dark row.
- **Color means something, or isn't used.** One accent (primary) for what matters, containers for kinds of thing, surfaces for the rest. Don't color every node differently.
- **The diagram may be read in light or dark mode.** A light palette with its own dark text stays readable on either, as long as every background is set; a palette that leaves a background to the theme is not.
- Colored text on a colored background (primary `#6750a4` on primary-container, 5.0:1) passes for large text only: keep body text to the "on" colors.

## Before you finish

1. Every `fill` (and every row color) has a text `color` from its own pair.
2. In an ER diagram, both `attributeBackgroundColorOdd` and `attributeBackgroundColorEven` are set whenever you set a fill or a text color.
3. The theme is `base` if you set `themeVariables`, and every color is hex.
4. Read the diagram back: open it for the user (`open_file` on the Markdown file) and, if you can, check every row's text is visible.
