# The demo recording, as a VHS tape. Regenerate with docs/demo/record.sh.
#
# Everything on screen is the real binary printing real output. The only thing
# arranged in advance is the machine it reads: HOME points at the fixture built
# by docs/demo/fixture.sh, so what is inventoried is that invented machine and
# not this one.
#
# It is the default summary, which counts the notes rather than printing them.
# The inventory itself now fits on one screen; what runs past it is the blind
# spots section, which is long by design. So the recording still pages through
# with less, which is how you would read it yourself, and the still is page one.
# Recording -verbose instead would page for about a minute.

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

# Page one is the whole inventory: the count, every category, and the count of
# notes the default held back. On this fixture it fits, which is the point of
# the compact default and is why this is the still.
Sleep 7s
Screenshot docs/demo/agentsurface.png
Sleep 3s

# What the run could not read, and the start of the blind spots.
PageDown
Sleep 6s

# The rest of "What this did not look at", which prints on every run.
PageDown
Sleep 7s
