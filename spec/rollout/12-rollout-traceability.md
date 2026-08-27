# 12 — Rollout Traceability

Every use case and every requirement mapped to the phase that delivers it and the test that
would fail if it stopped being met, plus the reverse check that no phase exists without
something to deliver. [11 — Traceability](../11-traceability.md) maps requirements to *modules*;
this maps them to *time*, and to *evidence*.

The evidence columns name tests that exist in the repository. They are checked against
`go test ./internal/... -list '.*'` rather than maintained by hand, so a renamed test shows up
here as a broken reference instead of quietly becoming fiction.

**How to read the phase column.** Where two phases appear, the first delivers the capability and
the second completes its proof — most often P2 producing something and P7 proving it survives a
round trip.

## 12.1 Use cases → phase and evidence

Rows marked *(integration)* run under `-tags=integration`. Rows marked *(demo)*
are proved by the scripted walkthrough in the phase document rather than by an
assertion — "does this error message actually help me?" has no assertion form
([01 §1.2](./01-test-strategy.md)).

### Environments (9)

| UC | Title | Phase | Evidence |
|----|-------|:-----:|----------|
| UC-E1 | Add a Local environment | P1 | `TestConfig_EnvironmentKinds_RoundTrip` |
| UC-E2 | Add an SSH environment | P1 | `TestConfig_EnvironmentKinds_RoundTrip`; `TestHandle_Validity` |
| UC-E3 | Add a Docker environment | P1 | `TestConfig_EnvironmentKinds_RoundTrip` |
| UC-E4 | Add a Kubernetes / OpenShift environment | P1 | `TestConfig_EnvironmentKinds_RoundTrip` |
| UC-E5 | Test an environment | P1 | `TestLocal_Contract/probe_reads_facts_and_changes_nothing`; `TestReachable` |
| UC-E6 | Edit an environment | P1 | `TestConfig_CRUD`; `TestConfig_UnknownFieldsSurviveAChange` |
| UC-E7 | Duplicate an environment | P1 | `TestConfig_CRUD`; `TestResolve_NamesTheEntryThatNeedsReentering` |
| UC-E8 | Delete an environment | P1 | `TestConfig_CRUD` |
| UC-E9 | Review environments at a glance | P1 | `TestConfig_CRUD` *(demo)* |

### Storage (8)

| UC | Title | Phase | Evidence |
|----|-------|:-----:|----------|
| UC-S1 | Add Disk storage | P1 | `TestConfig_StorageKinds_RoundTrip`; `TestDisk_ExpandsHome` |
| UC-S2 | Add SSH storage | P1 · P3 | `TestSFTP_Contract` *(integration)* |
| UC-S3 | Add S3-compatible storage | P1 · P5 | `TestS3_Contract` *(integration)* |
| UC-S4 | Add Azure Blob / Azurite storage | P1 · P5 | `TestAzure_Contract` *(integration)* |
| UC-S5 | Test a storage | P1 | `TestDisk_ProbeDistinguishesMissingFromUnwritable`; `TestDisk_ProbeReportsReadOnlyRatherThanFailing` |
| UC-S6 | Edit or delete a storage | P1 | `TestConfig_DeleteStorageKeepsTheDataAndGuardsTheDefault` |
| UC-S7 | Set the default storage | P1 | `TestConfig_DefaultStorageIsExclusive` |
| UC-S8 | Browse the contents of a storage | P5 | `TestGroup_SeparatesSnapshotsFromForeignObjects`; `TestDisk_LayoutIsBrowsable` |

*S2, S3 and S4 are definable and testable in P1; the working transfer path lands with the
backend itself (SFTP in P3, S3 and Azure in P5).*

### Capture (12)

