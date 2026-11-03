# Crash-safety test cases

Keel's main promise is that the database survives anything short of a dead
disk. This page lists every case we test or plan to test. Automated cases
run with `go test ./...` (they need MariaDB installed; see the README).

**Rule behind the design:** anything other than a *confirmed* clean stop
counts as unclean. An unneeded check costs a few seconds; a missed one can
cost a damaged database.

## Automated

### Noticing an unclean shutdown

| # | Case | Expected | Test |
|---|---|---|---|
| 1 | No journal (first run on an old folder, or journal deleted) | Unclean: "no record" | `TestNeedsRecovery`, `TestJournalProblemsTriggerRecovery/journal_missing` |
| 2 | Journal is empty, garbage, or cut off mid-write | Unclean: "can't be read" | `TestReadJournalProblems`, `.../journal_corrupt` |
| 3 | Journal has an empty or unknown state | Unclean | `TestNeedsRecovery` |
| 4 | MariaDB killed from outside Keel (`kill -9`, Task Manager), crash, or power cut | Journal still says running, so unclean | `TestOutsideKillIsDetected` |
| 5 | Keel died in the middle of stopping | Journal says stopping, so unclean | `.../journal_says_stopping` |
| 6 | Force-stopped with Kill | Unclean: "force-stopped" | `TestKillThenStartRecovers` |
| 7 | Stopped, but MariaDB's log doesn't confirm "Shutdown complete" | Unclean: "wasn't confirmed" | `TestUnconfirmedStopCountsAsUnclean` |
| 8 | Journal says clean, but MariaDB's log shows a later start that never shut down | Unclean: "log shows" | `TestLogCatchesKillTheJournalMissed`, `TestReadLogEvidence` |
| 9 | Log has InnoDB's "Shutdown completed" but not MariaDB's final "Shutdown complete" | Not a clean stop | `TestReadLogEvidence` |
| 10 | Log is several MB | Only the last 256 KB is read; result still right | `TestReadLogEvidenceReadsOnlyTheTail` |
| 11 | Log has Windows line endings | Still read correctly | `TestReadLogEvidence` |
| 12 | Normal clean stop | Clean; next start skips recovery | `TestStopIsClean` |
| 13 | Stop is pressed after MariaDB already crashed | Journal stays unclean | `TestStopAfterCrashKeepsJournalDirty` |
| 14 | Two Keels (window and keel-lab) on the same data folder | Both see the same journal | `TestStopFindsServerStartedElsewhere` |

### The journal file itself

| # | Case | Expected | Test |
|---|---|---|---|
| 15 | Many writes in a row | Last write wins; no temporary files left | `TestWriteJournalReplacesAndCleansUp` |
| 16 | Write then read | Same state, PID, snapshot and a timestamp | `TestJournalRoundTrip` |
| 17 | Folder is read-only | Write fails with an error | `TestWriteJournalFailsInReadOnlyFolder` |
| 18 | Journal can't be written before a start | MariaDB is **not** started | `TestStartRefusesWhenJournalCantBeWritten` |
| 19 | Snapshot path across states | Kept until a clean stop, then cleared | `TestSetJournalKeepsSnapshotUntilCleanStop` |

### The safety copy (snapshot)

| # | Case | Expected | Test |
|---|---|---|---|
| 20 | Copy a data folder | Same files, contents, permissions, symlinks, empty files and folders | `TestCopyTreeCopiesEverything` |
| 21 | Copy taken after a kill | Matches the data folder exactly as it was before recovery | `TestKillThenStartRecovers` |
| 22 | Data changes after the copy (as recovery does) | Copy unchanged, even with APFS clones | `TestSnapshotIsCompleteAndIndependent` |
| 23 | File system can't clone | Falls back to a byte-for-byte copy | `TestSnapshotWithoutCloning` |
| 24 | Folder contains a socket | Socket skipped, everything else copied | `TestCopyTreeSkipsSockets` |
| 25 | Several copies in the same second | Each gets its own name | `TestSnapshotNamesAreUnique` |
| 26 | Not enough free disk space | Refuses before copying; leaves nothing behind | `TestSnapshotRefusesWhenDiskIsFull` |
| 27 | A file can't be read mid-copy | Fails; no half-made copy left | `TestFailedSnapshotLeavesNothing` |
| 28 | Copy fails before recovery | MariaDB **not** started, journal unchanged; "start without a copy" works only when chosen | `TestSnapshotFailureBlocksStart` |
| 29 | Recovery fails, then is retried several times | Keeps the copy from before the *first* attempt; no pile-up | `TestFailedRecoveryReusesFirstSnapshot` |
| 30 | New crash after a successful recovery | Gets a fresh copy | `TestCommittedRowsSurviveRepeatedKills` |
| 31 | Old copies | Older than 7 days deleted; newest always kept; the journal's copy kept; abandoned `.partial` copies deleted; in-progress ones and unrelated folders left alone | `TestPruneSnapshots`, `TestPruneKeepsNewestEvenIfOld` |
| 32 | Clean start | No backups folder is created | `TestStartIsReadyAndOurs` |

