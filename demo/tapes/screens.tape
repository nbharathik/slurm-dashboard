# One PNG per view for the docs, from the default demo cluster (tundra).
# Clean layout first; Usage and Rerun use the detailed layout (Wasteful line).
Output demo/out/raw/screens.mp4

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 680
Set Padding 10
Set Framerate 10
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

Hide
Type 'export PS1="$ " HOME=$(mktemp -d) PATH=$PWD:$PATH TERM=xterm-256color COLORTERM=truecolor LANG=C.UTF-8'
Enter
Type "cp demo/tapes/clean.toml /tmp/clean.toml && cp demo/tapes/detailed.toml /tmp/detailed.toml && chmod 600 /tmp/clean.toml /tmp/detailed.toml"
Enter
Type "export SDASH_CONFIG=/tmp/clean.toml && clear"
Enter
Show

Type "sdash --demo"
Sleep 300ms
Enter
Sleep 3s
Screenshot docs/img/overview.png
Sleep 500ms

Type "2"
Sleep 1500ms
Type "jjj"
Sleep 500ms
Screenshot docs/img/jobs.png
Sleep 500ms

Type "w"
Sleep 1500ms
Screenshot docs/img/why.png
Sleep 500ms
Escape
Sleep 300ms

Type "3"
Sleep 1500ms
Screenshot docs/img/queue.png
Sleep 500ms

Type "4"
Sleep 2s
Screenshot docs/img/nodes.png
Sleep 500ms

Type "6"
Sleep 1500ms
Type "j"
Sleep 500ms
Screenshot docs/img/storage.png
Sleep 500ms

Type "q"
Sleep 300ms

Hide
Type "export SDASH_CONFIG=/tmp/detailed.toml && clear"
Enter
Type "sdash --demo"
Enter
Sleep 2s
Show

Type "5"
Sleep 2500ms
Type "jj"
Sleep 500ms
Screenshot docs/img/usage.png
Sleep 500ms

Type "n"
Sleep 1500ms
Screenshot docs/img/rerun.png
Sleep 500ms
Escape
Sleep 300ms

Type "q"
Sleep 300ms
