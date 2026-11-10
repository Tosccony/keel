# Test cases

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
| 50 | Root has a password, so SQL SHUTDOWN is refused: Stop falls back to SIGTERM, still clean | `TestStopNudgesWhenSQLIsRefused` |
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

### Repair

Repair works even when MariaDB won't start. These cases use real MariaDB and the exact damage behind XAMPP's best-known errors.

| # | Case | Test |
|---|---|---|
| 77 | `mysql.global_priv` (the accounts) has a wiped index header: MariaDB quits with "Can't open and lock privilege tables". Repair fixes it in repair mode; root can log in; all tables pass | `TestRepairFixesDamagedAccountsTable` |
| 78 | Aria's control file is garbage: MariaDB quits with "Unknown storage engine 'Aria'". Repair rebuilds Aria's log files and zerofills every Aria table | `TestRepairRebuildsDamagedAriaLog` |
| 79 | A MyISAM table's index is damaged: repaired with its rows intact. An InnoDB table's file is missing: reported as needing a restore, not "repaired" | `TestRepairFixesMyISAMAndReportsInnoDB` |
| 80 | Nothing is wrong: Repair says so, and leaves the last shutdown clean | `TestRepairWithNothingWrong` |
| 81 | The copy Repair saves first holds the data exactly as it was, damage included | `TestRepairFixesDamagedAccountsTable`, `TestRepairRebuildsDamagedAriaLog` |
| 82 | Repair, Back up and Restore refuse while MariaDB runs, and leave nothing behind | `TestRepairBackUpAndRestoreNeedMariaDBStopped` |
| 83 | Aria log damage is recognised from MariaDB's own error lines | `TestAriaLogDamaged` |
| 84 | Stop reaches the server on the port it really runs on, even if Keel's setting changed since (on Windows a wrong port meant a force-stop) | `TestStopUsesTheServersRealPort` |
| 85 | A start that dies offers Repair, quoting MariaDB's "Fatal error" line | `TestStartFailureOffersRepair`, `TestStartErrorLine` |
| 86 | The repair summary: repaired, nothing to do, damage left, didn't finish | `TestRepairSummary` |

### Back up and restore

| # | Case | Test |
|---|---|---|
| 87 | Back up, change a row, restore: the row is back; the next start recovers and checks, using the backup as its fallback instead of copying again | `TestBackUpAndRestoreWithMariaDB` |
| 88 | Restore saves the current data first; restoring that copy undoes the restore; no temporary folders are left | `TestRestoreSwapsAndCanBeUndone` |
| 89 | Restore when the data folder is missing | `TestRestoreIntoMissingDataFolder` |
| 90 | Restore refuses folders outside the backups folder, unfinished copies, and folders that aren't MariaDB data, and changes nothing | `TestRestoreRejectsBadCopies` |
| 91 | Keel dies between the restore's two renames: the next start says where the old data is | `TestInterruptedRestoreIsExplained` |
| 92 | The copies are listed newest first with their kind; unfinished copies and other folders are left out | `TestBackupsListsNewestFirst` |
| 93 | The user's own backups are never pruned; Keel's own copies are pruned after a week, keeping the newest of each kind | `TestPruneNeverDeletesTheUsersBackups` |
| 94 | The restore list gives every copy a different label | `TestBackupLabelsAreUnique` |
| 95 | The ⋯ menu follows the state: Check only while running, everything off while busy | `TestMoreMenuFollowsState` |
| 96 | Back up, Restore and Repair report their result and put the card back | `TestMaintenanceRunsWorkThenReports` |

### Shutting down without Keel

| # | Case | Test |
|---|---|---|
| 117 | The computer shuts down or the user signs out: the system sends SIGTERM, MariaDB stops cleanly by itself, and its log confirms it, so the next start needs no recovery | `TestSystemShutdownSignalIsClean` |
| 118 | The log is trusted only for the run Keel recorded (same PID, no later start): a crash early in a new start doesn't borrow the previous run's clean shutdown | `TestRunEndedCleanly`, `TestNeedsRecovery` |
| 119 | A stop Keel couldn't confirm counts as clean once MariaDB's own log shows that run shut down cleanly; without the line it stays unclean | `TestConfirmationInTheLogCountsLater`, `TestUnconfirmedStopCountsAsUnclean` |

### The other parts

