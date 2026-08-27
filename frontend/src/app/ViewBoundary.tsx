// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * Every view puts a spinner up and replaces it once the engine has answered. If
 * it throws in between, nothing replaces the spinner and the application looks
 * hung rather than broken — which is exactly how it looked on a first launch,
 * when the engine still answered an unconfigured PortCloak with null lists and
 * the first `.length` threw.
 *
 * That cause is fixed in the engine and tested there. This is the guarantee
 * that the next one is visible: a view that fails says so, and offers the only
 * useful action.
 *
 * It is a class because an error boundary can only be one — `componentDidCatch`
 * has no hook equivalent.
 */
import { Component, type ErrorInfo, type ReactNode } from "react";

import { Button, Notice } from "../design-system";

interface Props {
  /** Remounts the subtree, and so re-runs its loads, when it changes. */
  resetKey: string;
  children: ReactNode;
}

interface State {
  error: unknown;
}

export class ViewBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: unknown): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    // Into the console, not the screen: the screen gets the sentence, the
    // console gets the component stack a developer needs.
    console.error("A screen could not be drawn.", error, info.componentStack);
  }

  componentDidUpdate(previous: Props): void {
    // Navigating somewhere else clears the failure. Without this the whole
    // content column stays on the error notice for the rest of the session.
    if (previous.resetKey !== this.props.resetKey && this.state.error) {
      this.setState({ error: null });
    }
  }

  render(): ReactNode {
    const { error } = this.state;
    if (!error) return this.props.children;

    return (
      <div>
        <Notice
          tone="danger"
          title="This screen could not be drawn."
          body={error instanceof Error ? error.message : String(error)}
        />
        <Button onClick={() => this.setState({ error: null })}>Try again</Button>
      </div>
    );
  }
}
