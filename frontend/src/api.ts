/**
 * The bridge to the Go engine.
 *
 * Every method here is one call to a bound controller. Nothing in the frontend
 * decides anything about a capture or a restore — the engine does, and this
 * file exists so a screen can ask it.
 */
import { Call, Events } from "@wailsio/runtime";

export interface Failure {
  message: string;
  hint?: string;
  retryable: boolean;
}

/** Anything a controller returns that might carry a failure instead. */
export interface MaybeFailed {
  failure?: Failure | null;
}

/**
 * Wails registers a bound method under its fully-qualified Go name —
 * `<package path>.<type>.<method>` — and `Call.ByName` looks the string up
 * verbatim. The Go package path is therefore part of the address, not
 * decoration: without it every call fails with "unknown bound method name".
 *
 * A controller's `ServiceName()` does not change this. That method only names
 * the service in log lines, which is exactly why it is easy to assume it
 * controls the binding and to write `"ConfigController.Load"` here instead.
 *
 * `TestBindings_EveryFrontendCallResolves` reads this file and checks every
 * call below against the real methods, so renaming the Go package breaks the
 * build rather than the application.
 */
const goPackage = "portcloak/internal/app";

async function call<T>(service: string, method: string, ...args: unknown[]): Promise<T> {
  return (await Call.ByName(`${goPackage}.${service}.${method}`, ...args)) as T;
}

/* ── Configuration ─────────────────────────────────────────────────────── */

export type EnvironmentKind = "local" | "ssh" | "docker" | "kubernetes";
export type StorageKind = "disk" | "ssh" | "s3" | "azure";

export interface Readiness {
  ready: boolean;
  reason?: string;
}

export interface ProbeStamp {
  at: string;
  ok: boolean;
  summary?: string;
  keycloakVersion?: string;
  cloneCapable?: boolean;
  writable?: boolean;
}

export interface Environment {
  name: string;
  kind: EnvironmentKind;
  serverFolder?: string;
  javaHome?: string;
  host?: string;
  port?: number;
  user?: string;
  auth?: "key" | "agent" | "password";
  jumpHost?: { host: string; port?: number; user?: string; auth?: string } | null;
  sudo?: boolean;
  dockerEndpoint?: string;
  container?: string;
  runtime?: string;
  context?: string;
  kubeconfig?: string;
  namespace?: string;
  workload?: string;
  containerName?: string;
  adminBaseUrl?: string;
  adminRealm?: string;
  adminUser?: string;
  adminCredentialRef?: string;
  credentialRef?: string;
  lastProbe?: ProbeStamp | null;
}

export interface EnvironmentView extends Environment {
  target: string;
  readiness: Readiness;
  stale: boolean;
  probeAge?: string;
  credentialPresent: boolean;
}

export interface Storage {
  name: string;
  kind: StorageKind;
  default?: boolean;
  encryptionRequired?: boolean;
  folder?: string;
  host?: string;
  port?: number;
  user?: string;
  auth?: "key" | "agent" | "password";
  endpoint?: string;
  region?: string;
  bucket?: string;
  pathStyle?: boolean;
  partSizeMb?: number;
  storageClass?: string;
  account?: string;
  container?: string;
  blockSizeMb?: number;
  prefix?: string;
  credentialRef?: string;
  lastProbe?: ProbeStamp | null;
}

export interface StorageView extends Storage {
  root: string;
  readiness: Readiness;
  stale: boolean;
  probeAge?: string;
  credentialPresent: boolean;
}

export interface Preferences {
  usersMode?: string;
  usersPerFile?: number;
  verifyByDefault?: boolean;
  encryptByDefault?: boolean;
  allowSecretReveal?: boolean;
}

export interface ConfigProblem {
  line: number;
  path: string;
  message: string;
}

export interface ConfigSnapshot {
  environments: EnvironmentView[];
  storage: StorageView[];
  preferences: Preferences;
  configFile: string;
  firstRun: boolean;
  loadProblems?: ConfigProblem[];
  noSignIn: string;
}

export interface Check {
  name: string;
  value: string;
  status: "pass" | "warn" | "fail" | "skipped";
  blocking: boolean;
  advice?: string;
}

export interface TargetFacts {
  kind: string;
  reachable: boolean;
  keycloakVersion?: string;
  kcPath?: string;
  tempDir?: string;
  freeBytes?: number;
  hasTar: boolean;
  mode: string;
  cloneCapable: boolean;
  cloneDetail?: string;
  adminReachable: boolean;
  adminDetail?: string;
  ports: { http: number; https: number; management: number };
  realms?: string[];
  checks: Check[];
  probedAt: string;
  readOnlyNote: string;
}

