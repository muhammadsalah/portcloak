// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The title bar.
 *
 * It is also the window's drag handle, which is most of why it is shaped the
 * way it is — see the comments on the styles below.
 */
import styled from "styled-components";

import { Mark, onDark } from "@/components/Logo";

export function Masthead() {
  return (
    <Bar>
      {/*
        The mark and the wordmark are one unit, so the masthead can align the
        pair as a block against the navigation below it while the two stay
        centred on each other inside it.
      */}
      <Brand>
        <Mark size={30} tone={onDark} />
        {/*
          One word, two colours: the wordmark is set live rather than shipped as
          a picture of text, so it stays crisp at any scale and searchable to
          anything reading the DOM.
        */}
        <Wordmark>
          Port<WordmarkAccent>Cloak</WordmarkAccent>
        </Wordmark>
      </Brand>
    </Bar>
  );
}

const Bar = styled.div`
  height: ${(p) => p.theme.size.mastheadHeight};
  min-height: ${(p) => p.theme.size.mastheadHeight};
  background: ${(p) => p.theme.color.masthead};
  color: #fff;
  display: flex;
  /*
   * The brand sits at the BOTTOM left of the masthead rather than indented on
   * its centre line.
   *
   * It used to be pushed 88px right to clear the macOS traffic lights, which
   * left it floating in the middle of the navigation rail's width, aligned to
   * nothing. Going down instead of across buys the same clearance and puts the
   * mark's left edge on the same 16px line as every navigation icon below it,
   * so the rail reads as one column from the logo down.
   *
   * The masthead is 88px for exactly this reason: the traffic lights occupy
   * roughly the top 38px of an inset title bar, and the 30px mark needs to
   * start below them.
   */
  align-items: flex-end;
  padding: 0 24px 16px 16px;
  /* Wails v3 reads this custom property, not -webkit-app-region: its runtime
     calls getComputedStyle(target) and compares --wails-draggable to "drag".
     -webkit-app-region is an Electron convention and is inert in WKWebView,
     which is why the window could not be dragged by its masthead at all. */
  --wails-draggable: drag;
  /* The masthead is a title bar, so a drag across it must not paint a text
     selection over the wordmark. */
  user-select: none;

  svg {
    flex: none;
    /* The traffic lights and the drag region are above; the mark is not a
       control and must not swallow a drag on the title bar. */
    pointer-events: none;
  }

  /* --wails-draggable inherits, so anything interactive placed in the masthead
     has to opt out or the first press would start dragging the window instead
     of arming the control. */
  button,
  a,
  input,
  select,
  [role="button"] {
    --wails-draggable: no-drag;
  }
`;

/** Mark and wordmark centred on each other, and moved as one block. */
const Brand = styled.div`
  display: flex;
  align-items: center;
  gap: 12px;
`;

const Wordmark = styled.div`
  font-family: ${(p) => p.theme.font.heading};
  font-size: 19px;
  font-weight: 500;
  /* The wordmark is one word set tight, not two words with a space. */
  letter-spacing: -0.3px;
`;

const WordmarkAccent = styled.span`
  color: ${(p) => p.theme.color.brandAccent};
`;
