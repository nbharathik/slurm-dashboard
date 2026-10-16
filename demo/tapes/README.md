# Demo tapes

Rendered by VHS in GitHub Actions only, on the default demo cluster (`sdash --demo`):

    gh workflow run demo.yml --ref release/v0.2.0
    gh run download RUN_ID -n demo-assets

Review the artifact, then commit `docs/demo.gif` and `docs/img/*`; `demo/out/*` (LinkedIn MP4s) is not committed.
`hero` is the README GIF, `screens` the docs PNGs, `social` the 1280x640 card, `linkedin*` the vertical and square videos.
Captions are `start|end|font size|text` lines in `linkedin-captions.txt` (seconds); adjust them if they drift from the scenes.
`detailed.toml` shows the Wasteful line on Usage; `clean.toml` is the default look. Edit a tape, push, run again.