export interface ProbeResult extends MaybeFailed {
  ok: boolean;
  facts: TargetFacts;
}

export interface Reach {
  access: "unreachable" | "read-only" | "writable";
  root: string;
  latency: number;
  resumable: boolean;
  integrity: string;
  freeBytes?: number;
  failedStep?: string;
  detail?: string;
}

export interface StorageProbeResult extends MaybeFailed {
  ok: boolean;
  reach: Reach;
  note: string;
}

export const ConfigAPI = {
  load: () => call<ConfigSnapshot>("ConfigController", "Load"),
  reload: () => call<ConfigSnapshot>("ConfigController", "Reload"),
  environmentKinds: () => call<string[]>("ConfigController", "EnvironmentKinds"),
  storageKinds: () => call<string[]>("ConfigController", "StorageKinds"),
  saveEnvironment: (originalName: string, env: Environment, secret: string) =>
    call<Failure | null>("ConfigController", "SaveEnvironment", originalName, env, secret),
  saveAdminCredential: (name: string, secret: string) =>
    call<Failure | null>("ConfigController", "SaveAdminCredential", name, secret),
  duplicateEnvironment: (name: string) =>
    call<[EnvironmentView, Failure | null]>("ConfigController", "DuplicateEnvironment", name),
  deleteEnvironment: (name: string) =>
    call<Failure | null>("ConfigController", "DeleteEnvironment", name),
  saveStorage: (originalName: string, st: Storage, secret: string) =>
    call<Failure | null>("ConfigController", "SaveStorage", originalName, st, secret),
  duplicateStorage: (name: string) =>
    call<[StorageView, Failure | null]>("ConfigController", "DuplicateStorage", name),
  deleteStorage: (name: string) => call<Failure | null>("ConfigController", "DeleteStorage", name),
  setDefaultStorage: (name: string) =>
    call<Failure | null>("ConfigController", "SetDefaultStorage", name),
  savePreferences: (p: Preferences) =>
    call<Failure | null>("ConfigController", "SavePreferences", p),
  testEnvironment: (name: string) => call<ProbeResult>("ConfigController", "TestEnvironment", name),
  testStorage: (name: string) => call<StorageProbeResult>("ConfigController", "TestStorage", name),
  createStorageFolder: (name: string) =>
    call<Failure | null>("ConfigController", "CreateStorageFolder", name),
};

/* ── Capture ───────────────────────────────────────────────────────────── */

export interface WizardDefaults {
  environments: EnvironmentView[];
  storages: StorageView[];
  defaultStorage: string;
  preferences: Preferences;
  encryptionNotice: string;
  declineNotice: string;
}

export interface RealmsResult extends MaybeFailed {
  realms: string[];
  discovered: boolean;
  note: string;
}

export interface CaptureOptions {
  environment: string;
  realms: string[];
  storage: string;
  usersMode: string;
  usersPerFile: number;
  verify: boolean;
  detectDependencies: boolean;
  encrypt: boolean;
  encryptionMode: string;
  passphrase: string;
  recipients: string[];
  acknowledgedUnencrypted: boolean;
}

export interface StartResult extends MaybeFailed {
  jobIds: string[];
  realms: string[];
}

export interface Identity {
  privateKey: string;
  publicKey: string;
  warning: string;
}

export const CaptureAPI = {
  defaults: () => call<WizardDefaults>("CaptureController", "Defaults"),
  realms: (environment: string) => call<RealmsResult>("CaptureController", "Realms", environment),
  start: (opts: CaptureOptions) => call<StartResult>("CaptureController", "Start", opts),
  generateIdentity: () => call<[Identity, Failure | null]>("CaptureController", "GenerateIdentity"),
};

/* ── Library ───────────────────────────────────────────────────────────── */

export interface LibraryEntry {
  snapshotId: string;
  realm: string;
  createdAt: string;
  storage: string;
  bundleKey: string;
  bytes: number;
  environment?: string;
  executionMode?: string;
  keycloakVersion?: string;
  users: number;
  clients: number;
  verdict: string;
  encrypted: boolean;
  encryptionMode?: string;
  secretCount: number;
  dependencyCount: number;
  tokenContinuity: boolean;
  warning?: string;
  metadataReadable: boolean;
  metadataNote?: string;
}

export interface StorageStatus {
  name: string;
  kind: string;
  reachable: boolean;
  snapshots: number;
  error?: string;
}

export interface FirstRun {
  heading: string;
  body: string;
  needsEnvironment: boolean;
  needsStorage: boolean;
  environmentBody: string;
  storageBody: string;
  noAccountHeading: string;
  noAccountBody: string;
  configFile: string;
}

