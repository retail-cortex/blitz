import { Component, type ReactNode } from "react";

/**
 * Catches a crash while drawing the page, so the window shows what went
 * wrong and a way on (reload) instead of freezing or going blank.
 */
export class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  render() {
    const e = this.state.error;
    if (!e) return this.props.children;
    const details = `${e.name}: ${e.message}\n${e.stack ?? ""}`;
    return (
      <div className="splash">
        <div className="drag-region" />
        <div className="splash-card">
          <h1 className="t-headline">Something went wrong</h1>
          <p className="muted">The window hit an error it couldn't recover from. Your workspaces and sessions are safe in the Blitz service.</p>
          <pre className="json crash-details">{details}</pre>
          <div className="row">
            <button className="btn filled" onClick={() => location.reload()}>
              Reload the window
            </button>
            <button className="btn text" onClick={() => navigator.clipboard?.writeText(details)}>
              Copy the details
            </button>
          </div>
        </div>
      </div>
    );
  }
}