| UC | Title | Phase | Evidence |
|----|-------|:-----:|----------|
| UC-C1 | Capture a realm from a Local environment | P2 | `TestCapture_Local_ProducesASealedBundleAndBothSidecars` |
| UC-C2 | Capture a realm over SSH | P3 | `TestSSH_ExecutorContract` *(integration)* |
| UC-C3 | Capture a realm from Docker via an ephemeral clone | P3 | `TestCapture_ThroughAnEphemeralClone`; `TestDocker_ExecutorContract` *(integration)* |
| UC-C4 | Capture a realm from Kubernetes / OpenShift via an ephemeral clone | P3 | `TestKubernetes_ExecutorContract` *(integration)* |
| UC-C5 | Capture several realms in one run | P2 | `TestCapture_MultiRealm_ProducesOneBundlePerRealm`; `TestCapture_MultiRealmSharesOneClone`; `TestCapture_MultiRealm_PartialFailure` |
| UC-C6 | Probe an environment before capture | P1 | `TestLocal_Contract/probe_reads_facts_and_changes_nothing`; `TestCapture_UnknownEnvironmentOrStorageIsNamed` |
| UC-C7 | Isolate the export with free ports | P2 | `TestAllocate_ReturnsThreeDistinctFreePorts`; `TestCapture_UsesOfflineExportWithTheOptionsKcAccepts`; `TestAllocate_DoesNotLeakFileDescriptors` |
| UC-C8 | Verify exported secrets are unmasked | P8 | `TestSecretVerification_DetectsMask`; `TestSecretVerification_MaskedKeyMaterialIsFlagged`; `TestSecretVerification_AdminAPIMaskingIsNotAFinding` |
| UC-C9 | Detect external dependencies | P8 | `TestDependencyScan_FindsWhatTheRealmActuallyReferences`; `TestDependencyScan_NoFalsePositives` |
| UC-C10 | Encrypt a snapshot (opt-in) | P5 | `TestCrypto_RoundTrip_Recipients`; `TestCapture_EncryptionRequiredStorageRejectsPlaintext`; `TestKeys_GeneratedKeyIsStoredAndUsable`; `TestKeys_ImportDerivesThePublicHalf` |
| UC-C11 | Destroy the ephemeral clone | P3 | `TestCapture_CloneIsDestroyedBeforePackaging`; `TestTeardown_AllExitPaths`; `TestCapture_FailedTeardownIsRaisedNotSwallowed` |
| UC-C12 | Reap orphaned clones | P3 | `TestOrphans_AreListedOldestFirstAndRemovedOnRequest` |

### Inspection (13)

| UC | Title | Phase | Evidence |
|----|-------|:-----:|----------|
| UC-I1 | Browse the snapshot library | P6 | `TestLibrary_AllBackendsNoKey`; `TestLibrary_UnencryptedBundleIsLabelled` |
| UC-I2 | Open a snapshot and view its details | P6 | `TestOpen_Tier1_ReadsFullDetail`; `TestOpen_EncryptedRoundTripAndWrongKey`; `TestOpen_UsesAStoredIdentityWithoutBeingAsked`; `TestOpen_UsesAStoredPassphraseWithoutBeingAsked`; `TestOpen_SaysWhatItTried` |
| UC-I3 | Build the inspection index | P6 | `TestIndexSchemaHasNoSecretColumns`; `TestIndex_HoldsNoSecretMaterial` |
| UC-I4 | Search users within a snapshot | P6 | `TestIndex_SearchPageAndFacets` |
| UC-I5 | Filter and facet users | P6 | `TestIndex_FacetsIntersectWithSearch` |
| UC-I6 | View a single user's detail | P6 | `TestUserDetail_IsCredentialPresenceOnly` |
| UC-I7 | Browse clients, keys, federations and flows | P6 | `TestOpen_Tier1_ReadsFullDetail`; `TestBuild_KeyProvidersCarryTheirTypeAndPrivateFlag` |
| UC-I8 | View the secret ledger | P6 | `TestLedger_ContainsNoValuesAndSaysWhatIsRevealable`; `TestLedger_ContainsNoValues` |
| UC-I9 | Reveal a single secret | P6 | `TestReveal_WritesAuditAndNeverLogs`; `TestReveal_FromAnUnencryptedSnapshotSaysSo`; `TestReveal_RefusalsAreExplained` |
| UC-I10 | Review external dependencies | P6 · P8 | `TestSession_DependenciesAreTheSameRecordsAsTheManifest`; `TestBuild_KeystoreFileIsReportedAsADependency` |
| UC-I11 | Verify a snapshot without restoring | P6 | `TestVerify_ReportsPerArtifactAndContactsNothing`; `TestOpen_TamperedBundleOpensDegradedAndNamesTheArtifact` |
| UC-I12 | Export an inspection view | P6 | `TestExport_IsRedactedAndAudited`; `TestExport_UnwritableDestinationNamesThePath` |
| UC-I13 | Close a snapshot | P6 | `TestSession_CloseDestroysTheIndexAndWorkingFiles` |