export interface LibraryView {
  entries: LibraryEntry[];
  storages: StorageStatus[];
  summary: string;
  realms: string[];
  firstRun?: FirstRun;
}

export interface ObjectInfo {
  key: string;
  size: number;
  modTime: string;
}

export interface BrowseResult extends MaybeFailed {
  storage: string;
  snapshots: LibraryEntry[];
  foreign: ObjectInfo[];
  status: StorageStatus;
  note: string;
}

export interface DeleteResult extends MaybeFailed {
  removed: string[];
  note: string;
}

export const SnapshotAPI = {
  library: () => call<LibraryView>("SnapshotController", "Library"),
  browse: (storage: string) => call<BrowseResult>("SnapshotController", "Browse", storage),
  remove: (storage: string, bundleKey: string) =>
    call<DeleteResult>("SnapshotController", "Delete", storage, bundleKey),
};

/* ── Inspection ────────────────────────────────────────────────────────── */

export interface Category {
  name: string;
  status: "captured" | "partial" | "missing" | "outOfScope" | "notChecked";
  count?: number;
  reason?: string;
}

export interface Completeness {
  categories: Category[];
  warnings?: string[];
  verdict: string;
}

export interface Dependency {
  type: string;
  name: string;
  detectedAt?: string;
  referencedBy?: string;
  action: string;
  consequence: string;
}

export interface Overview extends MaybeFailed {
  snapshotId: string;
  realm: string;
  storage: string;
  bundleKey: string;
  encrypted: boolean;
  encryptionMode?: string;
  warning?: string;
  counts: Record<string, number>;
  credentials: {
    passwordHashes: number;
    otp: number;
    webauthn: number;
    recoveryCodes: number;
    algorithms?: Record<string, number>;
  };
  settings: Record<string, unknown>;
  completeness: Completeness;
  provenance: Record<string, unknown>;
  tokenContinuity: boolean;
  tokenContinuityNote: string;
  integrityOk: boolean;
  integrityMessage: string;
  degraded: boolean;
  degradedNote?: string;
  dependencies: Dependency[];
  secretCount: number;
  indexNote: string;
}

export interface UserRow {
  id: string;
  username: string;
  email?: string;
  firstName?: string;
  lastName?: string;
  enabled: boolean;
  emailVerified: boolean;
  origin: string;
  hasPassword: boolean;
  passwordAlgorithm?: string;
  passwordIterations?: number;
  otpCount: number;
  webauthnCount: number;
  recoveryCodes: boolean;
  secondFactor: string;
  requiredActions?: string[];
  groups?: string[];
  serviceAccount?: string;
}

export interface FacetValue {
  value: string;
  label: string;
  count: number;
}

export interface Facets {
  status: FacetValue[];
  origin: FacetValue[];
  secondFactor: FacetValue[];
  realmRoles: FacetValue[];
  clientRoles: FacetValue[];
  groups: FacetValue[];
  requiredActions: FacetValue[];
}

export interface UsersQuery {
  snapshotId: string;
  query: string;
  enabled: string;
  origin: string;
  secondFactor: string;
  realmRole: string;
  clientRole: string;
  client: string;
  group: string;
  requiredAction: string;
  sort: string;
  descending: boolean;
  offset: number;
  limit: number;
}

export interface UsersResult extends MaybeFailed {
  page: { rows: UserRow[]; total: number; offset: number; limit: number };
  facets: Facets;
  note: string;
  empty?: string;
}

export interface CredentialPresence {
  type: string;
  label?: string;
  algorithm?: string;
  iterations?: number;
  created?: string;
}

export interface UserDetail extends UserRow {
  attributes?: Record<string, string[]>;
  realmRoles?: string[];
  clientRoles?: Record<string, string[]>;
  federatedIdentities?: { identityProvider: string; userName: string }[];
  credentials: CredentialPresence[];
}