### Recovery and the table check

| # | Case | Expected | Test |
|---|---|---|---|
| 33 | Kill, then start | Recovers; rows written before the kill are there | `TestKillThenStartRecovers` |
| 34 | 5 kills during constant writes | Every row MariaDB confirmed as committed is still there | `TestCommittedRowsSurviveRepeatedKills` |
| 35 | A table's data file is missing | The check reports that table | `TestRecoveryCheckFindsDamagedTable` |
| 36 | Names with backticks, spaces and emoji; Aria tables; views | All checked correctly; views skipped | `TestCheckTablesHandlesOddNamesAndViews` |
| 37 | CHECK TABLE results (OK, errors, warnings, notes, "Corrupt", nothing) | Classified correctly | `TestClassifyCheck` |
| 38 | Quoting table names | Backticks escaped | `TestQuoteIdent` |
| 39 | Clean start after a recovery | Old recovery report cleared | `TestRecoveryReportClearedOnCleanStart` |
| 40 | MariaDB can't start during recovery | Error quotes MariaDB's log and says where the copy is | `TestFailedRecoveryReusesFirstSnapshot` |

### Creating the data folder (Init)

| # | Case | Expected | Test |
|---|---|---|---|
| 41 | New data folder | Works, counts as cleanly stopped, no temporary folders left | `TestInitCreatesWorkingDataFolder` |
| 42 | Data folder already exists | Left untouched; no journal invented for it | `TestInitLeavesExistingDataAlone` |
| 43 | mariadb-install-db missing or failing | Nothing left behind | `TestInitFailureLeavesNothing` |

### Starting and stopping (from earlier steps)

| # | Case | Test |
|---|---|---|
| 44 | Start waits for a real ping and confirms the server is using our data folder | `TestStartIsReadyAndOurs` |
| 45 | Port already taken: refused before launching, journal untouched | `TestStartRefusesBusyPort` |
| 46 | Same data folder already running (even on another port): refused | `TestStartRefusesWhenAlreadyRunning` |
| 47 | Missing data folder or over-long socket path: refused with a plain message | `TestStartCatchesSetupMistakes` |
| 48 | MariaDB exits during startup: error quotes its log | `TestStartReportsEarlyExit` |
| 49 | Start gives up waiting: server keeps running | `TestCancelledStartLeavesServerRunning` |
| 50 | SQL unreachable: Stop falls back to SIGTERM, still clean | `TestStopNudgesWhenSQLUnreachable` |
| 51 | Frozen server: Stop gives up but **never kills** | `TestStopNeverKills` |
| 52 | Stale PID file pointing at another program: ignored by Stop and Kill | `TestStalePIDFileIsIgnored` |
| 53 | Nothing running: Stop says so | `TestStopWhenNotRunning` |

### The window

| # | Case | Test |
|---|---|---|
| 54 | Card shows Running / Stopped / busy (with spinner) correctly | `TestRowFollowsServerState` |
| 55 | Background checks don't overwrite "Starting…" / "Stopping…" | `TestBusyIgnoresBackgroundChecks` |
| 56 | Clean shutdown shows in the status bar; an unclean one shows the warning banner; neither while running; the reason is logged once | `TestShutdownStatusIsShown` |
| 57 | A crash and a clean stop from elsewhere are logged differently | `TestOutsideStopIsLoggedByKind` |
| 58 | Recovery and "start without a copy" dialogs open without errors | `TestRecoveryDialogsDontPanic` |
| 59 | Activity log stays capped at 500 lines | `TestLogIsCapped` |
| 60 | Copy puts the whole activity log on the clipboard | `TestCopyActivity` |
| 61 | Progress messages get the right mark (none, tick, warning, error) | `TestProgressLevel` |
| 62 | Keel's colours exist in both light and dark mode, and differ | `TestThemeHasBothModes` |

### The data-folder lock

Two Keels (two windows, or a window and keel-lab) must never start, stop or recover the same MariaDB at once.