### Restore (8)

| UC | Title | Phase | Evidence |
|----|-------|:-----:|----------|
| UC-R1 | Restore a snapshot into an environment | P7 | `TestRestore_AppliesAndValidates`; `TestRestoreWizard_NeverHardcodesAnEmptyKey`; `TestOpen_ASuppliedKeyIsNotAttributedToAStoredOne` |
| UC-R2 | Review restore preconditions | P7 | `TestPreconditions_NeverBlocks`; `TestPreconditions_NotCheckedIsNotNone` |
| UC-R3 | Preview changes with a dry run | P7 | `TestDryRun_ReflectsStrategy`; `TestDryRun_NotesTheThingsThatBreakLogins`; `TestDryRun_UnreadableTargetIsSaidNotHidden` |
| UC-R4 | Choose an import strategy | P7 | `TestBuildImport_Strategies`; `TestStrategyExplanation_IsAboutResourcesNotFlags` |
| UC-R5 | Apply the import | P7 | `TestRestore_MergeUsesPartialImportAndSaysSoWhenItCannot`; `TestRestore_PartialApplicationIsReportedHonestly` |
| UC-R6 | Validate after restore | P7 | `TestPostRestoreValidation_ChecksTheSigningKeyByKID`; `TestPostRestoreValidation_UnreachableIsNotPassed` |
| UC-R7 | Restore into a freshly provisioned Keycloak | P7 | `TestDryRun_EmptyTarget` |
| UC-R8 | Cancel a restore | P7 | `TestRestore_OverwriteRequiresConfirmingTheRealmName`; `TestLocal_Contract/a_cancelled_run_stops_rather_than_finishing` |

### Operations (10)

| UC | Title | Phase | Evidence |
|----|-------|:-----:|----------|
| UC-O1 | Monitor running work | P4 | `TestCapture_ProgressEventsReachTheSink`; `TestCapture_PhasesAreWrittenOntoTheJobRecord`; `TestCapture_SharedPhasesReachEveryRealmInTheBatch` |
| UC-O2 | Resume an interrupted job | P4 | `TestResume_ConvergesOnTheSameObject`; `TestCapture_InterruptedUploadLeavesAResumableJob`; `TestResume_RestartsWhenTheSealedBundleIsGone`; `TestResume_RestoreIsNotResumedAutomatically` |
| UC-O3 | Cancel a job | P4 | `TestCapture_CancelRunsTeardown`; `TestCapture_CancelDestroysTheClone` |
| UC-O4 | Discard an interrupted job | P4 | `TestResume_TerminalJobsAreNotOffered`; `TestSweep_RemovesIndexesAndWorkDirsACrashLeftBehind` |
| UC-O5 | Understand a failure | P4 | `TestError_CarriesAdviceAndEndpoint`; `TestClassifyFailure_ProducesSentencesNotExitCodes`; `TestAuthFailure_IsPlainAndNotRetried` |
| UC-O6 | Survive a flaky connection | P4 | `TestWrapStore_RetriesTransientFailures`; `TestWrapStore_RetriesFromTheLastCheckpoint`; `TestBreaker_OpensRecoversAndSaysSo`; `TestRetrier_CancellationDuringBackoffReturnsPromptly` |
| UC-O7 | Edit configuration outside the app | P0 | `TestConfigRoundTrip_IsByteStable`; `TestConfig_UnknownFieldsSurviveAChange`; `TestConfig_MalformedFileNamesEveryProblemWithALine` |
| UC-O8 | Review the audit log | P4 | `TestAuditLog_IsRedacted`; `TestCapture_RecordsProvenanceAndAudit`; `TestKeys_RevealIsAudited` |
| UC-O9 | Start the application | P0 | `TestHome_BootstrapIsIdempotentAndSelfHealing` |
| UC-O10 | Purge local working data | P6 | `TestSweep_RemovesIndexesAndWorkDirsACrashLeftBehind`; `TestSession_CloseDestroysTheIndexAndWorkingFiles` |

