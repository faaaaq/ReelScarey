import { Component, type ReactNode } from "react";

// A tab opened before an upgrade can request an old lazy chunk. Keep a clear
// recovery action available instead of leaving the application blank.
export class PageLoadBoundary extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    if (this.state.failed) {
      return (
        <section className="panel system-panel" role="alert">
          <h1>This page could not be loaded</h1>
          <p>Reload to get the current application and try again.</p>
          <button onClick={() => window.location.reload()}>
            Reload application
          </button>
        </section>
      );
    }
    return this.props.children;
  }
}
