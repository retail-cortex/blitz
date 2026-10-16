---
title: Blitz 0.5.1
weight: 8
---

**Equations, pictures and room to breathe.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.5.1).

**v0.5.1 makes your Markdown look the way you meant it.** Equations render beautifully, in the app and in the PDFs you export. Pictures embedded in your docs actually show. Diagrams pan and zoom. And on a big monitor, documents finally use all of it.

**See it in a minute:**

1. Open any Markdown file and add an equation, the way you would on GitHub:

   ```markdown
   The mean is $\bar{x} = \frac{1}{n}\sum_{i=1}^{n} x_i$, and the model is:

   $$
   \hat{y}_t = \beta_0 + \beta_1 T_{t-1} + \varepsilon_t
   $$
   ```

2. Hold **⌘** (or **Ctrl**) and scroll, or pinch, over the editor to zoom in. Do the same over a diagram to zoom into just the diagram.
3. Click **Export as PDF** in the file's bar: your equations come out properly typeset.

### Math, everywhere you write

- **Equations render** in previews, the visual editor and the chat: `$…$` inline, `$$…$$` and ```` ```math ```` blocks, exactly as on GitHub.
- **Prices stay prices.** "It costs $5 and $10" is still text; Blitz reads dollar signs the way GitHub and Pandoc do.
- **Edit equations in place.** In the visual editor, click a formula to edit its TeX with a live preview, type `$x^2$` to create one, or add a block with **Equation** (Σ) in the toolbar.
- **PDFs typeset math properly**, with the right fonts embedded, ready to share.
- A formula with a mistake shows its source in red, with the reason when you hover over it.

### Pictures that show up

- **Embedded (base64) images render** in previews, the visual editor and the chat, instead of showing as code.
- **Images from your workspace show too**: `![chart](results/chart.png)` displays the picture.
- Still private: Blitz never fetches a picture from the web, so web images stay a click-to-open link.

### Room to read and zoom

- **Zoom the editor and documents** from 50% to 200%: ⌘ or Ctrl with `+` / `−` / `0`, the scroll wheel, or a pinch. The chat stays as it is, and the zoom level is remembered.
- **Documents use your whole screen.** Markdown now fills the editor, which is great on wide monitors. Prefer a reading column? Switch with the **↔** button on the tab or in **Settings › Appearance**.
- **Diagrams pan and zoom.** Zoom at the pointer, drag to move around, double-click to fit, or **Expand** a diagram to see it over the whole window.
- **Readable hover help.** Hovering over code shows documentation in a proper, wide box again, instead of a narrow column.

### Good to know

- Math uses standard LaTeX via KaTeX. A few TeX habits don't carry over: for example, a line break inside `\text{…}` isn't `\n`; use an `aligned` block with `\\` instead.
- Base64 that's split across lines, or contains spaces, isn't a valid Markdown image, so it shows as text.
- Tested on macOS. Linux and Windows are expected to work the same; let us know if anything looks off.

Thanks to everyone who filed issues: #6, #18, #20, #21 and #22 are all fixed in this release. 