| # | Case | Test |
|---|---|---|
| 120 | Mail catcher: plain mail with a Bcc only the envelope knows; HTML with a text alternative, encoded headers and a base64 attachment; dot-stuffing; long lines; any login accepted | `TestCatchesPlainMail`, `TestCatchesHTMLMailWithAttachment`, `TestSMTPEdgeCases` |
| 121 | Mail catcher's inbox page: the email's HTML is sandboxed, paths outside the inbox are refused, and a foreign Host header (DNS rebinding) gets nothing | `TestInboxPageAndAPI` |
| 122 | Mail catcher: start, stop, restart at once, a taken port, oldest mail pruned | `TestStartAndStop`, `TestOldMessagesArePruned` |
| 123 | PHP's mail() on macOS and Linux goes through "keel --sendmail": recipients from headers or arguments, Bcc removed, a clear error when the catcher is stopped | `TestSendmailStandIn`, `TestWebPartServesPHPAndCatchesItsMail` |
| 124 | FTP server: upload, list, download, rename, delete; wrong logins refused; nothing outside the websites folder is reachable | `TestUploadListDownload`, `TestWrongLoginIsRefused`, `TestCantLeaveTheWebsitesFolder` |
| 125 | Web server: PHP with Keel's php.ini, .htaccess rewrites, .htaccess hidden, the user's custom.conf and custom.ini read and never overwritten | `TestServesPHP`, `TestCustomSettings` |
| 126 | Web server: a mistake in custom.conf is reported before Apache starts; a taken port isn't mistaken for Apache answering; PHP-FPM is stopped if Apache fails | `TestCustomSettings`, `TestStartFailsCleanly` |
| 127 | Web server found again after Keel restarts, and both Apache and PHP-FPM stop | `TestServesPHP` |
| 128 | phpMyAdmin logs in to a real MariaDB as root, on MariaDB's current port; its config is valid PHP even with an apostrophe in a path | `TestPHPMyAdminReachesMariaDB`, `TestPHPMyAdminConfig` |
| 129 | Perl CGI scripts run (macOS and Linux through their #! line; Windows through the registry, checked in CI) | `TestPerlCGI`, `scripts/test-web-runtime.sh` |
| 130 | Tomcat downloads on the first start (checksum checked), serves its welcome JSP, and stops | `TestTomcatPart` (with `KEEL_TEST_DOWNLOADS=1`) |
| 131 | Downloads: zip and tar.gz, next mirror on failure, bad checksum installs nothing, entries outside the folder refused, reinstall replaces | `internal/fetch` tests |
| 132 | Helper processes: found again by PID file only when the command line matches, stopped, forced after a timeout | `internal/proc` tests |
| 133 | Settings: ports, websites folder, autostart, FTP login and the rest survive a restart; parts saving at once don't undo each other; older files are read; a moved part skips other parts' ports | `internal/setup` tests |
| 134 | Opening at sign-in: on and off (macOS LaunchAgent, Linux autostart entry; Windows' Run key in CI) | `TestStartAtSignIn` |
| 135 | Every promised PHP extension, Keel's php.ini, a rewrite, Perl and phpMyAdmin on each system's built Apache and PHP | `scripts/test-web-runtime.sh` in the Web workflow |

## Still to cover

These need real hardware, features that aren't built yet, or a longer run.

| # | Case | Why it matters | Blocked on |
|---|---|---|---|
| 136 | Power cut to the whole machine during writes (`prlctl stop "Windows 11" --kill`) | The real XAMPP failure | Windows support |
| 137 | OS restart or shutdown while MariaDB runs | Must stop cleanly before the OS kills it | Background service (phase 2) |
| 138 | Laptop sleep and hibernate mid-write | Server frozen, then resumed | Manual test |
| 139 | Disk really full during recovery (not simulated) | MariaDB needs space to recover | Manual test |
| 140 | Data on exFAT/USB, or backups on another drive | No cloning; plain copy across drives | Manual test |
| 141 | Linux on Btrfs/XFS (reflink) and ext4 (plain copy) | Different copy paths | Linux test machine |
| 142 | Windows antivirus locking files mid-copy; OneDrive folders | Copy failures on Windows | Windows support |
| 143 | Keel killed during the safety copy | Must never use a `.partial` copy | Add an automated test |
| 144 | Multi-GB databases | Copy and check time, progress feedback | Manual test |
| 145 | Clock or time-zone changes | Copy names and pruning use local time | Add an automated test |
| 146 | Backups folder read-only or full | Clear message, MariaDB not started | Add an automated test |
| 147 | Data folder moved or renamed between runs | Journal and backups follow the folder | Add an automated test |
| 148 | MariaDB upgraded between an unclean shutdown and the next start | Recovering with a newer version can be unsafe; should refuse | Upgrade handling |
| 149 | Server started by hand with a *different* log file, then killed | Neither the journal nor the log sees it; MariaDB's own recovery still runs | Manual test |
| 150 | 200 start/stop cycles under writes, plus an overnight soak | Phase 1 exit test | Torture-test command |
| 151 | A port held by another user's program or a system service (XAMPP's MySQL installed as a Windows service) | Windows names it; macOS and Linux can only say "another program" without admin rights | Windows support |
| 152 | InnoDB damage that stops MariaDB starting at all | Repair can't fix InnoDB in place; the answer is a restore, or innodb_force_recovery and a dump | Manual test |
| 153 | Keel killed while repair mode is running | MariaDB keeps running without account checks until stopped; the window should say so | Add an automated test |
| 154 | Repair on Windows (aria_chk.exe, Windows paths) | The XAMPP errors are mostly seen on Windows | Windows support |
| 155 | Root has a password on **Windows** | No SIGTERM there, so Stop can't shut MariaDB down and ends at the force-stop prompt. Needs Keel's own admin account or a named-pipe login | Windows support |
