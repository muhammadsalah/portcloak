/**
 * The table.
 *
 * `Table` is the element with the app's styling; `TableScroll` is the box that
 * lets a wide one scroll sideways instead of pushing the page out. Every table
 * in the app is inside one, because a snapshot listing with an eight-column
 * header is wider than a laptop at the width the navigation rail leaves it.
 */
import styled from "styled-components";

export const TableScroll = styled.div`
  overflow-x: auto;
`;

export const Table = styled.table`
  width: 100%;
  border-collapse: collapse;

  thead th {
    text-align: left;
    font-size: 11px;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: ${(p) => p.theme.color.textSecondary};
    font-weight: 600;
    padding: 10px 16px;
    border-bottom: 1px solid ${(p) => p.theme.color.border};
    background: ${(p) => p.theme.color.surfaceSubtle};
    white-space: nowrap;
  }

  tbody td {
    padding: 11px 16px;
    border-bottom: 1px solid ${(p) => p.theme.color.borderSubtle};
    vertical-align: top;
  }

  tbody tr:last-child td {
    border-bottom: none;
  }
`;

/** A row that can be picked, and the one that has been. */
export const Tr = styled.tr<{ $selectable?: boolean; $selected?: boolean }>`
  ${(p) => p.$selectable && `cursor: pointer;`}
  ${(p) => p.$selected && `background: ${p.theme.color.rowSelected};`}

  &:hover {
    ${(p) => p.$selectable && `background: ${p.theme.color.rowHover};`}
  }
`;

/**
 * A column of figures, right-aligned with digits of equal width so the eye can
 * compare magnitudes down the column rather than reading each number.
 */
export const Numeric = styled.td`
  text-align: right;
  font-variant-numeric: tabular-nums;
`;

export const NumericHeader = styled.th`
  text-align: right !important;
`;

/**
 * A description list of label and value, used wherever the app states facts
 * rather than offers controls: provenance, probe results, build details.
 */
export const KeyValue = styled.dl`
  display: grid;
  grid-template-columns: 170px 1fr;
  gap: 6px 16px;
  font-size: 13px;
  margin: 0;

  dt {
    color: ${(p) => p.theme.color.textSecondary};
  }

  dd {
    margin: 0;
    /* A value here can be a filesystem path or a bucket and prefix, which have
       nowhere to break. This column has the width to wrap one, so it wraps
       rather than running past the card as the list rows did. */
    min-width: 0;
    overflow-wrap: anywhere;
  }
`;

/* ── Statistics ────────────────────────────────────────────────────────── */

export const StatGrid = styled.div`
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 20px;
`;

const StatValue = styled.div`
  font-family: ${(p) => p.theme.font.heading};
  font-size: 26px;
  line-height: 1.1;
`;

const StatLabel = styled.div`
  font-size: 12px;
  color: ${(p) => p.theme.color.textSecondary};
  margin-top: 2px;
`;

/** One figure, and what it counts. */
export function Stat({ value, label }: { value: string; label: string }) {
  return (
    <div>
      <StatValue>{value}</StatValue>
      <StatLabel>{label}</StatLabel>
    </div>
  );
}
