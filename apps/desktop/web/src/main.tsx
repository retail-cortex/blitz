import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import "./m3.css";
import "./app.css";

async function start() {
  // A development build can talk to a fake service (?fake), for working on
  // the page without one. Production builds leave this out.
  if (import.meta.env.DEV && new URLSearchParams(location.search).has("fake")) {
    const { installFake } = await import("./dev/fake");
    installFake();
  }
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
start();