| # | Case | Test |
|---|---|---|
| 63 | The lock is exclusive between two Keels; the lock file stays in place | `TestLockIsExclusive` |
| 64 | Another process holds the lock: busy. Kill that process: the OS frees the lock, so it never goes stale | `TestLockAcrossProcesses` |
| 65 | While locked, Start, Start without a copy, Stop, Kill and Init all refuse and change nothing | `TestBusyFolderRefusesEverything` |
| 66 | Two Starts pressed at the same moment: exactly one MariaDB starts | `TestTwoStartsAtOnce` |
| 67 | The window explains "another Keel is busy" for a start and a force-stop | `TestBusyLockIsExplained` |
| 68 | A failed force-stop puts the card back instead of leaving it on "Stopping…" | `TestFailedForceStopRestoresTheCard` |

### Ports

| # | Case | Test |
|---|---|---|
| 69 | A port counts as taken when something listens on 127.0.0.1, on every address (`*:port`), or on `::1` only | `TestFreeAndAnswers`, `TestAllAddressesListenerCountsAsTaken`, `TestIPv6OnlyListenerCountsAsTaken` |
| 70 | Impossible ports are rejected, by the checks and by `-port` | `TestFreeRejectsImpossiblePorts`, `TestPortFlagRejectsNonsense` |
| 71 | The next free port skips taken and ruled-out ports, gives up after 100, and never goes past 65535 | `TestNextPassesOverTakenPorts`, `TestNextGivesUp` |
| 72 | The program holding a port is named, with "from XAMPP" when its path says so | `TestWhoFindsTheListener`, `TestWhoOnFreePort`, `TestHolderString` |
| 73 | Port taken: MariaDB moves to the next free port, the note names the holder, and the new port is remembered; `-port` still overrides | `TestChoosePortMovesAndRemembers` |
| 74 | Port free: nothing moves and nothing is saved | `TestChoosePortKeepsAFreePort` |
| 75 | The choice can't be saved: MariaDB still moves, with a warning. A broken settings file is ignored | `TestChoosePortStillMovesWhenSettingsCantBeSaved`, `TestBrokenSettingsFileIsIgnored` |
| 76 | The window shows the new port and why; no free port stops the start with a clear message | `TestStartMovesPortAndSaysWhy`, `TestNoFreePortStopsTheStart` |

## Still to cover

These need real hardware, features that aren't built yet, or a longer run.

| # | Case | Why it matters | Blocked on |
|---|---|---|---|
| 77 | Power cut to the whole machine during writes (`prlctl stop "Windows 11" --kill`) | The real XAMPP failure | Windows support |
| 78 | OS restart or shutdown while MariaDB runs | Must stop cleanly before the OS kills it | Background service (phase 2) |
| 79 | Laptop sleep and hibernate mid-write | Server frozen, then resumed | Manual test |
| 80 | Disk really full during recovery (not simulated) | MariaDB needs space to recover | Manual test |
| 81 | Data on exFAT/USB, or backups on another drive | No cloning; plain copy across drives | Manual test |
| 82 | Linux on Btrfs/XFS (reflink) and ext4 (plain copy) | Different copy paths | Linux test machine |
| 83 | Windows antivirus locking files mid-copy; OneDrive folders | Copy failures on Windows | Windows support |
| 84 | Keel killed during the safety copy | Must never use a `.partial` copy | Add an automated test |
| 85 | Damaged Aria system tables (`mysql.global_priv`), the exact XAMPP error | Needs the repair flow | Repair and restore buttons |
| 86 | Restoring from a safety copy | Needs the restore feature | Restore button |
| 87 | Multi-GB databases | Copy and check time, progress feedback | Manual test |
| 88 | Clock or time-zone changes | Copy names and pruning use local time | Add an automated test |
| 89 | Backups folder read-only or full | Clear message, MariaDB not started | Add an automated test |
| 90 | Data folder moved or renamed between runs | Journal and backups follow the folder | Add an automated test |
| 91 | MariaDB upgraded between an unclean shutdown and the next start | Recovering with a newer version can be unsafe; should refuse | Upgrade handling |
| 92 | Server started by hand with a *different* log file, then killed | Neither the journal nor the log sees it; MariaDB's own recovery still runs | Manual test |
| 93 | 200 start/stop cycles under writes, plus an overnight soak | Phase 1 exit test | Torture-test command |
| 94 | A port held by another user's program or a system service (XAMPP's MySQL installed as a Windows service) | Windows names it; macOS and Linux can only say "another program" without admin rights | Windows support |
