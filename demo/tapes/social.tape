# The 1280x640 repository social preview, from the default demo cluster.
Output demo/out/raw/social.mp4

Set Shell "bash"
Set FontSize 22
Set Width 1280
Set Height 640
Set Padding 14
Set Framerate 10
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

Hide
Type 'export PS1="$ " HOME=$(mktemp -d) PATH=$PWD:$PATH TERM=xterm-256color COLORTERM=truecolor LANG=C.UTF-8 SDASH_CONFIG=/tmp/sdash.toml'
Enter
Type "cp demo/tapes/clean.toml $SDASH_CONFIG && chmod 600 $SDASH_CONFIG && clear"
Enter
Show

Type "sdash --demo"
Sleep 300ms
Enter
Sleep 3s
Screenshot docs/img/social-preview.png
Sleep 500ms

Type "q"
Sleep 300ms