## 12.2 Functional requirements → phase and evidence

Evidence is the test that would fail if the requirement stopped being met. Where
a requirement is proved by a contract table, the table is named rather than each
implementation that runs it — that is the point of having one.

Rows marked *(integration)* run under `-tags=integration` against real service
containers. A missing container makes them "not run", never a silent pass
([01 §1.2](./01-test-strategy.md)).

### Capture

| Req | Phase | Evidence |
|-----|:-----:|----------|
| FR-C1 local capture | P2 | `TestCapture_Local_ProducesASealedBundleAndBothSidecars` |
| FR-C2 capture over SSH | P3 | `TestSSH_ExecutorContract` *(integration)* |
| FR-C3 capture from Docker | P3 | `TestDocker_ExecutorContract` *(integration)*; `TestCapture_ThroughAnEphemeralClone` |
| FR-C4 capture from Kubernetes | P3 | `TestKubernetes_ExecutorContract` *(integration)*; `TestCapture_ThroughAnEphemeralClone` |
| FR-C5 users in separate files | P2 | `TestReadLayout_OrdersUserFilesNumerically`; `TestBuild_UsersInTheRealmFileAreCountedToo` |
| FR-C6 verify secrets via Admin API | P8 | `TestSecretVerification_DetectsMask`; `TestSecretVerification_UnknownClientIsReportedNotAssumedGood` |
| FR-C7 detect version and `kc.sh` | P1 · P2 | `TestParseVersion_AcrossBannerShapes`; `TestLocal_Contract/probe_reads_facts_and_changes_nothing` |
| FR-C8 offline export is primary | P2 | `TestCapture_UsesOfflineExportWithTheOptionsKcAccepts`; `TestBuildExport_DefaultInvocation` |
| FR-C9 ephemeral clone on Docker/K8s | P3 | `TestLabelTrap_ACloneCarriesOnlyPortCloaksOwnLabels`; `TestCloneSpec_Derivation` |
| FR-C10 automatically free ports | P2 | `TestAllocate_ReturnsThreeDistinctFreePorts`; `TestCapture_RetriesAPortConflictWithFreshPorts` |
| FR-C11 always clean up | P3 | `TestCapture_TeardownRunsOnEveryExitPath`; `TestTeardown_AllExitPaths`; `TestLocal_Contract/teardown_is_idempotent_and_safe_with_nothing_prepared` |

### Fidelity

Each of FR-F1…F9 is captured in P2 and proved in P7. The capture-side assertion
shows the bundle holds the material; the round-trip assertion shows the
destination can use it. Only the second proves the promise.

