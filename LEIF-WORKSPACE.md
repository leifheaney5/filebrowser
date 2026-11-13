# Leif File Workspace

This fork is based on FileBrowser Quantum `v2.1.4-beta`. The Leif deployment
still runs its pinned `v2.0.8-beta` runtime until a separately reviewed upgrade
is prepared. Workspace features do not change the account database schema,
authentication, or source mounts.
The repository's original file browser is the default at `/` and after login.
The custom workspace remains available at `/workspace`.
Existing file viewers, shares, settings, and the original interface remain
available. The original interface's sidebar links to Workspace.

## Organizing behavior

- Bulk copy, move, trash, and restore run in a serial server queue. Closing the
  page does not cancel them. Activity shows each file's outcome.
- Stop pending finishes the active item and cancels items not yet started.
  Retry failed retries only failed items. Cancelled items can be submitted again.
- A server interruption marks uncertain active operations for review. They are
  never replayed automatically. Queued operations resume at startup.
- Destination conflicts never overwrite. Restore uses the original location.
  Partial copies remain visible for review rather than being silently retried.
- Linux moves within a mount use an exclusive atomic rename when available.
  Cross-device or distinct bind-mount moves fall back to synchronized copying
  and removal of the unchanged source. This requires enough destination space,
  with a 256 MiB reserve.
- Jobs accept up to 2,000 selected entries. Select their parent folder for larger
  batches. An individual folder operation is limited to 100,000 entries and
  128 levels. Symlinks and special files are refused by organizer operations.
- Duplicate cleanup records a keeper and expected full SHA-256 digest. The
  server rechecks both files, permissions and identities before copying and
  before removal. A changed or missing keeper stops cleanup.

Trash payloads are private and must stay outside every configured source.
On Leif, bind `/mnt/music-hdd/.filebrowser-organizer-trash` to
`/home/filebrowser/data/organizer/payloads`. This avoids consuming the small
system disk. No automatic purge is performed.

## Library and navigation

Library scans are explicit, recursive background jobs. Metadata and SHA-256
fingerprints are cached in a separate SQLite database under the private
`organizer` directory beside Quantum's database. Personal tags do not modify
original files. Search and sorting apply before pagination, with missing
metadata last. Hardlinks are not counted as separate reclaimable copies.
Cached results are checked against current file identity and access rules.

Media metadata depends on the format. EXIF capture times without a timezone
retain their original wall clock. ffprobe uses an already opened, seekable
descriptor with file/network protocols disabled. Missing metadata stays empty.
Leif's pinned runtime includes ffprobe 9.0.

The file list is virtualized; folders are cached briefly. Favorites are stored
per account in the current browser. Ctrl+L edits the path, arrows and Shift
select, Ctrl+A selects results, Space previews, and Escape clears selection.
Transfers use Quantum's existing upload/download managers. Browser uploads
still require the page to stay open. Stateful organizing/library APIs require
an interactive account session; named API tokens are refused.

## Verification and packaging

Run the existing frontend tests, Vue typecheck and production build. Run
`go test ./...` in Linux with the entire repository present: backend tests also
use `_docker` and frontend fixtures. Git's `backend/internal/preview/testdata`
symlink must be preserved. Windows checkouts with symlinks disabled store it
as a text file and cannot run those fixture tests directly.

`scripts/verify-workspace.py` exercises real HTTP copy, move, trash, restore,
conflicts, library scans, tags, duplicate cleanup and traversal rejection.
It requires a marked disposable Music/Inbox fixture and a private credential
file. It must not be run against production sources.

Build the Linux binary with Go 1.27 and `CGO_ENABLED=1`, after running
`go run ./scripts/setup-gofitz-cgo`. Use tags
`netgo,osusergo,analytics,mupdf` and static external linking to preserve PDF
previews in the Alpine runtime. Package that binary and the built frontend
using `_docker/Dockerfile.leif`. Its base digest pins the existing runtime.

Before replacement, back up the live SQLite database with SQLite's backup API,
preserve configuration and the exact old image, and validate a copied database
in an isolated container. Replace only File Browser. Verify authenticated
browsing and operations after deployment; an HTTP 200 login page alone does
not establish those behaviors. Keep the prior image, database snapshot and
Compose override together for rollback.
