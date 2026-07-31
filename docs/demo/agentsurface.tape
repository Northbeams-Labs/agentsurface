# The demo recording, as a VHS tape. Regenerate with docs/demo/record.sh.
#
# Everything on screen is the real binary printing real output. The only thing
# arranged in advance is the machine it reads: HOME points at the fixture built
# by docs/demo/fixture.sh, so what is inventoried is that invented machine and
# not this one.
#
# The output is longer than a terminal, so the recording pages through it with
# less. That is how you would read it yourself.

Output docs/demo/agentsurface.gif

Set Shell "bash"
Set FontSize 12
Set Width 1260
Set Height 880
Set Padding 20
Set Framerate 12
Set TypingSpeed 55ms
# A window bar so the recording still reads as a terminal window when it is
# dropped on a dark page.
Set WindowBar Colorful
Set BorderRadius 6

# Point the shell at the fixture machine and at the binary built by record.sh,
# and deny the machine-wide browser directories, which are absolute paths that
# HOME cannot move. See docs/demo/machine-browsers.sb: the two the recording
# machine actually has are reported under "Could not read", in the open.
#
# Hidden because it is setup, not the demonstration. Nothing here changes what
# the tool prints about the fixture.
Hide
Type `exec sandbox-exec -f docs/demo/machine-browsers.sb bash --norc --noprofile`
Enter
Sleep 2s
Type `export PS1="> " PATH="$PWD/bin:$PATH" HOME=/tmp/ashome`
Enter
Type "cd /tmp/ashome/work/orchard-checkout"
Enter
Type "clear"
Enter
Sleep 1s
Show

Sleep 1s
Type "agentsurface | less"
Sleep 700ms
Enter

# The summary line, the browser extension, the native messaging host, the
# desktop extensions.
Sleep 5s
Screenshot docs/demo/agentsurface.png
Sleep 2s

# The rest of the extensions, the instruction files and their notes.
PageDown
Sleep 6s

# MCP servers, skills, and what the run could not read.
PageDown
Sleep 6s

# What this did not look at, which prints on every run.
PageDown
Sleep 7s
