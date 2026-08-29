// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The design system, in one import.
 *
 * A page reaches for `../../design-system` and gets everything it can draw
 * with. Nothing outside this folder should be styling a button or picking a
 * hex value; if a screen needs something that is not here, it belongs here.
 */
export { Button, IconButton, type ButtonVariant } from "./Button";
export { Card, CardBody, CardFoot, CardHead, CardTitle } from "./Card";
export {
  Checkbox,
  FacetCount,
  FacetGroup,
  FacetLabel,
  Field,
  FieldBox,
  FieldHint,
  Input,
  Label,
  Textarea,
  Toggle,
} from "./Form";
export { Select, type SelectOption } from "./Select";
export { Pagination, pageNumbers, pageSizes } from "./Pagination";
export { GlobalStyle } from "./GlobalStyle";
export { Menu, type MenuItem } from "./Menu";
export {
  Divider,
  FieldRow,
  Grow,
  Right,
  Row,
  Search,
  Split,
  SplitWide,
  Stack,
  Toolbar,
  Truncate,
  truncate,
} from "./Layout";
export { ModalProvider, useModal, useModalControls, type ModalOptions } from "./Modal";
export {
  Log,
  LogCommand,
  Pipeline,
  PipelineStep,
  ProgressBar,
  ProgressTrack,
  Step,
  StepLabel,
  StepMarker,
  Stepper,
  type StepState,
} from "./Progress";
export {
  Badge,
  Chip,
  Dot,
  Encryption,
  FailureNotice,
  Lines,
  Notice,
  NoticeBox,
  NoticeTitle,
  Spinner,
  StepMark,
  StepNumber,
} from "./Status";
export { KeyValue, Numeric, NumericHeader, Stat, StatGrid, Table, TableScroll, Tr } from "./Table";
export { Breadcrumb, Tab, TabBar, Tabs, type TabItem } from "./Tabs";
export { theme, type NoticeTone, type Theme, type Tone } from "./theme";
export {
  BulletList,
  GroupTitle,
  Hint,
  Link,
  Mono,
  Muted,
  PageHead,
  PageSubtitle,
  PageTitle,
  PathBox,
  RevealValue,
  SectionTitle,
  Small,
  Strong,
} from "./Typography";
export {
  StepRail,
  StepRailItem,
  WizardFrame,
  WizardPanel,
  WizardStep,
  WizardSteps,
} from "./Wizard";
