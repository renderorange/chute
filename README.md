# chute

A terminal mail client for IMAP and SMTP. Three panes — folders, message list, reader —
with threaded conversations, compose/reply/forward, drafts, attachments and flag triage.
One binary, one TOML file, no daemon.

## Requirements

- Go 1.26.3 or newer to build from source.
- A terminal at least **60x20**; the layout is tested at that size.
- An IMAP server and an SMTP server. Password or `password_command` only: chute does not
  do OAuth.

## Install

```sh
go install github.com/renderorange/chute@latest
```

Release builds are produced by the pipeline; `chute -version` prints the build's version.

## Quick start

```sh
mkdir -p ~/.config/chute
cp config.example.toml ~/.config/chute/config.toml
chmod 600 ~/.config/chute/config.toml
$EDITOR ~/.config/chute/config.toml     # host, username, credential
chute -check                            # IMAP and SMTP preflight, print the TLS modes
chute
```

`chute -check` is the fast way to find a wrong host, port or credential: it connects to
every account over IMAP (listing folders) and over SMTP (EHLO, AUTH, QUIT, sending no
mail), prints `tls=implicit` or `tls=starttls` for each half, and exits non-zero when
either fails.

## Configuration

The default path is `$XDG_CONFIG_HOME/chute/config.toml`, which is normally
`~/.config/chute/config.toml`. Override it with `-config`.

The file holds credentials, so chute refuses any mode other than **0600**. Set
`general.unsafe_config = true` only if the password lives elsewhere and you mean to skip
that check. A complete, loadable example ships as
[`config.example.toml`](config.example.toml); a test loads it, so the sample cannot drift
into an illegal value.

### `[general]`

| Key | Default | Meaning |
|---|---|---|
| `poll_interval` | `60` | Seconds between incremental new-mail checks. `0` means the default. |
| `download_dir` | `~/Downloads` | Where attachments are saved. A leading `~` is expanded. |
| `reply_position` | `"top"` | `"top"` writes above the quoted original, `"bottom"` below it. |
| `unsafe_config` | `false` | Skips the 0600 permission check. |

### `[layout]`

Pane widths. `0` means automatic. An explicit width wins over the percentage for its
pane, and a width too small for the terminal is clamped rather than rejected, because one
config is used at every terminal size.

| Key | Meaning |
|---|---|
| `folders_percent` | Folder pane as a share of the content width, `0..90`. |
| `messages_percent` | Message list as a share, `0..90`. |
| `folders_width` | Folder pane in columns. |
| `messages_width` | Message list in columns. |

### `[list]`

| Key | Default | Meaning |
|---|---|---|
| `columns` | `["from", "subject", "date"]` | Draw order. Accepted names: `from`, `subject`, `size`, `date`. `subject` is required; it takes the leftover width. |
| `date_format` | `"Jan 02"` | Go reference layout, at most 16 cells. |

`size` renders a right-aligned byte count in six cells, binary units: `999B`, `1.5K`,
`2.0M`, `1.2G`. Adding it narrows the subject; the date keeps its three-cell gap.

### `[[accounts]]`

One table per account. More than one is supported: `a` opens the account dialog to switch, and `]` / `[` cycle without it.

| Key | Meaning |
|---|---|
| `name` | Required, unique. The name shown in the account dialog and saved in the state file. |
| `default` | The account selected at startup. At most one account may set it. |
| `from` | The `From:` header. Use it when you send as an address different from the login. |
| `start_folder` | Opened automatically after the folder load. Unset means `INBOX`; `""` disables it. |

### `[accounts.imap]`

| Key | Default | Meaning |
|---|---|---|
| `host` | required | Server hostname. |
| `port` | `993`, or `143` with `use_tls = false` | Server port. |
| `username` | required | Login name. |
| `password` | — | Literal password. Mutually exclusive with `password_command`. |
| `password_command` | — | Command whose first line is the password. One of these two is required. |
| `use_tls` | `true` | `true`/unset dials implicit TLS. `false` dials plaintext and requires STARTTLS before login; a server that cannot upgrade is refused rather than sent the password. |
| `sent`, `drafts`, `trash`, `archive`, `junk` | detected | Mailbox names overriding IMAP special-use detection. |
| `hide_folders` | — | Mailbox names to leave out of the list. Their subfolders are hidden too; matching is case-insensitive. |

### `[accounts.smtp]`

| Key | Default | Meaning |
|---|---|---|
| `host` | the IMAP host | Server hostname. The fallback is reported as `host=inherited` by `-check`. |
| `port` | `465` | Server port. |
| `username` | the IMAP username | Login name. |
| `password`, `password_command` | the IMAP values | Credential source. |
| `use_tls` | `true` | `true`/unset connects with implicit TLS. `false` uses plaintext with an opportunistic STARTTLS upgrade. |

### `[keys]`

Overrides the shipped keymap per action. The names are the first column of the key table
below, for example:

```toml
[keys]
move = ["ctrl+m"]
delete = ["x", "d"]
```

An override **replaces** the shipped keys rather than adding to them. A key may belong to
only one action, an action needs at least one key, and an unknown name is a load error.
`?` in the app shows the effective map, so a typo is visible immediately.

## State

chute remembers the last account and the folder each account had open, in
`$XDG_STATE_HOME/chute/state.toml` or `~/.local/state/chute/state.toml` when
`XDG_STATE_HOME` is unset. It holds names only — no credentials, no addresses, no message
data — is written `0600`, and deleting it is safe: the config remains the authority and a
name that no longer exists is ignored.

