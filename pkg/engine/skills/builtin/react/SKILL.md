---
name: react
description: Write and change React components in TypeScript (components, hooks, state, effects, accessibility, tests with Testing Library), following the project's own stack and styles first
tags: [react, typescript, frontend, development]
version: "1.0.0"
author: "Blitz"
category: development
agents: [blitz, coder]
trigger_phrases:
  - build a react component
  - change the react page
  - fix the front end
  - add a form in react
  - react hook
---
# React

## 1. Look first

Read `package.json` (React version, the framework: Vite, Next.js, Remix; the package manager from its lock file: `pnpm-lock.yaml`, `yarn.lock`, `package-lock.json`), the `tsconfig`, and two or three components near the one you're changing. Copy their patterns: how they style (CSS modules, Tailwind, a component library), fetch data, manage state and name files. Never invent a new visual style; reuse the project's components and tokens.

## 2. Write it

- **Components:** function components, one per file when exported, named for what they show. Props typed with an interface or type; no `any`. Keep a component to one job: split when it fetches, decides and renders a long tree all at once.
- **State:** keep it as close as possible to where it's used; lift it only when siblings share it. Derive what you can during render instead of storing it. Use the project's store or data library (React Query, Redux, Zustand) rather than adding another.
- **Effects:** for synchronising with something outside React (a subscription, the DOM, a timer), not for deriving state or handling events. Every effect that subscribes cleans up; dependency arrays are complete (don't silence the lint rule).
- **Lists and keys:** stable keys from the data, never the index for lists that change.
- **Events and forms:** handle in event handlers; controlled inputs with a label each; disable the submit button while it's sending, and show errors where they happen.
- **Accessibility:** semantic elements (`button`, not a clickable `div`), labels on inputs, alt text on images, keyboard focus that works, and enough colour contrast in light and dark themes.
- **Performance:** measure before `useMemo`, `useCallback` or `memo`; virtualise long lists; lazy-load heavy routes.

## 3. Test it

With the project's runner (Vitest or Jest) and Testing Library: render the component, act as a user would (`userEvent`), and query by role and label (`getByRole("button", { name: "Save" })`), not by class or test id where a role works. Test what the user sees, not internal state.

## 4. Check it

Before saying it's done: the type check passes (`tsc --noEmit` or the project's script), the linter is clean, the tests pass, and for a visible change, run the dev server and look at it (with the `browser` tool when you have it) in the states that matter: empty, loading, error, full. Say what you ran and what you looked at.
