# The README GIF, recorded from the default demo cluster (tundra).
# Rendered by .github/workflows/demo.yml; see demo/tapes/README.md.
Output docs/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 680
Set Padding 10
Set Framerate 10
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

Hide
Type 'export PS1="$ " HOME=$(mktemp -d) PATH=$PWD:$PATH TERM=xterm-256color COLORTERM=truecolor LANG=C.UTF-8 SDASH_CONFIG=/tmp/sdash.toml'
Enter
Type "cp demo/tapes/detailed.toml $SDASH_CONFIG && chmod 600 $SDASH_CONFIG && clear"
Enter
Show

Type "sdash --demo"
Sleep 300ms
Enter
Sleep 3500ms

# Jobs: the pending sweep array, and why it waits.
Type "2"
Sleep 800ms
Type "jjj"
Sleep 400ms
Type "w"
Sleep 2500ms

# Queue: when will my pending job start.
Type "3"
Sleep 600ms
Type "/"
Type@15ms "user:you state:PD"
Enter
Sleep 400ms
Type "j"
Type "e"
Sleep 2500ms

# Usage: idle and wasteful jobs, then rerun tokenize right-sized.
Type "5"
Sleep 2500ms
Type "jj"
Sleep 400ms
Type "n"
Sleep 3000ms
Escape
Sleep 300ms

# Storage: Scratch is filling up.
Type "6"
Sleep 400ms
Type "j"
Sleep 2500ms

Type "q"
Sleep 300ms