| Req | Phase | Evidence |
|-----|:-----:|----------|
| FR-F1 password hashes | P2 · P7 | `TestLedger_EnumeratesEverySecretBearingLocation`; `TestUserDetail_IsCredentialPresenceOnly`; `TestRestore_AppliesAndValidates` |
| FR-F2 OTP and WebAuthn credentials | P2 · P7 | `TestBuild_CredentialCounts`; `TestRestore_AppliesAndValidates` |
| FR-F3 client secrets unmasked | P2 · P8 | `TestBuild_MaskedSecretIsFlaggedPartialAndNamesTheClient`; `TestSecretVerification_DetectsMask` |
| FR-F4 key providers with private keys | P2 · P7 | `TestBuild_KeyProvidersCarryTheirTypeAndPrivateFlag`; `TestManifest_TokenContinuity`; `TestPostRestoreValidation_ChecksTheSigningKeyByKID` |
| FR-F5 LDAP/Kerberos federation | P2 · P7 | `TestBuild_FederationRecordsWhetherTheBindTravelled`; `TestDryRun_NotesTheThingsThatBreakLogins` |
| FR-F6 identity providers | P2 · P7 | `TestLedger_EnumeratesEverySecretBearingLocation`; `TestBuild_InventoryMatchesTheSource` |
| FR-F7 clients, scopes, roles, groups | P2 · P7 | `TestBuild_InventoryMatchesTheSource`; `TestRestore_AppliesAndValidates` |
| FR-F8 flows and authenticator configs | P2 · P7 | `TestLedger_EnumeratesEverySecretBearingLocation`; `TestIndex_SearchPageAndFacets` |
| FR-F9 realm settings including SMTP | P2 · P7 | `TestBuild_InventoryMatchesTheSource`; `TestLedger_EnumeratesEverySecretBearingLocation` |

### Configuration

| Req | Phase | Evidence |
|-----|:-----:|----------|
| FR-N1 multiple environments | P1 | `TestConfig_EnvironmentKinds_RoundTrip`; `TestConfig_CRUD` |
| FR-N2 multiple storage definitions | P1 | `TestConfig_StorageKinds_RoundTrip` |
| FR-N3 test on demand | P1 | `TestDisk_Contract`; `TestLocal_Contract/probe_reads_facts_and_changes_nothing`; `TestReachable` |
| FR-N4 create, edit, duplicate, delete | P1 | `TestConfig_CRUD`; `TestConfig_DeleteStorageKeepsTheDataAndGuardsTheDefault` |
| FR-N5 default storage | P1 | `TestConfig_DefaultStorageIsExclusive` |
| FR-N6 human-readable config file | P0 | `TestConfigRoundTrip_IsByteStable`; `TestConfig_UnknownFieldsSurviveAChange`; `TestConfig_MalformedFileNamesEveryProblemWithALine` |

### Dependencies

| Req | Phase | Evidence |
|-----|:-----:|----------|
| FR-D1 detect external dependencies | P8 | `TestDependencyScan_FindsWhatTheRealmActuallyReferences`; `TestDependencyScan_NoFalsePositives`; `TestBuild_KeystoreFileIsReportedAsADependency` |
| FR-D2 surface as restore preconditions | P7 | `TestPreconditions_NeverBlocks`; `TestPreconditions_NotCheckedIsNotNone` |

### Storage

| Req | Phase | Evidence |
|-----|:-----:|----------|
| FR-S1 local disk folder | P2 | `TestDisk_Contract`; `TestDisk_LayoutIsBrowsable` |
| FR-S2 remote folder over SFTP | P3 | `TestSFTP_Contract`, `TestSFTP_OffsetResumeContract` *(integration)* |
| FR-S3 S3-compatible with multipart | P5 | `TestS3_Contract`, `TestS3_ResumableContract` *(integration)* |
| FR-S4 Azure Blob against Azurite | P5 | `TestAzure_Contract`, `TestAzure_ResumableContract` *(integration)* |
| FR-S5 list, retrieve, delete | P5 | `TestDisk_Contract`; `TestLibrary_AllBackendsNoKey` |
| FR-S6 one snapshot, one realm | P2 | `TestCapture_MultiRealm_ProducesOneBundlePerRealm`; `TestLayout_KeysAreRootedAtThePrefixAndPartitionedByRealm` |

### Restore

