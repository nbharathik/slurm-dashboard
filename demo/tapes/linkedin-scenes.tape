Hide
Type 'export PS1="$ " HOME=$(mktemp -d) PATH=$PWD:$PATH TERM=xterm-256color COLORTERM=truecolor LANG=C.UTF-8 SDASH_CONFIG=/tmp/sdash.toml'
Enter
Type "cp demo/tapes/detailed.toml $SDASH_CONFIG && chmod 600 $SDASH_CONFIG"
Enter
Type 'squeue() { sdash --demo jobs | awk -F"  +" -f demo/tapes/squeue.awk; }'
Enter
Type "clear"
Enter
Show

# Plain squeue-style output, rendered from the demo cluster.
Type "squeue --me"
Sleep 300ms
Enter
Sleep 1000ms

Hide
Type "clear"
Enter
Show
Type "sdash --demo"
Sleep 300ms
Enter
Sleep 1200ms

# Why is my job pending?
Type "2"
Sleep 800ms
Type "jjj"
Sleep 500ms
Type "w"
Sleep 4500ms

# When will it start?
Type "3"
Sleep 800ms
Type "/"
Type@20ms "user:you state:PD"
Enter
Sleep 500ms
Type "j"
Type "e"
Sleep 4500ms

# What did my jobs waste?
Type "5"
Sleep 3500ms
Type "jj"
Sleep 2000ms

# Rerun it right-sized.
Type "n"
Sleep 5500ms
Escape
Sleep 500ms

# Scratch is filling up.
Type "6"
Sleep 500ms
Type "j"
Sleep 500ms
Enter
Sleep 4000ms

# Install.
Type "q"
Sleep 300ms
Hide
Type "clear"
Enter
Show
Type@15ms "curl -fsSL https://raw.githubusercontent.com/nbharathik/slurm-dashboard/main/install.sh | sh"
Sleep 2500ms