## Flags

| Flag | Meaning |
|---|---|
| `-config` | Path to the config file. Defaults to `$XDG_CONFIG_HOME/chute/config.toml`. |
| `-check` | Connect to every account over IMAP (list folders) and SMTP (authenticate, send nothing), print both TLS modes, exit non-zero on any failure. |
| `-debug` | Write the IMAP protocol log to this file. The credential is masked in it. |
| `-version` | Print the version and exit. |

## Keys

`?` opens the same list in the app, resolved against `[keys]`.

Composer keys are fixed, not part of `[keys]`:

- `tab` / `shift+tab` — next / previous field (To, Cc, Bcc, Subject, body)
- `ctrl+s` — save to Drafts
- `ctrl+d` — send
- `esc` — cancel; asks before discarding a changed message

### Global

| Action | Default | Does |
|---|---|---|
| `quit` | `q` | Quit. Types as text while composing. |
| `force_quit` | `ctrl+c` | Quit from anywhere, including the composer and modals. |
| `next_panel` | `tab` | Move focus to the next pane. |
| `prev_panel` | `shift+tab` | Move focus to the previous pane. |
| `reload` | `ctrl+r` | Reconnect and reload folders and the open folder. |
| `account` | `a` | Open the account dialog. |
| `account_next` | `]` | Switch to the next account and open its start folder. |
| `account_prev` | `[` | Switch to the previous account. |
| `folders_overlay` | `\` | Open the folder picker over the content. |
| `help` | `?` | Show this keymap. |
| `settings` | `,` | Read-only report of the effective account, roles, layout, list, keys and paths. |

### List

| Action | Default | Does |
|---|---|---|
| `up` | `up` | Move up one message. |
| `down` | `down` | Move down one message. |
| `top` | `home`, `g` | First message. |
| `bottom` | `end`, `G` | Last message. |
| `open` | `enter` | Open the message, or load the folder under the cursor. |
| `filter` | `/` | Fuzzy, case-insensitive filter over the loaded list's sender, subject and recipients. |
| `search` | `S` | Server-side search of the open folder; free text plus `from:`, `subject:`, `text:`, `body:` and `since:YYYY-MM-DD`. `esc` returns to the folder. |
| `visual_range` | `v` | Anchor a selection range for the actions below. |
| `delete` | `d` | Move to Trash; inside Trash, delete permanently after a confirmation. |
| `archive` | `A` | Move to the archive mailbox. |
| `junk` | `!` | Move to the junk mailbox. |
| `move` | `M` | Move to a mailbox chosen from a picker. |
| `empty_trash` | `X` | Delete everything in Trash, after a confirmation. Only works inside Trash. |
| `collapse` | `z` | Fold or unfold the conversation under the cursor. |
| `toggle_flag` | `f` | Flag or unflag. |
| `toggle_read` | `m` | Mark read or unread. |
| `attachments` | `s` | List attachments and save them to `download_dir`. |

### Reader

| Action | Default | Does |
|---|---|---|
| `up` | `up` | Scroll up. |
| `down` | `down` | Scroll down. |
| `delete` | `d` | Move to Trash; inside Trash, delete permanently after a confirmation. |
| `archive` | `A` | Move to the archive mailbox. |
| `junk` | `!` | Move to the junk mailbox. |
| `move` | `M` | Move to a mailbox chosen from a picker. |
| `toggle_flag` | `f` | Flag or unflag. |
| `toggle_read` | `m` | Mark read or unread. |
| `next_thread_message` | `n` | Next message in the same conversation. |
| `prev_thread_message` | `p` | Previous message in the same conversation. |
| `attachments` | `s` | List attachments and save them to `download_dir`. |
| `compose` | `c` | Write a new message. |
| `reply` | `r` | Reply to the sender. |
| `reply_all` | `R` | Reply to the sender and every recipient. |
| `forward` | `F` | Forward the message's text. |
| `back` | `esc` | Close the reader and return to the list. |

### Modals

| Action | Default | Does |
|---|---|---|
| `up` | `up` | Move the picker cursor up. |
| `down` | `down` | Move the picker cursor down. |
| `open` | `enter` | Accept the choice, or answer the confirmation. |
| `back` | `esc` | Cancel and close. |

## What chute does not do

- **No outbound attachments.** Replies and forwards drop them, and forwarding a message
  with attachments says `forwarding without N attachments` so the loss is not silent. A
  reopened draft says so too. Downloading attachments (`s`) and reading them in the reader
  work normally.
- **No HTML.** Messages are shown as their `text/plain` part; an HTML-only message shows
  the stripped text, not a rendered page.
- **No OAuth.** Password or `password_command` only.
- **No blind copy in the Sent copy.** Bcc recipients are delivered and kept in drafts, but
  the copy filed in Sent has the header removed, so Sent does not record who was blind
  copied.
- **No encryption or signing.** No PGP, no S/MIME.
- **No all-folder search.** `S` runs a server search in the open folder only; there is no
  cross-folder or all-mailbox search. `/` still filters the loaded list locally, with no
  network.
- **No cross-folder threads.** A conversation is grouped within one folder.
- **One window.** There is no detached viewer or second account pane.

## Development

```sh
gofmt -l .          # must print nothing
go vet ./...
go test ./...
go test -race ./...
```