| Req | Phase | Evidence |
|-----|:-----:|----------|
| FR-R1 restore via import or partialImport | P7 | `TestRestore_AppliesAndValidates`; `TestRestore_MergeUsesPartialImportAndSaysSoWhenItCannot` |
| FR-R2 dry-run diff and manifest preview | P7 | `TestDryRun_ReflectsStrategy`; `TestDryRun_EmptyTarget`; `TestDryRun_UnreadableTargetIsSaidNotHidden` |
| FR-R3 overwrite / skip / merge | P7 | `TestBuildImport_Strategies`; `TestRestore_OverwriteRequiresConfirmingTheRealmName` |
| FR-R4 verify and decrypt before restore | P7 | `TestRestore_RefusesTamperedBundleBeforeContactingTarget`; `TestVerifyDecryptable_CatchesABundleNobodyCanOpen` |

### Manifest

| Req | Phase | Evidence |
|-----|:-----:|----------|
| FR-M1 manifest of categories and secrets | P2 | `TestBuild_InventoryMatchesTheSource`; `TestLedger_EnumeratesEverySecretBearingLocation` |
| FR-M2 completeness report | P2 | `TestCompleteness_OutOfScopeIsNotMissing`; `TestCompleteness_SkippedChecksAreNotReportedAsClean` |
| FR-M3 machine-readable and rendered | P2 | `TestBuild_SidecarIsSecretFree`; `TestCapture_SidecarIsReadableWithoutAnyKey` |

### Inspection

| Req | Phase | Evidence |
|-----|:-----:|----------|
| FR-V1 library across all backends | P6 | `TestLibrary_AllBackendsNoKey`; `TestLibrary_UnreachableStorageIsNotSilentlyShort` |
| FR-V2 snapshot details | P6 | `TestOpen_Tier1_ReadsFullDetail` |
| FR-V3 browse users | P6 | `TestIndex_SearchPageAndFacets` |
| FR-V4 filter and facet | P6 | `TestIndex_FacetsIntersectWithSearch` |
| FR-V5 individual user detail | P6 | `TestUserDetail_IsCredentialPresenceOnly` |
| FR-V6 every other entity type | P6 | `TestSession_DependenciesAreTheSameRecordsAsTheManifest`; `TestIndex_SearchPageAndFacets` |
| FR-V7 audited single-secret reveal | P6 | `TestReveal_WritesAuditAndNeverLogs`; `TestReveal_RefusalsAreExplained` |
| FR-V8 verify without restoring | P6 | `TestVerify_ReportsPerArtifactAndContactsNothing` |
| FR-V10 export an inspection view | P6 | `TestExport_IsRedactedAndAudited`; `TestExport_UsersCarriesPresenceNotValues` |

Withdrawn and therefore delivered by nobody: **FR-F10** (sessions,
[D1](../12-decisions.md)) and **FR-V9** (snapshot comparison). Both are verified
negatively — the tool reports them as out of scope rather than silently omitting
them: `TestCompleteness_OutOfScopeIsNotMissing`.

## 12.3 Non-functional requirements → phase and evidence

