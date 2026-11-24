HWiNFO, as shipped with hwinfo-mcp
==================================

This folder belongs to the hwinfo-mcp bundle "with HWiNFO". hwinfo-mcp only
reads what HWiNFO measures; this folder is here so that HWiNFO does not have to
be downloaded separately. Nothing in it runs unless you start it.


What is in it
-------------

HWINFO_PACKAGE_FILE
    HWiNFO's portable package, version HWINFO_PACKAGE_VERSION, exactly as its
    author publishes it at https://www.hwinfo.com/download/
    SHA-256: HWINFO_PACKAGE_SHA256

    HWiNFO is not part of the hwinfo-mcp project. It belongs to REALiX, is
    freeware for non-commercial use only, and comes under its own licence, not
    under hwinfo-mcp's MIT licence: https://www.hwinfo.com/licenses/
    It is included here with its author's permission.

HWiNFO64.INI
    A settings file written by the hwinfo-mcp project, not by HWiNFO's author.
    With it HWiNFO starts in sensors-only mode with "Shared Memory Support"
    switched on, which is how hwinfo-mcp reads the sensors.

Setup-HWiNFO.cmd
    A helper written by the hwinfo-mcp project, not by HWiNFO's author.


Setting HWiNFO up
-----------------

Double-click Setup-HWiNFO.cmd. It unpacks the package into the folder
hwinfo-mcp\HWiNFO of your local application data (%LOCALAPPDATA%), puts the
settings file next to it and starts HWiNFO. Windows asks for administrator
rights, which HWiNFO needs to reach the hardware; the publisher it names should
be REALiX.

The helper leaves alone what is already there: a HWiNFO already in that folder,
a settings file already next to it, and a HWiNFO that is already running. It
never closes or restarts HWiNFO and does not make it start with Windows.

To do the same by hand, unpack HWINFO_PACKAGE_FILE into a folder of your own,
copy HWiNFO64.INI next to HWiNFO64.exe and start HWiNFO64.exe. Do not run
HWiNFO from inside this folder: it is replaced whenever the bundle is updated.

If HWiNFO is already installed on this PC, you need none of this, and the
helper does nothing but say so: start the HWiNFO you have, enable "Shared
Memory Support" in its settings and restart it. Running the helper with
/portable sets this copy up next to it all the same.


What stays with you
-------------------

The free edition of HWiNFO switches Shared Memory Support off after 12 hours of
run time. Switching it on again is done by hand, in HWiNFO's settings; neither
this helper nor hwinfo-mcp does it for you. HWiNFO Pro has no such limit.

To remove this copy of HWiNFO, close it and delete the folder it was unpacked
into.

hwinfo-mcp: https://github.com/templarfelix/hwinfo-mcp