export interface Entities extends MaybeFailed {
  clients: {
    clientId: string;
    name?: string;
    enabled: boolean;
    protocol: string;
    confidential: boolean;
    secretPresent: boolean;
    secretMasked: boolean;
    mappers: number;
    authorization: boolean;
    serviceAccount: boolean;
  }[];
  clientScopes: { name: string; protocol: string; mappers: number }[];
  realmRoles: { name: string; composite: boolean }[];
  clientRoles: { name: string; client?: string; composite: boolean }[];
  groups: { path: string; realmRoles: number; clientRoles: number; attributes: number }[];
  keys: {
    kid?: string;
    provider: string;
    name?: string;
    type?: string;
    algorithm?: string;
    use?: string;
    active: boolean;
    privateCarried: boolean;
    keystoreFile?: string;
  }[];
  identityProviders: {
    alias: string;
    protocol: string;
    enabled: boolean;
    secretCarried: boolean;
    mappers: number;
  }[];
  federations: {
    name: string;
    provider: string;
    enabled: boolean;
    connectionUrl?: string;
    usersDn?: string;
    bindDn?: string;
    bindCarried: boolean;
    mappers: number;
  }[];
  flows: {
    alias: string;
    description?: string;
    topLevel: boolean;
    builtIn: boolean;
    executions: number;
    boundAs?: string;
    configSecret: boolean;
  }[];
  dependencies: Dependency[];
  dependencyNote: string;
}

export interface LedgerEntry {
  location: string;
  kind: string;
  kindLabel: string;
  carried: boolean;
  masked: boolean;
  status: string;
  note?: string;
  algorithm?: string;
  revealable: boolean;
}

export interface LedgerView extends MaybeFailed {
  entries: LedgerEntry[];
  summary: string;
  note: string;
  revealAllowed: boolean;
}

export interface RevealResult extends MaybeFailed {
  value: string;
  note: string;
}

export interface VerifiedArtifact {
  name: string;
  ok: boolean;
  digest?: string;
  note?: string;
}

export interface VerifyReport {
  snapshotId: string;
  realm: string;
  ok: boolean;
  message: string;
  decryptable: boolean;
  root: string;
  artifacts: VerifiedArtifact[];
  note: string;
}

export interface ExportResult {
  path: string;
  rows: number;
  bytes: number;
  note: string;
}

export interface CloseResult extends MaybeFailed {
  confirmed: string;
}

export const InspectAPI = {
  open: (req: {
    storage: string;
    bundleKey: string;
    snapshotId: string;
    passphrase: string;
    identities: string[];
  }) => call<Overview>("InspectController", "Open", req),
  reopen: (snapshotId: string) => call<Overview>("InspectController", "Reopen", snapshotId),
  users: (q: UsersQuery) => call<UsersResult>("InspectController", "Users", q),
  user: (snapshotId: string, userId: string) =>
    call<[UserDetail, Failure | null]>("InspectController", "User", snapshotId, userId),
  entities: (snapshotId: string) => call<Entities>("InspectController", "Entities", snapshotId),
  ledger: (snapshotId: string) => call<LedgerView>("InspectController", "Ledger", snapshotId),
  reveal: (snapshotId: string, location: string, reason: string) =>
    call<RevealResult>("InspectController", "Reveal", snapshotId, location, reason),
  verify: (snapshotId: string) =>
    call<[VerifyReport, Failure | null]>("InspectController", "Verify", snapshotId),
  exportView: (
    snapshotId: string,
    req: { view: string; format: string; path: string },
    q: UsersQuery,
  ) => call<[ExportResult, Failure | null]>("InspectController", "Export", snapshotId, req, q),
  close: (snapshotId: string) => call<CloseResult>("InspectController", "Close", snapshotId),
};

/* ── Restore ───────────────────────────────────────────────────────────── */

export interface Strategy {
  value: string;
  label: string;
  description: string;
  needsAdminApi: boolean;
  destructive: boolean;
}

export interface Precondition {
  type: string;
  name: string;
  detectedAt?: string;
  action: string;
  consequence: string;
}

export interface PreconditionReport {
  dependencies: Precondition[];
  checked: boolean;
  integrityPassed: boolean;
  decrypted: boolean;
  summary: string;
  blocks: boolean;
}

export interface DiffCategory {
  category: string;
  create: number;
  overwrite: number;
  leaveAlone: number;
  note?: string;
  noteLevel?: string;
}

export interface DryRun {
  strategy: string;
  available: boolean;
  targetExists: boolean;
  categories: DiffCategory[];
  summary: string;
  caveat: string;
  unavailable?: string;
}

export interface Plan extends MaybeFailed {
  preconditions: PreconditionReport;
  dryRun: DryRun;
  blocked: boolean;
  blockedNote?: string;
  confirmationRequired: boolean;
}

export interface ApplyResult extends MaybeFailed {
  jobId: string;
}

export const RestoreAPI = {
  strategies: () => call<Strategy[]>("RestoreController", "Strategies"),
  destinations: () => call<EnvironmentView[]>("RestoreController", "Destinations"),
  plan: (req: { snapshotId: string; environment: string; strategy: string }) =>
    call<Plan>("RestoreController", "Plan", req),
  apply: (req: {
    snapshotId: string;
    storage: string;
    bundleKey: string;
    realm: string;
    environment: string;
    strategy: string;
    passphrase: string;
    identities: string[];
    confirmRealm: string;
  }) => call<ApplyResult>("RestoreController", "Apply", req),
  outOfScopeNote: () => call<string[]>("RestoreController", "OutOfScopeNote"),
};