| Req | Phase | Where it is proved |
|-----|:-----:|--------------------|
| NFR-1 Resilience | P4 | `TestRetrier_RetriesRetryableAndStopsAtBudget`, `TestBreaker_OpensRecoversAndSaysSo`, `TestWrapStore_RetriesFromTheLastCheckpoint`, `TestResume_ConvergesOnTheSameObject`. |
| NFR-2 Integrity | P2 · P7 | `TestIntegrityTree_DetectsSingleByteFlip`, `TestIntegrityTree_RootBindsNameToContent`; restore refusal in `TestRestore_RefusesTamperedBundleBeforeContactingTarget`. |
| NFR-3 Security | P0 · P5 | Redaction CI stage over `TestRedaction_SensitiveKeys`, `TestRedaction_ValueShapes`, `TestRedaction_HandlerScrubsMessage`, `TestRedaction_LogValuerAndStringer` and `TestAuditLog_IsRedacted` (P0); `TestCrypto_RoundTrip_Passphrase`, `TestCrypto_WrongKey`, `TestLabels_AreTheOnlyOnesApplied` (P5). |
| NFR-4 Portability | P0 | Three platform binaries built and launched standalone in CI; `modernc.org/sqlite` keeps the build cgo-free. |
| NFR-5 Observability | P0 · P4 | `TestError_CarriesAdviceAndEndpoint`, `TestClassifyFailure_ProducesSentencesNotExitCodes` (P0); `TestCapture_ProgressEventsReachTheSink`, `TestCapture_RecordsProvenanceAndAudit` (P4). |
| NFR-6 Performance | P2 · P5 | `TestPackager_BoundedMemory`, `TestCrypto_BoundedMemory`. |
| NFR-7 Least privilege | P1 · P3 | `TestLocal_Contract/probe_reads_facts_and_changes_nothing`, `TestLabelTrap_ACloneCarriesOnlyPortCloaksOwnLabels`, `TestExecutor_RefusesToUseATornDownClone`. |
| NFR-8 Idempotence | P2 · P4 | `TestPackager_Deterministic`, `TestPackager_NormalisesArchiveMetadata`, `TestResume_ConvergesOnTheSameObject`, `TestDisk_OffsetResumeContract`. |
| NFR-9 Inspection responsiveness | P6 | Recorded latency and memory numbers on the `large` fixture; `TestIndex_SearchPageAndFacets`. |
| NFR-10 No inspection residue | P6 | `TestSession_CloseDestroysTheIndexAndWorkingFiles`, `TestSweep_RemovesIndexesAndWorkDirsACrashLeftBehind`, `TestIndexSchemaHasNoSecretColumns`, `TestIndex_HoldsNoSecretMaterial`. |
| NFR-11 File-based configuration | P0 | `TestConfigRoundTrip_IsByteStable`, `TestConfig_SaveIsAtomic`, `TestConfig_ContainsNoSecretMaterial`; SQLite used only for inspection indexes. |

## 12.4 Reverse check — every phase earns its place

| Phase | Delivers | Would anything be lost by cutting it? |
|-------|----------|---------------------------------------|
| P0 | 2 UC, FR-N6, NFR-4, NFR-11, and the redaction floor | Everything. Also the only point at which redaction can be established before secrets exist. |
| P1 | 17 UC, FR-N1…N5, and `Probe` | The tool could not reach anything, and `Test` would drift from capture. |
| P2 | 3 UC, 18 FR, 3 NFR | The entire capture pipeline. |
| P3 | 5 UC, 6 FR, NFR-7 | Every target except the operator's own laptop. |
| P4 | 7 UC, 3 NFR | The tool would be unusable over any real network. |
| P5 | 2 UC, 3 FR, NFR-3 | Cloud storage and encryption. |
| P6 | 14 UC, 9 FR, 2 NFR | The ability to know what is in a snapshot before trusting it. |
| P7 | 8 UC, 5 FR | The other half of the product. |
| P8 | 2 UC, 2 FR | The guarantee that a carried secret is a real secret. |

Every use case appears exactly once as an owner. Every non-withdrawn requirement has a phase.
No phase delivers nothing.

## 12.5 Coverage totals

| | Count | Has a phase | Has named evidence |
|---|---:|---:|---:|
| Use cases | 60 | 60 | 60 |
| Functional requirements (active) | 50 | 50 | 50 |
| Functional requirements (withdrawn) | 2 | — | verified negatively |
| Non-functional requirements | 11 | 11 | 11 |

`build/ci/check-traceability.sh` verifies every test named above still exists, and CI runs it on
every push. 144 distinct tests are cited across the three tables.

**What the evidence does not cover.** Nine use cases and requirements rest partly or wholly on
tests behind `-tags=integration` — the SSH, Docker and Kubernetes targets, and the SFTP, S3 and
Azure stores. Those run in CI against real service containers rather than on every commit, so a
local `go test ./...` proves less than the table implies. Two rows are marked *(demo)*: the
question "is this readable at a glance" has no assertion form and is answered by the scripted
walkthrough in the phase document instead.
