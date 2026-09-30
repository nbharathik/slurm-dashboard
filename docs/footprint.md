# Footprint

Measured on 30 September 2026 using Go 1.26.8 and the release configuration
(`CGO_ENABLED=0`, `-trimpath`, `-s -w`). These are local prerelease measurements;
the release build reports its own sizes and rejects installations at **20 MiB**
or above, including optional bash completion.

| Platform | Installed executable | Compressed download |
| --- | ---: | ---: |
| Linux amd64 | 14,737,570 B (14.05 MiB) | 5,563,491 B (5.31 MiB) |
| Linux arm64 | 13,631,650 B (13.00 MiB) | 5,047,356 B (4.81 MiB) |
| macOS arm64 | 13,891,538 B (13.25 MiB) | 5,240,159 B (5.00 MiB) |

Optional bash completion adds 16,093 bytes. Archives also contain the licence,
README and completion scripts; the installer copies only requested files.

On Linux amd64, the installer reached **20,301,179 bytes** of file contents on a
clean install and **35,038,728 bytes** when replacing an equally sized binary.
Allocated file blocks were 20,316,160 and 35,053,568 bytes respectively. These
measurements include downloaded metadata and staged files, with no signature
tool present; directory overhead and filesystem-specific allocation may differ.

`sdash update` buffers downloads in memory. With no external verifier, its disk
peak is the existing plus staged executable: **29,475,140 bytes** for the measured
binary. This is calculated from measured executable sizes and the replacement
sequence. Verification tools may use additional temporary space or caches.
No downloaded archives or backup executables remain after success or failure.

Runtime cache and state grow separately with use. Recordings and user job logs
are outside the installed application budget. See [installation](install.md).
