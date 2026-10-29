---
name: playwright
description: Check a web app in Blitz's built-in browser, then keep what you checked as Playwright tests (@playwright/test); and debug a failing Playwright test by replaying its steps in the built-in browser
tags: [playwright, e2e, testing, browser, frontend]
version: "1.0.0"
author: "Blitz"
category: development
agents: [blitz, coder]
trigger_phrases:
  - write playwright tests
  - add an end-to-end test
  - e2e test this flow
  - the playwright test is failing
  - check the page in the browser and test it
---
# Playwright with the built-in browser

Two tools, two jobs. The **`browser` tool** is how you see the app: it drives Blitz's own Chromium (in a tab the user can watch in the desktop app, or headless), through Blitz's proxy, so site approvals and `deny_domains` apply. **Playwright** is how the project keeps what you saw: test files it runs in CI. Explore with the first, then write the second; never the other way round.

Don't connect Playwright to the built-in browser (its debugging port is Blitz's, not the project's), and don't open pages with the shell. Playwright runs its own browsers with `npx playwright test`.

## 1. Look first

- Is Playwright set up? `playwright.config.*` and `@playwright/test` in `package.json`. Follow its `testDir`, `baseURL`, projects and `webServer`. Without it, ask before adding it (`npm init playwright@latest`, or the project's package manager), and say it downloads browsers.
- Read two or three existing specs and copy their style: fixtures, page objects, how they sign in, test ids.
- Start the app the way the config's `webServer` does (or the project's dev command) with `manage_background_process`, and wait for its ready line.
- A local app needs `browser.allow_local = true`; if the browser refuses `localhost`, tell the user to set it rather than working around it.

## 2. Walk the flow in the built-in browser

Go through what the test will do, one action per step, and look as you go:

| Step | `browser` action |
|---|---|
| Open the page | `navigate` with the app's URL |
| Find what's on it | `read` (text), `html` with a selector for structure, `screenshot` to see it |
| Act | `click`, `type` (with `submit` for Enter), `select`; selectors are CSS or `text=<visible text>` |
| Check | `read` the result, `console` for errors, `script` for a value (`document.title`, an element's state) |
| Keep a record | `record` before the flow and `stop_recording` after: a screenshot per step, for the user or a bug report |

Note for each step what identifies the element to a person (its role and accessible name, its label, its visible text), whether the page waited on something (a request, an animation), and anything that failed or logged an error. A flow that's broken in the browser is a bug to report or fix first, not a test to write around.

## 3. Write the test

Turn each step into Playwright, preferring what a user perceives:

| In the browser tool | In Playwright |
|---|---|
| `navigate` | `await page.goto("/path")` (relative to `baseURL`) |
| `click` on `text=Save` | `await page.getByRole("button", { name: "Save" }).click()` |
| `type` into a field | `await page.getByLabel("Email").fill("ana@example.com")` |
| `type` with `submit` | `.fill(…)` then `.press("Enter")` |
| `select` | `await page.getByLabel("Country").selectOption("Canada")` |
| `read` to check text | `await expect(page.getByRole("status")).toHaveText("Saved")` |
| `console` showed no errors | collect `page.on("console")` errors in a fixture and expect none |

- Locators: `getByRole`, `getByLabel`, `getByText`, then `getByTestId` where the project uses test ids; CSS only as a last resort. The CSS selectors you used in the browser tool are for exploring, not for the test.
- Waiting: rely on auto-waiting and web-first assertions (`await expect(locator).toBeVisible()`); never `waitForTimeout`. Wait on a response with `page.waitForResponse` when the page gives nothing visible.
- Each test sets up its own state (an API call or a fixture, not the previous test) and runs alone and in parallel; sign in once with `storageState` if the project does.
- One flow per test, named for what the user does: `test("saves a new address", …)`.

## 4. Run and fix it

- Run just the new spec: `npx playwright test path/to/spec.ts --project=chromium` (or the project's script), then the whole suite.
- When it fails, read the error and the trace (`--trace on`, then `npx playwright show-trace`), and replay the failing step in the built-in browser (`navigate`, then the same actions) with `screenshot` and `console` to see the page as it was. Fix the cause (a wrong locator, a missing wait on a response, a real bug), not the symptom: no retries or timeouts added to make it pass.
- A test that passes alone but fails in the suite shares state: find it.

## 5. Check it

Before saying it's done: the new specs pass three runs in a row (`--repeat-each=3`), the whole suite passes, and the app's process is stopped. Say which flows the tests cover, what you saw in the browser that they don't (and why), and any bug you found.