/* ── Jobs ──────────────────────────────────────────────────────────────── */

export interface LedgerRow {
  phase: string;
  item?: string;
  attempts: number;
  lastError?: string;
  outcome: string;
  retryable: boolean;
  at: string;
}

export interface PhaseView {
  phase: string;
  label: string;
  done: boolean;
  live: boolean;
}

export interface JobView {
  id: string;
  kind: string;
  state: string;
  phase?: string;
  realm?: string;
  source?: string;
  environment?: string;
  storage?: string;
  snapshotId?: string;
  storageKey?: string;
  encrypted: boolean;
  createdAt: string;
  message?: string;
  hint?: string;
  retryable?: boolean;
  ledger?: LedgerRow[];
  provenance: { executionMode?: string; cloneRef?: string; keycloakVersion?: string };
  phases: PhaseView[];
  elapsed: string;
  resumable: boolean;
  discardable: boolean;
  cancellable: boolean;
  checkpointNote?: string;
  /** What pressing Resume would actually do: repeat the upload, or export again. */
  resumeNote?: string;
}

export interface ActivityView extends MaybeFailed {
  jobs: JobView[];
  running: number;
  interrupted: number;
  summary: string;
}

export interface DiscardResult extends MaybeFailed {
  note: string;
}

export const JobsAPI = {
  list: () => call<ActivityView>("JobsController", "List"),
  cancel: (jobId: string) => call<Failure | null>("JobsController", "Cancel", jobId),
  discard: (jobId: string) => call<DiscardResult>("JobsController", "Discard", jobId),
  resume: (jobId: string) => call<StartResult>("JobsController", "Resume", jobId),
};

/* ── Maintenance ───────────────────────────────────────────────────────── */

export interface AuditEntry {
  at: string;
  action: string;
  outcome: string;
  realm?: string;
  snapshotId?: string;
  environment?: string;
  storage?: string;
  detail?: string;
  reason?: string;
  host?: string;
}

export interface AuditView extends MaybeFailed {
  entries: AuditEntry[];
  path: string;
  note: string;
}

export interface CredentialStatus {
  name: string;
  kind: string;
  handle: string;
  present: boolean;
}

export interface ConfigFileView {
  path: string;
  note: string;
  credentials: CredentialStatus[];
}

export interface OrphanView {
  environment: string;
  kind: string;
  ref: string;
  jobId?: string;
  createdAt: string;
  state?: string;
  age: string;
  description: string;
}

export interface OrphanReport {
  orphans: OrphanView[];
  unchecked: { environment: string; reason: string }[];
  note: string;
}

export interface WorkingData {
  indexCount: number;
  indexBytes: number;
  indexNote: string;
  finishedJobs: number;
  finishedBytes: number;
  interruptedJobs: number;
  workBytes: number;
  logBytes: number;
  keeps: string[];
  note: string;
}

export interface PurgeResult extends MaybeFailed {
  removed: string[];
  bytes: number;
  note: string;
}

export const MaintenanceAPI = {
  audit: (action: string, sinceDays: number) =>
    call<AuditView>("MaintenanceController", "Audit", action, sinceDays),
  configFile: () => call<ConfigFileView>("MaintenanceController", "ConfigFile"),
  orphans: () => call<OrphanReport>("MaintenanceController", "Orphans"),
  removeOrphan: (environment: string, ref: string) =>
    call<Failure | null>("MaintenanceController", "RemoveOrphan", environment, ref),
  workingData: () => call<WorkingData>("MaintenanceController", "WorkingData"),
  purge: () => call<PurgeResult>("MaintenanceController", "Purge"),
};

/* ── Progress events ───────────────────────────────────────────────────── */

export interface ProgressEvent {
  jobId: string;
  kind: string;
  phase?: string;
  label?: string;
  message?: string;
  item?: string;
  current?: number;
  total?: number;
  unit?: string;
  attempt?: number;
  retryIn?: number;
  at: string;
}

/**
 * Subscribes to the single event stream every job reports on. The frontend
 * routes by job id rather than the engine opening one channel per job.
 */
export function onProgress(handler: (e: ProgressEvent) => void): () => void {
  return Events.On("portcloak:progress", (event: { data: unknown }) => {
    const payload = Array.isArray(event.data) ? event.data[0] : event.data;
    handler(payload as ProgressEvent);
  });
}
