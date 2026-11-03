# Errors the interface can show

This is the recorded decision for every failure class the app can produce, from GH-23.
The status bar is one line, `ContentWidth` cells wide; the mail layer's wrapped text is
built for logs, so the interface translates the failures a person can act on and leaves
everything else alone.

Two rules sit above the table:

- **Shortening is a whitelist.** A failure not named here keeps its wrapped text, because
  an unanticipated failure is exactly the case where the detail matters most.
- **The credential never appears in a displayed error.** Passwords are not part of any
  error string, and a test in `mail` pins that for a rejected login.

The phrasing lives in `tui/status.go` (`statusText`), next to the interface; the
sentinels live in `mail/errors.go`, next to the failures. This is the split
`ErrAlreadyInTrash` established in GH-14.

## Loader failures

| failure | shown | notes |
|---|---|---|
| Cannot dial the IMAP or SMTP server (`mail.ErrConnect`) | `cannot connect to the mail server` | Account, host and dial error stay in the wrapped text for logs. |
| Login rejected (`mail.ErrAuth`) | `login rejected: check the password` | Covers IMAP login and SMTP auth. |
| Timeout (`net.Error.Timeout()`) | `server timed out` | Checked before connect, because a timed-out dial is also a connect failure. |
| Folder listing fails for another reason | full text | `LIST` protocol failure, permission, server refusal. |
| Opening a folder or fetching envelopes fails | full text | Includes "no such folder": the IMAP `NO` code is not parsed, so the detail is kept rather than guessed at. |
| Fetching a message body fails | full text | Includes a message that vanished and a parse failure. |
| Updating flags fails | full text | A quiet failure (the automatic mark-as-read) is silent instead. |
| Polling, or reconciling a deletion made elsewhere, fails | mapped above, else full text | A failed poll is visible, not modal; the next tick recovers. |
| Saving an attachment fails | full text | The path and the filesystem error are the actionable part. |

## Triage and filing

| failure | shown |
|---|---|
| Delete with no Trash mailbox (`mail.ErrNoRoleFolder`) | `no trash folder configured` |
| Archive or junk with no such mailbox (`mail.ErrNoRoleFolder`) | `no archive folder configured` / `no junk folder configured` |
| Already in Trash (`mail.ErrAlreadyInTrash`) | `already in Trash` |
| Already in the destination (`mail.ErrAlreadyInRoleFolder`) | `already in <folder>` |
| Server advertises neither MOVE nor UIDPLUS (`mail.ErrNoMoveSupport`) | `server cannot move mail` |
| Any other move/delete failure | full text |

## Composer and sending

| failure | shown |
|---|---|
| Send step failed | `send failed: <phrase or full text>` |
| Sent, but the copy could not be filed in Sent | `message sent, but the copy was not filed in Sent: set [accounts.imap] sent` |
| Sent, but the draft is still in Drafts | `message sent; the draft is still in Drafts` |
| Draft save failed | `draft not saved: <phrase or full text>` |
| Draft open failed | `draft not opened: <phrase or full text>` |
| Draft saved, earlier copy could not be removed | `draft saved; the earlier copy could not be removed: <phrase or full text>` |
| Server cannot name an appended draft | `draft saved; this server cannot name it, so it cannot be removed after sending` |

The prefixes stay: "send failed" and "draft not saved" say which step the user was in.

## Fixed phrases set by the interface itself

These do not come from an error and are already short: `no message selected`,
`no folders`, `no accounts`, `no From address: set [accounts] from or imap.username`, `open the message first so the reply can quote it`,
`open the message first so the forward can include it`, `start folder "<name>" not
found`, `<folder> is a container, not a mailbox`, `this message has no attachments`,
`saved <path>`, `moved <n> message(s) to <folder>`, and the confirmation statuses
(`delete cancelled`, `kept editing`, `message discarded`).

## Configuration failures

`config.Load` runs before the TUI starts and writes the full error to stderr, then exits
1. That includes a missing or unreadable file, a mode other than 0600, invalid TOML, an
unknown `[keys]` action, a duplicate key, and a missing account field. Nothing here
reaches the status bar, and the same credential rule applies.

## Width and truncation

The status wins the width. When a status is present and the joined line exceeds
`ContentWidth`, the interface drops, in order:

1. the unread count,
2. the message count,
3. the folder name,
4. the folder count,
5. the connection state word (`connecting...` / `disconnected`).

The account tag and the status never drop. After that the line is truncated with `...`
counting display cells, not runes. A status that is itself wider than the bar is
therefore the only thing that gets cut. Without a status the bar packs as before.

For an unrecognised error the wrapped text is shown, minus one redundant wrapper: the
active account's `mail: account "<name>": ` prefix is removed because the bar already
starts with `[<name>]`. A different account named inside an error is left intact.

## What is not reachable

For a failure named in the tables above, the wrapped text is not available inside the
app: there is no error viewer or log file (GH-23 ruling). The short phrase is the whole
display. Unrecognised failures still show their wrapped text, which is the recovery path
for a case nobody anticipated.
